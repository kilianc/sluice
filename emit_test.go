package sluice_test

import (
	"strings"
	"testing"

	"github.com/kilianc/sluice"
	"github.com/kilianc/sluice/dialect/postgres"
)

// The four predicates in the origin implementation that are not a column
// comparison, expressed through Builder. PLAN.md §1 treats them as an
// acceptance test on the emitter interface rather than as code to port: if they
// express cleanly here, the escape hatch is sufficient; if they do not, the
// interface needs rethinking before a caller depends on it.
//
// Note what changed in the move. Every one of these interpolated its value in
// the origin — `fmt.Sprintf("= '%s'", v)` — and none of them can here, because
// Builder has no method that writes a value into SQL text.

const (
	operationHead = "EXISTS (SELECT 1 FROM jsonb_each(inv.operations) AS op(name, payload) WHERE "
	inProgress    = "op.payload ->> 'status' = 'in-progress'"
)

// operation: an EXISTS over a JSONB column, with two values that mean "any" and
// "none" rather than naming an operation.
func operationEmitter(b *sluice.Builder, op sluice.Operator, v sluice.Value) error {
	switch v.String() {
	case "none":
		b.WriteSQL("NOT " + operationHead + inProgress + ")")
		return nil
	case "any":
		b.WriteSQL(operationHead + inProgress + ")")
		return nil
	}
	b.WriteSQL(operationHead + "LOWER(op.name) ")
	switch op {
	case "~", "!~":
		like := "LIKE"
		if op == "!~" {
			like = "NOT LIKE"
		}
		b.WriteSQL(like + " " + b.Bind(sluice.LikePattern(v.String())) + b.Dialect().LikeEscapeClause())
	default:
		b.WriteSQL(string(op) + " " + b.Bind(v.String()))
	}
	b.WriteSQL(" AND " + inProgress + ")")
	return nil
}

// blocked: one name over two columns, chosen by the value.
func blockedEmitter(b *sluice.Builder, _ sluice.Operator, v sluice.Value) error {
	switch v.String() {
	case "true", "false":
		b.WriteSQL("inv.blocked = " +
			b.Bind(b.Dialect().BoolArg(v.String() == "true")))
	default:
		b.WriteSQL("LOWER(inv.blocked_reason) = " + b.Bind(v.String()))
	}
	return nil
}

// online: a derived predicate over a heartbeat timestamp, where the value
// selects the comparison rather than being compared.
func onlineEmitter(b *sluice.Builder, _ sluice.Operator, v sluice.Value) error {
	op := "<="
	if !v.Bool() {
		op = ">"
	}
	b.WriteSQL("FLOOR(EXTRACT(EPOCH FROM (NOW() - inv.last_heartbeat_at)) / 60) " +
		op + " " + b.Bind(15))
	return nil
}

// moving: a comparison between two columns.
func movingEmitter(b *sluice.Builder, _ sluice.Operator, v sluice.Value) error {
	b.WriteSQL("(inv.desired_group_id <> inv.current_group_id) = " +
		b.Bind(b.Dialect().BoolArg(v.Bool())))
	return nil
}

