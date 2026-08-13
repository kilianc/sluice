package sluice_test

import (
	"testing"

	"github.com/kilianc/sluice"
	"github.com/kilianc/sluice/dialect/postgres"
)

func TestParseDuration(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int64
	}{
		{"2 days", 172800},
		{"36h", 129600},
		{"1w 2d", 777600},
		{"90 minutes", 5400},
		{"1s", 1},
		{"30sec", 30},
		{"2HOURS", 7200},
		{"1d12h", 129600},
		{"0d", 0},
	} {
		got, err := sluice.ParseDuration(tc.in)
		if err != nil {
			t.Errorf("ParseDuration(%q) errored: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseDuration(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestParseDurationRejects(t *testing.T) {
	// Months and years are not fixed-length, so they are not units.
	for _, in := range []string{
		"", "  ", "2 fortnights", "2", "days", "-1d", "1.5h", "1 month", "2y",
		"1d 2", "INTERVAL '1 day'",
	} {
		if got, err := sluice.ParseDuration(in); err == nil {
			t.Errorf("ParseDuration(%q) = %d, want an error", in, got)
		}
	}
}

func TestCoercionPerType(t *testing.T) {
	c := machines(t, postgres.Dialect)
	for _, tc := range []struct {
		input string
		arg   any
	}{
		{`name = "Web-1"`, "web-1"},
		{`phase = "IN-USE"`, "in-use"},
		{`online = true`, true},
		{`cores > 8`, float64(8)},
		{`id = "3F2504E0-4F89-11D3-9A0C-0305E82C3301"`, "3f2504e0-4f89-11d3-9a0c-0305e82c3301"},
		{`os_age > "2 days"`, int64(172800)},
	} {
		res, err := c.Compile(tc.input)
		if err != nil {
			t.Errorf("%q: %v", tc.input, err)
			continue
		}
		if len(res.Args) != 1 || res.Args[0] != tc.arg {
			t.Errorf("%q: args = %#v, want [%#v]", tc.input, res.Args, tc.arg)
		}
	}
}

func TestTimestampsAreNormalizedToUTC(t *testing.T) {
	c, err := sluice.New(sluice.Schema{
		Fields: []sluice.Field{{Name: "seen", Type: sluice.TypeTimestamp, Column: "inv.seen_at"}},
	}, postgres.Dialect)
	if err != nil {
		t.Fatal(err)
	}
	res, err := c.Compile(`seen > "2026-08-13T09:30:00+02:00"`)
	if err != nil {
		t.Fatal(err)
	}
	if want := "inv.seen_at > $1::timestamptz"; res.SQL != want {
		t.Errorf("sql = %q, want %q", res.SQL, want)
	}
	if want := "2026-08-13T07:30:00Z"; res.Args[0] != want {
		t.Errorf("arg = %#v, want %q", res.Args[0], want)
	}
	if _, err := c.Compile(`seen > "yesterday"`); err == nil {
		t.Error("a non-RFC-3339 timestamp was accepted")
	}
}

func TestLiteralTypeMustMatchTheFieldType(t *testing.T) {
	c := machines(t, postgres.Dialect)
	for _, in := range []string{
		`cores > "8"`,
		`name = 8`,
		`online = "true"`,
		`phase = true`,
		`os_age > 2`,
	} {
		if _, err := c.Compile(in); err == nil {
			t.Errorf("%q was accepted, want invalid_value_for_field", in)
		}
	}
}

func TestDynamicEnumsAreNotCachedOnTheCompiler(t *testing.T) {
	c := machines(t, postgres.Dialect)
	first := c.WithDynamic(map[string][]string{"rack": {"ash1-r01"}})
	second := c.WithDynamic(map[string][]string{"rack": {"chi1-r09"}})

	if got := first.Suggest(`rack = "`, 8); len(got) != 1 || got[0].Text != "ash1-r01" {
		t.Errorf("first view suggested %+v", got)
	}
	if got := second.Suggest(`rack = "`, 8); len(got) != 1 || got[0].Text != "chi1-r09" {
		t.Errorf("second view suggested %+v", got)
	}
}
