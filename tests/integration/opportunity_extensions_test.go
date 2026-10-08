package integration

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestOpportunityExtensionsEnforceScopedPostgreSQLContracts(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for PostgreSQL Opportunity extension verification")
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

	const (
		msp         = "019fa066-0000-7000-8000-000000000001"
		clientA     = "019fa066-0000-7000-8000-000000000002"
		clientB     = "019fa066-0000-7000-8000-000000000003"
		actor       = "019fa066-0000-7000-8000-000000000004"
		team        = "019fa066-0000-7000-8000-000000000005"
		contactA    = "019fa066-0000-7000-8000-000000000006"
		contactB    = "019fa066-0000-7000-8000-000000000007"
		pipeline    = "019fa066-0000-7000-8000-000000000008"
		stage       = "019fa066-0000-7000-8000-000000000009"
		opportunity = "019fa066-0000-7000-8000-000000000010"
	)
	seedOpportunityExtensionFixture(
		t, ctx, tx, msp, clientA, clientB, actor, team,
		contactA, contactB, pipeline, stage, opportunity,
	)

	if _, err := tx.Exec(ctx, `
UPDATE opportunities
SET custom_fields = '{"risk_summary":"Weekend cutover"}',
    team_id = $2
WHERE id = $1
`, opportunity, team); err != nil {
		t.Fatalf("store custom fields and team: %v", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO opportunity_contacts (
  opportunity_id, contact_id, msp_id, client_id, position
) VALUES ($1, $2, $3, $4, 1)
`, opportunity, contactA, msp, clientA); err != nil {
		t.Fatalf("link same-Client Opportunity Contact: %v", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO attachments (
  id, msp_id, client_id, opportunity_id, storage_key, filename,
  content_type, size_bytes, sha256, uploaded_by
) VALUES (
  '019fa066-0000-7000-8000-000000000011', $1, $2, $3,
  'msp/client/attachment', 'scope.txt', 'text/plain', 5,
  decode(repeat('00', 32), 'hex'), $4
)
`, msp, clientA, opportunity, actor); err != nil {
		t.Fatalf("store Opportunity attachment metadata: %v", err)
	}

	expectPostgreSQLRejection(t, ctx, tx, `
UPDATE opportunities SET custom_fields = '[]'::jsonb WHERE id = $1
`, opportunity)
	expectPostgreSQLRejection(t, ctx, tx, `
INSERT INTO opportunity_contacts (
  opportunity_id, contact_id, msp_id, client_id, position
) VALUES ($1, $2, $3, $4, 2)
`, opportunity, contactB, msp, clientA)
	expectPostgreSQLRejection(t, ctx, tx, `
INSERT INTO attachments (
  id, msp_id, client_id, opportunity_id, storage_key, filename,
  content_type, size_bytes, sha256, uploaded_by
) VALUES (
  '019fa066-0000-7000-8000-000000000012', $1, $2, $3,
  'msp/other/attachment', 'other.txt', 'text/plain', 5,
  decode(repeat('00', 32), 'hex'), $4
)
`, msp, clientB, opportunity, actor)
}

func seedOpportunityExtensionFixture(
	t *testing.T,
	ctx context.Context,
	tx pgx.Tx,
	msp, clientA, clientB, actor, team, contactA, contactB,
	pipeline, stage, opportunity string,
) {
	t.Helper()
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO msp_organizations
		  (id, display_id, name, created_by, updated_by)
		  VALUES ($1, 'EXT-MSP', 'Extension MSP', $2, $2)`,
			[]any{msp, actor}},
		{`INSERT INTO client_organizations
		  (id, msp_id, display_id, name, created_by, updated_by)
		  VALUES
		  ($1, $3, 'EXT-A', 'Extension A', $4, $4),
		  ($2, $3, 'EXT-B', 'Extension B', $4, $4)`,
			[]any{clientA, clientB, msp, actor}},
		{`INSERT INTO technicians (id, msp_id, email, display_name)
		  VALUES ($1, $2, 'extension@example.test', 'Extension Actor')`,
			[]any{actor, msp}},
		{`INSERT INTO teams (id, msp_id, key, name)
		  VALUES ($1, $2, 'extension', 'Extension Team')`,
			[]any{team, msp}},
		{`INSERT INTO contacts (
		    id, msp_id, client_id, display_id, display_name, created_by, updated_by
		  ) VALUES
		  ($1, $3, $4, 'CONTACT-A', 'Contact A', $5, $5),
		  ($2, $3, $6, 'CONTACT-B', 'Contact B', $5, $5)`,
			[]any{contactA, contactB, msp, clientA, actor, clientB}},
		{`INSERT INTO pipelines
		  (id, msp_id, key, name, created_by, updated_by)
		  VALUES ($1, $2, 'extension', 'Extension', $3, $3)`,
			[]any{pipeline, msp, actor}},
		{`INSERT INTO pipeline_stages
		  (id, pipeline_id, msp_id, key, name, position, probability, forecast_category)
		  VALUES ($1, $2, $3, 'open', 'Open', 1, 10, 'pipeline')`,
			[]any{stage, pipeline, msp}},
		{`INSERT INTO opportunities (
		    id, msp_id, client_id, pipeline_id, stage_id, display_id, name,
		    currency, created_by, updated_by
		  ) VALUES ($1, $2, $3, $4, $5, 'OPP-EXT', 'Extension',
		    'USD', $6, $6)`,
			[]any{opportunity, msp, clientA, pipeline, stage, actor}},
	}
	for _, statement := range statements {
		if _, err := tx.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed Opportunity extension fixture: %v", err)
		}
	}
}

func expectPostgreSQLRejection(
	t *testing.T,
	ctx context.Context,
	tx pgx.Tx,
	query string,
	args ...any,
) {
	t.Helper()
	if _, err := tx.Exec(ctx, "SAVEPOINT expected_rejection"); err != nil {
		t.Fatalf("create rejection savepoint: %v", err)
	}
	if _, err := tx.Exec(ctx, query, args...); err == nil {
		t.Fatal("out-of-contract PostgreSQL mutation unexpectedly succeeded")
	}
	if _, err := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT expected_rejection"); err != nil {
		t.Fatalf("rollback expected rejection: %v", err)
	}
}
