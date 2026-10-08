package migrations_test

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/store"
	"github.com/rarity-ticket-intelligence/rarity/backend/migrations"
)

func TestUnifiedCalendarMigrationsRoundTripAndUpgradeGlobalAdmin(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for PostgreSQL calendar migration verification")
	}
	ctx := context.Background()
	if err := store.Migrate(ctx, databaseURL); err != nil {
		t.Fatalf("migrate calendar schema to head: %v", err)
	}
	database, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("open migration database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	goose.SetBaseFS(migrations.FS)
	t.Cleanup(func() { goose.SetBaseFS(nil) })
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("set PostgreSQL migration dialect: %v", err)
	}
	if err := goose.DownToContext(ctx, database, ".", 90); err != nil {
		t.Fatalf("roll back unified calendar migrations: %v", err)
	}

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open calendar verification pool: %v", err)
	}
	t.Cleanup(pool.Close)
	var anchorsAbsent bool
	if err := pool.QueryRow(ctx, `
SELECT to_regclass('calendar_event_projections') IS NULL
   AND to_regclass('technician_schedule_versions') IS NULL
   AND to_regclass('project_milestones') IS NULL
   AND NOT EXISTS (
     SELECT 1 FROM information_schema.columns
     WHERE table_schema = current_schema()
       AND table_name = 'work_records'
       AND column_name = 'scheduled_starts_at'
   )
`).Scan(&anchorsAbsent); err != nil || !anchorsAbsent {
		t.Fatalf("calendar rollback anchors absent=%v error=%v", anchorsAbsent, err)
	}

	const (
		mspID   = "00000000-0000-0000-0000-000000009101"
		actorID = "00000000-0000-0000-0000-000000009102"
		roleID  = "00000000-0000-0000-0000-000000009103"
	)
	if _, err := pool.Exec(ctx, `
INSERT INTO msp_organizations(id,display_id,name,created_by,updated_by)
		VALUES($1,'calendar-upgrade','Calendar upgrade',$2,$2)
		ON CONFLICT (id) DO NOTHING
`, mspID, actorID); err != nil {
		t.Fatalf("seed pre-calendar MSP: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO roles(id,msp_id,key,name,system_role)
		VALUES($1,$2,'global-admin','Global administrator',true)
		ON CONFLICT (id) DO NOTHING
`, roleID, mspID); err != nil {
		t.Fatalf("seed pre-calendar global administrator: %v", err)
	}
	if err := goose.UpContext(ctx, database, "."); err != nil {
		t.Fatalf("reapply unified calendar migrations: %v", err)
	}

	var anchorsPresent bool
	if err := pool.QueryRow(ctx, `
SELECT to_regclass('calendar_event_projections') IS NOT NULL
   AND to_regclass('technician_schedule_versions') IS NOT NULL
   AND to_regclass('project_milestones') IS NOT NULL
   AND EXISTS (
     SELECT 1 FROM information_schema.columns
     WHERE table_schema = current_schema()
       AND table_name = 'work_records'
       AND column_name = 'scheduled_starts_at'
   )
`).Scan(&anchorsPresent); err != nil || !anchorsPresent {
		t.Fatalf("calendar reapply anchors present=%v error=%v", anchorsPresent, err)
	}
	var capabilityCount int
	if err := pool.QueryRow(ctx, `
SELECT count(*)
FROM role_capabilities
WHERE role_id=$1 AND msp_id=$2
  AND capability=ANY($3::text[])
`, roleID, mspID, []string{
		"calendar.read", "calendar.schedule", "calendar.workforce.manage",
		"calendar.commitment.manage", "calendar.policy.manage", "calendar.ai.recommend",
	}).Scan(&capabilityCount); err != nil || capabilityCount != 6 {
		t.Fatalf("calendar capabilities=%d error=%v, want 6", capabilityCount, err)
	}
}

func TestCalendarProjectionChildrenRejectCrossClientParents(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for PostgreSQL calendar scope verification")
	}
	ctx := context.Background()
	if err := store.Migrate(ctx, databaseURL); err != nil {
		t.Fatalf("migrate calendar schema: %v", err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open calendar scope pool: %v", err)
	}
	t.Cleanup(pool.Close)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin calendar scope transaction: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })

	const (
		mspID      = "00000000-0000-0000-0000-000000009201"
		actorID    = "00000000-0000-0000-0000-000000009202"
		clientAID  = "00000000-0000-0000-0000-000000009203"
		clientBID  = "00000000-0000-0000-0000-000000009204"
		projection = "00000000-0000-0000-0000-000000009205"
		proposalID = "00000000-0000-0000-0000-000000009206"
		zeroClient = "00000000-0000-0000-0000-000000000000"
		globalProj = "00000000-0000-0000-0000-000000009207"
		zeroProj   = "00000000-0000-0000-0000-000000009208"
	)
	if _, err := tx.Exec(ctx, `
INSERT INTO msp_organizations(id,display_id,name,created_by,updated_by)
VALUES($1,'calendar-scope','Calendar scope',$2,$2)
`, mspID, actorID); err != nil {
		t.Fatalf("seed calendar MSP: %v", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO technicians(id,msp_id,email,display_name)
VALUES($1,$2,'calendar-scope@example.test','Calendar scope')
`, actorID, mspID); err != nil {
		t.Fatalf("seed calendar technician: %v", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO client_organizations(id,msp_id,display_id,name,created_by,updated_by)
VALUES
  ($1,$3,'calendar-a','Calendar A',$4,$4),
  ($2,$3,'calendar-b','Calendar B',$4,$4),
  ($5,$3,'calendar-zero','Calendar Zero',$4,$4)
`, clientAID, clientBID, mspID, actorID, zeroClient); err != nil {
		t.Fatalf("seed calendar scopes: %v", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO calendar_event_projections(
  id,msp_id,client_id,source_type,source_id,event_role,source_revision,title,
  starts_on,all_day,scheduling_mode,created_at,updated_at
) VALUES($1,$2,$3,'work_record',$1,'due',1,'Scoped event',DATE '2026-09-01',true,'informational',now(),now())
`, projection, mspID, clientAID); err != nil {
		t.Fatalf("seed scoped projection: %v", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO calendar_scheduling_proposals(
  id,msp_id,client_id,actor_id,authorization_context_hash,
  source_revision_bindings,conflict_policy_version,expires_at
) VALUES($1,$2,$3,$4,'x'::bytea,'{}'::jsonb,1,now()+interval '1 hour')
`, proposalID, mspID, clientAID, actorID); err != nil {
		t.Fatalf("seed scoped proposal: %v", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO calendar_event_projections(
  id,msp_id,client_id,source_type,source_id,event_role,source_revision,title,
  starts_on,all_day,scheduling_mode,created_at,updated_at
) VALUES
  ($1,$3,NULL,'pto',$1,'unavailability',1,'Global event',DATE '2026-09-01',true,'informational',now(),now()),
  ($2,$3,$4,'work_record',$2,'due',1,'Zero-client event',DATE '2026-09-01',true,'informational',now(),now())
`, globalProj, zeroProj, mspID, zeroClient); err != nil {
		t.Fatalf("seed null and zero-client projections: %v", err)
	}

	assertRejected := func(name, query string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, "SAVEPOINT calendar_scope_check"); err != nil {
			t.Fatalf("savepoint for %s: %v", name, err)
		}
		_, rejectedErr := tx.Exec(ctx, query, args...)
		if _, err := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT calendar_scope_check"); err != nil {
			t.Fatalf("rollback %s check: %v", name, err)
		}
		if rejectedErr == nil {
			t.Errorf("%s accepted a cross-client parent", name)
		}
	}
	assertRejected("recurrence exception", `
INSERT INTO calendar_recurrence_exceptions(
  id,msp_id,client_id,client_scope_key,projection_id,original_local_key,state,source_revision,
  created_by,updated_by
) VALUES('00000000-0000-0000-0000-000000009211',$1,$2,'client:'||$2::uuid::text,$3,'2026-09-01','cancelled',1,$4,$4)
`, mspID, clientBID, projection, actorID)
	assertRejected("live change", `
INSERT INTO calendar_live_changes(
  msp_id,client_id,client_scope_key,projection_id,source_type,source_id,event_role,change_type,source_revision
) VALUES($1,$2,'client:'||$2::uuid::text,$3,'work_record',$3,'due','upserted',1)
`, mspID, clientBID, projection)
	assertRejected("reminder fact", `
INSERT INTO calendar_reminder_facts(
  id,msp_id,client_id,client_scope_key,projection_id,reminder_kind,remind_at
) VALUES('00000000-0000-0000-0000-000000009212',$1,$2,'client:'||$2::uuid::text,$3,'due',now()+interval '1 hour')
`, mspID, clientBID, projection)
	assertRejected("proposal change", `
INSERT INTO calendar_proposal_changes(
  id,proposal_id,msp_id,client_id,client_scope_key,ordinal,source_type,source_id,event_role,
  source_revision,change_type,current_value,proposed_value
) VALUES(
  '00000000-0000-0000-0000-000000009213',$1,$2,$3,'client:'||$3::uuid::text,1,'work_record',$4,
  'due',1,'reschedule','{}'::jsonb,'{}'::jsonb
)
`, proposalID, mspID, clientBID, projection)
	assertRejected("zero client against global projection", `
INSERT INTO calendar_recurrence_exceptions(
  id,msp_id,client_id,client_scope_key,projection_id,original_local_key,state,
  source_revision,created_by,updated_by
) VALUES(
  '00000000-0000-0000-0000-000000009214',$1,$2,'client:'||$2::uuid::text,$3,
  '2026-09-01','cancelled',1,$4,$4
)
`, mspID, zeroClient, globalProj, actorID)
	assertRejected("global scope against zero-client projection", `
INSERT INTO calendar_recurrence_exceptions(
  id,msp_id,client_id,client_scope_key,projection_id,original_local_key,state,
  source_revision,created_by,updated_by
) VALUES(
  '00000000-0000-0000-0000-000000009215',$1,NULL,'global',$2,
  '2026-09-01','cancelled',1,$3,$3
)
`, mspID, zeroProj, actorID)
}

