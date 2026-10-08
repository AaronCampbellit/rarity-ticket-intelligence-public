package migrations_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/store"
	"github.com/rarity-ticket-intelligence/rarity/backend/migrations"
)

func TestInternalMentionMigrationSequenceIndexUpDownAgainstPostgres(t *testing.T) {
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
	schema := fmt.Sprintf("mention_sequence_roundtrip_%d", time.Now().UnixNano())
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
	if err = goose.UpToContext(ctx, database, ".", 89); err != nil {
		t.Fatalf("migrate through internal mentions: %v", err)
	}
	pool, err := pgxpool.New(ctx, schemaURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	assertGlobalSequenceIndex := func() {
		t.Helper()
		var global bool
		if queryErr := pool.QueryRow(ctx, `SELECT index_row.indpred IS NULL FROM pg_index index_row WHERE index_row.indexrelid='event_outbox_mention_invalidation_idx'::regclass`).Scan(&global); queryErr != nil || !global {
			t.Fatalf("mention invalidation sequence index global=%v err=%v", global, queryErr)
		}
	}
	assertGlobalSequenceIndex()
	if err = goose.DownContext(ctx, database, "."); err != nil {
		t.Fatalf("down internal mentions migration: %v", err)
	}
	var removed bool
	if err = pool.QueryRow(ctx, `SELECT to_regclass('event_outbox_mention_invalidation_idx') IS NULL AND NOT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='event_outbox' AND column_name='mention_invalidation_sequence')`).Scan(&removed); err != nil || !removed {
		t.Fatalf("internal mention down left sequence intake artifacts: removed=%v err=%v", removed, err)
	}
	if err = goose.UpByOneContext(ctx, database, "."); err != nil {
		t.Fatalf("reapply internal mentions migration: %v", err)
	}
	assertGlobalSequenceIndex()
}

func TestInternalMentionDeliveryRequiresExistingClientScopedOccurrence(t *testing.T) {
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
	schema := fmt.Sprintf("mention_delivery_scope_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create isolated schema: %v", err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE") })

	schemaURL := databaseURLWithSearchPath(t, databaseURL, schema)
	if err := store.Migrate(ctx, schemaURL); err != nil {
		t.Fatalf("migrate isolated schema: %v", err)
	}
	pool, err := pgxpool.New(ctx, schemaURL)
	if err != nil {
		t.Fatalf("open isolated schema pool: %v", err)
	}
	t.Cleanup(pool.Close)

	const (
		mspID               = "00000000-0000-0000-0000-000000000901"
		clientID            = "00000000-0000-0000-0000-000000000902"
		authorID            = "00000000-0000-0000-0000-000000000903"
		recipientID         = "00000000-0000-0000-0000-000000000904"
		policyID            = "00000000-0000-0000-0000-000000000905"
		sourceID            = "00000000-0000-0000-0000-000000000906"
		parentID            = "00000000-0000-0000-0000-000000000907"
		occurrenceID        = "00000000-0000-0000-0000-000000000908"
		missingOccurrenceID = "00000000-0000-0000-0000-000000000909"
		invalidEventID      = "00000000-0000-0000-0000-00000000090a"
		legacyEventID       = "00000000-0000-0000-0000-00000000090b"
		validEventID        = "00000000-0000-0000-0000-00000000090c"
		resolutionID        = "00000000-0000-0000-0000-000000000910"
		itemID              = "00000000-0000-0000-0000-000000000911"
		invalidationID      = "00000000-0000-0000-0000-000000000912"
		leaseToken          = "00000000-0000-0000-0000-000000000913"
		tokenID             = "00000000-0000-0000-0000-000000000915"
	)
	execSQL(t, ctx, pool, `
INSERT INTO msp_organizations(id,display_id,name,created_by,updated_by)
VALUES($1,'mention-delivery','Mention delivery',$3,$3);
INSERT INTO client_organizations(id,msp_id,display_id,name,created_by,updated_by)
VALUES($2,$1,'client','Client',$3,$3);
INSERT INTO technicians(id,msp_id,email,display_name)
VALUES($3,$1,'author@example.test','Author'),($4,$1,'recipient@example.test','Recipient');
INSERT INTO notification_policies(id,msp_id,key,name,event_type,channels)
VALUES($5,$1,'mentions','Mentions','mention.created','[]'::jsonb);
INSERT INTO notification_policy_versions(
  policy_id,msp_id,version,event_type,conditions,destinations,published_at,published_by
) VALUES($5,$1,1,'mention.created','{}'::jsonb,'[]'::jsonb,now(),$3);
INSERT INTO internal_collaboration_sources(
  id,msp_id,client_id,parent_type,parent_id,source_kind,body,author_id,created_at,updated_at
) VALUES($6,$1,$2,'work_record',$7,'comment','Internal mention',$3,now(),now());
INSERT INTO mention_occurrences(
  id,msp_id,client_id,source_id,source_revision,parent_type,parent_id,token_id,
  author_id,target_type,target_id,mentioned_at,correlation_id
) VALUES($8,$1,$2,$6,1,'work_record',$7,$12,$3,'technician',$4,now(),$8);
INSERT INTO event_outbox(
  event_id,event_type,schema_version,occurred_at,msp_id,client_id,actor_type,
  actor_id,subject_type,subject_id,subject_version,correlation_id,source
) VALUES
  ($9,'mention.created',1,now(),$1,NULL,'technician',$3,'work_record',$7,1,$9,'test'),
  ($10,'legacy.event',1,now(),$1,NULL,'technician',$3,'work_record',$7,1,$10,'test'),
  ($11,'mention.created',1,now(),$1,$2,'technician',$3,'work_record',$7,1,$11,'test');
`, mspID, clientID, authorID, recipientID, policyID, sourceID, parentID, occurrenceID, invalidEventID, legacyEventID, validEventID, tokenID)

	if _, err := pool.Exec(ctx, `
INSERT INTO notification_deliveries(
  id,policy_id,policy_version,msp_id,event_id,channel,recipient_ref,
  content_classification,mention_occurrence_id,recipient_technician_id
) VALUES('00000000-0000-0000-0000-00000000090d',$1,1,$2,$3,'in_app','recipient','internal',$4,$5)
	`, policyID, mspID, invalidEventID, missingOccurrenceID, recipientID); err == nil {
		t.Fatal("notification delivery accepted a mention occurrence without Client scope or matching occurrence")
	} else {
		var postgresError *pgconn.PgError
		if !errors.As(err, &postgresError) || postgresError.ConstraintName != "notification_delivery_mention_client_scope_check" {
			t.Fatalf("invalid mention delivery error=%v, want mention Client scope constraint", err)
		}
	}

	if _, err := pool.Exec(ctx, `
INSERT INTO notification_deliveries(
  id,policy_id,policy_version,msp_id,event_id,channel,recipient_ref,content_classification
) VALUES('00000000-0000-0000-0000-00000000090e',$1,1,$2,$3,'in_app','legacy','internal')
`, policyID, mspID, legacyEventID); err != nil {
		t.Fatalf("insert legacy non-mention delivery without Client scope: %v", err)
	}

	if _, err := pool.Exec(ctx, `
INSERT INTO notification_deliveries(
  id,policy_id,policy_version,msp_id,client_id,event_id,channel,recipient_ref,
  content_classification,mention_occurrence_id,recipient_technician_id
) VALUES('00000000-0000-0000-0000-00000000090f',$1,1,$2,$3,$4,'in_app','recipient','internal',$5,$6)
`, policyID, mspID, clientID, validEventID, occurrenceID, recipientID); err != nil {
		t.Fatalf("insert valid occurrence-linked delivery: %v", err)
	}

	execSQL(t, ctx, pool, `
INSERT INTO mention_recipient_resolutions(
  id,msp_id,client_id,occurrence_id,recipient_id,decision,resolution_path,
  contributing_team_ids,decided_at,reason_code
) VALUES($1,$2,$3,$4,$5,'eligible','direct','{}',now(),'eligible');
INSERT INTO mention_items(
  id,msp_id,client_id,recipient_id,parent_type,parent_id,
  latest_occurrence_id,state,last_mentioned_at
) VALUES($6,$2,$3,$5,'work_record',$7,$4,'unread',now());
INSERT INTO mention_access_invalidations(
  id,msp_id,client_id,mention_item_id,recipient_id,parent_type,parent_id,
  snapshot_occurrence_id,
  invalidated_at,reason_code,correlation_id,causation_id,
  lease_token,lease_until
) VALUES($8,$2,$3,$6,$5,'work_record',$7,$4,now(),'role.unassigned',$8,$9,$10,now()+interval '5 minutes');
UPDATE mention_access_invalidations
SET completed_at=now(),lease_token=NULL,lease_until=NULL
WHERE id=$8 AND lease_token=$10;
`, resolutionID, mspID, clientID, occurrenceID, recipientID, itemID, parentID, invalidationID, validEventID, leaseToken)
	var completed bool
	if err := pool.QueryRow(ctx, `SELECT completed_at IS NOT NULL AND lease_token IS NULL AND lease_until IS NULL FROM mention_access_invalidations WHERE id=$1`, invalidationID).Scan(&completed); err != nil || !completed {
		t.Fatalf("leased invalidation did not complete: completed=%v err=%v", completed, err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO mention_access_invalidations(id,msp_id,client_id,mention_item_id,recipient_id,parent_type,parent_id,snapshot_occurrence_id,invalidated_at,reason_code,correlation_id,causation_id) VALUES('00000000-0000-0000-0000-000000000914',$1,$2,$3,$4,'work_record',$5,$6,now(),'replay',$7,$8)`, mspID, clientID, itemID, recipientID, parentID, occurrenceID, invalidationID, validEventID); err == nil {
		t.Fatal("duplicate event/item invalidation replay was accepted")
	}
}
