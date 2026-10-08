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

func TestCalendarResourcePlanAllowlistSurvivesMigration94DownAndReapply(t *testing.T) {
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
	schema := fmt.Sprintf("calendar_resource_plan_%d", time.Now().UnixNano())
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
	if err = goose.UpToContext(ctx, database, ".", 93); err != nil {
		t.Fatalf("migrate to 93: %v", err)
	}
	pool, err := pgxpool.New(ctx, schemaURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	const (
		mspID       = "00000000-0000-0000-0000-000000094001"
		clientID    = "00000000-0000-0000-0000-000000094002"
		actorID     = "00000000-0000-0000-0000-000000094003"
		proposalID  = "00000000-0000-0000-0000-000000094004"
		projection1 = "00000000-0000-0000-0000-000000094005"
		source1     = "00000000-0000-0000-0000-000000094006"
		change1     = "00000000-0000-0000-0000-000000094007"
		projection2 = "00000000-0000-0000-0000-000000094008"
		source2     = "00000000-0000-0000-0000-000000094009"
		change2     = "00000000-0000-0000-0000-000000094010"
	)
	seed := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO msp_organizations(id,display_id,name,created_by,updated_by) VALUES($1,'calendar-resource-plan','Calendar resource plan',$2,$2)`, []any{mspID, actorID}},
		{`INSERT INTO client_organizations(id,msp_id,display_id,name,created_by,updated_by) VALUES($1,$2,'client','Client',$3,$3)`, []any{clientID, mspID, actorID}},
		{`INSERT INTO technicians(id,msp_id,email,display_name) VALUES($1,$2,'calendar-resource-plan@example.test','Calendar actor')`, []any{actorID, mspID}},
		{`INSERT INTO calendar_scheduling_proposals(
			id,msp_id,client_id,actor_id,authorization_context_hash,
			source_revision_bindings,conflict_policy_version,expires_at
		) VALUES($1,$2,$3,$4,decode('94','hex'),'{}',1,now()+interval '1 hour')`, []any{proposalID, mspID, clientID, actorID}},
	}
	for _, statement := range seed {
		if _, err = pool.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed migration 93: %v", err)
		}
	}
	insertProjection := func(id, sourceID string) error {
		_, insertErr := pool.Exec(ctx, `INSERT INTO calendar_event_projections(
			id,msp_id,client_id,source_type,source_id,event_role,source_revision,title,
			starts_on,ends_on,all_day,scheduling_mode
		) VALUES($1,$2,$3,'resource_plan',$4,'allocation',1,'Resource plan',
			DATE '2026-08-10',DATE '2026-08-10',true,'informational')`, id, mspID, clientID, sourceID)
		return insertErr
	}
	insertChange := func(id, sourceID string, ordinal int) error {
		_, insertErr := pool.Exec(ctx, `INSERT INTO calendar_proposal_changes(
			id,proposal_id,msp_id,client_id,client_scope_key,ordinal,source_type,
			source_id,event_role,source_revision,change_type,current_value,proposed_value
		) VALUES($1,$2,$3,$4,$5,$6,'resource_plan',$7,
			'allocation',1,'schedule','{}','{}')`, id, proposalID, mspID, clientID, "client:"+clientID, ordinal, sourceID)
		return insertErr
	}
	if err = insertProjection(projection1, source1); err == nil {
		t.Fatal("migration 93 unexpectedly accepted a resource_plan projection")
	}
	if err = goose.UpByOneContext(ctx, database, "."); err != nil {
		t.Fatalf("upgrade to 94: %v", err)
	}
	if err = insertProjection(projection1, source1); err != nil {
		t.Fatalf("migration 94 rejected resource_plan projection: %v", err)
	}
	if err = insertChange(change1, source1, 1); err != nil {
		t.Fatalf("migration 94 rejected resource_plan proposal change: %v", err)
	}
	if err = goose.DownContext(ctx, database, "."); err != nil {
		t.Fatalf("migration 94 down with resource_plan rows: %v", err)
	}
	if err = insertProjection(projection2, source2); err != nil {
		t.Fatalf("down migration contracted projection allowlist: %v", err)
	}
	if err = insertChange(change2, source2, 2); err != nil {
		t.Fatalf("down migration contracted proposal-change allowlist: %v", err)
	}
	if err = goose.UpByOneContext(ctx, database, "."); err != nil {
		t.Fatalf("reapply migration 94: %v", err)
	}
	var projectionCount, changeCount int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM calendar_event_projections WHERE source_type='resource_plan'`).Scan(&projectionCount); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM calendar_proposal_changes WHERE source_type='resource_plan'`).Scan(&changeCount); err != nil {
		t.Fatal(err)
	}
	if projectionCount != 2 || changeCount != 2 {
		t.Fatalf("resource plan rows after reapply: projections=%d changes=%d", projectionCount, changeCount)
	}
}
