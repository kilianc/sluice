package sluice_test

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/kilianc/sluice"
	"github.com/kilianc/sluice/ast"
	"github.com/kilianc/sluice/dialect/duckdb"
	"github.com/kilianc/sluice/dialect/postgres"
)

// documents loads the same schema the conformance corpus uses, so the unit tests
// and the corpus talk about the same fields.
func documents(t *testing.T, d sluice.Dialect) *sluice.Compiler {
	t.Helper()
	data, err := os.ReadFile("conformance/schemas/documents.json")
	if err != nil {
		t.Fatal(err)
	}
	schema, err := sluice.LoadSchema(data)
	if err != nil {
		t.Fatal(err)
	}
	c, err := sluice.New(schema, d)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func diagnosticOf(t *testing.T, err error) sluice.Diagnostic {
	t.Helper()
	var e *sluice.Error
	if !errors.As(err, &e) {
		t.Fatalf("error %v is not a *sluice.Error", err)
	}
	return e.Diagnostic
}

func TestCompileEmptyInputIsEmpty(t *testing.T) {
	c := documents(t, postgres.Dialect)
	res, err := c.Compile("   ")
	if err != nil {
		t.Fatal(err)
	}
	if res.SQL != "" {
		t.Errorf("sql = %q, want empty — never 1=1", res.SQL)
	}
	if len(res.Args) != 0 || len(res.Fields) != 0 {
		t.Errorf("args = %v, fields = %v, want both empty", res.Args, res.Fields)
	}
}

// TestCompileNeverEmitsInputText is the standing audit of invariant 1: no text
// originating in the input may appear in the emitted SQL, whatever the input is.
func TestCompileNeverEmitsInputText(t *testing.T) {
	c := documents(t, postgres.Dialect)
	const marker = "Zq7Marker"
	inputs := []string{
		`name = "` + marker + `"`,
		`name ~ "` + marker + `"`,
		`name !~ "%` + marker + `_"`,
		`state = "` + marker + `"`,
		`team = "` + marker + `"`,
		`id = "` + marker + `"`,
		`edited > "` + marker + `"`,
		`name = "` + marker + `" AND (active = true OR NOT words > 4)`,
		`name = "'; DROP TABLE document --` + marker + `"`,
		`name = "\\" OR 1=1 --` + marker + `"`,
	}
	for _, in := range inputs {
		res, err := c.Compile(in)
		if err != nil {
			continue // rejected inputs emit nothing at all
		}
		if strings.Contains(res.SQL, marker) {
			t.Errorf("input %q leaked a value into SQL: %s", in, res.SQL)
		}
		for _, frag := range []string{"'", "DROP", "--", "1=1"} {
			if strings.Contains(res.SQL, frag) && frag != "'" {
				t.Errorf("input %q leaked %q into SQL: %s", in, frag, res.SQL)
			}
		}
	}
}

func TestCompileRejectsEveryOriginInjection(t *testing.T) {
	// The shapes 006-security.json pins, asserted here too so a refactor that
	// breaks them fails the unit suite as well as the corpus.
	c := documents(t, postgres.Dialect)
	for _, in := range []string{
		`1=1`,
		`state = "shared" AND 1=1`,
		`state = "shared" OR EXISTS (SELECT 1 FROM document)`,
		`bogus_column = "x"`,
		`name = "x"; DROP TABLE document`,
		`name = "x" UNION SELECT 1`,
		`name = "x" -- comment`,
		`name = 'x' OR 1=1`,
		`state = "shared") OR (1=1`,
		`name = pg_sleep(10)`,
	} {
		res, err := c.Compile(in)
		if err == nil {
			t.Errorf("input %q compiled to %q, want a diagnostic", in, res.SQL)
		}
		if res.SQL != "" {
			t.Errorf("input %q produced SQL alongside its diagnostic: %q", in, res.SQL)
		}
	}
}

func TestCompileArgumentsFollowPlaceholderOrder(t *testing.T) {
	c := documents(t, postgres.Dialect)
	res, err := c.Compile(`(name ~ "a" OR name ~ "b") AND words > 2`)
	if err != nil {
		t.Fatal(err)
	}
	want := `((LOWER(doc.name) LIKE $1 ESCAPE '\' OR LOWER(doc.name) LIKE $2 ESCAPE '\') AND doc.words > $3)`
	if res.SQL != want {
		t.Errorf("sql  = %s\nwant = %s", res.SQL, want)
	}
	if len(res.Args) != 3 || res.Args[0] != "%a%" || res.Args[1] != "%b%" || res.Args[2] != float64(2) {
		t.Errorf("args = %#v", res.Args)
	}
}

func TestCompileIsDeterministic(t *testing.T) {
	// Invariant 4: map iteration order must never be observable in output.
	c := documents(t, postgres.Dialect).WithDynamic(map[string][]string{
		"team": {"design-a", "design-b", "desi-r03", "deploy-a"},
	})
	const in = `team = "design-a" AND (state = "shared" OR NOT active = true) AND name ~ "web"`
	first, err := c.Compile(in)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 200; i++ {
		got, err := c.Compile(in)
		if err != nil {
			t.Fatal(err)
		}
		if got.SQL != first.SQL {
			t.Fatalf("run %d produced different SQL:\n%s\n%s", i, first.SQL, got.SQL)
		}
		if len(got.Fields) != len(first.Fields) {
			t.Fatalf("run %d produced different fields: %v vs %v", i, first.Fields, got.Fields)
		}
		for j := range got.Fields {
			if got.Fields[j] != first.Fields[j] {
				t.Fatalf("run %d reordered fields: %v vs %v", i, first.Fields, got.Fields)
			}
		}
	}
}

func TestCompilerIsSafeForConcurrentUse(t *testing.T) {
	c := documents(t, postgres.Dialect)
	inputs := []string{
		`state = "shared" AND name ~ "web"`,
		`NOT active = true`,
		`edited > "1w 2d" OR words <= 4`,
		`team = "design-a"`,
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			view := c.WithDynamic(map[string][]string{"team": {"design-a"}})
			for n := 0; n < 50; n++ {
				in := inputs[(i+n)%len(inputs)]
				if _, err := view.Compile(in); err != nil {
					t.Errorf("%q: %v", in, err)
					return
				}
				view.Suggest(in, len(in))
			}
		}(i)
	}
	wg.Wait()
}

