package psa

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sla"
)

func TestSLARepositoryReturnsEmptyCalendarCollection(t *testing.T) {
	found, err := NewSLARepository(
		&fakeSalesDB{queryRows: &fakeRows{}},
	).ListCalendars(
		context.Background(),
		scope.Target{MSPID: "msp-id", ClientID: "client-id"},
	)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(found)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != "[]" {
		t.Fatalf("empty calendars encoded as %s", encoded)
	}
}

func TestSLARepositoryPublishesCalendarVersionAndFactsAtomically(t *testing.T) {
	tx := &fakeSalesTx{}
	at := time.Date(2026, time.July, 29, 22, 30, 0, 0, time.UTC)
	err := NewSLARepository(&fakeSalesDB{tx: tx}).PublishCalendarAtomic(
		context.Background(),
		sla.CalendarPublishMutation{
			Calendar: sla.PublishedCalendar{
				ID: "calendar", MSPID: "msp", ClientID: "client",
				Key: "support-hours", Name: "Support Hours", Version: 1,
				Definition: sla.CalendarDefinition{
					Timezone: "UTC",
					Weekly: map[string][]sla.Window{
						"monday": {{StartMinute: 540, EndMinute: 1020}},
					},
				},
				PublishedAt: at, PublishedBy: "actor",
			},
			Created: true,
			Audit: mutation.AuditRecord{
				ID: "audit", OccurredAt: at, MSPID: "msp", ClientID: "client",
				ActorType: "technician", ActorID: "actor",
				Action: "business_calendar.published", SubjectType: "business_calendar",
				SubjectID: "calendar", SubjectVersion: 1, Source: "api",
				CorrelationID: "correlation",
			},
			Event: mutation.EventRecord{
				EventID: "event", EventType: "business_calendar.published", SchemaVersion: 1,
				OccurredAt: at, MSPID: "msp", ClientID: "client",
				ActorType: "technician", ActorID: "actor",
				SubjectType: "business_calendar", SubjectID: "calendar",
				SubjectVersion: 1, Source: "api", CorrelationID: "correlation",
			},
		},
	)
	if err != nil {
		t.Fatalf("PublishCalendarAtomic() error = %v", err)
	}
	assertQueryOrder(
		t, tx.queries,
		"INSERT INTO business_calendars", "INSERT INTO business_calendar_versions",
		"INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
}

func TestFindCalendarIsMSPAndClientScoped(t *testing.T) {
	db := &fakeSalesDB{queryRow: fakeRow{err: context.Canceled}}
	_, _ = NewSLARepository(db).FindCalendar(
		context.Background(),
		scope.Target{MSPID: "msp", ClientID: "client"},
		"calendar",
	)
	if !strings.Contains(db.query, "c.msp_id = $2") ||
		!strings.Contains(db.query, "c.client_id = $3") {
		t.Fatalf("calendar lookup is not exact-scope: %s", db.query)
	}
}

func TestSLARepositoryPublishesPolicyVersionAndFactsAtomically(t *testing.T) {
	tx := &fakeSalesTx{}
	at := time.Date(2026, time.July, 29, 23, 30, 0, 0, time.UTC)
	err := NewSLARepository(&fakeSalesDB{tx: tx}).PublishPolicyAtomic(
		context.Background(),
		sla.PolicyPublishMutation{
			Policy: sla.PublishedPolicy{
				ID: "policy", MSPID: "msp", Key: "default", Name: "Default",
				Version:               1,
				Calendar:              sla.PublishedCalendar{ID: "calendar", Version: 2},
				ResponseTargetSeconds: 3600, ResolutionTargetSeconds: 14400,
				WarningPercent: 80, Enabled: true, StableOrder: 1, Fallback: true,
				PublishedAt: at, PublishedBy: "actor",
			},
			Created: true,
			Audit: mutation.AuditRecord{
				ID: "audit", OccurredAt: at, MSPID: "msp",
				ActorType: "technician", ActorID: "actor",
				Action: "sla_policy.published", SubjectType: "sla_policy",
				SubjectID: "policy", SubjectVersion: 1, Source: "api",
				CorrelationID: "correlation",
			},
			Event: mutation.EventRecord{
				EventID: "event", EventType: "sla_policy.published", SchemaVersion: 1,
				OccurredAt: at, MSPID: "msp", ActorType: "technician", ActorID: "actor",
				SubjectType: "sla_policy", SubjectID: "policy", SubjectVersion: 1,
				Source: "api", CorrelationID: "correlation",
			},
		},
	)
	if err != nil {
		t.Fatalf("PublishPolicyAtomic() error = %v", err)
	}
	assertQueryOrder(
		t, tx.queries,
		"INSERT INTO sla_policies", "INSERT INTO sla_policy_versions",
		"INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
}

func TestSLARepositoryListsOnlyDueUnpausedActiveTimers(t *testing.T) {
	db := &fakeSalesDB{queryRows: &fakeRows{}}
	_, err := NewSLARepository(db).ListDue(
		context.Background(),
		time.Date(2026, time.July, 30, 16, 0, 0, 0, time.UTC),
		100,
	)
	if err != nil {
		t.Fatalf("ListDue() error = %v", err)
	}
	for _, fragment := range []string{
		"paused_at IS NULL", "response_state IN ('running', 'warning')",
		"resolution_state IN ('running', 'warning')",
	} {
		if !strings.Contains(db.query, fragment) {
			t.Fatalf("due timer query missing %q: %s", fragment, db.query)
		}
	}
}

func TestSLARepositoryUpdatesEvaluationAndFactsAtomically(t *testing.T) {
	tx := &fakeSalesTx{}
	at := time.Date(2026, time.July, 30, 16, 0, 0, 0, time.UTC)
	err := NewSLARepository(&fakeSalesDB{tx: tx}).UpdateEvaluation(
		context.Background(),
		sla.EvaluationMutation{
			Timer: sla.Timer{
				ID: "sla", MSPID: "msp", ClientID: "client",
				ResponseState: sla.Warning, ResolutionState: sla.Breached, Version: 2,
			},
			Audits: []mutation.AuditRecord{
				validAudit(at, "sla.response.warning", "work_record_sla", "sla"),
				validAudit(at, "sla.resolution.breached", "work_record_sla", "sla"),
			},
			Events: []mutation.EventRecord{
				validEvent(at, "sla.response.warning", "work_record_sla", "sla"),
				validEvent(at, "sla.resolution.breached", "work_record_sla", "sla"),
			},
		},
	)
	if err != nil {
		t.Fatalf("UpdateEvaluation() error = %v", err)
	}
	assertQueryOrder(
		t, tx.queries,
		"UPDATE work_record_slas",
		"INSERT INTO audit_ledger", "INSERT INTO event_outbox",
		"INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
}
