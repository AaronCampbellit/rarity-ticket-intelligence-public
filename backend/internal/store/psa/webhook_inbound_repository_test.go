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
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/webhooks"
)

func TestWebhookInboundRepositoryLoadsOnlyNamedConnection(t *testing.T) {
	db := &fakeSalesDB{queryRow: fakeRow{err: context.Canceled}}
	_, _ = NewWebhookInboundRepository(db).LoadInboundConnection(
		context.Background(), "connection",
	)
	for _, required := range []string{
		"FROM webhook_connections",
		"WHERE id = CASE",
		"THEN $1::uuid",
	} {
		if !strings.Contains(db.query, required) {
			t.Fatalf("connection lookup missing %q: %s", required, db.query)
		}
	}
}

func TestWebhookInboundRepositoryCreatesReplayEventAndFactsAtomically(t *testing.T) {
	tx := &fakeSalesTx{}
	now := time.Date(2026, time.July, 29, 23, 0, 0, 0, time.UTC)
	err := NewWebhookInboundRepository(&fakeSalesDB{tx: tx}).CreateInboundAtomic(
		context.Background(),
		webhooks.InboundMutation{
			ConnectionID: "connection", ReplayExpiresAt: now.Add(5 * time.Minute),
			System: intake.SystemMutation{
				Inbound: intake.InboundEvent{
					ID: "inbound", MSPID: "msp", ClientID: "client",
					ConnectionID: "connection",
					Source:       intake.SourceInboundWebhook, ExternalID: "external",
					ReceivedAt: now, AuthenticationResult: intake.AuthenticationPassed,
					RawPayloadRef: "intake/raw", ProcessingState: intake.StateReceived,
				},
				Audit: mutation.AuditRecord{
					ID: "audit", OccurredAt: now, MSPID: "msp", ClientID: "client",
					ActorType: "webhook_connection", ActorID: "connection",
					Action: "intake.webhook.received", SubjectType: "inbound_event",
					SubjectID: "inbound", SubjectVersion: 1, Source: "api",
					CorrelationID: "correlation",
				},
				Event: mutation.EventRecord{
					EventID: "outbox", EventType: "intake.webhook.received",
					SchemaVersion: 1, OccurredAt: now, MSPID: "msp",
					ClientID: "client", ActorType: "webhook_connection",
					ActorID: "connection", SubjectType: "inbound_event",
					SubjectID: "inbound", SubjectVersion: 1, Source: "api",
					CorrelationID: "correlation",
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("CreateInboundAtomic() error=%v", err)
	}
	assertQueryOrder(
		t, tx.queries, "INSERT INTO webhook_replay_claims",
		"INSERT INTO inbound_events", "INSERT INTO audit_ledger",
		"INSERT INTO event_outbox",
	)
	for _, required := range []string{
		"FROM webhook_connections connection",
		"connection.msp_id = $4",
		"connection.client_id = $5",
		"connection.enabled",
		"connection.direction IN ('inbound', 'bidirectional')",
	} {
		if !strings.Contains(tx.queries[0], required) {
			t.Fatalf("atomic replay claim missing %q: %s", required, tx.queries[0])
		}
	}
}

func TestWebhookInboundRepositoryRejectsConnectionChangedAfterAuthentication(t *testing.T) {
	tx := &fakeSalesTx{zeroRowsAt: 1}
	err := NewWebhookInboundRepository(&fakeSalesDB{tx: tx}).CreateInboundAtomic(
		context.Background(),
		webhooks.InboundMutation{
			ConnectionID: "connection", ReplayExpiresAt: time.Now().Add(time.Minute),
			System: intake.SystemMutation{Inbound: intake.InboundEvent{
				MSPID: "msp", ClientID: "client", ExternalID: "event",
			}},
		},
	)
	if !errors.Is(err, scope.ErrNotFound) || !tx.rolledBack {
		t.Fatalf("changed connection error=%v rolled_back=%t", err, tx.rolledBack)
	}
}

func TestWebhookInboundRepositoryMapsOnlyReplayUniqueness(t *testing.T) {
	for _, constraint := range []string{
		"webhook_replay_claims_pkey",
		"inbound_events_webhook_external_id_idx",
	} {
		err := normalizeWebhookInboundWriteError(&pgconn.PgError{
			Code: "23505", ConstraintName: constraint,
		})
		if !errors.Is(err, webhooks.ErrReplay) {
			t.Fatalf("constraint=%q error=%v", constraint, err)
		}
	}
	other := &pgconn.PgError{Code: "23505", ConstraintName: "inbound_events_pkey"}
	if got := normalizeWebhookInboundWriteError(other); !errors.Is(got, other) {
		t.Fatalf("unrelated uniqueness was hidden: %v", got)
	}
}
