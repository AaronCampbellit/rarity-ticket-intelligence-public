package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

func TestCalendarNotificationRoutingMigrationContract(t *testing.T) {
	body := migrationBody(t, "000099_calendar_notification_routing.sql")
	for _, fragment := range []string{
		"ADD CONSTRAINT notification_deliveries_id_msp_unique UNIQUE (id,msp_id)",
		"ADD CONSTRAINT event_outbox_id_msp_unique UNIQUE (event_id,msp_id)",
		"ADD CONSTRAINT notification_delivery_event_msp_fk",
		"FOREIGN KEY (event_id,msp_id) REFERENCES event_outbox(event_id,msp_id)",
		"ADD COLUMN calendar_delivery_key text",
		"ADD CONSTRAINT notification_delivery_calendar_key_required",
		"recipient_ref <> 'calendar.assignee' OR calendar_delivery_key IS NOT NULL",
		"notification_delivery_calendar_key_uniq",
		"ON notification_deliveries (msp_id,calendar_delivery_key)",
		"CREATE TABLE notification_calendar_delivery_payloads",
		"delivery_id uuid PRIMARY KEY",
		"FOREIGN KEY (delivery_id,msp_id) REFERENCES notification_deliveries(id,msp_id) ON DELETE CASCADE",
		"FOREIGN KEY (recipient_technician_id,msp_id) REFERENCES technicians(id,msp_id)",
		"CHECK (jsonb_typeof(source_refs)='array')",
		"CHECK (action_path LIKE '/%')",
		"CREATE TABLE recipient_notifications",
		"delivery_id uuid NOT NULL UNIQUE",
		"FOREIGN KEY (delivery_id,msp_id) REFERENCES notification_deliveries(id,msp_id)",
		"FOREIGN KEY (event_id,msp_id) REFERENCES event_outbox(event_id,msp_id)",
		"UNIQUE (msp_id, recipient_technician_id, deduplication_key)",
		"recipient_notifications_pagination_idx",
		"recipient_notifications_unread_idx",
		"recipient_notifications_version_idx",
	} {
		if !strings.Contains(body, fragment) {
			t.Errorf("calendar notification routing migration missing %q", fragment)
		}
	}
}

func TestCalendarNotificationTypedDeliveryKeyMigrationContract(t *testing.T) {
	body := migrationBody(t, "000100_calendar_notification_typed_delivery_key.sql")
	for _, fragment := range []string{
		"ADD COLUMN calendar_delivery_key text",
		"ALTER COLUMN calendar_delivery_key SET NOT NULL",
		"notification_deliveries_id_msp_calendar_key_unique",
		"notification_calendar_payload_delivery_key_fk",
		"FOREIGN KEY (delivery_id,msp_id,calendar_delivery_key)",
		"REFERENCES notification_deliveries(id,msp_id,calendar_delivery_key)",
	} {
		if !strings.Contains(body, fragment) {
			t.Errorf("typed calendar delivery-key migration missing %q", fragment)
		}
	}
}

