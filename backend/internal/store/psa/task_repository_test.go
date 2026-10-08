package psa

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tasks"
)

func TestCreateOpportunityTaskValidatesOpportunityParent(t *testing.T) {
	tx := &fakeSalesTx{}
	err := NewTaskRepository(&fakeSalesDB{tx: tx}).CreateAtomic(
		context.Background(),
		tasks.CreateMutation{Task: tasks.Task{
			ID: "task", MSPID: "msp", ClientID: "client",
			Parent: tasks.Ref{
				Type: tasks.ParentOpportunity, ID: "opportunity",
				MSPID: "msp", ClientID: "client",
			},
			Title: "Prepare proposal", Status: "open", Position: 1,
			Version: 1, CreatedBy: "actor",
		}},
	)
	if err != nil {
		t.Fatalf("CreateAtomic() error = %v", err)
	}
	if !strings.Contains(tx.queries[1], "FROM opportunities") {
		t.Fatalf("opportunity parent was not validated: %s", tx.queries[1])
	}
}

func TestCreateProjectTaskValidatesProjectParent(t *testing.T) {
	tx := &fakeSalesTx{}
	err := NewTaskRepository(&fakeSalesDB{tx: tx}).CreateAtomic(
		context.Background(),
		tasks.CreateMutation{Task: tasks.Task{
			ID: "task", MSPID: "msp", ClientID: "client",
			Parent: tasks.Ref{
				Type: tasks.ParentProject, ID: "project",
				MSPID: "msp", ClientID: "client",
			},
			Title: "Schedule kickoff", Status: "open", Position: 1,
			OwnerID: "technician", EstimateMinutes: 60,
			Version: 1, CreatedBy: "actor",
		}},
	)
	if err != nil {
		t.Fatalf("CreateAtomic() error = %v", err)
	}
	if !strings.Contains(tx.queries[1], "FROM projects") ||
		!strings.Contains(tx.queries[1], "FROM technicians") {
		t.Fatalf("Project parent or owner was not validated: %s", tx.queries[1])
	}
}

func TestCreateTaskValidatesWorkAndParentThenWritesFacts(t *testing.T) {
	tx := &fakeSalesTx{}
	at := time.Date(2026, time.July, 30, 17, 0, 0, 0, time.UTC)
	err := NewTaskRepository(&fakeSalesDB{tx: tx}).CreateAtomic(
		context.Background(),
		tasks.CreateMutation{
			Task: tasks.Task{
				ID: "task", MSPID: "msp", ClientID: "client",
				Parent: tasks.Ref{
					Type: tasks.ParentWorkRecord, ID: "work",
					MSPID: "msp", ClientID: "client",
				},
				WorkRecordID: "work", ParentTaskID: "parent",
				Title: "Collect logs", Status: "open", Position: 2,
				Version: 1, CreatedBy: "actor",
			},
			Audit:       validAudit(at, "task.created", "task", "task"),
			Event:       validEvent(at, "task.created", "task", "task"),
			InitialTags: testInitialTags(at),
		},
	)
	if err != nil {
		t.Fatalf("CreateAtomic() error = %v", err)
	}
	assertQueryOrder(
		t, tx.queries,
		"FROM client_organizations", "INSERT INTO tasks",
		"INSERT INTO object_tag_assignments", "INSERT INTO tag_assignment_events",
		"INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
	if !strings.Contains(
		tx.queries[1],
		"CASE WHEN $4 = 'work_record' THEN $5::uuid ELSE NULL END",
	) || !strings.Contains(tx.queries[1], "WHERE id = $5::uuid") {
		t.Fatalf("task parent id is not typed as UUID: %s", tx.queries[1])
	}
}

func TestCreateTaskRejectsInaccessibleWorkOrParentBeforeFacts(t *testing.T) {
	tx := &fakeSalesTx{zeroRowsAt: 2}
	err := NewTaskRepository(&fakeSalesDB{tx: tx}).CreateAtomic(
		context.Background(),
		tasks.CreateMutation{Task: tasks.Task{
			ID: "task", MSPID: "msp", ClientID: "client",
			WorkRecordID: "missing", ParentTaskID: "parent",
		}},
	)
	if !errors.Is(err, scope.ErrNotFound) || len(tx.queries) != 2 || !tx.rolledBack {
		t.Fatalf("inaccessible task parent wrote facts: err=%v tx=%+v", err, tx)
	}
}
