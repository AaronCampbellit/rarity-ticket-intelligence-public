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

func TestCreateAssetWritesScopedProvenanceAndFactsAtomically(t *testing.T) {
	tx := &fakeSalesTx{}
	repository := NewAssetRepository(&fakeSalesDB{tx: tx})
	at := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	envelope := object.Envelope{
		ID: "asset", MSPID: "msp", ClientID: "client",
		DisplayID: "ASSET-1", LifecycleState: "active", Version: 1,
		CreatedAt: at, CreatedBy: "actor", UpdatedAt: at, UpdatedBy: "actor",
	}
	err := repository.CreateAtomic(context.Background(), clientresources.CreateMutation{
		Kind: "asset", Object: envelope,
		Payload: clientresources.Asset{
			Envelope: envelope, LocationID: "location", Name: "MAIL01",
			AssetType: "server", Provenance: clientresources.Provenance{
				SourceSystem: "datto", ExternalID: "device-100",
				Authority: clientresources.Discovered,
			},
		},
		Audit: mutation.AuditRecord{
			ID: "audit", OccurredAt: at, MSPID: "msp", ClientID: "client",
			ActorType: "technician", ActorID: "actor", Action: "asset.created",
			SubjectType: "asset", SubjectID: "asset", SubjectVersion: 1,
			Source: "api", CorrelationID: "correlation",
		},
		Event: mutation.EventRecord{
			EventID: "event", EventType: "asset.created", SchemaVersion: 1,
			OccurredAt: at, MSPID: "msp", ClientID: "client",
			ActorType: "technician", ActorID: "actor",
			SubjectType: "asset", SubjectID: "asset", SubjectVersion: 1,
			Source: "api", CorrelationID: "correlation",
		},
		InitialTags: testInitialTags(at),
	})
	if err != nil {
		t.Fatalf("CreateAtomic() error = %v", err)
	}
	assertQueryOrder(
		t, tx.queries,
		"FROM client_organizations", "FROM locations",
		"INSERT INTO assets", "INSERT INTO object_tag_assignments", "INSERT INTO tag_assignment_events",
		"INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
	if !containsAll(tx.queries[1], "lifecycle_state = 'active'", "FOR SHARE") {
		t.Fatalf("Asset creation did not serialize on active Location: %s", tx.queries[1])
	}
}

func TestCreateAssetRejectsInaccessibleLocationBeforeFacts(t *testing.T) {
	tx := &fakeSalesTx{zeroRowsAt: 2}
	envelope := object.Envelope{ID: "asset", MSPID: "msp", ClientID: "client"}
	err := NewAssetRepository(&fakeSalesDB{tx: tx}).CreateAtomic(
		context.Background(),
		clientresources.CreateMutation{
			Kind: "asset", Object: envelope,
			Payload: clientresources.Asset{
				Envelope: envelope, LocationID: "inaccessible",
				Name: "MAIL01", AssetType: "server",
				Provenance: clientresources.Provenance{
					Authority: clientresources.Discovered,
				},
			},
		},
	)
	if !errors.Is(err, scope.ErrNotFound) || len(tx.queries) != 2 || !tx.rolledBack {
		t.Fatalf("inaccessible Location wrote dependent facts: err=%v tx=%+v", err, tx)
	}
}
