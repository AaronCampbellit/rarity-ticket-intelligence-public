package psa

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/webhooks"
)

func TestWebhookOutboundRepositoryPlansExplicitScopedEventTypesOnce(t *testing.T) {
	tx := &fakeSalesTx{}
	planned, err := NewWebhookOutboundRepository(
		&fakeSalesDB{tx: tx}, func() string { return "attempt" },
	).Plan(context.Background(), 100, time.Now())
	if err != nil || planned != 1 {
		t.Fatalf("Plan() planned=%d error=%v", planned, err)
	}
	if len(tx.queries) != 1 {
		t.Fatalf("plan queries=%d", len(tx.queries))
	}
	for _, required := range []string{
		"webhook_event_plans",
		"webhook_event_deliveries",
		"FOR UPDATE OF event SKIP LOCKED",
		"connection.direction IN ('outbound', 'bidirectional')",
		"event.event_type = ANY(connection.event_types)",
		"connection.msp_id = event.msp_id",
		"connection.client_id IS NULL OR connection.client_id = event.client_id",
		"extract(epoch FROM connection.retry_window)::integer",
	} {
		if !strings.Contains(tx.queries[0], required) {
			t.Fatalf("outbound plan missing %q: %s", required, tx.queries[0])
		}
	}
}

func TestWebhookOutboundRepositoryClaimsWithAtomicLeaseAndSafeEnvelope(t *testing.T) {
	db := &fakeSalesDB{queryRows: &fakeRows{}}
	jobs, err := NewWebhookOutboundRepository(
		db, func() string { return "attempt" },
	).Claim(
		context.Background(), 100, time.Now(), 30*time.Second,
	)
	if err != nil || len(jobs) != 0 {
		t.Fatalf("Claim() jobs=%+v error=%v", jobs, err)
	}
	for _, required := range []string{
		"FOR UPDATE OF delivery SKIP LOCKED",
		"UPDATE webhook_event_deliveries",
		"lease_until = $1 +",
		"attempt_count = delivery.attempt_count + 1",
		"'db://webhook/' || connection.id::text",
		"jsonb_build_object",
		"'schema_version'",
		"'data', event.data",
	} {
		if !strings.Contains(db.query, required) {
			t.Fatalf("outbound claim missing %q: %s", required, db.query)
		}
	}
}

func TestWebhookOutboundRepositoryGuardsCompletionByClaimAttempt(t *testing.T) {
	at := time.Date(2026, time.July, 29, 23, 45, 0, 0, time.UTC)
	for _, test := range []struct {
		name string
		run  func(*WebhookOutboundRepository) error
		want string
	}{
		{
			name: "delivered",
			run: func(repository *WebhookOutboundRepository) error {
				return repository.MarkDelivered(context.Background(), webhooks.OutboundCompletion{
					ConnectionID: "connection", EventID: "event",
					Attempt: 2, CompletedAt: at,
				})
			},
			want: "state = 'delivered'",
		},
		{
			name: "retry",
			run: func(repository *WebhookOutboundRepository) error {
				return repository.MarkRetry(context.Background(), webhooks.OutboundCompletion{
					ConnectionID: "connection", EventID: "event", Attempt: 2,
					CompletedAt: at, NextAttemptAt: at.Add(time.Minute),
					ErrorCode: "delivery_failed",
				})
			},
			want: "state = 'pending'",
		},
		{
			name: "failed",
			run: func(repository *WebhookOutboundRepository) error {
				return repository.MarkFailed(context.Background(), webhooks.OutboundCompletion{
					ConnectionID: "connection", EventID: "event",
					Attempt: 2, CompletedAt: at,
					ErrorCode: "retry_window_expired",
				})
			},
			want: "state = 'failed'",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := &fakeSalesTx{}
			if err := test.run(NewWebhookOutboundRepository(
				&fakeSalesDB{tx: tx}, func() string { return "attempt" },
			)); err != nil {
				t.Fatalf("completion error=%v", err)
			}
			if len(tx.queries) != 1 ||
				!strings.Contains(tx.queries[0], test.want) ||
				!strings.Contains(tx.queries[0], "attempt_count = $3") {
				t.Fatalf("completion lacks attempt guard: %+v", tx.queries)
			}
		})
	}
}

func TestWebhookOutboundRepositoryRecordsImmutableAttemptHistory(t *testing.T) {
	tx := &fakeSalesTx{}
	err := NewWebhookOutboundRepository(
		&fakeSalesDB{tx: tx}, func() string { return "attempt" },
	).Record(
		context.Background(),
		webhooks.DeliveryAttempt{
			ConnectionID: "connection", EventID: "event", Attempt: 2,
			AttemptedAt: time.Now(), State: webhooks.DeliveryFailed,
			HTTPStatus: 503, ErrorCode: "remote_5xx",
		},
	)
	if err != nil || len(tx.queries) != 1 ||
		!strings.Contains(tx.queries[0], "INSERT INTO webhook_delivery_attempts") ||
		!strings.Contains(tx.queries[0], "ON CONFLICT (connection_id, event_id, attempt) DO NOTHING") {
		t.Fatalf("attempt history error=%v queries=%+v", err, tx.queries)
	}
}

func TestWebhookOutboundRepositoryListsDeliveriesAcrossLegacyTextAttemptIDs(t *testing.T) {
	db := &fakeSalesDB{queryRows: &fakeRows{}}
	deliveries, err := NewWebhookOutboundRepository(
		db, func() string { return "attempt" },
	).ListManagedDeliveries(
		context.Background(),
		scope.Target{MSPID: "msp", ClientID: "client"},
		200,
	)
	if err != nil || len(deliveries) != 0 {
		t.Fatalf("ListManagedDeliveries() deliveries=%+v error=%v", deliveries, err)
	}
	if !strings.Contains(
		db.query,
		"event_id = delivery.event_id::text",
	) {
		t.Fatalf("delivery history does not bridge legacy text event IDs: %s", db.query)
	}
}
