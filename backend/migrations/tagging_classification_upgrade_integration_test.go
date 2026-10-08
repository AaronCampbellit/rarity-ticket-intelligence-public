package migrations_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/store"
	"github.com/rarity-ticket-intelligence/rarity/backend/migrations"
)

func TestTaggingClassificationMigrationUpgradesExistingGlobalAdmin(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for PostgreSQL migration integration tests")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open PostgreSQL administration pool: %v", err)
	}
	t.Cleanup(admin.Close)
	schema := fmt.Sprintf("tagging_upgrade_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create isolated schema: %v", err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE") })
	schemaURL := databaseURLWithSearchPath(t, databaseURL, schema)
	if err := store.Migrate(ctx, schemaURL); err != nil {
		t.Fatalf("migrate isolated schema: %v", err)
	}
	database, err := sql.Open("pgx", schemaURL)
	if err != nil {
		t.Fatalf("open migration database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	goose.SetBaseFS(migrations.FS)
	t.Cleanup(func() { goose.SetBaseFS(nil) })
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("set PostgreSQL migration dialect: %v", err)
	}
	if err := goose.DownToContext(ctx, database, ".", 79); err != nil {
		t.Fatalf("roll back tagging migration: %v", err)
	}
	pool, err := pgxpool.New(ctx, schemaURL)
	if err != nil {
		t.Fatalf("open isolated schema pool: %v", err)
	}
	t.Cleanup(pool.Close)
	const mspID = "00000000-0000-0000-0000-000000000801"
	const actorID = "00000000-0000-0000-0000-000000000802"
	const roleID = "00000000-0000-0000-0000-000000000803"
	if _, err := pool.Exec(ctx, `INSERT INTO msp_organizations (id,display_id,name,created_by,updated_by) VALUES ($1,'legacy','Legacy',$2,$2)`, mspID, actorID); err != nil {
		t.Fatalf("create legacy MSP: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO roles (id,msp_id,key,name,system_role) VALUES ($1,$2,'global-admin','Global administrator',true)`, roleID, mspID); err != nil {
		t.Fatalf("create legacy global admin: %v", err)
	}
	if err := goose.UpByOneContext(ctx, database, "."); err != nil {
		t.Fatalf("apply tagging migration: %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM role_capabilities WHERE role_id = $1 AND capability = ANY($2::text[])`, roleID, []string{"classification.manage", "classification.apply", "classification.report", "classification.ai.manage"}).Scan(&count); err != nil || count != 4 {
		t.Fatalf("classification grants=%d error=%v", count, err)
	}
}

// Removing the MSP-only uniqueness rule from migration 80 itself does not
// upgrade databases that already applied it. This test starts at the published
// version-80 shape, preserves its existing evidence row, and requires HEAD to
// permit a second historical run for the same MSP.
func TestTaggingClassificationMigrationUpgradesLegacyRunHistoryFrom80(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for PostgreSQL migration integration tests")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open PostgreSQL administration pool: %v", err)
	}
	t.Cleanup(admin.Close)
	schema := fmt.Sprintf("tagging_history_upgrade_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create isolated schema: %v", err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE") })
	schemaURL := databaseURLWithSearchPath(t, databaseURL, schema)
	database, err := sql.Open("pgx", schemaURL)
	if err != nil {
		t.Fatalf("open migration database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	goose.SetBaseFS(migrations.FS)
	t.Cleanup(func() { goose.SetBaseFS(nil) })
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("set PostgreSQL migration dialect: %v", err)
	}
	if err := goose.UpToContext(ctx, database, ".", 80); err != nil {
		t.Fatalf("migrate isolated schema to version 80: %v", err)
	}
	pool, err := pgxpool.New(ctx, schemaURL)
	if err != nil {
		t.Fatalf("open isolated schema pool: %v", err)
	}
	t.Cleanup(pool.Close)
	const (
		mspID    = "00000000-0000-0000-0000-000000000861"
		actorID  = "00000000-0000-0000-0000-000000000862"
		firstID  = "00000000-0000-0000-0000-000000000863"
		secondID = "00000000-0000-0000-0000-000000000864"
	)
	if _, err := pool.Exec(ctx, `INSERT INTO msp_organizations(id,display_id,name,created_by,updated_by) VALUES($1,'legacy-history','Legacy history',$2,$2)`, mspID, actorID); err != nil {
		t.Fatalf("create legacy MSP: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO classification_migration_runs(id,msp_id,started_at,completed_at,status,evidence) VALUES($1,$2,now(),now(),'completed','{"migration":"000080_tagging_classification","strategy":"full_cutover"}')`, firstID, mspID); err != nil {
		t.Fatalf("create legacy migration evidence: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO classification_migration_runs(id,msp_id,started_at,completed_at,status,evidence) VALUES($1,$2,now(),now(),'completed','{"migration":"historical","strategy":"audit"}')`, secondID, mspID); err == nil {
		t.Fatal("published migration 80 did not enforce its legacy one-run-per-MSP constraint")
	}
	if err := goose.UpContext(ctx, database, "."); err != nil {
		t.Fatalf("upgrade version 80 schema to HEAD: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO classification_migration_runs(id,msp_id,started_at,completed_at,status,evidence) VALUES($1,$2,now(),now(),'completed','{"migration":"historical","strategy":"audit"}')`, secondID, mspID); err != nil {
		t.Fatalf("insert second migration history row after upgrade: %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM classification_migration_runs WHERE msp_id=$1 AND id=ANY($2::uuid[])`, mspID, []string{firstID, secondID}).Scan(&count); err != nil || count != 2 {
		t.Fatalf("preserved migration history rows=%d error=%v, want 2", count, err)
	}
}
