// Command sluice compiles, validates and inspects Sluice queries from the
// shell, and speaks the conformance adapter protocol (AGENTS.md §11).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/kilianc/sluice"
	"github.com/kilianc/sluice/dialect/duckdb"
	"github.com/kilianc/sluice/dialect/postgres"
)

var dialects = map[string]sluice.Dialect{
	"postgres": postgres.Dialect,
	"duckdb":   duckdb.Dialect,
}

const usage = `usage: sluice <command> [flags] [query]

commands:
  compile   -schema FILE [-dialect NAME] [-dynamic JSON] QUERY
  validate  -schema FILE QUERY
  schema    -schema FILE [-dynamic JSON]     print the browser-facing schema
  conformance-adapter                        JSON Lines protocol on stdin/stdout
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "compile":
		err = cmdCompile(os.Args[2:])
	case "validate":
		err = cmdValidate(os.Args[2:])
	case "schema":
		err = cmdSchema(os.Args[2:])
	case "conformance-adapter":
		err = runAdapter(os.Stdin, os.Stdout)
	case "-h", "--help", "help":
		fmt.Print(usage)
		return
	default:
		fmt.Fprintf(os.Stderr, "sluice: unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "sluice:", err)
		os.Exit(1)
	}
}

type commonFlags struct {
	set     *flag.FlagSet
	schema  string
	dialect string
	dynamic string
}

func newFlags(name string, args []string) (*commonFlags, []string, error) {
	f := &commonFlags{set: flag.NewFlagSet(name, flag.ContinueOnError)}
	f.set.StringVar(&f.schema, "schema", "", "path to a schema JSON file")
	f.set.StringVar(&f.dialect, "dialect", "postgres", "sql dialect: postgres or duckdb")
	f.set.StringVar(&f.dynamic, "dynamic", "", `dynamic enum values, e.g. {"team":["design-a"]}`)
	if err := f.set.Parse(args); err != nil {
		return nil, nil, err
	}
	return f, f.set.Args(), nil
}

func (f *commonFlags) compiler() (*sluice.Compiler, error) {
	if f.schema == "" {
		return nil, fmt.Errorf("-schema is required")
	}
	data, err := os.ReadFile(f.schema)
	if err != nil {
		return nil, err
	}
	schema, err := sluice.LoadSchema(data)
	if err != nil {
		return nil, err
	}
	d, ok := dialects[f.dialect]
	if !ok {
		return nil, fmt.Errorf("unknown dialect %q", f.dialect)
	}
	c, err := sluice.New(schema, d)
	if err != nil {
		return nil, err
	}
	if f.dynamic != "" {
		var values map[string][]string
		if err := json.Unmarshal([]byte(f.dynamic), &values); err != nil {
			return nil, fmt.Errorf("-dynamic: %w", err)
		}
		c = c.WithDynamic(values)
	}
	return c, nil
}

func cmdCompile(args []string) error {
	f, rest, err := newFlags("compile", args)
	if err != nil {
		return err
	}
	c, err := f.compiler()
	if err != nil {
		return err
	}
	res, err := c.Compile(strings.Join(rest, " "))
	if err != nil {
		return err
	}
	fmt.Println(res.SQL)
	enc, err := json.Marshal(res.Args)
	if err != nil {
		return err
	}
	fmt.Println(string(enc))
	return nil
}

func cmdValidate(args []string) error {
	f, rest, err := newFlags("validate", args)
	if err != nil {
		return err
	}
	c, err := f.compiler()
	if err != nil {
		return err
	}
	diags := c.Validate(strings.Join(rest, " "))
	for _, d := range diags {
		fmt.Printf("%d:%d %s: %s\n", d.Span.Start, d.Span.End, d.Code, d.Message)
	}
	if len(diags) > 0 {
		os.Exit(1)
	}
	return nil
}

func cmdSchema(args []string) error {
	f, _, err := newFlags("schema", args)
	if err != nil {
		return err
	}
	c, err := f.compiler()
	if err != nil {
		return err
	}
	var values map[string][]string
	if f.dynamic != "" {
		if err := json.Unmarshal([]byte(f.dynamic), &values); err != nil {
			return err
		}
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(c.PublicSchema(values))
}