func fleetCompiler(t *testing.T) *sluice.Compiler {
	t.Helper()
	schema := sluice.Schema{
		Name: "fleet",
		Fields: []sluice.Field{
			{Name: "operation", Type: sluice.TypeString, Emit: operationEmitter},
			{Name: "blocked", Type: sluice.TypeString, Emit: blockedEmitter},
			{Name: "online", Type: sluice.TypeBoolean, Emit: onlineEmitter},
			{Name: "moving", Type: sluice.TypeBoolean, Emit: movingEmitter},
			{Name: "name", Type: sluice.TypeString, Column: "inv.name"},
		},
	}
	c, err := sluice.New(schema, postgres.Dialect)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCustomEmittersExpressTheOriginPredicates(t *testing.T) {
	c := fleetCompiler(t)
	for _, tc := range []struct {
		input string
		sql   string
		args  []any
	}{
		{
			`operation = "any"`,
			operationHead + inProgress + ")",
			nil,
		},
		{
			`operation = "none"`,
			"NOT " + operationHead + inProgress + ")",
			nil,
		},
		{
			`operation = "reboot"`,
			operationHead + "LOWER(op.name) = $1 AND " + inProgress + ")",
			[]any{"reboot"},
		},
		{
			`operation ~ "reboot"`,
			operationHead + `LOWER(op.name) LIKE $1 ESCAPE '\' AND ` + inProgress + ")",
			[]any{"%reboot%"},
		},
		{
			`blocked = "true"`,
			"inv.blocked = $1",
			[]any{true},
		},
		{
			`blocked = "disk-failure"`,
			"LOWER(inv.blocked_reason) = $1",
			[]any{"disk-failure"},
		},
		{
			`online = true`,
			"FLOOR(EXTRACT(EPOCH FROM (NOW() - inv.last_heartbeat_at)) / 60) <= $1",
			[]any{15},
		},
		{
			`online = false`,
			"FLOOR(EXTRACT(EPOCH FROM (NOW() - inv.last_heartbeat_at)) / 60) > $1",
			[]any{15},
		},
		{
			`moving = true`,
			"(inv.desired_group_id <> inv.current_group_id) = $1",
			[]any{true},
		},
	} {
		t.Run(tc.input, func(t *testing.T) {
			res, err := c.Compile(tc.input)
			if err != nil {
				t.Fatal(err)
			}
			if res.SQL != tc.sql {
				t.Errorf("sql  = %s\nwant = %s", res.SQL, tc.sql)
			}
			if len(res.Args) != len(tc.args) {
				t.Fatalf("args = %#v, want %#v", res.Args, tc.args)
			}
			for i := range tc.args {
				if res.Args[i] != tc.args[i] {
					t.Errorf("arg %d = %#v, want %#v", i, res.Args[i], tc.args[i])
				}
			}
		})
	}
}

func TestCustomEmittersShareThePlaceholderSequence(t *testing.T) {
	c := fleetCompiler(t)
	res, err := c.Compile(`name = "web-1" AND operation = "reboot" AND moving = true`)
	if err != nil {
		t.Fatal(err)
	}
	var (
		name      = "LOWER(inv.name) = $1"
		operation = operationHead + "LOWER(op.name) = $2 AND " + inProgress + ")"
		moving    = "(inv.desired_group_id <> inv.current_group_id) = $3"
	)
	want := "((" + name + " AND " + operation + ") AND " + moving + ")"
	if res.SQL != want {
		t.Errorf("sql  = %s\nwant = %s", res.SQL, want)
	}
	if len(res.Args) != 3 {
		t.Fatalf("args = %#v, want three", res.Args)
	}
}

func TestCustomEmittersCannotLeakInputText(t *testing.T) {
	// Builder exposes no method that writes a value into SQL text, so invariant
	// 1 holds even for host-authored emitters.
	c := fleetCompiler(t)
	const marker = "Zq7Marker"
	for _, in := range []string{
		`operation = "` + marker + `"`,
		`operation ~ "` + marker + `"`,
		`blocked = "` + marker + `"`,
	} {
		res, err := c.Compile(in)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(res.SQL, marker) {
			t.Errorf("input %q leaked into %s", in, res.SQL)
		}
	}
}

func TestLikePatternEscapesMetacharactersBeforeWrapping(t *testing.T) {
	for _, tc := range [][2]string{
		{"cell", "%cell%"},
		{"100%_x", `%100\%\_x%`},
		{`a\b`, `%a\\b%`},
		{"%", `%\%%`},
	} {
		if got := sluice.LikePattern(tc[0]); got != tc[1] {
			t.Errorf("LikePattern(%q) = %q, want %q", tc[0], got, tc[1])
		}
	}
}

func TestCaseFoldingIsPerFieldAndAsciiOnly(t *testing.T) {
	sensitive := false
	schema := sluice.Schema{
		Fields: []sluice.Field{
			{Name: "name", Type: sluice.TypeString, Column: "inv.name"},
			{Name: "tag", Type: sluice.TypeString, Column: "inv.tag", CaseInsensitive: &sensitive},
		},
	}
	c, err := sluice.New(schema, postgres.Dialect)
	if err != nil {
		t.Fatal(err)
	}

	res, err := c.Compile(`name = "MixedÉ"`)
	if err != nil {
		t.Fatal(err)
	}
	if res.SQL != "LOWER(inv.name) = $1" || res.Args[0] != "mixedÉ" {
		t.Errorf("folded field: sql = %q, args = %#v", res.SQL, res.Args)
	}

	res, err = c.Compile(`tag = "MixedÉ"`)
	if err != nil {
		t.Fatal(err)
	}
	if res.SQL != "inv.tag = $1" || res.Args[0] != "MixedÉ" {
		t.Errorf("unfolded field: sql = %q, args = %#v", res.SQL, res.Args)
	}
}
