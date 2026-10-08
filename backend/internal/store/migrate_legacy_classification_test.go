package store

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/rarity-ticket-intelligence/rarity/backend/migrations"
)

func TestMigrateRepairsLegacyRelocatedVersions(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for PostgreSQL integration tests")
	}
	ctx := context.Background()
	database, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	goose.SetBaseFS(migrations.FS)
	t.Cleanup(func() { goose.SetBaseFS(nil) })
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("set goose dialect: %v", err)
	}
	if err := goose.UpToContext(ctx, database, ".", 79); err != nil {
		t.Fatalf("migrate through version 79: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
		ALTER TABLE locations ADD CONSTRAINT locations_lifecycle_state_check
			CHECK (lifecycle_state IN ('active', 'inactive')) NOT VALID;
		ALTER TABLE contacts ADD CONSTRAINT contacts_lifecycle_state_check
			CHECK (lifecycle_state IN ('active', 'inactive')) NOT VALID;
		ALTER TABLE assets ADD CONSTRAINT assets_lifecycle_state_check
			CHECK (lifecycle_state IN ('active', 'inactive')) NOT VALID;
		ALTER TABLE services ADD CONSTRAINT services_lifecycle_state_check
			CHECK (lifecycle_state IN ('active', 'inactive')) NOT VALID;
		ALTER TABLE contracts ADD CONSTRAINT contracts_lifecycle_state_check
			CHECK (lifecycle_state IN ('active', 'inactive')) NOT VALID;
		ALTER TABLE locations VALIDATE CONSTRAINT locations_lifecycle_state_check;
		ALTER TABLE contacts VALIDATE CONSTRAINT contacts_lifecycle_state_check;
		ALTER TABLE assets VALIDATE CONSTRAINT assets_lifecycle_state_check;
		ALTER TABLE services VALIDATE CONSTRAINT services_lifecycle_state_check;
		ALTER TABLE contracts VALIDATE CONSTRAINT contracts_lifecycle_state_check;
		CREATE INDEX contacts_active_location_dependencies_idx
			ON contacts (msp_id, client_id, location_id)
			WHERE lifecycle_state = 'active' AND location_id IS NOT NULL;
		CREATE INDEX assets_active_location_dependencies_idx
			ON assets (msp_id, client_id, location_id)
			WHERE lifecycle_state = 'active' AND location_id IS NOT NULL;
	`); err != nil {
		t.Fatalf("install legacy migration 80 lifecycle schema: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO goose_db_version (version_id, is_applied)
		VALUES (80, true), (81, true)
	`); err != nil {
		t.Fatalf("record reserved versions: %v", err)
	}

	if err := Migrate(ctx, databaseURL); err != nil {
		t.Fatalf("repair and migrate database: %v", err)
	}

	var version int64
	var coreRelations, lifecycleObjects int
	if err := database.QueryRowContext(ctx, `
		SELECT max(version_id) FILTER (WHERE is_applied) FROM goose_db_version
	`).Scan(&version); err != nil {
		t.Fatalf("read migration version: %v", err)
	}
	if err := database.QueryRowContext(ctx, `
		SELECT count(*)
		FROM (VALUES
			('tag_groups'),
			('tag_usage_daily'),
			('classification_object_versions'),
			('classification_tag_operations')
		) AS expected(name)
		WHERE to_regclass(current_schema() || '.' || expected.name) IS NOT NULL
	`).Scan(&coreRelations); err != nil {
		t.Fatalf("read classification schema: %v", err)
	}
	if err := database.QueryRowContext(ctx, `
		SELECT count(*)
		FROM pg_constraint
		WHERE conname IN (
			'locations_lifecycle_state_check',
			'contacts_lifecycle_state_check',
			'assets_lifecycle_state_check',
			'services_lifecycle_state_check',
			'contracts_lifecycle_state_check'
		) AND convalidated
	`).Scan(&lifecycleObjects); err != nil {
		t.Fatalf("read lifecycle schema: %v", err)
	}
	if version != 101 || coreRelations != 4 || lifecycleObjects != 5 {
		t.Fatalf(
			"migration repair produced version %d, %d classification relations, and %d lifecycle constraints",
			version,
			coreRelations,
			lifecycleObjects,
		)
	}
}
