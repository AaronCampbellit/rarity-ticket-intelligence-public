package psa

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/notifications"
)

func TestCalendarPreferenceReplacementWritesRulesAuditAndOutboxAtomically(t *testing.T) {
	tx := &fakeSalesTx{queryRows: []row{fakeRow{scan: func(values ...any) { *(values[0].(*int64)) = 3 }}}}
	repository := NewCalendarNotificationRepository(&fakeSalesDB{tx: tx}, func() string { return "event-id" })
	preference := notifications.CalendarPreference{TechnicianID: "00000000-0000-4000-8000-000000000002", Version: 3, Rules: []notifications.CalendarPreferenceRule{{EventClass: notifications.CalendarScheduleChanged, ChangeClass: notifications.CalendarAssigned, Urgency: notifications.CalendarNormal, Channel: notifications.InApp, Enabled: true}}}
	mutationValue := notifications.CalendarPreferenceMutation{MSPID: "00000000-0000-4000-8000-000000000001", Preference: preference, ExpectedVersion: 2, Audit: mutation.AuditRecord{ID: "audit", ActorID: preference.TechnicianID}, Event: mutation.EventRecord{EventID: "event", EventType: "calendar.notification_preferences.replaced"}}
	found, err := repository.ReplaceCalendarPreference(context.Background(), mutationValue)
	if err != nil || found.Version != 3 || !tx.committed {
		t.Fatalf("preference=%+v err=%v committed=%v", found, err, tx.committed)
	}
	queries := strings.Join(append(tx.queries, tx.query), "\n")
	for _, fragment := range []string{"calendar_notification_preference_sets", "calendar_notification_preference_rules", "audit_ledger", "event_outbox"} {
		if !strings.Contains(queries, fragment) {
			t.Fatalf("transaction missing %q: %s", fragment, queries)
		}
	}
}

func TestCalendarReminderQueryIsBoundedReauthorizedAndPreferenceIndependent(t *testing.T) {
	due := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	rows := &fakeRows{scans: []func(...any){func(values ...any) {
		*(values[0].(*string)) = "msp"
		*(values[1].(*string)) = "projection"
		*(values[2].(*string)) = "occurrence"
		*(values[3].(*int64)) = 2
		*(values[4].(*string)) = "24h"
		*(values[5].(*time.Time)) = due
		*(values[6].(*string)) = "UTC"
		*(values[7].(*string)) = "tech"
		*(values[8].(*bool)) = true
		*(values[9].(*bool)) = true
	}}}
	db := &fakeSalesDB{queryRows: rows}
	found, err := NewCalendarNotificationRepository(db, func() string { return "event" }).DueReminderCandidates(context.Background(), "msp", due, 25)
	if err != nil || len(found) != 1 {
		t.Fatalf("found=%+v err=%v", found, err)
	}
	for _, fragment := range []string{"calendar_event_projections", "terminal_state='active'", "source_revision", "NOT EXISTS", "role_assignments", "LIMIT $3"} {
		if !strings.Contains(db.query, fragment) {
			t.Fatalf("reminder query missing %q: %s", fragment, db.query)
		}
	}
	if strings.Contains(db.query, "calendar_notification_preference_rules") {
		t.Fatalf("canonical reminder discovery was gated by delivery preferences: %s", db.query)
	}
}

