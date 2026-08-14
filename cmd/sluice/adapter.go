package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/kilianc/sluice"
	"github.com/kilianc/sluice/ast"
)

// The conformance adapter (AGENTS.md §11): JSON Lines on stdin/stdout, one
// request per line, one response per line, in order. No banner output;
// anything for humans goes to stderr.

type request struct {
	ID        string              `json:"id"`
	Op        string              `json:"op"`
	Schema    json.RawMessage     `json:"schema"`
	Dialect   string              `json:"dialect"`
	Dynamic   map[string][]string `json:"dynamic"`
	Input     string              `json:"input"`
	Cursor    int                 `json:"cursor"`
	Sort      string              `json:"sort"`
	Direction string              `json:"direction"`
	AST       *ast.Node           `json:"ast"`
}

type response struct {
	ID          string              `json:"id"`
	Tokens      []ast.Token         `json:"tokens,omitempty"`
	AST         *ast.Node           `json:"ast,omitempty"`
	HasAST      bool                `json:"-"`
	SQL         *string             `json:"sql,omitempty"`
	Args        []any               `json:"args,omitempty"`
	Fields      []string            `json:"fields,omitempty"`
	Suggestions []sluice.Suggestion `json:"suggestions,omitempty"`
	OrderBy     *string             `json:"orderBy,omitempty"`
	Diagnostics []sluice.Diagnostic `json:"diagnostics,omitempty"`
	Schema      *sluice.Schema      `json:"schema,omitempty"`
	Error       string              `json:"error,omitempty"`
}

func runAdapter(in io.Reader, out io.Writer) error {
	scan := bufio.NewScanner(in)
	scan.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	enc := json.NewEncoder(out)
	a := &adapter{compilers: map[string]*sluice.Compiler{}}

	for scan.Scan() {
		line := strings.TrimSpace(scan.Text())
		if line == "" {
			continue
		}
		var req request
		resp := response{}
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			resp.Error = "malformed request: " + err.Error()
		} else {
			resp = a.handle(req)
		}
		resp.ID = req.ID
		if err := enc.Encode(marshalResponse(resp)); err != nil {
			return err
		}
	}
	return scan.Err()
}

// marshalResponse applies the protocol's presence rules: keys irrelevant to the
// op are absent, empty arrays that carry meaning are present, and sql is never
// emitted alongside diagnostics.
func marshalResponse(r response) map[string]any {
	out := map[string]any{"id": r.ID}
	if r.Tokens != nil {
		out["tokens"] = r.Tokens
	}
	if r.HasAST {
		out["ast"] = r.AST
	}
	if len(r.Diagnostics) == 0 && r.SQL != nil {
		out["sql"] = *r.SQL
		out["args"] = r.Args
		out["fields"] = r.Fields
	}
	if r.Suggestions != nil {
		out["suggestions"] = r.Suggestions
	}
	if len(r.Diagnostics) == 0 && r.OrderBy != nil {
		out["orderBy"] = *r.OrderBy
	}
	if r.Diagnostics != nil {
		out["diagnostics"] = r.Diagnostics
	}
	if r.Schema != nil {
		out["schema"] = r.Schema
	}
	if r.Error != "" {
		out["error"] = r.Error
	}
	return out
}

type adapter struct {
	compilers map[string]*sluice.Compiler
}

func (a *adapter) handle(req request) response {
	resp := response{}

	if req.Op == "lex" {
		// Lexing needs no schema.
		toks, diags := ast.Lex(req.Input)
		resp.Tokens = trimEOF(toks)
		resp.Diagnostics = nonNilDiags(diags)
		return resp
	}

	c, err := a.compiler(req)
	if err != nil {
		resp.Error = err.Error()
		return resp
	}
	if len(req.Dynamic) > 0 {
		c = c.WithDynamic(req.Dynamic)
	}

	switch req.Op {
	case "parse":
		res := c.Parse(req.Input)
		resp.AST, resp.HasAST = res.Node, true
		resp.Diagnostics = nonNilDiags(res.Diagnostics)

	case "compile":
		var (
			out  sluice.Result
			cerr error
		)
		if req.AST != nil {
			out, cerr = c.CompileAST(req.AST)
		} else {
			out, cerr = c.Compile(req.Input)
		}
		resp.Diagnostics = []sluice.Diagnostic{}
		if cerr != nil {
			var e *sluice.Error
			if errors.As(cerr, &e) {
				resp.Diagnostics = []sluice.Diagnostic{e.Diagnostic}
			} else {
				resp.Error = cerr.Error()
			}
			break
		}
		sql := out.SQL
		resp.SQL, resp.Args, resp.Fields = &sql, out.Args, out.Fields

	case "validate":
		resp.Diagnostics = nonNilDiags(c.Validate(req.Input))

	case "suggest":
		out := c.Suggest(req.Input, req.Cursor)
		if out == nil {
			out = []sluice.Suggestion{}
		}
		resp.Suggestions = out

	case "orderby":
		dir := sluice.Asc
		if req.Direction == "desc" {
			dir = sluice.Desc
		}
		resp.Diagnostics = []sluice.Diagnostic{}
		clause, oerr := c.OrderBy(req.Sort, dir)
		if oerr != nil {
			var e *sluice.Error
			if errors.As(oerr, &e) {
				resp.Diagnostics = []sluice.Diagnostic{e.Diagnostic}
			} else {
				resp.Error = oerr.Error()
			}
			break
		}
		resp.OrderBy = &clause

	case "schema":
		pub := c.PublicSchema(req.Dynamic)
		resp.Schema = &pub

	default:
		resp.Error = "unknown op " + req.Op
	}
	return resp
}

// compiler resolves the request's schema and dialect, caching per pair so a run
// of thousands of cases builds each compiler once.
func (a *adapter) compiler(req request) (*sluice.Compiler, error) {
	dialect := req.Dialect
	if dialect == "" {
		dialect = "postgres"
	}
	d, ok := dialects[dialect]
	if !ok {
		return nil, fmt.Errorf("unknown dialect %q", dialect)
	}

	key := dialect + "\x00" + string(req.Schema)
	if c, ok := a.compilers[key]; ok {
		return c, nil
	}

	data, err := schemaBytes(req.Schema)
	if err != nil {
		return nil, err
	}
	schema, err := sluice.LoadSchema(data)
	if err != nil {
		return nil, err
	}
	c, err := sluice.New(schema, d)
	if err != nil {
		return nil, err
	}
	a.compilers[key] = c
	return c, nil
}

// schemaBytes accepts either an inline schema object or the basename of a file
// in the corpus schema directory.
func schemaBytes(raw json.RawMessage) ([]byte, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil, fmt.Errorf("request has no schema")
	}
	if trimmed[0] == '{' {
		return raw, nil
	}
	var name string
	if err := json.Unmarshal(raw, &name); err != nil {
		return nil, fmt.Errorf("schema must be an object or a name: %w", err)
	}
	dir := os.Getenv("SLUICE_CONFORMANCE_SCHEMAS")
	if dir == "" {
		dir = filepath.Join("conformance", "schemas")
	}
	return os.ReadFile(filepath.Join(dir, name+".json"))
}

func trimEOF(toks []ast.Token) []ast.Token {
	if n := len(toks); n > 0 && toks[n-1].Kind == ast.EOF {
		toks = toks[:n-1]
	}
	if toks == nil {
		return []ast.Token{}
	}
	return toks
}

func nonNilDiags(d []sluice.Diagnostic) []sluice.Diagnostic {
	if d == nil {
		return []sluice.Diagnostic{}
	}
	return d
}
