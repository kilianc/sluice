package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// exchange runs the adapter over a set of request lines and returns the decoded
// responses, checking the transport rules of AGENTS.md §11 along the way.
func exchange(t *testing.T, lines ...string) []map[string]any {
	t.Helper()
	t.Setenv("SLUICE_CONFORMANCE_SCHEMAS", "../../conformance/schemas")

	var out strings.Builder
	if err := runAdapter(strings.NewReader(strings.Join(lines, "\n")+"\n"), &out); err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(got) != len(lines) {
		t.Fatalf("sent %d requests, got %d responses:\n%s", len(lines), len(got), out.String())
	}
	resps := make([]map[string]any, 0, len(got))
	for _, line := range got {
		var resp map[string]any
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			t.Fatalf("response is not JSON: %s", line)
		}
		if msg, ok := resp["error"]; ok {
			t.Fatalf("adapter reported an error: %v", msg)
		}
		resps = append(resps, resp)
	}
	return resps
}

func TestAdapterAnswersInOrder(t *testing.T) {
	resps := exchange(t,
		`{"id":"a","op":"compile","schema":"machines","dialect":"postgres","input":"phase = \"in-use\""}`,
		`{"id":"b","op":"compile","schema":"machines","dialect":"duckdb","input":"online = true"}`,
		`{"id":"c","op":"lex","input":"a=1"}`,
	)
	for i, want := range []string{"a", "b", "c"} {
		if resps[i]["id"] != want {
			t.Errorf("response %d has id %v, want %s", i, resps[i]["id"], want)
		}
	}
	if resps[0]["sql"] != "LOWER(inv.phase) = $1" {
		t.Errorf("postgres sql = %v", resps[0]["sql"])
	}
	if resps[1]["sql"] != "inv.online = ?" {
		t.Errorf("duckdb sql = %v", resps[1]["sql"])
	}
}

func TestAdapterNeverReturnsSQLWithDiagnostics(t *testing.T) {
	resps := exchange(t,
		`{"id":"a","op":"compile","schema":"machines","input":"phse = \"x\""}`,
		`{"id":"b","op":"compile","schema":"machines","input":"1=1"}`,
	)
	for _, resp := range resps {
		diags, _ := resp["diagnostics"].([]any)
		if len(diags) == 0 {
			t.Fatalf("expected diagnostics: %v", resp)
		}
		if _, ok := resp["sql"]; ok {
			t.Errorf("response carries sql alongside diagnostics: %v", resp)
		}
	}
}

func TestAdapterOmitsTheTrailingEOFToken(t *testing.T) {
	resps := exchange(t, `{"id":"a","op":"lex","input":"a = \"b\""}`)
	toks, _ := resps[0]["tokens"].([]any)
	if len(toks) != 3 {
		t.Fatalf("tokens = %v, want three", toks)
	}
	for _, tok := range toks {
		if tok.(map[string]any)["kind"] == "EOF" {
			t.Errorf("EOF was not omitted: %v", toks)
		}
	}
}

func TestAdapterAcceptsAnInlineSchema(t *testing.T) {
	resps := exchange(t, `{"id":"a","op":"compile","dialect":"postgres",`+
		`"schema":{"fields":[{"name":"tag","type":"string","column":"t.name"}]},`+
		`"input":"tag = \"x\""}`)
	if resps[0]["sql"] != "LOWER(t.name) = $1" {
		t.Errorf("sql = %v", resps[0]["sql"])
	}
}

func TestAdapterCompilesATransportedAST(t *testing.T) {
	// AGENTS.md §12 Mode B: the client sends the AST, the server compiles it
	// under its own schema.
	resps := exchange(t, `{"id":"a","op":"compile","schema":"machines","dialect":"postgres",`+
		`"ast":{"kind":"predicate","field":"phase","op":"=","value":{"type":"string","value":"in-use"}}}`)
	if resps[0]["sql"] != "LOWER(inv.phase) = $1" {
		t.Errorf("sql = %v", resps[0]["sql"])
	}
}

func TestAdapterReportsDynamicValues(t *testing.T) {
	resps := exchange(t,
		`{"id":"a","op":"suggest","schema":"machines","input":"rack = \"","cursor":8,`+
			`"dynamic":{"rack":["ASH1-R01","ASH2-R01"]}}`,
		`{"id":"b","op":"schema","schema":"machines","dynamic":{"rack":["ASH1-R01"]}}`,
	)
	sugg, _ := resps[0]["suggestions"].([]any)
	if len(sugg) != 2 {
		t.Errorf("suggestions = %v, want both racks", sugg)
	}
	pub, _ := resps[1]["schema"].(map[string]any)
	if strings.Contains(mustString(t, pub), "inv.phase") {
		t.Errorf("the browser-facing schema leaked column SQL: %v", pub)
	}
}

func mustString(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
