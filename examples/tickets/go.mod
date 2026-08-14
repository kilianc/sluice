// A separate module on purpose: the library depends on nothing outside the
// standard library, and this example needs a Postgres driver.
module github.com/kilianc/sluice/examples/tickets

go 1.23

require (
	github.com/jackc/pgx/v5 v5.7.2
	github.com/kilianc/sluice v0.2.0
)

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	golang.org/x/crypto v0.31.0 // indirect
	golang.org/x/sync v0.10.0 // indirect
	golang.org/x/text v0.21.0 // indirect
)

replace github.com/kilianc/sluice => ../..