func TestCalendarNotificationRoutingSchemaEnforcesOwnershipCardinalityAndInboxDedupe(t *testing.T) {
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
	schema := fmt.Sprintf("calendar_notification_routing_%d", time.Now().UnixNano())
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE") })
	schemaURL := calendarRoutingDatabaseURL(t, databaseURL, schema)
	database, err := sql.Open("pgx", schemaURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	goose.SetBaseFS(FS)
	t.Cleanup(func() { goose.SetBaseFS(nil) })
	if err = goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err = goose.UpContext(ctx, database, "."); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := pgxpool.New(ctx, schemaURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	const (
		mspID           = "00000000-0000-4000-8000-000000009901"
		otherMSPID      = "00000000-0000-4000-8000-000000009902"
		techID          = "00000000-0000-4000-8000-000000009903"
		otherTech       = "00000000-0000-4000-8000-000000009904"
		policyID        = "00000000-0000-4000-8000-000000009905"
		eventID         = "00000000-0000-4000-8000-000000009906"
		deliveryID      = "00000000-0000-4000-8000-000000009907"
		otherPolicyID   = "00000000-0000-4000-8000-000000009911"
		otherEventID    = "00000000-0000-4000-8000-000000009912"
		otherDeliveryID = "00000000-0000-4000-8000-000000009913"
	)
	seeds := []string{
		`INSERT INTO msp_organizations(id,display_id,name,created_by,updated_by) VALUES ('` + mspID + `','routing-one','Routing one','` + techID + `','` + techID + `'),('` + otherMSPID + `','routing-two','Routing two','` + otherTech + `','` + otherTech + `')`,
		`INSERT INTO technicians(id,msp_id,email,display_name) VALUES ('` + techID + `','` + mspID + `','routing@example.test','Routing'),('` + otherTech + `','` + otherMSPID + `','other@example.test','Other')`,
		`INSERT INTO notification_policies(id,msp_id,key,name,event_type,channels) VALUES ('` + policyID + `','` + mspID + `','calendar','Calendar','calendar.schedule_changed','[{"channel":"in_app","recipient_ref":"calendar.assignee","content_classification":"internal"}]')`,
		`INSERT INTO notification_policy_versions(policy_id,msp_id,version,event_type,destinations,published_at,published_by) VALUES ('` + policyID + `','` + mspID + `',1,'calendar.schedule_changed','[{"channel":"in_app","recipient_ref":"calendar.assignee","content_classification":"internal"}]',now(),'` + techID + `')`,
		`INSERT INTO event_outbox(event_id,event_type,schema_version,occurred_at,msp_id,actor_type,actor_id,subject_type,subject_id,subject_version,correlation_id,source) VALUES ('` + eventID + `','calendar.schedule_changed',1,now(),'` + mspID + `','system','00000000-0000-0000-0000-000000000000','calendar_projection','` + eventID + `',1,'` + eventID + `','calendar')`,
		`INSERT INTO notification_deliveries(id,policy_id,policy_version,msp_id,event_id,channel,recipient_ref,content_classification,recipient_technician_id,calendar_delivery_key) VALUES ('` + deliveryID + `','` + policyID + `',1,'` + mspID + `','` + eventID + `','in_app','calendar.assignee','internal','` + techID + `','routing-in-app')`,
		`INSERT INTO notification_deliveries(id,policy_id,policy_version,msp_id,event_id,channel,recipient_ref,content_classification,recipient_technician_id,calendar_delivery_key) VALUES ('00000000-0000-4000-8000-000000009908','` + policyID + `',1,'` + mspID + `','` + eventID + `','email','calendar.assignee','internal','` + techID + `','routing-email')`,
		`INSERT INTO notification_policies(id,msp_id,key,name,event_type,channels) VALUES ('` + otherPolicyID + `','` + otherMSPID + `','calendar','Calendar','calendar.schedule_changed','[{"channel":"in_app","recipient_ref":"calendar.assignee","content_classification":"internal"}]')`,
		`INSERT INTO notification_policy_versions(policy_id,msp_id,version,event_type,destinations,published_at,published_by) VALUES ('` + otherPolicyID + `','` + otherMSPID + `',1,'calendar.schedule_changed','[{"channel":"in_app","recipient_ref":"calendar.assignee","content_classification":"internal"}]',now(),'` + otherTech + `')`,
		`INSERT INTO event_outbox(event_id,event_type,schema_version,occurred_at,msp_id,actor_type,actor_id,subject_type,subject_id,subject_version,correlation_id,source) VALUES ('` + otherEventID + `','calendar.schedule_changed',1,now(),'` + otherMSPID + `','system','00000000-0000-0000-0000-000000000000','calendar_projection','` + otherEventID + `',1,'` + otherEventID + `','calendar')`,
		`INSERT INTO notification_deliveries(id,policy_id,policy_version,msp_id,event_id,channel,recipient_ref,content_classification,recipient_technician_id,calendar_delivery_key) VALUES ('` + otherDeliveryID + `','` + otherPolicyID + `',1,'` + otherMSPID + `','` + otherEventID + `','in_app','calendar.assignee','internal','` + otherTech + `','routing-other')`,
	}
	for _, seed := range seeds {
		if _, err = pool.Exec(ctx, seed); err != nil {
			t.Fatalf("seed routing schema: %v", err)
		}
	}
	t.Run("delivery event belongs to organization", func(t *testing.T) {
		if _, crossErr := pool.Exec(ctx, `INSERT INTO notification_deliveries(id,policy_id,policy_version,msp_id,event_id,channel,recipient_ref,content_classification,calendar_delivery_key) VALUES('00000000-0000-4000-8000-000000009916',$1,1,$2,$3,'in_app','calendar.assignee','internal','cross-msp-event-fk')`, policyID, mspID, otherEventID); crossErr == nil {
			t.Fatal("delivery accepted an event from another organization")
		}
	})
	t.Run("calendar delivery requires logical key", func(t *testing.T) {
		if _, keyErr := pool.Exec(ctx, `INSERT INTO notification_deliveries(id,policy_id,policy_version,msp_id,event_id,channel,recipient_ref,content_classification) VALUES('00000000-0000-4000-8000-000000009917',$1,1,$2,$3,'in_app','calendar.assignee','internal')`, policyID, mspID, eventID); keyErr == nil {
			t.Fatal("calendar delivery accepted a missing logical key")
		}
	})
	t.Run("typed payload requires parent logical key independently of recipient label", func(t *testing.T) {
		const keylessDeliveryID = "00000000-0000-4000-8000-000000009918"
		if _, insertErr := pool.Exec(ctx, `INSERT INTO notification_deliveries(id,policy_id,policy_version,msp_id,event_id,channel,recipient_ref,content_classification) VALUES($1,$2,1,$3,$4,'in_app','legacy.calendar','internal')`, keylessDeliveryID, policyID, mspID, eventID); insertErr != nil {
			t.Fatalf("insert keyless non-calendar parent: %v", insertErr)
		}
		if _, payloadErr := pool.Exec(ctx, `INSERT INTO notification_calendar_delivery_payloads(delivery_id,msp_id,recipient_technician_id,correlation_id,change_class,urgency,source_refs,action_path) VALUES($1,$2,$3,$4,'schedule','routine','[]','/calendar')`, keylessDeliveryID, mspID, techID, eventID); payloadErr == nil {
			t.Fatal("typed calendar payload accepted a parent without a logical key")
		}
	})
	if _, err = pool.Exec(ctx, `INSERT INTO notification_calendar_delivery_payloads(delivery_id,msp_id,recipient_technician_id,correlation_id,change_class,urgency,source_refs,action_path,calendar_delivery_key) VALUES($1,$2,$3,$4,'schedule','routine','[]','/calendar','routing-in-app')`, deliveryID, mspID, techID, eventID); err != nil {
		t.Fatalf("insert calendar payload: %v", err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO notification_calendar_delivery_payloads(delivery_id,msp_id,recipient_technician_id,correlation_id,change_class,urgency,source_refs,action_path,calendar_delivery_key) VALUES($1,$2,$3,$4,'schedule','routine','[]','/calendar','routing-in-app')`, deliveryID, mspID, techID, eventID); err == nil {
		t.Fatal("delivery accepted a second calendar payload")
	}
	if _, err = pool.Exec(ctx, `INSERT INTO notification_calendar_delivery_payloads(delivery_id,msp_id,recipient_technician_id,correlation_id,change_class,urgency,source_refs,action_path,calendar_delivery_key) VALUES('00000000-0000-4000-8000-000000009908',$1,$2,$3,'schedule','routine','[]','/calendar','routing-email')`, mspID, otherTech, eventID); err == nil {
		t.Fatal("payload accepted a technician from another organization")
	}
	t.Run("payload delivery belongs to organization", func(t *testing.T) {
		if _, crossErr := pool.Exec(ctx, `INSERT INTO notification_calendar_delivery_payloads(delivery_id,msp_id,recipient_technician_id,correlation_id,change_class,urgency,source_refs,action_path,calendar_delivery_key) VALUES($1,$2,$3,$4,'schedule','routine','[]','/calendar','routing-other')`, otherDeliveryID, mspID, techID, eventID); crossErr == nil {
			t.Fatal("payload accepted a delivery from another organization")
		}
	})
	insertInbox := `INSERT INTO recipient_notifications(id,delivery_id,msp_id,recipient_technician_id,event_id,deduplication_key,title,body,action_path,content_classification,created_at) VALUES($1,$2,$3,$4,$5,'calendar-dedupe','Calendar changed','One item changed','/calendar','internal',now())`
	if _, err = pool.Exec(ctx, insertInbox, "00000000-0000-4000-8000-000000009909", deliveryID, mspID, techID, eventID); err != nil {
		t.Fatalf("insert inbox item: %v", err)
	}
	if _, err = pool.Exec(ctx, insertInbox, "00000000-0000-4000-8000-000000009910", deliveryID, mspID, techID, eventID); err == nil {
		t.Fatal("duplicate recipient inbox item was accepted")
	}
	t.Run("inbox delivery belongs to organization", func(t *testing.T) {
		if _, crossErr := pool.Exec(ctx, `INSERT INTO recipient_notifications(id,delivery_id,msp_id,recipient_technician_id,event_id,deduplication_key,title,body,action_path,content_classification,created_at) VALUES('00000000-0000-4000-8000-000000009915',$1,$2,$3,$4,'cross-delivery','Calendar changed','One item changed','/calendar','internal',now())`, otherDeliveryID, mspID, techID, eventID); crossErr == nil {
			t.Fatal("inbox accepted a delivery from another organization")
		}
	})
	t.Run("inbox event belongs to organization", func(t *testing.T) {
		if _, crossErr := pool.Exec(ctx, `INSERT INTO recipient_notifications(id,delivery_id,msp_id,recipient_technician_id,event_id,deduplication_key,title,body,action_path,content_classification,created_at) VALUES('00000000-0000-4000-8000-000000009914','00000000-0000-4000-8000-000000009908',$1,$2,$3,'cross-event','Calendar changed','One item changed','/calendar','internal',now())`, mspID, techID, otherEventID); crossErr == nil {
			t.Fatal("inbox accepted an event from another organization")
		}
	})
	var indexes int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM pg_indexes WHERE schemaname=current_schema() AND indexname IN ('recipient_notifications_pagination_idx','recipient_notifications_unread_idx','recipient_notifications_version_idx')`).Scan(&indexes); err != nil || indexes != 3 {
		t.Fatalf("recipient notification indexes=%d err=%v", indexes, err)
	}
	if err = goose.DownToContext(ctx, database, ".", 98); err != nil {
		t.Fatalf("migrate down from calendar notification routing: %v", err)
	}
	var remaining int
	if err = pool.QueryRow(ctx, `SELECT
	  (SELECT count(*) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='notification_deliveries' AND column_name='calendar_delivery_key') +
	  (SELECT count(*) FROM pg_constraint WHERE connamespace=current_schema()::regnamespace AND conname='notification_delivery_event_msp_fk') +
	  (SELECT count(*) FROM pg_indexes WHERE schemaname=current_schema() AND indexname='notification_delivery_calendar_key_uniq')`).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("calendar routing Down left durable-key schema objects=%d err=%v", remaining, err)
	}
}

func TestCalendarNotificationTypedDeliveryKeyUpgradeTerminalizesMalformedRows(t *testing.T) {
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
	schema := fmt.Sprintf("calendar_notification_key_upgrade_%d", time.Now().UnixNano())
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE") })
	schemaURL := calendarRoutingDatabaseURL(t, databaseURL, schema)
	database, err := sql.Open("pgx", schemaURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	goose.SetBaseFS(FS)
	t.Cleanup(func() { goose.SetBaseFS(nil) })
	if err = goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err = goose.UpToContext(ctx, database, ".", 99); err != nil {
		t.Fatalf("migrate to 99: %v", err)
	}
	pool, err := pgxpool.New(ctx, schemaURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	const (
		mspID            = "00000000-0000-4000-8000-000000010001"
		techID           = "00000000-0000-4000-8000-000000010002"
		policyID         = "00000000-0000-4000-8000-000000010003"
		eventID          = "00000000-0000-4000-8000-000000010004"
		validDeliveryID  = "00000000-0000-4000-8000-000000010005"
		brokenDeliveryID = "00000000-0000-4000-8000-000000010006"
		deliveredID      = "00000000-0000-4000-8000-000000010007"
		suppressedID     = "00000000-0000-4000-8000-000000010008"
		validKey         = "calendar-upgrade-valid-key"
	)
	seeds := []string{
		`INSERT INTO msp_organizations(id,display_id,name,created_by,updated_by) VALUES ('` + mspID + `','key-upgrade','Key upgrade','` + techID + `','` + techID + `')`,
		`INSERT INTO technicians(id,msp_id,email,display_name) VALUES ('` + techID + `','` + mspID + `','key-upgrade@example.test','Key upgrade')`,
		`INSERT INTO notification_policies(id,msp_id,key,name,event_type,channels) VALUES ('` + policyID + `','` + mspID + `','calendar-key-upgrade','Calendar','calendar.schedule_changed','[{"channel":"in_app","recipient_ref":"calendar.assignee","content_classification":"internal"}]')`,
		`INSERT INTO notification_policy_versions(policy_id,msp_id,version,event_type,destinations,published_at,published_by) VALUES ('` + policyID + `','` + mspID + `',1,'calendar.schedule_changed','[{"channel":"in_app","recipient_ref":"calendar.assignee","content_classification":"internal"}]',now(),'` + techID + `')`,
		`INSERT INTO event_outbox(event_id,event_type,schema_version,occurred_at,msp_id,actor_type,actor_id,subject_type,subject_id,subject_version,correlation_id,source) VALUES ('` + eventID + `','calendar.schedule_changed',1,now(),'` + mspID + `','system','00000000-0000-0000-0000-000000000000','calendar_projection','` + eventID + `',1,'` + eventID + `','calendar')`,
		`INSERT INTO notification_deliveries(id,policy_id,policy_version,msp_id,event_id,channel,recipient_ref,content_classification,calendar_delivery_key) VALUES ('` + validDeliveryID + `','` + policyID + `',1,'` + mspID + `','` + eventID + `','in_app','calendar.assignee','internal','` + validKey + `')`,
		`INSERT INTO notification_deliveries(id,policy_id,policy_version,msp_id,event_id,channel,recipient_ref,content_classification) VALUES ('` + brokenDeliveryID + `','` + policyID + `',1,'` + mspID + `','` + eventID + `','in_app','legacy.pending','internal')`,
		`INSERT INTO notification_deliveries(id,policy_id,policy_version,msp_id,event_id,channel,recipient_ref,content_classification,state,delivered_at,failure_code) VALUES ('` + deliveredID + `','` + policyID + `',1,'` + mspID + `','` + eventID + `','in_app','legacy.delivered','internal','delivered','2026-08-15 10:00:00Z','delivered-audit')`,
		`INSERT INTO notification_deliveries(id,policy_id,policy_version,msp_id,event_id,channel,recipient_ref,content_classification,state,suppressed_at,suppression_reason,failure_code) VALUES ('` + suppressedID + `','` + policyID + `',1,'` + mspID + `','` + eventID + `','in_app','legacy.suppressed','internal','suppressed','2026-08-15 11:00:00Z','preexisting-suppression','suppressed-audit')`,
		`INSERT INTO notification_calendar_delivery_payloads(delivery_id,msp_id,recipient_technician_id,correlation_id,change_class,urgency,source_refs,action_path) VALUES ('` + validDeliveryID + `','` + mspID + `','` + techID + `','` + eventID + `','schedule','routine','[]','/calendar'),('` + brokenDeliveryID + `','` + mspID + `','` + techID + `','` + eventID + `','schedule','routine','[]','/calendar'),('` + deliveredID + `','` + mspID + `','` + techID + `','` + eventID + `','schedule','routine','[]','/calendar'),('` + suppressedID + `','` + mspID + `','` + techID + `','` + eventID + `','schedule','routine','[]','/calendar')`,
	}
	for _, seed := range seeds {
		if _, err = pool.Exec(ctx, seed); err != nil {
			t.Fatalf("seed migration 99 state: %v", err)
		}
	}

	if err = goose.UpToContext(ctx, database, ".", 100); err != nil {
		t.Fatalf("migrate malformed typed delivery to 100: %v", err)
	}
	var validPayloadKey, brokenState, brokenCode string
	var brokenPayloads, terminalPayloads int
	var terminalHistoryPreserved bool
	if err = pool.QueryRow(ctx, `SELECT
  (SELECT calendar_delivery_key FROM notification_calendar_delivery_payloads WHERE delivery_id=$1),
  (SELECT state FROM notification_deliveries WHERE id=$2),
  (SELECT failure_code FROM notification_deliveries WHERE id=$2),
	  (SELECT count(*) FROM notification_calendar_delivery_payloads WHERE delivery_id=$2),
	  (SELECT state='delivered' AND delivered_at='2026-08-15 10:00:00Z'::timestamptz AND failure_code='delivered-audit' FROM notification_deliveries WHERE id=$3)
	  AND (SELECT state='suppressed' AND suppressed_at='2026-08-15 11:00:00Z'::timestamptz AND suppression_reason='preexisting-suppression' AND failure_code='suppressed-audit' FROM notification_deliveries WHERE id=$4),
	  (SELECT count(*) FROM notification_calendar_delivery_payloads WHERE delivery_id IN ($3,$4))`, validDeliveryID, brokenDeliveryID, deliveredID, suppressedID).Scan(&validPayloadKey, &brokenState, &brokenCode, &brokenPayloads, &terminalHistoryPreserved, &terminalPayloads); err != nil {
		t.Fatal(err)
	}
	if validPayloadKey != validKey || brokenState != "failed" || brokenCode != "invalid_calendar_delivery_key" || brokenPayloads != 0 || !terminalHistoryPreserved || terminalPayloads != 0 {
		t.Fatalf("valid key=%q broken state=%q code=%q payloads=%d terminal_history=%t terminal_payloads=%d", validPayloadKey, brokenState, brokenCode, brokenPayloads, terminalHistoryPreserved, terminalPayloads)
	}
}

func calendarRoutingDatabaseURL(t *testing.T, databaseURL, schema string) string {
	t.Helper()
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}
