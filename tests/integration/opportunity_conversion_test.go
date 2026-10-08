package integration

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestOpportunityConversionTransactionRollsBackAndEnforcesOneProject(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for PostgreSQL conversion verification")
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

	ids := conversionFixtureIDs{
		msp:         "019fa006-0000-7000-8000-000000000001",
		client:      "019fa006-0000-7000-8000-000000000002",
		actor:       "019fa006-0000-7000-8000-000000000003",
		pipeline:    "019fa006-0000-7000-8000-000000000004",
		openStage:   "019fa006-0000-7000-8000-000000000005",
		wonStage:    "019fa006-0000-7000-8000-000000000006",
		opportunity: "019fa006-0000-7000-8000-000000000007",
		proposal:    "019fa006-0000-7000-8000-000000000008",
		version:     "019fa006-0000-7000-8000-000000000009",
		snapshot:    "019fa006-0000-7000-8000-000000000010",
		project:     "019fa006-0000-7000-8000-000000000011",
		conversion:  "019fa006-0000-7000-8000-000000000012",
	}
	seedConversionFixture(t, ctx, tx, ids)

	if _, err := tx.Exec(ctx, "SAVEPOINT conversion_attempt"); err != nil {
		t.Fatalf("create conversion savepoint: %v", err)
	}
	insertProjectFixture(t, ctx, tx, ids)
	if _, err := tx.Exec(ctx, "SELECT 1 / 0"); err == nil {
		t.Fatal("conversion failure fixture unexpectedly succeeded")
	}
	if _, err := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT conversion_attempt"); err != nil {
		t.Fatalf("rollback failed conversion: %v", err)
	}
	var count int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM projects WHERE id = $1", ids.project).Scan(&count); err != nil {
		t.Fatalf("count rolled-back projects: %v", err)
	}
	if count != 0 {
		t.Fatalf("failed conversion left %d projects", count)
	}
	var stage string
	if err := tx.QueryRow(ctx, "SELECT stage_id FROM opportunities WHERE id = $1", ids.opportunity).Scan(&stage); err != nil {
		t.Fatalf("load rolled-back opportunity: %v", err)
	}
	if stage != ids.openStage {
		t.Fatalf("failed conversion changed opportunity stage to %s", stage)
	}

	insertProjectFixture(t, ctx, tx, ids)
	if _, err := tx.Exec(ctx, `
		UPDATE opportunities
		SET stage_id = $2, version = version + 1, updated_by = $3
		WHERE id = $1 AND version = 1`,
		ids.opportunity, ids.wonStage, ids.actor,
	); err != nil {
		t.Fatalf("mark opportunity won: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO opportunity_conversions (
		  id, opportunity_id, proposal_version_id, project_id, msp_id, client_id,
		  request_key, preview_hash, conversion_snapshot, converted_by
		) VALUES ($1, $2, $3, $4, $5, $6, 'request-1', decode('ab', 'hex'), '{}', $7)`,
		ids.conversion, ids.opportunity, ids.version, ids.project,
		ids.msp, ids.client, ids.actor,
	); err != nil {
		t.Fatalf("insert conversion record: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO opportunity_conversions (
		  id, opportunity_id, proposal_version_id, project_id, msp_id, client_id,
		  request_key, preview_hash, conversion_snapshot, converted_by
		) VALUES (
		  '019fa006-0000-7000-8000-000000000013', $1, $2, $3, $4, $5,
		  'request-2', decode('cd', 'hex'), '{}', $6
		)`,
		ids.opportunity, ids.version, ids.project, ids.msp, ids.client, ids.actor,
	); err == nil {
		t.Fatal("second Project conversion for the same Opportunity succeeded")
	}
}

type conversionFixtureIDs struct {
	msp, client, actor, pipeline, openStage, wonStage string
	opportunity, proposal, version, snapshot          string
	project, conversion                               string
}

func seedConversionFixture(
	t *testing.T,
	ctx context.Context,
	tx pgx.Tx,
	ids conversionFixtureIDs,
) {
	t.Helper()
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO msp_organizations
			(id, display_id, name, created_by, updated_by)
			VALUES ($1, 'CONVERT-MSP', 'Conversion MSP', $2, $2)`,
			[]any{ids.msp, ids.actor}},
		{`INSERT INTO client_organizations
			(id, msp_id, display_id, name, created_by, updated_by)
			VALUES ($1, $2, 'CONVERT-CLIENT', 'Conversion Client', $3, $3)`,
			[]any{ids.client, ids.msp, ids.actor}},
		{`INSERT INTO pipelines
			(id, msp_id, key, name, created_by, updated_by)
			VALUES ($1, $2, 'conversion', 'Conversion', $3, $3)`,
			[]any{ids.pipeline, ids.msp, ids.actor}},
		{`INSERT INTO pipeline_stages
			(id, pipeline_id, msp_id, key, name, position, probability, forecast_category)
			VALUES
			($1, $3, $4, 'open', 'Open', 1, 50, 'weighted'),
			($2, $3, $4, 'won', 'Won', 2, 100, 'closed_won')`,
			[]any{ids.openStage, ids.wonStage, ids.pipeline, ids.msp}},
		{`INSERT INTO opportunities
			(id, msp_id, client_id, pipeline_id, stage_id, display_id, name,
			 currency, created_by, updated_by)
			VALUES ($1, $2, $3, $4, $5, 'OPP-CONVERT', 'Conversion',
			 'USD', $6, $6)`,
			[]any{ids.opportunity, ids.msp, ids.client, ids.pipeline, ids.openStage, ids.actor}},
		{`INSERT INTO proposals
			(id, msp_id, client_id, opportunity_id, display_id, current_version,
			 state, created_by, updated_by)
			VALUES ($1, $2, $3, $4, 'PROP-CONVERT', 1, 'accepted', $5, $5)`,
			[]any{ids.proposal, ids.msp, ids.client, ids.opportunity, ids.actor}},
		{`INSERT INTO proposal_versions
			(id, proposal_id, msp_id, version, currency, subtotal_minor, tax_minor,
			 total_minor, cost_minor, margin_minor, issued_by, pdf_snapshot_id)
			VALUES ($1, $2, $3, 1, 'USD', 12000, 0, 12000, 4000, 8000, $4, $5)`,
			[]any{ids.version, ids.proposal, ids.msp, ids.actor, ids.snapshot}},
	}
	for _, statement := range statements {
		if _, err := tx.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed conversion fixture: %v", err)
		}
	}
}

func insertProjectFixture(
	t *testing.T,
	ctx context.Context,
	tx pgx.Tx,
	ids conversionFixtureIDs,
) {
	t.Helper()
	if _, err := tx.Exec(ctx, `
		INSERT INTO projects (
		  id, msp_id, client_id, display_id, name, original_proposal_version_id,
		  created_by, updated_by
		) VALUES ($1, $2, $3, 'PRJ-CONVERT', 'Conversion Project', $4, $5, $5)`,
		ids.project, ids.msp, ids.client, ids.version, ids.actor,
	); err != nil {
		t.Fatalf("insert conversion project: %v", err)
	}
}
