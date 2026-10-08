package organizations

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/store"
)

func TestPostgresRepositoryRollsBackAllRecordsWhenOutboxInsertFails(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for PostgreSQL transaction verification")
	}
	ctx := context.Background()
	if err := store.Migrate(ctx, databaseURL); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(pool.Close)

	suffix := time.Now().UTC().Format("150405.000000000")
	mspID := "019f9c90-5f71-7000-8000-000000000001"
	actorID := "019f9c90-5f71-7000-8000-000000000002"
	clientID := "019f9c90-5f71-7000-8000-000000000003"
	auditID := "019f9c90-5f71-7000-8000-000000000004"
	eventID := "019f9c90-5f71-7000-8000-000000000005"
	correlationID := "019f9c90-5f71-7000-8000-000000000006"
	_, _ = pool.Exec(ctx, "DELETE FROM msp_organizations WHERE id = $1", mspID)
	_, err = pool.Exec(ctx, `
		INSERT INTO msp_organizations (
			id, display_id, name, created_by, updated_by
		) VALUES ($1, $2, 'Test MSP', $3, $3)`,
		mspID, "TEST-"+suffix, actorID,
	)
	if err != nil {
		t.Fatalf("seed MSP: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM msp_organizations WHERE id = $1", mspID)
	})

	now := time.Now().UTC()
	repository := NewPostgresRepository(pool)
	err = repository.CreateClientAtomic(ctx, CreateClientMutation{
		Client: Client{
			Envelope: object.Envelope{
				ID: clientID, ObjectType: "client_organization",
				MSPID: mspID, ClientID: clientID, DisplayID: "CLIENT-" + suffix,
				LifecycleState: "active", Version: 1,
				CreatedAt: now, CreatedBy: actorID, UpdatedAt: now, UpdatedBy: actorID,
			},
			Name: "Atomic Rollback",
		},
		Audit: mutation.AuditRecord{
			ID: auditID, OccurredAt: now, MSPID: mspID, ClientID: clientID,
			ActorType: "technician", ActorID: actorID, Action: "client.created",
			SubjectType: "client_organization", SubjectID: clientID,
			SubjectVersion: 1, Source: "test", CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: eventID, EventType: "client.created", SchemaVersion: 1,
			OccurredAt: now, MSPID: mspID, ClientID: clientID,
			ActorType: "technician", ActorID: actorID,
			SubjectType: "client_organization", SubjectID: clientID,
			SubjectVersion: 0, Source: "test", CorrelationID: correlationID,
		},
	})
	if err == nil {
		t.Fatal("CreateClientAtomic() succeeded with invalid outbox subject version")
	}

	for table, idColumn := range map[string]string{
		"client_organizations": "id",
		"audit_ledger":         "id",
		"event_outbox":         "event_id",
	} {
		var count int
		id := clientID
		if table == "audit_ledger" {
			id = auditID
		} else if table == "event_outbox" {
			id = eventID
		}
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE "+idColumn+" = $1", id).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("%s retained %d record(s) after rollback", table, count)
		}
	}
}
