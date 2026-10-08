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

func TestMentionDeliveryDedupeUpgradeAndDownPreserveLegacySemantics(t *testing.T) {
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
	schema := fmt.Sprintf("mention_delivery_dedupe_%d", time.Now().UnixNano())
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
		t.Fatalf("migrate to 89: %v", err)
	}
	pool, err := pgxpool.New(ctx, schemaURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	seedMentionDeliveryDedupe(t, ctx, pool)
	if err = goose.UpByOneContext(ctx, database, "."); err != nil {
		t.Fatalf("upgrade to 90: %v", err)
	}

	insertDelivery := func(id, policy, recipient, recipientRef string) error {
		_, err := pool.Exec(ctx, `INSERT INTO notification_deliveries(id,policy_id,policy_version,msp_id,client_id,event_id,channel,recipient_ref,content_classification,recipient_technician_id) VALUES($1,$2,1,$3,$4,$5,'email',$7,'internal',NULLIF($6,'')::uuid)`, id, policy, dedupeMSPID, dedupeClientID, dedupeEventID, recipient, recipientRef)
		return err
	}
	if err = insertDelivery("00000000-0000-0000-0000-000000009016", dedupePolicyID, dedupeTech2ID, "recipient"); err != nil {
		t.Fatalf("distinct mention recipient rejected: %v", err)
	}
	if err = insertDelivery("00000000-0000-0000-0000-000000009017", dedupePolicyID, dedupeTech2ID, "changed-policy-destination"); err == nil {
		t.Fatal("duplicate mention recipient was accepted after recipient_ref changed")
	}
	if err = insertDelivery("00000000-0000-0000-0000-000000009022", dedupePolicy2ID, dedupeTech2ID, "second-policy-destination"); err == nil {
		t.Fatal("duplicate mention recipient was accepted after policy_id changed")
	}
	if err = insertDelivery("00000000-0000-0000-0000-000000009018", dedupePolicyID, "", "recipient"); err != nil {
		t.Fatalf("first legacy delivery rejected: %v", err)
	}
	if err = insertDelivery("00000000-0000-0000-0000-000000009019", dedupePolicyID, "", "recipient"); err == nil {
		t.Fatal("duplicate legacy delivery was accepted")
	}
	if err = goose.DownContext(ctx, database, "."); err != nil {
		t.Fatalf("down migration: %v", err)
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM notification_deliveries WHERE policy_id=$1 AND event_id=$2 AND channel='email' AND recipient_ref='recipient'`, dedupePolicyID, dedupeEventID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("legacy collapse count=%d err=%v", count, err)
	}
	if err = insertDelivery("00000000-0000-0000-0000-000000009020", dedupePolicyID, dedupeTech2ID, "recipient"); err == nil {
		t.Fatal("down migration did not restore original cross-recipient uniqueness")
	}
	if err = goose.UpByOneContext(ctx, database, "."); err != nil {
		t.Fatalf("reapply migration 90: %v", err)
	}
	if err = insertDelivery("00000000-0000-0000-0000-000000009021", dedupePolicyID, dedupeTech2ID, "recipient"); err != nil {
		t.Fatalf("reapplied migration rejected distinct mention recipient: %v", err)
	}
}

const (
	dedupeMSPID     = "00000000-0000-0000-0000-000000009001"
	dedupeClientID  = "00000000-0000-0000-0000-000000009002"
	dedupeTech1ID   = "00000000-0000-0000-0000-000000009003"
	dedupeTech2ID   = "00000000-0000-0000-0000-000000009004"
	dedupePolicyID  = "00000000-0000-0000-0000-000000009005"
	dedupePolicy2ID = "00000000-0000-0000-0000-000000009009"
	dedupeEventID   = "00000000-0000-0000-0000-000000009006"
)

func seedMentionDeliveryDedupe(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	actor := dedupeTech1ID
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO msp_organizations(id,display_id,name,created_by,updated_by) VALUES($1,'mention-dedupe','Mention dedupe',$2,$2)`, []any{dedupeMSPID, actor}},
		{`INSERT INTO client_organizations(id,msp_id,display_id,name,created_by,updated_by) VALUES($1,$2,'client','Client',$3,$3)`, []any{dedupeClientID, dedupeMSPID, actor}},
		{`INSERT INTO technicians(id,msp_id,email,display_name) VALUES($1,$3,'one@example.test','One'),($2,$3,'two@example.test','Two')`, []any{dedupeTech1ID, dedupeTech2ID, dedupeMSPID}},
		{`INSERT INTO notification_policies(id,msp_id,client_id,key,name,event_type,channels) VALUES($1,$2,$3,'mentions','Mentions','mention.occurred','[{"channel":"email","recipient_ref":"recipient","content_classification":"internal"}]')`, []any{dedupePolicyID, dedupeMSPID, dedupeClientID}},
		{`INSERT INTO notification_policies(id,msp_id,client_id,key,name,event_type,channels) VALUES($1,$2,$3,'mentions-secondary','Mentions secondary','mention.occurred','[{"channel":"email","recipient_ref":"second-policy-destination","content_classification":"internal"}]')`, []any{dedupePolicy2ID, dedupeMSPID, dedupeClientID}},
		{`INSERT INTO notification_policy_versions(policy_id,msp_id,version,event_type,destinations,published_at,published_by) VALUES($1,$2,1,'mention.occurred','[{"channel":"email","recipient_ref":"recipient","content_classification":"internal"}]',now(),$3)`, []any{dedupePolicyID, dedupeMSPID, actor}},
		{`INSERT INTO notification_policy_versions(policy_id,msp_id,version,event_type,destinations,published_at,published_by) VALUES($1,$2,1,'mention.occurred','[{"channel":"email","recipient_ref":"second-policy-destination","content_classification":"internal"}]',now(),$3)`, []any{dedupePolicy2ID, dedupeMSPID, actor}},
		{`INSERT INTO event_outbox(event_id,event_type,schema_version,occurred_at,msp_id,client_id,actor_type,actor_id,subject_type,subject_id,subject_version,correlation_id,source) VALUES($1,'mention.occurred',1,now(),$2,$3,'technician',$4,'internal_collaboration_source',$5,1,$6,'test')`, []any{dedupeEventID, dedupeMSPID, dedupeClientID, actor, "00000000-0000-0000-0000-000000009007", "00000000-0000-0000-0000-000000009008"}},
		{`INSERT INTO notification_deliveries(id,policy_id,policy_version,msp_id,client_id,event_id,channel,recipient_ref,content_classification,recipient_technician_id) VALUES('00000000-0000-0000-0000-000000009015',$1,1,$2,$3,$4,'email','recipient','internal',$5)`, []any{dedupePolicyID, dedupeMSPID, dedupeClientID, dedupeEventID, dedupeTech1ID}},
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed dedupe upgrade: %v", err)
		}
	}
}
