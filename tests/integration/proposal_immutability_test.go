package integration

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/rarity-ticket-intelligence/rarity/backend/migrations"
)

func TestIssuedProposalVersionCannotBeUpdatedDirectly(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for PostgreSQL immutability verification")
	}
	ctx := context.Background()
	if err := migrateTestDatabase(ctx, databaseURL); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(pool.Close)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin fixture transaction: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })

	ids := struct {
		msp, client, actor, pipeline, stage, opportunity, proposal, version, snapshot string
	}{
		"019fa001-0000-7000-8000-000000000001",
		"019fa001-0000-7000-8000-000000000002",
		"019fa001-0000-7000-8000-000000000003",
		"019fa001-0000-7000-8000-000000000004",
		"019fa001-0000-7000-8000-000000000005",
		"019fa001-0000-7000-8000-000000000006",
		"019fa001-0000-7000-8000-000000000007",
		"019fa001-0000-7000-8000-000000000008",
		"019fa001-0000-7000-8000-000000000009",
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed proposal fixture: %v", err)
		}
	}
	exec(`INSERT INTO msp_organizations
		(id, display_id, name, created_by, updated_by)
		VALUES ($1, 'PSA-TEST', 'PSA Test', $2, $2)`, ids.msp, ids.actor)
	exec(`INSERT INTO client_organizations
		(id, msp_id, display_id, name, created_by, updated_by)
		VALUES ($1, $2, 'CLIENT-TEST', 'Client Test', $3, $3)`, ids.client, ids.msp, ids.actor)
	exec(`INSERT INTO pipelines
		(id, msp_id, key, name, created_by, updated_by)
		VALUES ($1, $2, 'test', 'Test', $3, $3)`, ids.pipeline, ids.msp, ids.actor)
	exec(`INSERT INTO pipeline_stages
		(id, pipeline_id, msp_id, key, name, position, probability, forecast_category)
		VALUES ($1, $2, $3, 'issued', 'Issued', 1, 50, 'weighted')`,
		ids.stage, ids.pipeline, ids.msp)
	exec(`INSERT INTO opportunities
		(id, msp_id, client_id, pipeline_id, stage_id, display_id, name,
		 currency, created_by, updated_by)
		VALUES ($1, $2, $3, $4, $5, 'OPP-TEST', 'Test Opportunity',
		 'USD', $6, $6)`,
		ids.opportunity, ids.msp, ids.client, ids.pipeline, ids.stage, ids.actor)
	exec(`INSERT INTO proposals
		(id, msp_id, client_id, opportunity_id, display_id, current_version,
		 state, created_by, updated_by)
		VALUES ($1, $2, $3, $4, 'PROP-TEST', 1, 'issued', $5, $5)`,
		ids.proposal, ids.msp, ids.client, ids.opportunity, ids.actor)
	exec(`INSERT INTO proposal_versions
		(id, proposal_id, msp_id, version, currency, subtotal_minor, tax_minor,
		 total_minor, cost_minor, margin_minor, issued_by, pdf_snapshot_id)
		VALUES ($1, $2, $3, 1, 'USD', 1000, 0, 1000, 500, 500, $4, $5)`,
		ids.version, ids.proposal, ids.msp, ids.actor, ids.snapshot)

	_, err = tx.Exec(ctx, `UPDATE proposal_versions SET terms = 'mutated' WHERE id = $1`, ids.version)
	if err == nil {
		t.Fatal("direct update of issued Proposal Version succeeded")
	}
	if pgErr := new(pgconn.PgError); !asPostgresError(err, pgErr) {
		t.Fatalf("immutability error is not PostgreSQL-backed: %v", err)
	}
}

func migrateTestDatabase(ctx context.Context, databaseURL string) error {
	database, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return err
	}
	defer database.Close()
	goose.SetBaseFS(migrations.FS)
	defer goose.SetBaseFS(nil)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	return goose.UpContext(ctx, database, ".")
}

func asPostgresError(err error, target *pgconn.PgError) bool {
	for err != nil {
		if typed, ok := err.(*pgconn.PgError); ok {
			*target = *typed
			return true
		}
		type unwrapper interface{ Unwrap() error }
		value, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = value.Unwrap()
	}
	return false
}
