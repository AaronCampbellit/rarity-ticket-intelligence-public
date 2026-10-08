package integration

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTaggingIsolationKeepsClientAssignmentsAndMutationsScoped(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for PostgreSQL tagging isolation verification")
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
		t.Fatalf("begin isolation fixture: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })

	ids := struct{ msp, first, second, actor, group, tag, work, other, assignment string }{
		"019fa080-0000-7000-8000-000000000001",
		"019fa080-0000-7000-8000-000000000002",
		"019fa080-0000-7000-8000-000000000003",
		"019fa080-0000-7000-8000-000000000004",
		"019fa080-0000-7000-8000-000000000005",
		"019fa080-0000-7000-8000-000000000006",
		"019fa080-0000-7000-8000-000000000007",
		"019fa080-0000-7000-8000-000000000008",
		"019fa080-0000-7000-8000-000000000009",
	}
	exec := func(statement string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, statement, args...); err != nil {
			t.Fatalf("seed tagging-isolation fixture: %v", err)
		}
	}
	exec(`INSERT INTO msp_organizations (id, display_id, name, created_by, updated_by)
VALUES ($1, 'TAGGING-ISO', 'Tagging isolation', $2, $2)`, ids.msp, ids.actor)
	for _, client := range []string{ids.first, ids.second} {
		exec(`INSERT INTO client_organizations
  (id, msp_id, display_id, name, created_by, updated_by)
VALUES ($1, $2, $3, $3, $4, $4)`, client, ids.msp, "CLIENT-"+client, ids.actor)
	}
	exec(`INSERT INTO tag_groups
  (id, msp_id, internal_key, label, position, created_by, updated_by)
VALUES ($1, $2, 'taxonomy.isolation', 'Isolation', 1, $3, $3)`,
		ids.group, ids.msp, ids.actor)
	exec(`INSERT INTO tags
  (id, msp_id, group_id, internal_key, label, created_by, updated_by)
VALUES ($1, $2, $3, 'taxonomy.custom.isolation', 'Network', $4, $4)`,
		ids.tag, ids.msp, ids.group, ids.actor)
	exec(`INSERT INTO work_records
  (id, msp_id, client_id, display_id, record_type, title, status, priority, created_by, updated_by)
VALUES ($1, $2, $3, 'ISO-1', 'incident', 'First client', 'new', 'normal', $4, $4)`,
		ids.work, ids.msp, ids.first, ids.actor)
	if _, err := tx.Exec(ctx, "SAVEPOINT duplicate_object_id"); err != nil {
		t.Fatalf("create duplicate-object savepoint: %v", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO work_records
  (id, msp_id, client_id, display_id, record_type, title, status, priority, created_by, updated_by)
VALUES ($1, $2, $3, 'ISO-REUSE', 'incident', 'Wrong client', 'new', 'normal', $4, $4)`,
		ids.work, ids.msp, ids.second, ids.actor); err == nil {
		t.Fatal("same work-record UUID was reusable across clients")
	}
	if _, err := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT duplicate_object_id"); err != nil {
		t.Fatalf("recover duplicate-object probe: %v", err)
	}
	exec(`INSERT INTO work_records
  (id, msp_id, client_id, display_id, record_type, title, status, priority, created_by, updated_by)
VALUES ($1, $2, $3, 'ISO-2', 'incident', 'Second client', 'new', 'normal', $4, $4)`,
		ids.other, ids.msp, ids.second, ids.actor)
	exec(`INSERT INTO object_tag_assignments
  (id, msp_id, client_id, object_type, object_id, object_version, tag_id,
   assignment_source, assigned_at, assigned_by)
VALUES ($1, $2, $3, 'work_record', $4, 1, $5, 'human', now(), $6)`,
		ids.assignment, ids.msp, ids.first, ids.work, ids.tag, ids.actor)

	var secondCount int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM object_tag_assignments
WHERE msp_id = $1 AND client_id = $2 AND object_type = 'work_record'`,
		ids.msp, ids.second).Scan(&secondCount); err != nil {
		t.Fatalf("count second-client assignments: %v", err)
	}
	if secondCount != 0 {
		t.Fatalf("second client observed %d first-client assignment(s)", secondCount)
	}
	command, err := tx.Exec(ctx, `UPDATE object_tag_assignments
SET version = version + 1
WHERE msp_id = $1 AND client_id = $2 AND object_type = 'work_record' AND object_id = $3`,
		ids.msp, ids.second, ids.work)
	if err != nil {
		t.Fatalf("perform scoped second-client mutation: %v", err)
	}
	if command.RowsAffected() != 0 {
		t.Fatalf("second-client mutation changed %d first-client association(s)", command.RowsAffected())
	}
}
