package psa

import (
	"context"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/clientresources"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
)

func TestCreateServiceResourceWritesCriticalityAndFactsAtomically(t *testing.T) {
	tx := &fakeSalesTx{}
	repository := NewServiceResourceRepository(&fakeSalesDB{tx: tx})
	at := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	envelope := object.Envelope{
		ID: "service", MSPID: "msp", ClientID: "client",
		DisplayID: "SERVICE-1", LifecycleState: "active", Version: 1,
		CreatedAt: at, CreatedBy: "actor", UpdatedAt: at, UpdatedBy: "actor",
	}
	err := repository.CreateAtomic(context.Background(), clientresources.CreateMutation{
		Kind: "service", Object: envelope,
		Payload: clientresources.ServiceRecord{
			Envelope: envelope, Name: "Email", Criticality: "high",
		},
		Audit: mutation.AuditRecord{
			ID: "audit", OccurredAt: at, MSPID: "msp", ClientID: "client",
			ActorType: "technician", ActorID: "actor", Action: "service.created",
			SubjectType: "service", SubjectID: "service", SubjectVersion: 1,
			Source: "api", CorrelationID: "correlation",
		},
		Event: mutation.EventRecord{
			EventID: "event", EventType: "service.created", SchemaVersion: 1,
			OccurredAt: at, MSPID: "msp", ClientID: "client",
			ActorType: "technician", ActorID: "actor",
			SubjectType: "service", SubjectID: "service", SubjectVersion: 1,
			Source: "api", CorrelationID: "correlation",
		},
	})
	if err != nil {
		t.Fatalf("CreateAtomic() error = %v", err)
	}
	assertQueryOrder(
		t, tx.queries,
		"FROM client_organizations", "INSERT INTO services", "INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
}
