// Package conformance runs the language-agnostic corpus against every
// registered implementation through the adapter protocol of AGENTS.md §11.
//
// Nothing in this file knows what language an implementation is written in: an
// adapter is a command that speaks JSON Lines on stdin and stdout, and adding a
// language to the matrix means adding one entry to adapters.json.
package conformance

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

var corpusFilter = flag.String("corpus", "", "restrict the run to corpus files whose name contains this string")

// requestKeys are the case keys that form the adapter request; everything else
// in a case is either an expectation or documentation.
var requestKeys = map[string]bool{
	"op": true, "schema": true, "dialect": true, "dynamic": true,
	"input": true, "cursor": true, "ast": true,
}

// ignoredKeys are documentation. "was" records what the input produced under
// the pre-Sluice implementation, and exists to make 006-security.json legible
// as history rather than a list of strings.
var ignoredKeys = map[string]bool{"name": true, "description": true, "was": true}

type adapterSpec struct {
	Name     string   `json:"name"`
	Command  []string `json:"command"`
	Dialects []string `json:"dialects"`
	Ops      []string `json:"ops"`
}

type registry struct {
	Adapters []adapterSpec `json:"adapters"`
}

type corpusFile struct {
	name     string
	Version  int              `json:"version"`
	Op       string           `json:"op"`
	Schema   json.RawMessage  `json:"schema"`
	Dialect  string           `json:"dialect"`
	Cases    []map[string]any `json:"cases"`
	Filename string           `json:"-"`
}

func TestConformance(t *testing.T) {
	root := moduleRoot(t)
	reg := loadRegistry(t, root)
	files := loadCorpus(t, root)

	for _, spec := range reg.Adapters {
		t.Run(spec.Name, func(t *testing.T) {
			if reason := unavailable(root, spec); reason != "" {
				t.Skip(reason)
			}
			for _, f := range files {
				t.Run(f.name, func(t *testing.T) {
					runFile(t, root, spec, f)
				})
			}
		})
	}
}

// unavailable explains why an adapter cannot run here, or returns "" when it
// can. A registered implementation whose runtime or entry point is missing is
// skipped rather than failed — the JS adapter needs a JavaScript runtime, which
// is why it is invoked through the registry rather than assumed present. An
// adapter that starts and then misbehaves is a failure, not a skip.
func unavailable(root string, spec adapterSpec) string {
	if len(spec.Command) == 0 {
		return "adapter " + spec.Name + " registers no command"
	}
	if _, err := exec.LookPath(spec.Command[0]); err != nil {
		return fmt.Sprintf("adapter %q needs %q, which is not on PATH", spec.Name, spec.Command[0])
	}
	for _, arg := range spec.Command[1:] {
		if strings.HasPrefix(arg, "-") || !strings.Contains(arg, "/") {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(arg))); err != nil {
			return fmt.Sprintf("adapter %q has no entry point at %s", spec.Name, arg)
		}
	}
	return ""
}

func runFile(t *testing.T, root string, spec adapterSpec, f corpusFile) {
	reqs := make([]map[string]any, 0, len(f.Cases))
	kept := make([]map[string]any, 0, len(f.Cases))
	for i, c := range f.Cases {
		op := stringOr(c["op"], f.Op)
		dialect := stringOr(c["dialect"], f.Dialect)
		if !supports(spec.Ops, op) {
			continue
		}
		if dialect != "" && !supports(spec.Dialects, dialect) {
			continue
		}
		req := map[string]any{"id": fmt.Sprintf("%s#%d", f.name, i), "op": op}
		if dialect != "" {
			req["dialect"] = dialect
		}
		if c["schema"] != nil {
			req["schema"] = c["schema"]
		} else {
			req["schema"] = json.RawMessage(f.Schema)
		}
		for k := range requestKeys {
			if k == "op" || k == "schema" || k == "dialect" {
				continue
			}
			if v, ok := c[k]; ok {
				if k == "ast" && !astIsInput(c, op) {
					continue
				}
				req[k] = v
			}
		}
		reqs = append(reqs, req)
		kept = append(kept, c)
	}
	if len(reqs) == 0 {
		t.Skipf("adapter %q claims no op or dialect used by %s", spec.Name, f.name)
	}

	responses := drive(t, root, spec, reqs)

	for i, c := range kept {
		id := reqs[i]["id"].(string)
		t.Run(caseName(c, i), func(t *testing.T) {
			got, ok := responses[id]
			if !ok {
				t.Fatalf("adapter returned no response for %s", id)
			}
			if msg, ok := got["error"].(string); ok && msg != "" {
				t.Fatalf("adapter reported an error: %s", msg)
			}
			checkProtocol(t, got)
			op := stringOr(c["op"], f.Op)
			for k, want := range c {
				if ignoredKeys[k] {
					continue
				}
				// "ast" is the expectation unless the case sent it as input.
				if requestKeys[k] && !(k == "ast" && !astIsInput(c, op)) {
					continue
				}
				gotVal, present := got[k]
				if !present {
					t.Errorf("response is missing %q\nwant %s", k, mustJSON(want))
					continue
				}
				if err := match(k, want, gotVal); err != nil {
					t.Errorf("%v\n want %s\n  got %s", err, mustJSON(want), mustJSON(gotVal))
				}
			}
		})
	}
}

