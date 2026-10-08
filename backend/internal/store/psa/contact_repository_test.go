package psa

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/clientresources"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

func TestCreateContactValidatesLocationAndWritesFactsAtomically(t *testing.T) {
	tx := &fakeSalesTx{}
	repository := NewContactRepository(&fakeSalesDB{tx: tx})
	at := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	envelope := object.Envelope{
		ID: "contact", MSPID: "msp", ClientID: "client",
		DisplayID: "CONTACT-1", LifecycleState: "active", Version: 1,
		CreatedAt: at, CreatedBy: "actor", UpdatedAt: at, UpdatedBy: "actor",
	}
	err := repository.CreateAtomic(context.Background(), clientresources.CreateMutation{
		Kind: "contact", Object: envelope,
		Payload: clientresources.Contact{
			Envelope: envelope, LocationID: "location",
			DisplayName: "Alex Client", Email: "alex@example.com",
		},
		Audit: mutation.AuditRecord{
			ID: "audit", OccurredAt: at, MSPID: "msp", ClientID: "client",
			ActorType: "technician", ActorID: "actor", Action: "contact.created",
			SubjectType: "contact", SubjectID: "contact", SubjectVersion: 1,
			Source: "api", CorrelationID: "correlation",
		},
		Event: mutation.EventRecord{
			EventID: "event", EventType: "contact.created", SchemaVersion: 1,
			OccurredAt: at, MSPID: "msp", ClientID: "client",
			ActorType: "technician", ActorID: "actor",
			SubjectType: "contact", SubjectID: "contact", SubjectVersion: 1,
			Source: "api", CorrelationID: "correlation",
		},
	})
	if err != nil {
		t.Fatalf("CreateAtomic() error = %v", err)
	}
	assertQueryOrder(
		t, tx.queries,
		"FROM client_organizations", "FROM locations", "INSERT INTO contacts", "INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
	if !containsAll(tx.queries[1], "lifecycle_state = 'active'", "FOR SHARE") {
		t.Fatalf("Contact creation did not serialize on active Location: %s", tx.queries[1])
	}
}

func TestCreateContactRejectsMissingOrCrossClientLocationBeforeFacts(t *testing.T) {
	tx := &fakeSalesTx{zeroRowsAt: 2}
	repository := NewContactRepository(&fakeSalesDB{tx: tx})
	envelope := object.Envelope{ID: "contact", MSPID: "msp", ClientID: "client"}
	err := repository.CreateAtomic(context.Background(), clientresources.CreateMutation{
		Kind: "contact", Object: envelope,
		Payload: clientresources.Contact{
			Envelope: envelope, LocationID: "inaccessible-location",
			DisplayName: "Alex Client",
		},
	})
	if !errors.Is(err, scope.ErrNotFound) || len(tx.queries) != 2 || !tx.rolledBack {
		t.Fatalf("inaccessible Location wrote dependent facts: err=%v tx=%+v", err, tx)
	}
}
