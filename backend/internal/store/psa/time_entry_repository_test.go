package psa

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/timeentries"
)

func TestCreateTaskOnlyTimeEntryValidatesProjectOrPhaseParent(t *testing.T) {
	tx := &fakeSalesTx{}
	at := time.Date(2026, time.July, 30, 15, 0, 0, 0, time.UTC)
	err := NewTimeEntryRepository(&fakeSalesDB{tx: tx}).CreateAtomic(
		context.Background(),
		timeentries.CreateMutation{
			Entry: timeentries.Entry{
				ID: "entry", MSPID: "msp", ClientID: "client",
				TaskID: "task", TechnicianID: "technician",
				StartedAt: at, EndedAt: at.Add(time.Hour),
				DurationSeconds: 3600, Version: 1, CreatedAt: at, CreatedBy: "actor",
			},
			Audit: validAudit(at, "time_entry.created", "time_entry", "entry"),
			Event: validEvent(at, "time_entry.created", "time_entry", "entry"),
		},
	)
	if err != nil {
		t.Fatalf("CreateAtomic() error = %v", err)
	}
	if !strings.Contains(tx.queries[0], "parent_type IN ('project', 'phase')") {
		t.Fatalf("task-only entry did not validate delivery parent: %s", tx.queries[0])
	}
}

func TestCreateTimeEntryValidatesWorkTaskTechnicianAndWritesFacts(t *testing.T) {
	tx := &fakeSalesTx{}
	repository := NewTimeEntryRepository(&fakeSalesDB{tx: tx})
	at := time.Date(2026, time.July, 30, 15, 0, 0, 0, time.UTC)
	err := repository.CreateAtomic(context.Background(), timeentries.CreateMutation{
		Entry: timeentries.Entry{
			ID: "entry", MSPID: "msp", ClientID: "client",
			WorkRecordID: "work", TaskID: "task", TechnicianID: "technician",
			StartedAt: at, EndedAt: at.Add(30 * time.Minute),
			DurationSeconds: 1800, Billable: true, Note: "Investigated",
			Version: 1, CreatedAt: at, CreatedBy: "actor",
		},
		Audit:       validAudit(at, "time_entry.created", "time_entry", "entry"),
		Event:       validEvent(at, "time_entry.created", "time_entry", "entry"),
		InitialTags: testInitialTags(at),
	})
	if err != nil {
		t.Fatalf("CreateAtomic() error = %v", err)
	}
	assertQueryOrder(
		t, tx.queries,
		"INSERT INTO time_entries", "INSERT INTO object_tag_assignments", "INSERT INTO tag_assignment_events",
		"INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
}

func TestCreateTimeEntryRejectsInaccessibleReferencesBeforeFacts(t *testing.T) {
	tx := &fakeSalesTx{zeroRowsAt: 1}
	err := NewTimeEntryRepository(&fakeSalesDB{tx: tx}).CreateAtomic(
		context.Background(),
		timeentries.CreateMutation{Entry: timeentries.Entry{
			ID: "entry", MSPID: "msp", ClientID: "client",
			WorkRecordID: "missing", TechnicianID: "technician",
		}},
	)
	if !errors.Is(err, scope.ErrNotFound) || len(tx.queries) != 1 || !tx.rolledBack {
		t.Fatalf("inaccessible references wrote facts: err=%v tx=%+v", err, tx)
	}
}
