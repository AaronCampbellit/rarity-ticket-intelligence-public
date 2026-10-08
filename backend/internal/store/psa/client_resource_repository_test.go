package psa

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/clientresources"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

func TestCreateLocationWritesResourceAuditAndOutboxAtomically(t *testing.T) {
	tx := &fakeSalesTx{}
	repository := NewLocationRepository(&fakeSalesDB{tx: tx})
	at := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	envelope := object.Envelope{
		ID: "location", MSPID: "msp", ClientID: "client",
		DisplayID: "LOC-1", LifecycleState: "active", Version: 1,
		CreatedAt: at, CreatedBy: "actor", UpdatedAt: at, UpdatedBy: "actor",
	}
	err := repository.CreateAtomic(context.Background(), clientresources.CreateMutation{
		Kind: "location", Object: envelope,
		Payload: clientresources.Location{Envelope: envelope, Name: "Headquarters"},
		Audit: mutation.AuditRecord{
			ID: "audit", OccurredAt: at, MSPID: "msp", ClientID: "client",
			ActorType: "technician", ActorID: "actor", Action: "location.created",
			SubjectType: "location", SubjectID: "location", SubjectVersion: 1,
			Source: "api", CorrelationID: "correlation",
		},
		Event: mutation.EventRecord{
			EventID: "event", EventType: "location.created", SchemaVersion: 1,
			OccurredAt: at, MSPID: "msp", ClientID: "client",
			ActorType: "technician", ActorID: "actor",
			SubjectType: "location", SubjectID: "location", SubjectVersion: 1,
			Source: "api", CorrelationID: "correlation",
		},
	})
	if err != nil {
		t.Fatalf("CreateAtomic() error = %v", err)
	}
	assertQueryOrder(
		t, tx.queries,
		"FROM client_organizations", "INSERT INTO locations", "INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
}

func TestClientResourceCreateLocksActiveClientBeforeEveryWrite(t *testing.T) {
	tests := []struct {
		kind string
		repo func(database) clientresources.Repository
	}{
		{"location", func(db database) clientresources.Repository { return NewLocationRepository(db) }},
		{"contact", func(db database) clientresources.Repository { return NewContactRepository(db) }},
		{"asset", func(db database) clientresources.Repository { return NewAssetRepository(db) }},
		{"service", func(db database) clientresources.Repository { return NewServiceResourceRepository(db) }},
		{"contract", func(db database) clientresources.Repository { return NewContractRepository(db) }},
	}
	for _, test := range tests {
		t.Run(test.kind, func(t *testing.T) {
			tx := &fakeSalesTx{}
			if err := test.repo(&fakeSalesDB{tx: tx}).CreateAtomic(context.Background(), validClientResourceMutation(test.kind)); err != nil {
				t.Fatalf("CreateAtomic() error=%v", err)
			}
			if len(tx.queries) == 0 || !containsAll(tx.queries[0], "FROM client_organizations", "lifecycle_state = 'active'", "FOR SHARE") {
				t.Fatalf("first query does not lock active Client: %v", tx.queries)
			}

			inactive := &fakeSalesTx{zeroRowsAt: 1}
			err := test.repo(&fakeSalesDB{tx: inactive}).CreateAtomic(context.Background(), validClientResourceMutation(test.kind))
			if !errors.Is(err, scope.ErrNotFound) || len(inactive.queries) != 1 || !inactive.rolledBack {
				t.Fatalf("inactive Client reached mutation: error=%v queries=%v rolled_back=%t", err, inactive.queries, inactive.rolledBack)
			}
		})
	}
}

func TestClientResourceCreateRollsBackEveryPartialFailure(t *testing.T) {
	for _, kind := range []string{"location", "contact", "asset", "service", "contract"} {
		for _, failAt := range []int{2, 3, 4} {
			t.Run(kind+"-write-"+string(rune('0'+failAt)), func(t *testing.T) {
				tx := &fakeSalesTx{failAt: failAt}
				if err := clientResourceRepository(kind, &fakeSalesDB{tx: tx}).CreateAtomic(context.Background(), validClientResourceMutation(kind)); err == nil || !tx.rolledBack {
					t.Fatalf("partial failure was not rolled back: error=%v tx=%+v", err, tx)
				}
			})
		}
	}
}

func TestResourceWriteErrorMapsOnlyResourceIdentityConflicts(t *testing.T) {
	for _, constraint := range []string{
		"locations_msp_id_client_id_display_id_key",
		"contacts_msp_id_client_id_display_id_key",
		"assets_msp_id_client_id_display_id_key",
		"services_msp_id_client_id_display_id_key",
		"contracts_msp_id_client_id_display_id_key",
		"assets_external_identity_unique",
	} {
		if err := resourceWriteError(&pgconn.PgError{Code: "23505", ConstraintName: constraint}); !errors.Is(err, clientresources.ErrResourceIdentityConflict) {
			t.Fatalf("constraint=%q error=%v", constraint, err)
		}
	}
	other := &pgconn.PgError{Code: "23505", ConstraintName: "event_outbox_pkey"}
	if got := resourceWriteError(other); got != other {
		t.Fatalf("unrelated uniqueness was hidden: %v", got)
	}
}

func validClientResourceMutation(kind string) clientresources.CreateMutation {
	at := time.Date(2026, time.August, 5, 0, 0, 0, 0, time.UTC)
	envelope := object.Envelope{ID: "resource", MSPID: "msp", ClientID: "client", DisplayID: "RESOURCE-1", LifecycleState: "active", Version: 1, CreatedAt: at, CreatedBy: "actor", UpdatedAt: at, UpdatedBy: "actor"}
	mutation := clientresources.CreateMutation{Kind: kind, Object: envelope, Audit: mutation.AuditRecord{ID: "audit", OccurredAt: at, MSPID: "msp", ClientID: "client", ActorType: "technician", ActorID: "actor", Action: kind + ".created", SubjectType: kind, SubjectID: "resource", SubjectVersion: 1, Source: "api", CorrelationID: "correlation"}, Event: mutation.EventRecord{EventID: "event", EventType: kind + ".created", SchemaVersion: 1, OccurredAt: at, MSPID: "msp", ClientID: "client", ActorType: "technician", ActorID: "actor", SubjectType: kind, SubjectID: "resource", SubjectVersion: 1, Source: "api", CorrelationID: "correlation"}}
	switch kind {
	case "location":
		mutation.Payload = clientresources.Location{Envelope: envelope, Name: "Headquarters"}
	case "contact":
		mutation.Payload = clientresources.Contact{Envelope: envelope, DisplayName: "Alex Client"}
	case "asset":
		mutation.Payload = clientresources.Asset{Envelope: envelope, Name: "MAIL01", AssetType: "server", Provenance: clientresources.Provenance{Authority: clientresources.Discovered}}
	case "service":
		mutation.Payload = clientresources.ServiceRecord{Envelope: envelope, Name: "Email"}
	case "contract":
		mutation.Payload = clientresources.Contract{Envelope: envelope, Name: "Managed Services", StartsOn: at}
	}
	return mutation
}

func clientResourceRepository(kind string, db database) clientresources.Repository {
	switch kind {
	case "location":
		return NewLocationRepository(db)
	case "contact":
		return NewContactRepository(db)
	case "asset":
		return NewAssetRepository(db)
	case "service":
		return NewServiceResourceRepository(db)
	default:
		return NewContractRepository(db)
	}
}

func containsAll(value string, fragments ...string) bool {
	for _, fragment := range fragments {
		if !strings.Contains(value, fragment) {
			return false
		}
	}
	return true
}

func TestLocationRepositoryRejectsUnsupportedResourceKind(t *testing.T) {
	tx := &fakeSalesTx{}
	err := NewLocationRepository(&fakeSalesDB{tx: tx}).CreateAtomic(
		context.Background(),
		clientresources.CreateMutation{Kind: "asset"},
	)
	if !errors.Is(err, clientresources.ErrInvalid) || len(tx.queries) != 0 {
		t.Fatalf("unsupported resource reached SQL: err=%v queries=%v", err, tx.queries)
	}
}