// astIsInput reports whether a case's "ast" key is the request (the case
// exercises AST decoding) rather than the expectation.
func astIsInput(c map[string]any, op string) bool {
	if op == "parse" {
		return false
	}
	_, hasInput := c["input"]
	return !hasInput
}

// checkProtocol enforces the response rules of AGENTS.md §11 that are not
// specific to any case: SQL is never returned alongside diagnostics.
func checkProtocol(t *testing.T, got map[string]any) {
	t.Helper()
	diags, _ := got["diagnostics"].([]any)
	if len(diags) == 0 {
		return
	}
	if _, hasSQL := got["sql"]; hasSQL {
		t.Errorf("response carries both diagnostics and sql:\n%s", mustJSON(got))
	}
}

// drive starts an adapter, feeds it every request and collects the responses by
// id. The adapter is expected to answer in order and exit 0 when stdin closes.
func drive(t *testing.T, root string, spec adapterSpec, reqs []map[string]any) map[string]map[string]any {
	t.Helper()
	cmd := exec.Command(spec.Command[0], spec.Command[1:]...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "SLUICE_CONFORMANCE_SCHEMAS="+filepath.Join(root, "conformance", "schemas"))
	stderr := &strings.Builder{}
	cmd.Stderr = stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting adapter %q: %v", spec.Name, err)
	}

	go func() {
		enc := json.NewEncoder(stdin)
		for _, req := range reqs {
			if err := enc.Encode(req); err != nil {
				break
			}
		}
		stdin.Close()
	}()

	out := make(map[string]map[string]any, len(reqs))
	scan := bufio.NewScanner(stdout)
	scan.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scan.Scan() {
		line := strings.TrimSpace(scan.Text())
		if line == "" {
			continue
		}
		var resp map[string]any
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			t.Fatalf("adapter %q wrote a line that is not JSON: %s", spec.Name, line)
		}
		id, _ := resp["id"].(string)
		out[id] = resp
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("adapter %q exited with %v\nstderr:\n%s", spec.Name, err, stderr)
	}
	return out
}

// match compares an expectation to a response value.
//
// Objects are compared as subsets: every key the corpus asserts must be present
// and equal, and keys the corpus does not mention are ignored. That is what
// lets a case assert a diagnostic's code and span without freezing its message,
// and lets an implementation carry optional spans and details (AGENTS.md §11).
// Arrays compare element-wise and must have the same length, so nothing can be
// silently missing.
func match(path string, want, got any) error {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: expected an object", path)
		}
		keys := make([]string, 0, len(w))
		for k := range w {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			gv, ok := g[k]
			if !ok {
				return fmt.Errorf("%s: missing key %q", path, k)
			}
			if err := match(path+"."+k, w[k], gv); err != nil {
				return err
			}
		}
		return nil
	case []any:
		g, ok := got.([]any)
		if !ok {
			return fmt.Errorf("%s: expected an array", path)
		}
		if len(g) != len(w) {
			return fmt.Errorf("%s: expected %d entries, got %d", path, len(w), len(g))
		}
		for i := range w {
			if err := match(fmt.Sprintf("%s[%d]", path, i), w[i], g[i]); err != nil {
				return err
			}
		}
		return nil
	default:
		if !reflect.DeepEqual(want, got) {
			return fmt.Errorf("%s: %v != %v", path, want, got)
		}
		return nil
	}
}

func loadRegistry(t *testing.T, root string) registry {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "conformance", "adapters.json"))
	if err != nil {
		t.Fatal(err)
	}
	var reg registry
	if err := json.Unmarshal(data, &reg); err != nil {
		t.Fatal(err)
	}
	if len(reg.Adapters) == 0 {
		t.Fatal("adapters.json registers no implementations")
	}
	return reg
}

func loadCorpus(t *testing.T, root string) []corpusFile {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(root, "conformance", "corpus", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	var files []corpusFile
	for _, p := range paths {
		name := strings.TrimSuffix(filepath.Base(p), ".json")
		if *corpusFilter != "" && !strings.Contains(name, *corpusFilter) {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		var f corpusFile
		if err := json.Unmarshal(data, &f); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		f.name = name
		files = append(files, f)
	}
	if len(files) == 0 {
		t.Fatalf("no corpus files matched %q", *corpusFilter)
	}
	return files
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find the module root")
		}
		dir = parent
	}
}

func supports(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

func stringOr(v any, fallback string) string {
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return fallback
}

func caseName(c map[string]any, i int) string {
	name, _ := c["name"].(string)
	if name == "" {
		return fmt.Sprintf("case-%d", i)
	}
	return strings.ReplaceAll(name, " ", "_")
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}
