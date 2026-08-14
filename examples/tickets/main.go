// Command tickets is the application built in docs/tutorial-postgres.md: an
// ordinary Postgres issue tracker with a Sluice filter bar in front of it.
//
// It is a separate Go module, so the library itself stays free of dependencies
// while this can use a real driver.
//
//	docker compose up -d          # or any Postgres
//	psql "$DATABASE_URL" -f schema.sql
//	go run .                      # http://localhost:8080
package main

import (
	"cmp"
	"context"
	_ "embed"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kilianc/sluice"
	"github.com/kilianc/sluice/dialect/postgres"
)

//go:embed schema.json
var schemaJSON []byte

//go:embed index.html
var indexHTML []byte

// The columns the list returns. The filter never names a table, so this is the
// only place the shape of the page and the shape of the database meet.
const base = `SELECT t.id, t.title, t.status::text AS status, t.priority,
       coalesce(u.display_name, '—') AS assignee,
       coalesce(tm.name, '—') AS team,
       (SELECT count(*) FROM comment c WHERE c.ticket_id = t.id) AS comments
FROM ticket t
LEFT JOIN app_user u ON u.id = t.assignee_id
LEFT JOIN team tm ON tm.id = t.team_id`

func main() {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = "postgres://postgres:sluice@localhost:55432/tickets"
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		log.Fatalf("connecting: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("connecting: %v", err)
	}

	schema, err := sluice.LoadSchema(schemaJSON)
	if err != nil {
		log.Fatalf("schema: %v", err) // every problem at once, at startup
	}
	compiler, err := sluice.New(schema, postgres.Dialect)
	if err != nil {
		log.Fatalf("schema: %v", err)
	}

	srv := &server{pool: pool, compiler: compiler}

	http.HandleFunc("GET /sluice/schema.json", srv.schema)
	http.HandleFunc("GET /api/tickets", srv.tickets)
	http.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(indexHTML)
	})
	// The packages, straight from this repository, so the example needs no
	// build step. A real deployment installs them from npm instead.
	http.Handle("GET /js/", http.StripPrefix("/js/", http.FileServer(http.Dir("../../js"))))

	addr := ":" + cmp.Or(os.Getenv("PORT"), "8080")
	log.Printf("listening on http://localhost%s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

type server struct {
	pool     *pgxpool.Pool
	compiler *sluice.Compiler
}

// teams reads the values of the one dynamic field. They come from the database
// per request rather than from the source, and are never cached on the
// compiler.
func (s *server) teams(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT name FROM team ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// schema serves the browser-facing schema: field names, types, values and
// descriptions, with every `column` removed. The client drives its autocomplete
// with it and learns nothing about the database.
func (s *server) schema(w http.ResponseWriter, r *http.Request) {
	teams, err := s.teams(r.Context())
	if err != nil {
		http.Error(w, "listing teams", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, s.compiler.PublicSchema(map[string][]string{"team": teams}))
}

func (s *server) tickets(w http.ResponseWriter, r *http.Request) {
	teams, err := s.teams(r.Context())
	if err != nil {
		http.Error(w, "listing teams", http.StatusInternalServerError)
		return
	}

	res, err := s.compiler.
		WithDynamic(map[string][]string{"team": teams}).
		Compile(r.URL.Query().Get("filter"))
	if err != nil {
		// A diagnostic, with a position. Hand it to the client as-is: the bar
		// can underline exactly the span the compiler objected to.
		var e *sluice.Error
		if ok := asSluiceError(err, &e); ok {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": e.Diagnostic})
			return
		}
		http.Error(w, "compiling filter", http.StatusInternalServerError)
		return
	}

	query := base
	if res.SQL != "" {
		// Parenthesized, so it stays one expression whatever the top-level
		// operator is — and AND-ed with whatever scoping you already apply.
		query += "\nWHERE (" + res.SQL + ")"
	}
	order, err := s.compiler.OrderBy(sortKey(r), direction(r))
	if err != nil {
		http.Error(w, "unknown sort key", http.StatusBadRequest)
		return
	}
	query += "\n" + order + "\nLIMIT 100"

	rows, err := s.pool.Query(r.Context(), query, res.Args...)
	if err != nil {
		log.Printf("query: %v", err)
		http.Error(w, "querying", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	out := []map[string]any{}
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			http.Error(w, "scanning", http.StatusInternalServerError)
			return
		}
		row := map[string]any{}
		for i, f := range rows.FieldDescriptions() {
			row[f.Name] = values[i]
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, "scanning", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"rows": out,
		// Useful to see while learning: what the filter became, and which
		// fields it touched.
		"sql":    res.SQL,
		"args":   res.Args,
		"fields": res.Fields,
	})
}

func sortKey(r *http.Request) string {
	if key := r.URL.Query().Get("sort"); key != "" {
		return key
	}
	return "priority"
}

func direction(r *http.Request) sluice.Direction {
	if strings.EqualFold(r.URL.Query().Get("dir"), "desc") {
		return sluice.Desc
	}
	return sluice.Asc
}

func asSluiceError(err error, target **sluice.Error) bool {
	e, ok := err.(*sluice.Error)
	if ok {
		*target = e
	}
	return ok
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Printf("writing response: %v", err)
	}
}
