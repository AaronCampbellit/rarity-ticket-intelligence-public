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
	"github.com/rarity-ticket-intelligence/rarity/backend/migrations"
)

func TestCalendarNotificationsUpgradePreservesLegacyReminderIdentities(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for PostgreSQL migration integration tests")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	schema := fmt.Sprintf("calendar_notifications_%d", time.Now().UnixNano())
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE") })
	schemaURL := databaseURLWithSearchPath(t, databaseURL, schema)
	database, err := sql.Open("pgx", schemaURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	goose.SetBaseFS(migrations.FS)
	t.Cleanup(func() { goose.SetBaseFS(nil) })
	if err = goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err = goose.UpToContext(ctx, database, ".", 94); err != nil {
		t.Fatalf("migrate to 94: %v", err)
	}
	pool, err := pgxpool.New(ctx, schemaURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	const mspID = "00000000-0000-4000-8000-000000009501"
	const techID = "00000000-0000-4000-8000-000000009502"
	const projectionID = "00000000-0000-4000-8000-000000009503"
	seeds := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO msp_organizations(id,display_id,name,created_by,updated_by) VALUES($1,'calendar-notifications','Calendar notifications',$2,$2)`, []any{mspID, techID}},
		{`INSERT INTO technicians(id,msp_id,email,display_name) VALUES($2,$1,'calendar@example.test','Calendar')`, []any{mspID, techID}},
		{`INSERT INTO calendar_event_projections(id,msp_id,source_type,source_id,event_role,source_revision,title,starts_on,all_day,scheduling_mode,assignee_id) VALUES($3,$1,'pto',$3,'unavailability',7,'Legacy reminder',DATE '2026-08-20',true,'informational',$2)`, []any{mspID, techID, projectionID}},
		{`INSERT INTO calendar_reminder_facts(id,msp_id,client_scope_key,projection_id,occurrence_key,reminder_kind,remind_at) VALUES ('00000000-0000-4000-8000-000000009504',$1,'global',$2,'one','due',TIMESTAMPTZ '2026-08-19 00:00:00+00'), ('00000000-0000-4000-8000-000000009505',$1,'global',$2,'one','due',TIMESTAMPTZ '2026-08-19 12:00:00+00')`, []any{mspID, projectionID}},
	}
	for _, seed := range seeds {
		if _, err = pool.Exec(ctx, seed.query, seed.args...); err != nil {
			t.Fatalf("seed legacy reminders: %v", err)
		}
	}
	if err = goose.UpByOneContext(ctx, database, "."); err != nil {
		t.Fatalf("upgrade to 95: %v", err)
	}
	var count int
	var minRevision, maxRevision int64
	var distinctThresholds, standardThresholds int
	if err = pool.QueryRow(ctx, `SELECT count(*),min(source_revision),max(source_revision),count(DISTINCT threshold),count(*) FILTER(WHERE threshold='24h') FROM calendar_reminder_facts WHERE msp_id=$1`, mspID).Scan(&count, &minRevision, &maxRevision, &distinctThresholds, &standardThresholds); err != nil {
		t.Fatal(err)
	}
	if count != 2 || minRevision != 7 || maxRevision != 7 || distinctThresholds != 2 || standardThresholds != 1 {
		t.Fatalf("legacy backfill count=%d revisions=%d..%d thresholds=%d standard=%d", count, minRevision, maxRevision, distinctThresholds, standardThresholds)
	}
}

func TestCalendarNotificationsDownDeterministicallyReconcilesCrossRevisionReminders(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for PostgreSQL migration integration tests")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	schema := fmt.Sprintf("calendar_notifications_down_%d", time.Now().UnixNano())
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE") })
	schemaURL := databaseURLWithSearchPath(t, databaseURL, schema)
	database, err := sql.Open("pgx", schemaURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	goose.SetBaseFS(migrations.FS)
	t.Cleanup(func() { goose.SetBaseFS(nil) })
	if err = goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err = goose.UpToContext(ctx, database, ".", 95); err != nil {
		t.Fatalf("migrate to 95: %v", err)
	}
	pool, err := pgxpool.New(ctx, schemaURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	const (
		mspID        = "00000000-0000-4000-8000-000000095101"
		techID       = "00000000-0000-4000-8000-000000095102"
		projectionID = "00000000-0000-4000-8000-000000095103"
		survivorID   = "00000000-0000-4000-8000-000000095105"
	)
	seeds := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO msp_organizations(id,display_id,name,created_by,updated_by) VALUES($1,'calendar-notifications-down','Calendar notifications down',$2,$2)`, []any{mspID, techID}},
		{`INSERT INTO technicians(id,msp_id,email,display_name) VALUES($2,$1,'calendar-down@example.test','Calendar down')`, []any{mspID, techID}},
		{`INSERT INTO calendar_event_projections(id,msp_id,source_type,source_id,event_role,source_revision,title,starts_on,all_day,scheduling_mode,assignee_id) VALUES($3,$1,'pto',$3,'unavailability',9,'Cross-revision reminder',DATE '2026-08-20',true,'informational',$2)`, []any{mspID, techID, projectionID}},
		{`INSERT INTO calendar_reminder_facts(id,msp_id,client_scope_key,projection_id,occurrence_key,reminder_kind,remind_at,source_revision,threshold) VALUES
			('00000000-0000-4000-8000-000000095104',$1,'global',$2,'one','due',TIMESTAMPTZ '2026-08-19 00:00:00+00',7,'24h'),
			($3,$1,'global',$2,'one','due',TIMESTAMPTZ '2026-08-19 00:00:00+00',9,'24h'),
			('00000000-0000-4000-8000-000000095106',$1,'global',$2,'one','due',TIMESTAMPTZ '2026-08-19 00:00:00+00',9,'48h')`, []any{mspID, projectionID, survivorID}},
	}
	for _, seed := range seeds {
		if _, err = pool.Exec(ctx, seed.query, seed.args...); err != nil {
			t.Fatalf("seed cross-revision reminders: %v", err)
		}
	}

	if err = goose.DownContext(ctx, database, "."); err != nil {
		t.Fatalf("downgrade from 95: %v", err)
	}
	var count int
	var retainedID string
	if err = pool.QueryRow(ctx, `SELECT count(*),min(id::text) FROM calendar_reminder_facts WHERE msp_id=$1 AND projection_id=$2`, mspID, projectionID).Scan(&count, &retainedID); err != nil {
		t.Fatal(err)
	}
	if count != 1 || retainedID != survivorID {
		t.Fatalf("legacy reminder reconciliation count=%d retained=%s, want count=1 retained=%s", count, retainedID, survivorID)
	}
}
