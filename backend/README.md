# Backend

Go application services, HTTP APIs, worker loops, PostgreSQL repositories and migrations.
`cmd/rarity-api` composes the runtime; `internal/` owns domain and infrastructure
boundaries. Record mutations enforce ordinary permissions and client scope and
persist correlated audit/outbox facts transactionally.

Run `go test ./backend/...` from the repository root. Database-dependent tests
require disposable PostgreSQL; use [the acceptance runner](../scripts/run-postgres-acceptance.sh)
and [local development instructions](../docs/06-development/local-development.md).
Operator commands and release gates live alongside the API in `cmd/`.
