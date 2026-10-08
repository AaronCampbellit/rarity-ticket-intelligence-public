package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/rarity-ticket-intelligence/rarity/backend/migrations"
)

func Open(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open postgres pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres pool: %w", err)
	}
	return pool, nil
}

func Migrate(ctx context.Context, databaseURL string) error {
	database, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return fmt.Errorf("open migration database: %w", err)
	}
	defer database.Close()
	if err := database.PingContext(ctx); err != nil {
		return fmt.Errorf("ping migration database: %w", err)
	}

	goose.SetBaseFS(migrations.FS)
	defer goose.SetBaseFS(nil)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set goose dialect: %w", err)
	}

	classificationLedgerRepaired, err := repairLegacyClassificationMigrationLedger(ctx, database)
	if err != nil {
		return fmt.Errorf("repair legacy classification migration ledger: %w", err)
	}
	if err := repairRelocatedClientLifecycleMigration(ctx, database, classificationLedgerRepaired); err != nil {
		return fmt.Errorf("repair relocated client lifecycle migration: %w", err)
	}
	if err := goose.UpContext(ctx, database, "."); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}

// repairLegacyClassificationMigrationLedger handles the constrained-demo
// database produced while migrations 80 and 81 were reserved in the ledger
// before their classification schema was finalized. A normal installation is
// untouched. The repair is intentionally limited to the exact empty-schema
// shape observed in that database; partial schema state fails closed.
func repairLegacyClassificationMigrationLedger(ctx context.Context, database *sql.DB) (bool, error) {
	tx, err := database.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	var ledgerExists bool
	if err := tx.QueryRowContext(ctx, `
		SELECT to_regclass(current_schema() || '.goose_db_version') IS NOT NULL
	`).Scan(&ledgerExists); err != nil {
		return false, fmt.Errorf("inspect migration ledger: %w", err)
	}
	if !ledgerExists {
		return false, tx.Commit()
	}
	if _, err := tx.ExecContext(ctx, `
		SELECT pg_advisory_xact_lock(
			hashtextextended('rarity:migrations:classification-ledger-repair', 0)
		)
	`); err != nil {
		return false, fmt.Errorf("lock migration repair: %w", err)
	}

	var legacyApplied, newerApplied bool
	if err := tx.QueryRowContext(ctx, `
		SELECT
			EXISTS (
				SELECT 1 FROM goose_db_version
				WHERE version_id IN (80, 81) AND is_applied
			),
			EXISTS (
				SELECT 1 FROM goose_db_version
				WHERE version_id >= 82 AND is_applied
			)
	`).Scan(&legacyApplied, &newerApplied); err != nil {
		return false, fmt.Errorf("inspect classification migration versions: %w", err)
	}
	if !legacyApplied {
		return false, tx.Commit()
	}

	var relationCount int
	if err := tx.QueryRowContext(ctx, `
		SELECT count(*)
		FROM (VALUES
			('tag_groups'),
			('tags'),
			('object_tag_assignments'),
			('tag_assignment_events'),
			('tag_ai_policies'),
			('tag_ai_suggestions'),
			('tag_projection_cursors'),
			('tag_usage_daily'),
			('tag_cooccurrence_daily'),
			('classification_migration_runs')
		) AS expected(name)
		WHERE to_regclass(current_schema() || '.' || expected.name) IS NOT NULL
	`).Scan(&relationCount); err != nil {
		return false, fmt.Errorf("inspect classification schema: %w", err)
	}
	if relationCount == 10 {
		return false, tx.Commit()
	}
	if relationCount != 0 {
		return false, fmt.Errorf(
			"classification migrations 80-81 have partial schema state (%d of 10 core relations)",
			relationCount,
		)
	}
	if newerApplied {
		return false, fmt.Errorf("classification schema is absent after migration 82 or newer")
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM goose_db_version
		WHERE version_id IN (80, 81) AND is_applied
	`); err != nil {
		return false, fmt.Errorf("reset reserved classification versions: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit migration repair: %w", err)
	}
	return true, nil
}

// repairRelocatedClientLifecycleMigration adopts the exact schema created when
// client-resource lifecycle was migration 80, before that version was reused
// for classification and the lifecycle migration moved to version 97. It only
// activates alongside that ledger repair or after migrations have reached 96.
// All five validated constraints and both indexes must exist on the expected
// relations; any partial or altered shape fails closed.
func repairRelocatedClientLifecycleMigration(
	ctx context.Context,
	database *sql.DB,
	classificationLedgerRepaired bool,
) error {
	var ledgerExists bool
	if err := database.QueryRowContext(ctx, `
		SELECT to_regclass(current_schema() || '.goose_db_version') IS NOT NULL
	`).Scan(&ledgerExists); err != nil {
		return fmt.Errorf("inspect lifecycle migration ledger: %w", err)
	}
	if !ledgerExists {
		return nil
	}
	var version96Applied, version97Applied bool
	if err := database.QueryRowContext(ctx, `
		SELECT
			EXISTS (
				SELECT 1 FROM goose_db_version
				WHERE version_id = 96 AND is_applied
			),
			EXISTS (
				SELECT 1 FROM goose_db_version
				WHERE version_id = 97 AND is_applied
			)
	`).Scan(&version96Applied, &version97Applied); err != nil {
		return fmt.Errorf("inspect lifecycle migration versions: %w", err)
	}
	if version97Applied || (!classificationLedgerRepaired && !version96Applied) {
		return nil
	}

	var objectCount, exactObjectCount int
	if err := database.QueryRowContext(ctx, `
		WITH expected_constraints(table_name, constraint_name) AS (
			VALUES
				('locations', 'locations_lifecycle_state_check'),
				('contacts', 'contacts_lifecycle_state_check'),
				('assets', 'assets_lifecycle_state_check'),
				('services', 'services_lifecycle_state_check'),
				('contracts', 'contracts_lifecycle_state_check')
		), expected_indexes(table_name, index_name) AS (
			VALUES
				('contacts', 'contacts_active_location_dependencies_idx'),
				('assets', 'assets_active_location_dependencies_idx')
		), constraint_state AS (
			SELECT
				e.table_name,
				e.constraint_name,
				c.oid IS NOT NULL AS present,
				c.contype = 'c'
					AND c.convalidated
					AND pg_get_constraintdef(c.oid) =
						'CHECK ((lifecycle_state = ANY (ARRAY[''active''::text, ''inactive''::text])))'
					AS exact
			FROM expected_constraints e
			LEFT JOIN pg_constraint c
				ON c.conrelid = to_regclass(current_schema() || '.' || e.table_name)
				AND c.conname = e.constraint_name
		), index_state AS (
			SELECT
				e.table_name,
				e.index_name,
				i.indexrelid IS NOT NULL AS present,
				i.indisvalid
					AND i.indisready
					AND NOT i.indisunique
					AND pg_get_expr(i.indpred, i.indrelid) =
						'((lifecycle_state = ''active''::text) AND (location_id IS NOT NULL))'
					AND (
						SELECT array_agg(a.attname ORDER BY keys.ordinality)
						FROM unnest(i.indkey) WITH ORDINALITY AS keys(attnum, ordinality)
						JOIN pg_attribute a
							ON a.attrelid = i.indrelid AND a.attnum = keys.attnum
					) = ARRAY['msp_id', 'client_id', 'location_id']::name[]
					AS exact
			FROM expected_indexes e
			LEFT JOIN pg_index i
				ON i.indrelid = to_regclass(current_schema() || '.' || e.table_name)
				AND i.indexrelid = to_regclass(current_schema() || '.' || e.index_name)
		), state AS (
			SELECT present, exact FROM constraint_state
			UNION ALL
			SELECT present, exact FROM index_state
		)
		SELECT count(*) FILTER (WHERE present), count(*) FILTER (WHERE exact)
		FROM state
	`).Scan(&objectCount, &exactObjectCount); err != nil {
		return fmt.Errorf("inspect relocated lifecycle schema: %w", err)
	}
	if objectCount == 0 {
		return nil
	}
	if objectCount != 7 || exactObjectCount != 7 {
		return fmt.Errorf(
			"relocated lifecycle migration has partial or altered schema state (%d present, %d exact, expected 7)",
			objectCount,
			exactObjectCount,
		)
	}

	if err := goose.UpToContext(ctx, database, ".", 96); err != nil {
		return fmt.Errorf("apply migrations through relocated lifecycle predecessor: %w", err)
	}
	tx, err := database.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin lifecycle ledger transaction: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		SELECT pg_advisory_xact_lock(
			hashtextextended('rarity:migrations:lifecycle-ledger-repair', 0)
		)
	`); err != nil {
		return fmt.Errorf("lock lifecycle migration repair: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO goose_db_version (version_id, is_applied)
		SELECT 97, true
		WHERE NOT EXISTS (
			SELECT 1 FROM goose_db_version
			WHERE version_id = 97 AND is_applied
		)
	`); err != nil {
		return fmt.Errorf("adopt relocated lifecycle migration: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit lifecycle migration repair: %w", err)
	}
	return nil
}
