package psa_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/id"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/store"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/store/psa"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
)

func TestClassificationPreflightAgainstPostgres(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for classification preflight verification")
	}
	ctx := context.Background()
	if err := store.Migrate(ctx, databaseURL); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	msp, client, actor := id.New(), id.New(), id.New()
	group, unclassified, retired, work, run, historicalRun := id.New(), id.New(), id.New(), id.New(), id.New(), id.New()
	displayID := "PREFLIGHT-" + msp
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	exec(`INSERT INTO msp_organizations(id,display_id,name,created_by,updated_by) VALUES($1,$2,'preflight',$3,$3)`, msp, displayID, actor)
	exec(`INSERT INTO client_organizations(id,msp_id,display_id,name,created_by,updated_by) VALUES($1,$2,$3,'preflight client',$4,$4)`, client, msp, "CLIENT-"+client, actor)
	exec(`INSERT INTO tag_groups(id,msp_id,internal_key,label,position,created_by,updated_by) VALUES($1,$2,'taxonomy.system','System',1,$3,$3)`, group, msp, actor)
	exec(`INSERT INTO tags(id,msp_id,group_id,internal_key,label,system_tag,created_by,updated_by) VALUES($1,$2,$3,'taxonomy.system.unclassified','Unclassified',true,$4,$4)`, unclassified, msp, group, actor)
	exec(`INSERT INTO tags(id,msp_id,group_id,internal_key,label,lifecycle_state,created_by,updated_by) VALUES($1,$2,$3,'taxonomy.retired','Retired','archived',$4,$4)`, retired, msp, group, actor)
	exec(`INSERT INTO tag_ai_policies(id,msp_id,automatic_apply_enabled,automatic_apply_threshold,created_by,updated_by) VALUES($1,$2,false,.95,$3,$3)`, id.New(), msp, actor)
	exec(`INSERT INTO classification_migration_runs(id,msp_id,started_at,completed_at,status,category_source_present,evidence) VALUES($1,$2,now()-interval '1 day',now()-interval '1 day','completed',false,'{"migration":"legacy_category_import","strategy":"historical"}'),($3,$2,now(),now(),'completed',false,'{"migration":"000080_tagging_classification","strategy":"full_cutover"}')`, historicalRun, msp, run)
	exec(`INSERT INTO work_records(id,msp_id,client_id,display_id,record_type,title,status,priority,created_by,updated_by) VALUES($1,$2,$3,'PREFLIGHT-1','incident','preflight','new','normal',$4,$4)`, work, msp, client, actor)

	service := tagging.NewMigrationService(psa.NewClassificationMigrationRepositoryFromPool(pool), time.Now)
	report, err := service.ClassificationPreflight(ctx, displayID)
	if !errors.Is(err, tagging.ErrClassificationPreflightFailed) || report.ByObjectType[tagging.ObjectWorkRecord].Invalid != 1 {
		t.Fatalf("missing classification report=%+v error=%v", report, err)
	}

	exec(`INSERT INTO object_tag_assignments(id,msp_id,client_id,object_type,object_id,object_version,tag_id,assignment_source) VALUES($1,$2,$3,'work_record',$4,1,$5,'system_fallback')`, id.New(), msp, client, work, unclassified)
	report, err = service.ClassificationPreflight(ctx, displayID)
	if err != nil || !report.Passed || report.Totals.Total != 1 || report.Totals.Unclassified != 1 || report.Totals.Invalid != 0 {
		t.Fatalf("classified report=%+v error=%v", report, err)
	}
	var verified bool
	if err := pool.QueryRow(ctx, `SELECT evidence->>'verified_no_op'='true' FROM classification_migration_runs WHERE id=$1`, run).Scan(&verified); err != nil || !verified {
		t.Fatalf("verified no-op=%v error=%v", verified, err)
	}
	var historicalVerified bool
	if err := pool.QueryRow(ctx, `SELECT COALESCE(evidence->>'verified_no_op','false')='true' FROM classification_migration_runs WHERE id=$1`, historicalRun).Scan(&historicalVerified); err != nil || historicalVerified {
		t.Fatalf("historical run was mutated verified=%v error=%v", historicalVerified, err)
	}

	// The preflight session must fence a mutation after its snapshot until the
	// exact evidence update commits.
	session, err := psa.NewClassificationMigrationRepositoryFromPool(pool).BeginClassificationPreflight(ctx, displayID)
	if err != nil {
		t.Fatalf("begin fenced preflight: %v", err)
	}
	lateWork := id.New()
	mutationDone := make(chan error, 1)
	go func() {
		_, err := pool.Exec(ctx, `INSERT INTO work_records(id,msp_id,client_id,display_id,record_type,title,status,priority,created_by,updated_by) VALUES($1,$2,$3,'PREFLIGHT-LATE','incident','late mutation','new','normal',$4,$4)`, lateWork, msp, client, actor)
		mutationDone <- err
	}()
	select {
	case err := <-mutationDone:
		t.Fatalf("classification mutation escaped preflight fence: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	if err := session.RecordVerifiedNoOp(ctx, report); err != nil {
		t.Fatalf("record exact no-op: %v", err)
	}
	if err := session.Commit(ctx); err != nil {
		t.Fatalf("commit fenced preflight: %v", err)
	}
	select {
	case err := <-mutationDone:
		if err != nil {
			t.Fatalf("late mutation after commit: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("classification mutation remained blocked after evidence commit")
	}
	report, err = service.ClassificationPreflight(ctx, displayID)
	if !errors.Is(err, tagging.ErrClassificationPreflightFailed) || report.ByObjectType[tagging.ObjectWorkRecord].Invalid != 1 {
		t.Fatalf("late unclassified object report=%+v error=%v", report, err)
	}
	exec(`INSERT INTO object_tag_assignments(id,msp_id,client_id,object_type,object_id,object_version,tag_id,assignment_source) VALUES($1,$2,$3,'work_record',$4,1,$5,'system_fallback')`, id.New(), msp, client, lateWork, unclassified)

	exec(`INSERT INTO object_tag_assignments(id,msp_id,client_id,object_type,object_id,object_version,tag_id,assignment_source) VALUES($1,$2,$3,'work_record',$4,1,$5,'migration')`, id.New(), msp, client, work, retired)
	report, err = service.ClassificationPreflight(ctx, displayID)
	if !errors.Is(err, tagging.ErrClassificationPreflightFailed) || report.UnresolvedRetiredReferences != 1 {
		t.Fatalf("retired-reference report=%+v error=%v", report, err)
	}
}
