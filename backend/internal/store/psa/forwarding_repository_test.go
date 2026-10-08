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

func TestForwardingRepositoryLoadsConnectionInsideTrustedMSPScope(t *testing.T) {
	db := &fakeSalesDB{queryRow: fakeRow{err: context.Canceled}}
	_, _ = NewForwardingRepository(db).LoadForwardingConnection(
		context.Background(), scope.Target{MSPID: "msp"}, "connection",
	)
	for _, required := range []string{
		"FROM forwarding_intake_connections",
		"connection.id = CASE",
		"THEN $1::uuid",
		"connection.msp_id = $2",
	} {
		if !strings.Contains(db.query, required) {
			t.Fatalf("connection lookup missing %q: %s", required, db.query)
		}
	}
}

func TestForwardingRepositoryRateLimitsGlobalAndSenderAtomically(t *testing.T) {
	tx := &fakeSalesTx{}
	allowed, err := NewForwardingRepository(&fakeSalesDB{tx: tx}).AllowForwarding(
		context.Background(), "connection", "alerts@customer.example", 60,
		time.Date(2026, time.July, 30, 0, 0, 45, 0, time.UTC),
	)
	if err != nil || !allowed || !tx.committed {
		t.Fatalf("AllowForwarding() allowed=%t error=%v tx=%+v", allowed, err, tx)
	}
	assertQueryOrder(
		t, tx.queries, "DELETE FROM forwarding_rate_limit_windows",
		"INSERT INTO forwarding_rate_limit_windows",
		"INSERT INTO forwarding_rate_limit_windows",
	)
	if !strings.Contains(tx.queries[1], "message_count < $4") ||
		!strings.Contains(tx.queries[1], "window_started_at") {
		t.Fatalf("rate limit upsert is not bounded: %s", tx.queries[1])
	}
}

func TestForwardingRepositoryRollsBackWholeRateClaimAtLimit(t *testing.T) {
	tx := &fakeSalesTx{zeroRowsAt: 3}
	allowed, err := NewForwardingRepository(&fakeSalesDB{tx: tx}).AllowForwarding(
		context.Background(), "connection", "alerts@customer.example", 60,
		time.Now(),
	)
	if err != nil || allowed || !tx.rolledBack || tx.committed {
		t.Fatalf("AllowForwarding() allowed=%t error=%v tx=%+v", allowed, err, tx)
	}
}

func TestForwardingRepositoryCreatesEventFactsAndHealthAtomically(t *testing.T) {
	tx := &fakeSalesTx{}
	at := time.Date(2026, time.July, 30, 0, 0, 0, 0, time.UTC)
	err := NewForwardingRepository(&fakeSalesDB{tx: tx}).CreateForwardingAtomic(
		context.Background(),
		intake.ForwardingMutation{
			Inbound: intake.InboundEvent{
				ID: "inbound", MSPID: "msp",
				ForwardingConnectionID: "connection",
				Source:                 intake.SourceForwardedEmail,
				ExternalID:             "<message@example.com>", ReceivedAt: at,
				AuthenticationResult: intake.AuthenticationPassed,
				RawPayloadRef:        "intake/raw", ProcessingState: intake.StateReceived,
			},
			Audit: mutation.AuditRecord{
				ID: "audit", OccurredAt: at, MSPID: "msp",
				ActorType: "service_key", ActorID: "actor",
				Action:      "intake.forwarding.received",
				SubjectType: "inbound_event", SubjectID: "inbound",
				SubjectVersion: 1, Source: "api", CorrelationID: "correlation",
			},
			Event: mutation.EventRecord{
				EventID: "outbox", EventType: "intake.forwarding.received",
				SchemaVersion: 1, OccurredAt: at, MSPID: "msp",
				ActorType: "service_key", ActorID: "actor",
				SubjectType: "inbound_event", SubjectID: "inbound",
				SubjectVersion: 1, Source: "api", CorrelationID: "correlation",
			},
		},
	)
	if err != nil {
		t.Fatalf("CreateForwardingAtomic() error=%v", err)
	}
	assertQueryOrder(
		t, tx.queries, "INSERT INTO inbound_events",
		"INSERT INTO audit_ledger", "INSERT INTO event_outbox",
		"UPDATE forwarding_intake_connections",
	)
	if !strings.Contains(tx.queries[0], "FROM forwarding_intake_connections connection") ||
		!strings.Contains(tx.queries[0], "connection.enabled") {
		t.Fatalf("forwarding write does not revalidate connection: %s", tx.queries[0])
	}
}

func TestForwardingRepositoryRejectsConnectionChangedAfterValidation(t *testing.T) {
	tx := &fakeSalesTx{zeroRowsAt: 1}
	err := NewForwardingRepository(&fakeSalesDB{tx: tx}).CreateForwardingAtomic(
		context.Background(),
		intake.ForwardingMutation{Inbound: intake.InboundEvent{
			ID: "inbound", MSPID: "msp",
			ForwardingConnectionID: "connection",
			Source:                 intake.SourceForwardedEmail, ExternalID: "event",
			ReceivedAt: time.Now(), AuthenticationResult: intake.AuthenticationPassed,
			RawPayloadRef: "raw", ProcessingState: intake.StateReceived,
		}},
	)
	if !errors.Is(err, scope.ErrNotFound) || !tx.rolledBack {
		t.Fatalf("changed connection error=%v rolled_back=%t", err, tx.rolledBack)
	}
}

func TestForwardingRepositoryMapsOnlyConnectionIdempotencyConflict(t *testing.T) {
	err := normalizeForwardingWriteError(&pgconn.PgError{
		Code:           "23505",
		ConstraintName: "inbound_events_forwarding_external_id_idx",
	})
	if !errors.Is(err, intake.ErrDuplicateForwardingEvent) {
		t.Fatalf("idempotency error=%v", err)
	}
	other := &pgconn.PgError{Code: "23505", ConstraintName: "inbound_events_pkey"}
	if got := normalizeForwardingWriteError(other); !errors.Is(got, other) {
		t.Fatalf("unrelated uniqueness hidden: %v", got)
	}
}
