package psa

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/automation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

func TestApplyAutomationDeadLetterActionWritesEvidenceAtomically(t *testing.T) {
	tx := &fakeSalesTx{}
	repository := NewAutomationRepository(&fakeSalesDB{tx: tx})
	at := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	err := repository.Apply(context.Background(), automation.DeadLetterMutation{
		Letter: automation.DeadLetter{
			ID: "letter", MSPID: "msp", ClientID: "client", State: "retrying",
		},
		ActionID: "action", Action: automation.DeadLetterRetry,
		Reason: "Connection repaired", ActedAt: at, ActedBy: "actor",
		Audit: mutation.AuditRecord{
			ID: "action", OccurredAt: at, MSPID: "msp", ClientID: "client",
			ActorType: "technician", ActorID: "actor",
			Action:      "automation.dead_letter.retry",
			SubjectType: "automation_dead_letter", SubjectID: "letter",
			SubjectVersion: 1, Source: "api", Reason: "Connection repaired",
			CorrelationID: "correlation",
		},
		Event: mutation.EventRecord{
			EventID: "event", EventType: "automation.dead_letter.retry",
			SchemaVersion: 1, OccurredAt: at, MSPID: "msp", ClientID: "client",
			ActorType: "technician", ActorID: "actor",
			SubjectType: "automation_dead_letter", SubjectID: "letter",
			SubjectVersion: 1, Source: "api", CorrelationID: "correlation",
		},
	})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	assertQueryOrder(t, tx.queries,
		"UPDATE automation_dead_letters",
		"UPDATE automation_runs",
		"INSERT INTO automation_dead_letter_actions",
		"INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
}

func TestApplyAutomationDeadLetterReplayQueuesNewIdempotencyGeneration(t *testing.T) {
	tx := &fakeSalesTx{}
	repository := NewAutomationRepository(&fakeSalesDB{tx: tx})
	at := time.Now().UTC()
	err := repository.Apply(context.Background(), automation.DeadLetterMutation{
		Letter: automation.DeadLetter{
			ID: "letter", RunID: "run", MSPID: "msp",
			ClientID: "client", State: "replayed",
		},
		ActionID: "action", Action: automation.DeadLetterReplay,
		Reason: "rerun from the original event", ActedAt: at,
		ActedBy: "actor",
		Audit: mutation.AuditRecord{
			ID: "action", OccurredAt: at, MSPID: "msp",
			ClientID: "client", ActorType: "technician",
			ActorID: "actor", Action: "automation.dead_letter.replay",
			SubjectType: "automation_dead_letter",
			SubjectID:   "letter", SubjectVersion: 1, Source: "api",
			Reason:        "rerun from the original event",
			CorrelationID: "correlation",
		},
		Event: mutation.EventRecord{
			EventID: "event", EventType: "automation.dead_letter.replay",
			SchemaVersion: 1, OccurredAt: at, MSPID: "msp",
			ClientID: "client", ActorType: "technician",
			ActorID: "actor", SubjectType: "automation_dead_letter",
			SubjectID: "letter", SubjectVersion: 1, Source: "api",
			CorrelationID: "correlation",
		},
	})
	if err != nil {
		t.Fatalf("Apply() error=%v", err)
	}
	assertQueryOrder(
		t, tx.queries,
		"UPDATE automation_dead_letters",
		"INSERT INTO automation_execution_jobs",
		"INSERT INTO automation_dead_letter_actions",
		"INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
	if !strings.Contains(tx.queries[1], "replay_generation") ||
		!strings.Contains(tx.queries[1], "max(existing.replay_generation)") {
		t.Fatalf("replay query does not create a new generation: %s", tx.queries[1])
	}
}

func TestFindAutomationDeadLetterIsClientScoped(t *testing.T) {
	db := &fakeSalesDB{queryRow: fakeRow{err: context.Canceled}}
	_, _ = NewAutomationRepository(db).Get(
		context.Background(),
		scope.Target{MSPID: "msp", ClientID: "client"},
		"letter",
	)
	if !strings.Contains(db.query, "dl.msp_id = $2 AND dl.client_id = $3") {
		t.Fatalf("automation dead-letter lookup is not Client scoped: %s", db.query)
	}
}

func TestAutomationListsAreClientScopedAndBounded(t *testing.T) {
	definitionsDB := &fakeSalesDB{queryRows: &fakeRows{}}
	_, _ = NewAutomationRepository(definitionsDB).ListDefinitions(
		context.Background(),
		scope.Target{MSPID: "msp", ClientID: "client"},
	)
	if !strings.Contains(definitionsDB.query, "$2::uuid = ANY(candidate.client_scopes)") ||
		!strings.Contains(definitionsDB.query, "definition.msp_id = $1") {
		t.Fatalf("automation definitions list is not scoped: %s", definitionsDB.query)
	}

	deadLettersDB := &fakeSalesDB{queryRows: &fakeRows{}}
	_, _ = NewAutomationRepository(deadLettersDB).List(
		context.Background(),
		scope.Target{MSPID: "msp", ClientID: "client"},
	)
	if !strings.Contains(deadLettersDB.query, "dl.msp_id = $1 AND dl.client_id = $2") ||
		!strings.Contains(deadLettersDB.query, "LIMIT 100") {
		t.Fatalf("automation dead-letter list is not scoped and bounded: %s", deadLettersDB.query)
	}
}