func TestValidateReportsEveryIndependentProblem(t *testing.T) {
	c := documents(t, postgres.Dialect)
	diags := c.Validate(`stat = "x" AND active ~ "y" AND state = "bogus"`)
	want := []string{"unknown_field", "unknown_operator_for_field", "invalid_value_for_field"}
	if len(diags) != len(want) {
		t.Fatalf("diagnostics = %+v, want %v", diags, want)
	}
	for i, code := range want {
		if diags[i].Code != code {
			t.Errorf("diagnostic %d = %s, want %s", i, diags[i].Code, code)
		}
	}
}

func TestValidateSuggestsNearbyFieldNames(t *testing.T) {
	c := documents(t, postgres.Dialect)
	diags := c.Validate(`stat = "x"`)
	if len(diags) != 1 || len(diags[0].Suggestions) == 0 || diags[0].Suggestions[0] != "state" {
		t.Fatalf("diagnostics = %+v, want state suggested first", diags)
	}
}

func TestCompileStopsAtTheFirstDiagnostic(t *testing.T) {
	c := documents(t, postgres.Dialect)
	_, err := c.Compile(`stat = "x" AND active ~ "y"`)
	d := diagnosticOf(t, err)
	if d.Code != "unknown_field" || d.Span != (sluice.Span{Start: 0, End: 4}) {
		t.Errorf("diagnostic = %+v, want unknown_field at [0,4)", d)
	}
}

func TestInputLengthIsBoundedBeforeLexing(t *testing.T) {
	schema := sluice.Schema{
		Options: sluice.Options{MaxLength: 16},
		Fields:  []sluice.Field{{Name: "name", Type: sluice.TypeString, Column: "doc.name"}},
	}
	c, err := sluice.New(schema, postgres.Dialect)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Compile(`name = "` + strings.Repeat("x", 100) + `"`)
	if d := diagnosticOf(t, err); d.Code != "input_too_long" {
		t.Errorf("diagnostic = %+v, want input_too_long", d)
	}
}

