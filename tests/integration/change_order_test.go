package integration

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestChangeOrderEvidenceIsImmutableReasonedAndAppliedOnce(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for PostgreSQL Change Order verification")
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
		msp:         "019fa007-0000-7000-8000-000000000001",
		client:      "019fa007-0000-7000-8000-000000000002",
		actor:       "019fa007-0000-7000-8000-000000000003",
		pipeline:    "019fa007-0000-7000-8000-000000000004",
		openStage:   "019fa007-0000-7000-8000-000000000005",
		wonStage:    "019fa007-0000-7000-8000-000000000006",
		opportunity: "019fa007-0000-7000-8000-000000000007",
		proposal:    "019fa007-0000-7000-8000-000000000008",
		version:     "019fa007-0000-7000-8000-000000000009",
		snapshot:    "019fa007-0000-7000-8000-000000000010",
		project:     "019fa007-0000-7000-8000-000000000011",
	}
	seedConversionFixture(t, ctx, tx, ids)
	insertProjectFixture(t, ctx, tx, ids)
	changeOrderID := "019fa007-0000-7000-8000-000000000012"
	changeVersionID := "019fa007-0000-7000-8000-000000000013"
	if _, err := tx.Exec(ctx, `
		INSERT INTO change_orders (
		  id, project_id, msp_id, client_id, display_id, state,
		  current_version, created_by, updated_by
		) VALUES ($1, $2, $3, $4, 'CO-1', 'approved', 1, $5, $5)`,
		changeOrderID, ids.project, ids.msp, ids.client, ids.actor,
	); err != nil {
		t.Fatalf("insert Change Order: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO change_order_versions (
		  id, change_order_id, msp_id, client_id, version, description,
		  currency, revenue_delta_minor, cost_delta_minor, labor_delta_minutes,
		  issued_by
		) VALUES ($1, $2, $3, $4, 1, 'Additional scope', 'USD', 2000, 500, 60, $5)`,
		changeVersionID, changeOrderID, ids.msp, ids.client, ids.actor,
	); err != nil {
		t.Fatalf("insert Change Order Version: %v", err)
	}
	if _, err := tx.Exec(ctx, "SAVEPOINT immutable_version"); err != nil {
		t.Fatalf("create immutable version savepoint: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE change_order_versions SET description = 'mutated' WHERE id = $1`,
		changeVersionID,
	); err == nil {
		t.Fatal("immutable Change Order Version update succeeded")
	}
	if _, err := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT immutable_version"); err != nil {
		t.Fatalf("recover from immutable version update: %v", err)
	}

	if _, err := tx.Exec(ctx, "SAVEPOINT invalid_override"); err != nil {
		t.Fatalf("create invalid override savepoint: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO change_order_decisions (
		  id, change_order_id, change_order_version_id, msp_id, client_id,
		  previous_state, decision, override, reason, decided_at, decided_by
		) VALUES (
		  '019fa007-0000-7000-8000-000000000014', $1, $2, $3, $4,
		  'issued', 'approved', true, '', now(), $5
		)`,
		changeOrderID, changeVersionID, ids.msp, ids.client, ids.actor,
	); err == nil {
		t.Fatal("override without a reason succeeded")
	}
	if _, err := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT invalid_override"); err != nil {
		t.Fatalf("rollback invalid override: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO change_order_decisions (
		  id, change_order_id, change_order_version_id, msp_id, client_id,
		  previous_state, decision, override, reason, decided_at, decided_by
		) VALUES (
		  '019fa007-0000-7000-8000-000000000015', $1, $2, $3, $4,
		  'issued', 'approved', true, 'Recorded customer approval', now(), $5
		)`,
		changeOrderID, changeVersionID, ids.msp, ids.client, ids.actor,
	); err != nil {
		t.Fatalf("insert reasoned override: %v", err)
	}
	if _, err := tx.Exec(ctx, "SAVEPOINT decision_update"); err != nil {
		t.Fatalf("create decision update savepoint: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE change_order_decisions SET reason = 'mutated'
		WHERE change_order_version_id = $1`,
		changeVersionID,
	); err == nil {
		t.Fatal("immutable Change Order decision update succeeded")
	}
	if _, err := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT decision_update"); err != nil {
		t.Fatalf("recover from immutable decision update: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO change_order_applications (
		  id, change_order_id, change_order_version_id, project_id,
		  msp_id, client_id, applied_at, applied_by
		) VALUES (
		  '019fa007-0000-7000-8000-000000000016', $1, $2, $3, $4, $5, now(), $6
		)`,
		changeOrderID, changeVersionID, ids.project, ids.msp, ids.client, ids.actor,
	); err != nil {
		t.Fatalf("insert Change Order application: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO change_order_applications (
		  id, change_order_id, change_order_version_id, project_id,
		  msp_id, client_id, applied_at, applied_by
		) VALUES (
		  '019fa007-0000-7000-8000-000000000017', $1, $2, $3, $4, $5, now(), $6
		)`,
		changeOrderID, changeVersionID, ids.project, ids.msp, ids.client, ids.actor,
	); err == nil {
		t.Fatal("second application of the same Change Order Version succeeded")
	}
}
