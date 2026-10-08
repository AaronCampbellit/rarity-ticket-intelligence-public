package psa

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/routing"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

func TestRoutingRepositoryPublishesRulesAndFactsAtomically(t *testing.T) {
	tx := &fakeSalesTx{}
	at := time.Date(2026, time.July, 29, 21, 0, 0, 0, time.UTC)
	err := NewRoutingRepository(&fakeSalesDB{tx: tx}).PublishAtomic(
		context.Background(),
		routing.PublishMutation{
			RuleSet: routing.RuleSet{
				ID: "set", MSPID: "msp", Version: 1, PublishedAt: at, PublishedBy: "actor",
				Rules: []routing.Rule{
					{ID: "critical", Position: 1, Priority: "critical", QueueID: "noc"},
					{ID: "fallback", Position: 2, QueueID: "triage"},
				},
			},
			Created: true,
			Audit: mutation.AuditRecord{
				ID: "audit", OccurredAt: at, MSPID: "msp", ActorType: "technician",
				ActorID: "actor", Action: "routing.rules.published",
				SubjectType: "routing_rule_set", SubjectID: "set", SubjectVersion: 1,
				Source: "api", CorrelationID: "correlation",
			},
			Event: mutation.EventRecord{
				EventID: "event", EventType: "routing.rules.published", SchemaVersion: 1,
				OccurredAt: at, MSPID: "msp", ActorType: "technician", ActorID: "actor",
				SubjectType: "routing_rule_set", SubjectID: "set", SubjectVersion: 1,
				Source: "api", CorrelationID: "correlation",
			},
		},
	)
	if err != nil {
		t.Fatalf("PublishAtomic() error = %v", err)
	}
	assertQueryOrder(
		t, tx.queries,
		"INSERT INTO routing_rule_sets", "INSERT INTO routing_rule_set_versions",
		"INSERT INTO routing_rule_versions", "INSERT INTO routing_rule_versions",
		"INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
}

func TestRoutingRepositoryValidatesGlobalAndClientDestinations(t *testing.T) {
	db := &fakeSalesDB{queryRow: fakeRow{scan: func(destinations ...any) {
		*(destinations[0].(*int)) = 2
	}}}
	err := NewRoutingRepository(db).ValidateDestinations(
		context.Background(),
		scope.Target{MSPID: "msp"},
		[]routing.Rule{
			{ID: "global", Position: 1, QueueID: "global-queue"},
			{ID: "client", Position: 2, ClientID: "client", QueueID: "client-queue"},
		},
	)
	if err != nil {
		t.Fatalf("ValidateDestinations() error = %v", err)
	}
	if !strings.Contains(db.query, "q.client_id IS NULL") ||
		!strings.Contains(db.query, "q.client_id = requested.client_id") {
		t.Fatalf("destination validation lacks scope rules: %s", db.query)
	}
}
