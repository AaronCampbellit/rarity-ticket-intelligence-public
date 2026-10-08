package psa

import (
	"context"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/clientresources"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
)

func TestCreateContractWritesEffectiveResourceAndFactsAtomically(t *testing.T) {
	tx := &fakeSalesTx{}
	repository := NewContractRepository(&fakeSalesDB{tx: tx})
	at := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	startsOn := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	endsOn := time.Date(2027, time.July, 31, 0, 0, 0, 0, time.UTC)
	envelope := object.Envelope{
		ID: "contract", MSPID: "msp", ClientID: "client",
		DisplayID: "CONTRACT-1", LifecycleState: "active", Version: 1,
		CreatedAt: at, CreatedBy: "actor", UpdatedAt: at, UpdatedBy: "actor",
	}
	err := repository.CreateAtomic(context.Background(), clientresources.CreateMutation{
		Kind: "contract", Object: envelope,
		Payload: clientresources.Contract{
			Envelope: envelope, Name: "Managed Services",
			StartsOn: startsOn, EndsOn: &endsOn,
		},
		Audit: mutation.AuditRecord{
			ID: "audit", OccurredAt: at, MSPID: "msp", ClientID: "client",
			ActorType: "technician", ActorID: "actor", Action: "contract.created",
			SubjectType: "contract", SubjectID: "contract", SubjectVersion: 1,
			Source: "api", CorrelationID: "correlation",
		},
		Event: mutation.EventRecord{
			EventID: "event", EventType: "contract.created", SchemaVersion: 1,
			OccurredAt: at, MSPID: "msp", ClientID: "client",
			ActorType: "technician", ActorID: "actor",
			SubjectType: "contract", SubjectID: "contract", SubjectVersion: 1,
			Source: "api", CorrelationID: "correlation",
		},
	})
	if err != nil {
		t.Fatalf("CreateAtomic() error = %v", err)
	}
	assertQueryOrder(
		t, tx.queries,
		"FROM client_organizations", "INSERT INTO contracts", "INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
}