func TestCompileASTIsValidatedLikeText(t *testing.T) {
	// Mode B: a hostile client can express only what the server's schema permits.
	c := documents(t, postgres.Dialect)

	for _, tc := range []struct {
		name string
		json string
		code string
	}{
		{"unknown field", `{"kind":"predicate","field":"bogus","op":"=","value":{"type":"string","value":"x"}}`, "unknown_field"},
		{"operator the field does not permit", `{"kind":"predicate","field":"active","op":"~","value":{"type":"string","value":"x"}}`, "unknown_operator_for_field"},
		{"value outside the enum", `{"kind":"predicate","field":"state","op":"=","value":{"type":"string","value":"nope"}}`, "invalid_value_for_field"},
		{"literal of the wrong type", `{"kind":"predicate","field":"words","op":">","value":{"type":"string","value":"8"}}`, "invalid_value_for_field"},
		{"duration that is not one", `{"kind":"predicate","field":"edited","op":">","value":{"type":"string","value":"2 fortnights"}}`, "invalid_duration"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var node ast.Node
			if err := json.Unmarshal([]byte(tc.json), &node); err != nil {
				t.Fatal(err)
			}
			res, err := c.CompileAST(&node)
			if err == nil {
				t.Fatalf("compiled to %q, want %s", res.SQL, tc.code)
			}
			if d := diagnosticOf(t, err); d.Code != tc.code {
				t.Errorf("diagnostic = %+v, want %s", d, tc.code)
			}
		})
	}
}

func TestCompileASTMatchesCompilingTheSource(t *testing.T) {
	c := documents(t, postgres.Dialect)
	const in = `state = "shared" AND NOT (name ~ "web" OR words >= 8)`
	fromText, err := c.Compile(in)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(fromText.AST)
	if err != nil {
		t.Fatal(err)
	}
	var node ast.Node
	if err := json.Unmarshal(encoded, &node); err != nil {
		t.Fatal(err)
	}
	fromAST, err := c.CompileAST(&node)
	if err != nil {
		t.Fatal(err)
	}
	if fromAST.SQL != fromText.SQL {
		t.Errorf("AST transport changed the SQL:\n%s\n%s", fromText.SQL, fromAST.SQL)
	}
}

func TestCompileASTRejectsHandBuiltNodesItCannotTrust(t *testing.T) {
	// Nothing stops a host from assembling a node itself, so the operator and
	// the shape are checked rather than assumed.
	c := documents(t, postgres.Dialect)
	pred := func(op string) *ast.Node {
		return &ast.Node{
			Kind:  ast.KindPredicate,
			Field: "name",
			Op:    op,
			Value: ast.Literal{Type: ast.LitString, Str: "x"},
		}
	}
	for _, tc := range []struct {
		name string
		node *ast.Node
		code string
	}{
		{"operator smuggled through the AST", pred("= 'x' OR 1=1 --"), "unexpected_token"},
		{"binary operator that is not and/or", ast.Binary("union", pred("="), pred("=")), "unexpected_token"},
		{"binary node with a missing operand", ast.Binary("and", pred("="), nil), "unexpected_token"},
		{"negation of nothing", ast.Not(nil), "unexpected_token"},
		{"unknown node kind", &ast.Node{Kind: "raw"}, "unexpected_token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := c.CompileAST(tc.node)
			if err == nil {
				t.Fatalf("compiled to %q, want %s", res.SQL, tc.code)
			}
			if d := diagnosticOf(t, err); d.Code != tc.code {
				t.Errorf("diagnostic = %+v, want %s", d, tc.code)
			}
		})
	}
}

func TestCompileASTBoundsNesting(t *testing.T) {
	c := documents(t, postgres.Dialect)
	node := &ast.Node{
		Kind:  ast.KindPredicate,
		Field: "active",
		Op:    "=",
		Value: ast.Literal{Type: ast.LitBoolean, Bool: true},
	}
	for i := 0; i < 32; i++ {
		node = ast.Not(node)
	}
	if _, err := c.CompileAST(node); diagnosticOf(t, err).Code != "depth_exceeded" {
		t.Errorf("deep AST was accepted; want depth_exceeded")
	}
}

func TestWithDynamicDoesNotMutateTheCompiler(t *testing.T) {
	c := documents(t, postgres.Dialect)
	view := c.WithDynamic(map[string][]string{"team": {"design-a"}})

	if _, err := view.Compile(`team = "design-a"`); err != nil {
		t.Fatalf("view rejected a supplied value: %v", err)
	}
	// The base compiler never learned the values: a dynamic field with none
	// supplied accepts any string rather than erroring.
	if _, err := c.Compile(`team = "anything-at-all"`); err != nil {
		t.Fatalf("base compiler should accept any string for an unresolved dynamic enum: %v", err)
	}
	if got := len(c.Suggest(`team = "`, 8)); got != 0 {
		t.Errorf("base compiler offered %d completions, want none", got)
	}
}

