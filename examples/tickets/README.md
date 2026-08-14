# tickets

The application built in [`docs/tutorial-postgres.md`](../../docs/tutorial-postgres.md):
an ordinary Postgres issue tracker with a Sluice filter bar in front of it.

```bash
docker run -d --name sluice-tut \
  -e POSTGRES_PASSWORD=sluice -e POSTGRES_DB=tickets \
  -p 55432:5432 postgres:16-alpine

export DATABASE_URL="postgres://postgres:sluice@localhost:55432/tickets"
psql "$DATABASE_URL" -f schema.sql

go run .          # http://localhost:8080, or set PORT
```

| file | |
|---|---|
| `schema.sql` | the database: four tables, an enum type, JSONB, ten tickets |
| `schema.json` | the ten fields someone can filter on, and the SQL each stands for |
| `main.go` | compile, publish the schema, serve the rows |
| `index.html` | the filter bar, wired in three calls |

It is a separate Go module on purpose: the library depends on nothing outside
the standard library, and this needs a Postgres driver.

Worth poking at, in this order:

- `GET /api/tickets?filter=…` returns the rows **and** the SQL, the bound
  arguments and the fields the query touched.
- `GET /sluice/schema.json` is what the browser gets. Grep it for `column`;
  there are none.
- Try `stat = "open"`, and watch the bar underline the four characters the
  compiler objected to.
- Try `title = "x"; DROP TABLE ticket`. It is a 400, and the table is still
  there — not because a semicolon was blocklisted, but because nothing copies an
  unrecognized token into the output.