func TestUnifiedCalendarSchemaEnforcesSchedulingSemantics(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for PostgreSQL calendar semantic verification")
	}
	ctx := context.Background()
	if err := store.Migrate(ctx, databaseURL); err != nil {
		t.Fatalf("migrate calendar schema: %v", err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open calendar semantic pool: %v", err)
	}
	t.Cleanup(pool.Close)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin calendar semantic transaction: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })

	const (
		mspID     = "00000000-0000-0000-0000-000000009301"
		actorID   = "00000000-0000-0000-0000-000000009302"
		clientAID = "00000000-0000-0000-0000-000000009303"
		clientBID = "00000000-0000-0000-0000-000000009304"
		serviceB  = "00000000-0000-0000-0000-000000009305"
		schedule1 = "00000000-0000-0000-0000-000000009306"
		schedule2 = "00000000-0000-0000-0000-000000009307"
	)
	seeds := []struct {
		name      string
		statement string
	}{
		{"MSP", `INSERT INTO msp_organizations(id,display_id,name,created_by,updated_by)
          VALUES('00000000-0000-0000-0000-000000009301','calendar-semantics','Calendar semantics','00000000-0000-0000-0000-000000009302','00000000-0000-0000-0000-000000009302')`,
		},
		{"technician", `INSERT INTO technicians(id,msp_id,email,display_name)
          VALUES('00000000-0000-0000-0000-000000009302','00000000-0000-0000-0000-000000009301','calendar-semantics@example.test','Calendar semantics')`,
		},
		{"clients", `INSERT INTO client_organizations(id,msp_id,display_id,name,created_by,updated_by) VALUES
          ('00000000-0000-0000-0000-000000009303','00000000-0000-0000-0000-000000009301','semantic-a','Semantic A','00000000-0000-0000-0000-000000009302','00000000-0000-0000-0000-000000009302'),
          ('00000000-0000-0000-0000-000000009304','00000000-0000-0000-0000-000000009301','semantic-b','Semantic B','00000000-0000-0000-0000-000000009302','00000000-0000-0000-0000-000000009302')`,
		},
		{"service", `INSERT INTO services(id,msp_id,client_id,display_id,name,created_by,updated_by)
          VALUES('00000000-0000-0000-0000-000000009305','00000000-0000-0000-0000-000000009301','00000000-0000-0000-0000-000000009304','semantic-service','Semantic service','00000000-0000-0000-0000-000000009302','00000000-0000-0000-0000-000000009302')`,
		},
		{"pipeline", `INSERT INTO pipelines(id,msp_id,key,name,created_by,updated_by)
          VALUES('00000000-0000-0000-0000-000000009320','00000000-0000-0000-0000-000000009301','calendar-semantic','Calendar semantic','00000000-0000-0000-0000-000000009302','00000000-0000-0000-0000-000000009302')`,
		},
		{"pipeline stage", `INSERT INTO pipeline_stages(
          id,pipeline_id,msp_id,key,name,position,probability,forecast_category
        ) VALUES(
          '00000000-0000-0000-0000-000000009321','00000000-0000-0000-0000-000000009320','00000000-0000-0000-0000-000000009301','won','Won',1,100,'closed_won'
        )`,
		},
		{"opportunity", `INSERT INTO opportunities(
          id,msp_id,client_id,pipeline_id,stage_id,display_id,name,currency,created_by,updated_by
        ) VALUES(
          '00000000-0000-0000-0000-000000009322','00000000-0000-0000-0000-000000009301','00000000-0000-0000-0000-000000009303','00000000-0000-0000-0000-000000009320','00000000-0000-0000-0000-000000009321','calendar-opportunity','Calendar opportunity','USD','00000000-0000-0000-0000-000000009302','00000000-0000-0000-0000-000000009302'
        )`,
		},
		{"proposal", `INSERT INTO proposals(
          id,msp_id,client_id,opportunity_id,display_id,created_by,updated_by
        ) VALUES(
          '00000000-0000-0000-0000-000000009323','00000000-0000-0000-0000-000000009301','00000000-0000-0000-0000-000000009303','00000000-0000-0000-0000-000000009322','calendar-proposal','00000000-0000-0000-0000-000000009302','00000000-0000-0000-0000-000000009302'
        )`,
		},
		{"proposal version", `INSERT INTO proposal_versions(
          id,proposal_id,msp_id,version,currency,subtotal_minor,tax_minor,total_minor,
          cost_minor,margin_minor,issued_by,pdf_snapshot_id
        ) VALUES(
          '00000000-0000-0000-0000-000000009324','00000000-0000-0000-0000-000000009323','00000000-0000-0000-0000-000000009301',1,'USD',0,0,0,0,0,'00000000-0000-0000-0000-000000009302','00000000-0000-0000-0000-000000009327'
        )`,
		},
		{"project", `INSERT INTO projects(
          id,msp_id,client_id,display_id,name,original_proposal_version_id,created_by,updated_by
        ) VALUES(
          '00000000-0000-0000-0000-000000009325','00000000-0000-0000-0000-000000009301','00000000-0000-0000-0000-000000009303','calendar-project','Calendar project','00000000-0000-0000-0000-000000009324','00000000-0000-0000-0000-000000009302','00000000-0000-0000-0000-000000009302'
        )`,
		},
	}
	for _, seed := range seeds {
		if _, err := tx.Exec(ctx, seed.statement); err != nil {
			t.Fatalf("seed calendar semantic %s: %v", seed.name, err)
		}
	}

	assertRejected := func(name, query string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, "SAVEPOINT calendar_semantic_check"); err != nil {
			t.Fatalf("savepoint for %s: %v", name, err)
		}
		_, rejectedErr := tx.Exec(ctx, query, args...)
		if _, err := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT calendar_semantic_check"); err != nil {
			t.Fatalf("rollback %s check: %v", name, err)
		}
		if rejectedErr == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	assertAccepted := func(name, query string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, query, args...); err != nil {
			t.Fatalf("%s was rejected: %v", name, err)
		}
	}

	assertRejected("unknown timezone", `
INSERT INTO calendar_event_projections(
  id,msp_id,source_type,source_id,event_role,source_revision,title,
  starts_at,timezone,all_day,scheduling_mode,created_at,updated_at
) VALUES('00000000-0000-0000-0000-000000009311',$1,'milestone','00000000-0000-0000-0000-000000009311','milestone',1,'Bad timezone',now(),'Mars/Olympus',false,'informational',now(),now())
`, mspID)
	assertRejected("empty recurrence object", `
INSERT INTO calendar_event_projections(
  id,msp_id,source_type,source_id,event_role,source_revision,title,
  starts_on,all_day,scheduling_mode,recurrence_rule,created_at,updated_at
) VALUES('00000000-0000-0000-0000-000000009312',$1,'task','00000000-0000-0000-0000-000000009312','due',1,'Bad recurrence',DATE '2026-09-01',true,'informational','{}'::jsonb,now(),now())
`, mspID)
	assertRejected("recurrence without positive interval", `
INSERT INTO calendar_event_projections(
  id,msp_id,source_type,source_id,event_role,source_revision,title,
  starts_on,all_day,scheduling_mode,recurrence_rule,created_at,updated_at
) VALUES('00000000-0000-0000-0000-000000009313',$1,'milestone','00000000-0000-0000-0000-000000009313','milestone',1,'Valid recurrence',DATE '2026-09-01',true,'informational','{"frequency":"weekly","weekdays":[1,3],"count":12}'::jsonb,now(),now())
`, mspID)
	assertRejected("recurrence with impossible until date", `
INSERT INTO calendar_event_projections(
  id,msp_id,source_type,source_id,event_role,source_revision,title,
  starts_on,all_day,scheduling_mode,recurrence_rule,created_at,updated_at
) VALUES('00000000-0000-0000-0000-00000000931a',$1,'milestone','00000000-0000-0000-0000-00000000931a','milestone',1,'Bad until',DATE '2026-09-01',true,'informational','{"frequency":"daily","interval":1,"until":"2026-02-30"}'::jsonb,now(),now())
`, mspID)
	assertRejected("recurrence with zero interval", `
INSERT INTO calendar_event_projections(
  id,msp_id,source_type,source_id,event_role,source_revision,title,
  starts_on,all_day,scheduling_mode,recurrence_rule,created_at,updated_at
) VALUES('00000000-0000-0000-0000-00000000931c',$1,'milestone','00000000-0000-0000-0000-00000000931c','milestone',1,'Zero interval',DATE '2026-09-01',true,'informational','{"frequency":"daily","interval":0,"count":2}'::jsonb,now(),now())
`, mspID)
	assertAccepted("structured all-day recurrence", `
INSERT INTO calendar_event_projections(
  id,msp_id,source_type,source_id,event_role,source_revision,title,
  starts_on,all_day,scheduling_mode,recurrence_rule,created_at,updated_at
) VALUES('00000000-0000-0000-0000-00000000931b',$1,'milestone','00000000-0000-0000-0000-00000000931b','milestone',1,'Valid recurrence',DATE '2026-09-01',true,'informational','{"frequency":"weekly","interval":1,"weekdays":[1,3],"count":12}'::jsonb,now(),now())
`, mspID)
	assertAccepted("due-only milestone", `
INSERT INTO project_milestones(
  id,msp_id,client_id,project_id,name,due_on,created_by,updated_by
) VALUES(
  '00000000-0000-0000-0000-000000009326',$1,$2,'00000000-0000-0000-0000-000000009325','Due only',DATE '2026-09-15',$3,$3
)
`, mspID, clientAID, actorID)

	assertAccepted("first schedule version", `
INSERT INTO technician_schedule_versions(
  id,msp_id,technician_id,timezone,effective_from,version,created_by
) VALUES($1,$2,$3,'America/Chicago',DATE '2026-09-01',1,$3)
`, schedule1, mspID, actorID)
	assertAccepted("second schedule version", `
INSERT INTO technician_schedule_versions(
  id,msp_id,technician_id,timezone,effective_from,version,created_by
) VALUES($1,$2,$3,'America/Chicago',DATE '2026-10-01',2,$3)
`, schedule2, mspID, actorID)
	assertAccepted("first all-day schedule exception", `
INSERT INTO technician_schedule_exceptions(
  id,schedule_version_id,msp_id,technician_id,exception_on,
  availability_state,all_day,capacity_percent,created_by,updated_by
) VALUES('00000000-0000-0000-0000-000000009314',$1,$2,$3,DATE '2026-10-15','unavailable',true,0,$3,$3)
`, schedule1, mspID, actorID)
	assertRejected("duplicate all-day exception in one schedule version", `
INSERT INTO technician_schedule_exceptions(
  id,schedule_version_id,msp_id,technician_id,exception_on,
  availability_state,all_day,capacity_percent,created_by,updated_by
) VALUES('00000000-0000-0000-0000-000000009315',$1,$2,$3,DATE '2026-10-15','unavailable',true,0,$3,$3)
`, schedule1, mspID, actorID)
	assertAccepted("same exception in another schedule version", `
INSERT INTO technician_schedule_exceptions(
  id,schedule_version_id,msp_id,technician_id,exception_on,
  availability_state,all_day,capacity_percent,created_by,updated_by
) VALUES('00000000-0000-0000-0000-000000009316',$1,$2,$3,DATE '2026-10-15','unavailable',true,0,$3,$3)
`, schedule2, mspID, actorID)

	assertRejected("maintenance without timed end", `
INSERT INTO maintenance_windows(
  id,msp_id,title,starts_at,timezone,all_day,owner_id,created_by,updated_by
) VALUES('00000000-0000-0000-0000-000000009317',$1,'No end',now(),'UTC',false,$2,$2,$2)
`, mspID, actorID)
	assertRejected("cross-client commercial service", `
INSERT INTO commercial_commitments(
  id,msp_id,client_id,commitment_type,title,vendor_name,effective_on,expiration_on,
  service_id,owner_id,created_by,updated_by
) VALUES('00000000-0000-0000-0000-000000009318',$1,$2,'license','License','Vendor',DATE '2026-01-01',DATE '2026-12-31',$3,$4,$4,$4)
`, mspID, clientAID, serviceB, actorID)
	assertRejected("date-only capacity custom field", `
INSERT INTO calendar_custom_date_fields(
  id,msp_id,object_type,internal_key,label,value_kind,event_role,category,
  color_category,calendar_mode,scheduling_mode,capacity_bearing,created_by,updated_by
) VALUES('00000000-0000-0000-0000-000000009319',$1,'task','capacity_date','Capacity date','date','capacity_date','Operations','blue','schedulable','fixed_block',true,$2,$2)
`, mspID, actorID)
}