func TestCalendarReminderClaimInsertsOnceAndEmitsOutbox(t *testing.T) {
	tx := &fakeSalesTx{queryRows: []row{fakeRow{scan: func(values ...any) {
		*(values[0].(*string)) = "commercial_commitment"
		*(values[1].(*string)) = "commitment"
		*(values[2].(*string)) = ""
		*(values[3].(*string)) = "renewal"
	}}}}
	repository := NewCalendarNotificationRepository(&fakeSalesDB{tx: tx}, func() string { return "00000000-0000-4000-8000-000000000099" })
	fact := calendar.ReminderFact{MSPID: "msp", ProjectionID: "projection", OccurrenceID: "occurrence", SourceRevision: 2, Threshold: "24h", DueAt: time.Now(), Timezone: "UTC", RecipientID: "tech", EvaluatedAt: time.Now()}
	owned, err := repository.ClaimReminder(context.Background(), "projection:occurrence:24h:2", fact)
	if err != nil || !owned || !tx.committed {
		t.Fatalf("owned=%v err=%v committed=%v", owned, err, tx.committed)
	}
	queries := strings.Join(tx.queries, "\n")
	if !strings.Contains(queries, "INSERT INTO calendar_reminder_facts") || !strings.Contains(queries, "projection.assignee_id=$9::uuid") || !strings.Contains(queries, "technician.lifecycle_state='active'") || !strings.Contains(queries, "role_assignments") || strings.Contains(queries, "calendar_notification_preference_rules") || !strings.Contains(queries, "ON CONFLICT") || !strings.Contains(queries, "'calendar.schedule_changed'") {
		t.Fatalf("claim transaction=%s", queries)
	}
	var payload map[string]any
	if err := json.Unmarshal(tx.args[1][len(tx.args[1])-1].([]byte), &payload); err != nil {
		t.Fatalf("decode reminder payload: %v", err)
	}
	if len(payload) != 5 || payload["recipient_id"] != "tech" || payload["change_class"] != "reminder" || payload["urgency"] != "routine" || payload["action_path"] != "/calendar" {
		t.Fatalf("canonical reminder payload=%#v", payload)
	}
	refs, ok := payload["source_refs"].([]any)
	if !ok || len(refs) != 1 {
		t.Fatalf("reminder source refs=%#v", payload["source_refs"])
	}
	ref := refs[0].(map[string]any)
	if len(ref) != 5 || ref["type"] != "commercial_commitment" || ref["id"] != "commitment" || ref["client_id"] != "" || ref["event_role"] != "renewal" || ref["source_revision"] != float64(2) {
		t.Fatalf("reminder source ref=%#v", ref)
	}
}

func TestCalendarReminderClaimLosingRevalidationDoesNotEmit(t *testing.T) {
	tx := &fakeSalesTx{zeroRowsAt: 1}
	repository := NewCalendarNotificationRepository(&fakeSalesDB{tx: tx}, func() string { return "00000000-0000-4000-8000-000000000099" })
	fact := calendar.ReminderFact{MSPID: "msp", ProjectionID: "projection", OccurrenceID: "occurrence", SourceRevision: 2, Threshold: "24h", DueAt: time.Now(), Timezone: "UTC", RecipientID: "tech", EvaluatedAt: time.Now()}
	owned, err := repository.ClaimReminder(context.Background(), "projection:occurrence:24h:2", fact)
	if err != nil || owned || !tx.rolledBack || tx.committed || len(tx.queries) != 1 || strings.Contains(tx.queries[0], "event_outbox") {
		t.Fatalf("owned=%v err=%v committed=%v rolledBack=%v queries=%v", owned, err, tx.committed, tx.rolledBack, tx.queries)
	}
}

func TestCalendarNotificationSourceAuthorizationUsesTypedSourceCapability(t *testing.T) {
	db := &fakeSalesDB{queryRow: fakeRow{scan: func(values ...any) { *(values[0].(*bool)) = true }}}
	repository := NewCalendarNotificationRepository(db, func() string { return "event" })
	allowed, err := repository.CanViewCalendarSource(context.Background(), "00000000-0000-4000-8000-000000000002", calendar.SourceRef{
		MSPID: "00000000-0000-4000-8000-000000000001", ClientID: "00000000-0000-4000-8000-000000000003",
		Type: "task", ID: "00000000-0000-4000-8000-000000000004",
	})
	if err != nil || !allowed {
		t.Fatalf("allowed=%v err=%v", allowed, err)
	}
	if !strings.Contains(db.query, "FROM tasks task") || !strings.Contains(db.query, "work_record.read") || strings.Contains(db.query, "capability='calendar.read'") {
		t.Fatalf("source authorization did not use typed task capability: %s", db.query)
	}
}
