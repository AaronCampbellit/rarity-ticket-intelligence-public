package psa

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/intake"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

func TestSystemIntakeRepositoryMapsOnlyIdempotencyUniqueness(t *testing.T) {
	err := normalizeSystemIntakeWriteError(&pgconn.PgError{
		Code: "23505", ConstraintName: "inbound_events_client_external_id_idx",
	})
	if !errors.Is(err, intake.ErrDuplicateSystemEvent) {
		t.Fatalf("idempotency conflict error=%v", err)
	}
	other := &pgconn.PgError{Code: "23505", ConstraintName: "inbound_events_pkey"}
	if got := normalizeSystemIntakeWriteError(other); !errors.Is(got, other) {
		t.Fatalf("unrelated uniqueness was hidden: %v", got)
	}
}

func TestSystemIntakeRepositoryFindsIdempotencyKeyWithinClientScope(t *testing.T) {
	db := &fakeSalesDB{queryRow: fakeRow{err: context.Canceled}}
	_, _ = NewSystemIntakeRepository(db).FindSystemEvent(
		context.Background(),
		scope.Target{MSPID: "msp", ClientID: "client"},
		intake.SourceDirectAPI,
		"external",
	)
	for _, required := range []string{
		"event.msp_id = $1",
		"event.client_id = $2::uuid",
		"event.source = $3",
		"event.external_id = $4",
	} {
		if !strings.Contains(db.query, required) {
			t.Fatalf("system intake lookup missing %q: %s", required, db.query)
		}
	}
}

func TestSystemIntakeRepositoryCreatesEventAndFactsAtomically(t *testing.T) {
	tx := &fakeSalesTx{}
	at := time.Date(2026, time.July, 29, 22, 0, 0, 0, time.UTC)
	err := NewSystemIntakeRepository(&fakeSalesDB{tx: tx}).CreateSystemEventAtomic(
		context.Background(),
		intake.SystemMutation{
			Inbound: intake.InboundEvent{
				ID: "inbound", MSPID: "msp", ClientID: "client",
				Source: intake.SourceDirectAPI, ExternalID: "external",
				ReceivedAt: at, AuthenticationResult: intake.AuthenticationPassed,
				RawPayloadRef: "intake/raw", ProcessingState: intake.StateReceived,
			},
			Audit: mutation.AuditRecord{
				ID: "audit", OccurredAt: at, MSPID: "msp", ClientID: "client",
				ActorType: "service_key", ActorID: "actor",
				Action: "intake.direct.received", SubjectType: "inbound_event",
				SubjectID: "inbound", SubjectVersion: 1, Source: "api",
				CorrelationID: "correlation",
			},
			Event: mutation.EventRecord{
				EventID: "event", EventType: "intake.direct.received",
				SchemaVersion: 1, OccurredAt: at, MSPID: "msp", ClientID: "client",
				ActorType: "service_key", ActorID: "actor",
				SubjectType: "inbound_event", SubjectID: "inbound",
				SubjectVersion: 1, Source: "api", CorrelationID: "correlation",
			},
		},
	)
	if err != nil {
		t.Fatalf("CreateSystemEventAtomic() error=%v", err)
	}
	assertQueryOrder(
		t, tx.queries,
		"INSERT INTO inbound_events",
		"INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
}