func TestPublicSchemaCarriesNoColumnSQL(t *testing.T) {
	c := documents(t, postgres.Dialect)
	pub := c.PublicSchema(map[string][]string{"team": {"design-a"}})
	data, err := json.Marshal(pub)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"doc.state", "rev.created_at", "grp.name", "doc.name"} {
		if strings.Contains(string(data), secret) {
			t.Errorf("public schema leaked %q:\n%s", secret, data)
		}
	}
	if !strings.Contains(string(data), "design-a") {
		t.Errorf("public schema should carry resolved dynamic values:\n%s", data)
	}
	for _, f := range pub.Fields {
		if f.Column != "" {
			t.Errorf("field %s kept its column", f.Name)
		}
	}
	for _, s := range pub.Sorts {
		if s.SQL != "" {
			t.Errorf("sort %s kept its sql", s.Key)
		}
	}
}

func TestDynamicValuesMayArriveResolvedOnTheField(t *testing.T) {
	// This is the shape PublicSchema emits for the browser: dynamic, with the
	// request's values already resolved onto the field. It has to load back in,
	// or an implementation rejects its own browser-facing schema.
	c := documents(t, postgres.Dialect)
	pub := c.PublicSchema(map[string][]string{"team": {"design-a", "design-b"}})
	data, err := json.Marshal(pub)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	// Go keeps requiring `column`, so a server hears about a missing one at load
	// time (AGENTS.md §4.3); restore them to exercise the rest of the round trip.
	restored := restoreColumns(t, decoded, c.Schema())
	schema, err := sluice.LoadSchema(restored)
	if err != nil {
		t.Fatalf("a resolved dynamic schema was rejected: %v", err)
	}
	client, err := sluice.New(schema, postgres.Dialect)
	if err != nil {
		t.Fatal(err)
	}
	if got := client.Suggest(`team = "desi`, 12); len(got) != 2 {
		t.Errorf("suggestions = %+v, want both resolved teams", got)
	}
	// The field is still dynamic, so the client never rejects a value its
	// server would have accepted.
	if diags := client.Validate(`team = "somewhere-else"`); len(diags) != 0 {
		t.Errorf("diagnostics = %+v, want none", diags)
	}
}

func restoreColumns(t *testing.T, schema map[string]any, source sluice.Schema) []byte {
	t.Helper()
	columns := map[string]string{}
	for _, f := range source.Fields {
		columns[f.Name] = f.Column
	}
	for _, raw := range schema["fields"].([]any) {
		field := raw.(map[string]any)
		field["column"] = columns[field["name"].(string)]
	}
	sqls := map[string]string{}
	for _, s := range source.Sorts {
		sqls[s.Key] = s.SQL
	}
	for _, raw := range schema["sorts"].([]any) {
		sort := raw.(map[string]any)
		sort["sql"] = sqls[sort["key"].(string)]
	}
	data, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestDialectsDifferOnlyWhereTheSpecSaysTheyMay(t *testing.T) {
	const in = `id = "3F2504E0-4F89-11D3-9A0C-0305E82C3301" AND edited > "2 days"`
	pg, err := documents(t, postgres.Dialect).Compile(in)
	if err != nil {
		t.Fatal(err)
	}
	duck, err := documents(t, duckdb.Dialect).Compile(in)
	if err != nil {
		t.Fatal(err)
	}
	wantPG := `(doc.id = $1::uuid AND EXTRACT(EPOCH FROM (NOW() - rev.created_at)) > $2)`
	wantDuck := `(doc.id = ?::UUID AND date_diff('second', rev.created_at, current_timestamp) > ?)`
	if pg.SQL != wantPG {
		t.Errorf("postgres = %s, want %s", pg.SQL, wantPG)
	}
	if duck.SQL != wantDuck {
		t.Errorf("duckdb = %s, want %s", duck.SQL, wantDuck)
	}
	if len(pg.Args) != len(duck.Args) || pg.Args[0] != duck.Args[0] || pg.Args[1] != duck.Args[1] {
		t.Errorf("arguments differ between dialects: %v vs %v", pg.Args, duck.Args)
	}
}
