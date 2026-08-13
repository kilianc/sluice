package sluice_test

import (
	"fmt"
	"log"

	"github.com/kilianc/sluice"
	"github.com/kilianc/sluice/dialect/postgres"
)

// The schema is the whole configuration: one declaration drives the compiler
// and the editor. This is the example in the README, executable so the output
// it advertises cannot drift from what the compiler emits.
const exampleSchema = `{
  "name": "machines",
  "options": { "fallbackFields": ["name"] },
  "fields": [
    { "name": "name",  "type": "string",  "column": "inv.name",         "description": "Machine hostname" },
    { "name": "phase", "type": "enum",    "column": "inv.phase", "values": ["in-use", "not-in-use"] },
    { "name": "cores", "type": "number",  "column": "inv.cores" },
    { "name": "rack",  "type": "enum",    "column": "loc.name",      "dynamic": true }
  ],
  "sorts": [ { "key": "name", "sql": "inv.name" } ]
}`

func Example() {
	schema, err := sluice.LoadSchema([]byte(exampleSchema))
	if err != nil {
		log.Fatal(err)
	}
	c, err := sluice.New(schema, postgres.Dialect)
	if err != nil {
		log.Fatal(err)
	}

	res, err := c.Compile(`phase = "in-use" AND rack ~ "ash1"`)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(res.SQL)
	fmt.Println(res.Args)
	fmt.Println(res.Fields)

	// Output:
	// (LOWER(inv.phase) = $1 AND LOWER(loc.name) LIKE $2 ESCAPE '\')
	// [in-use %ash1%]
	// [phase rack]
}

// Compile returns the first diagnostic and no SQL. Validate returns all of
// them, positioned, which is what an editor underlines.
func ExampleCompiler_Validate() {
	schema, _ := sluice.LoadSchema([]byte(exampleSchema))
	c, _ := sluice.New(schema, postgres.Dialect)

	for _, d := range c.Validate(`phse = "x" OR cores ~ 4`) {
		fmt.Printf("%d:%d %s: %s\n", d.Span.Start, d.Span.End, d.Code, d.Message)
	}

	// Output:
	// 0:4 unknown_field: unknown field phse; did you mean phase, name?
	// 20:21 unknown_operator_for_field: field cores does not support ~; it supports = != < <= > >=
}

// Suggest works on input that does not parse, because that is the state an
// editor is in while someone types.
func ExampleCompiler_Suggest() {
	schema, _ := sluice.LoadSchema([]byte(exampleSchema))
	c, _ := sluice.New(schema, postgres.Dialect)

	for _, s := range c.Suggest(`phase = `, 8) {
		fmt.Printf("%s (%s)\n", s.Text, s.Kind)
	}
	// A prefix that matches no field is wrapped into whole predicates against
	// the schema's fallback fields.
	for _, s := range c.Suggest(`web-1`, 5) {
		fmt.Printf("%s (%s)\n", s.Text, s.Kind)
	}

	// Output:
	// in-use (value)
	// not-in-use (value)
	// name = "web-1" (expression)
	// name ~ "web-1" (expression)
}

// Dynamic enum values are supplied per request and are never cached on the
// compiler, which stays immutable and safe for concurrent use.
func ExampleCompiler_WithDynamic() {
	schema, _ := sluice.LoadSchema([]byte(exampleSchema))
	c, _ := sluice.New(schema, postgres.Dialect)

	req := c.WithDynamic(map[string][]string{"rack": {"ash1-r01", "ash1-r02"}})
	res, err := req.Compile(`rack = "ash1-r01"`)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(res.SQL, res.Args)

	order, err := c.OrderBy("name", sluice.Desc)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(order)

	// Output:
	// LOWER(loc.name) = $1 [ash1-r01]
	// ORDER BY inv.name DESC NULLS LAST
}
