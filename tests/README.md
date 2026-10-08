# Cross-module tests

`fixtures/` contains synthetic foundation data; `integration/` covers cross-module
contracts, migrations and PostgreSQL acceptance. Domain tests live beside Go
services and frontend components. Browser journeys live in `frontend/tests/e2e`.

Run `go test ./tests/...` for portable checks. PostgreSQL-dependent tests skip
without `TEST_DATABASE_URL`; use [local development](../docs/06-development/local-development.md)
and the [testing guide](../docs/06-development/testing.md) for full verification.
