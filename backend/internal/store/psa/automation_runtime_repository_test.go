package psa

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/automation"
)

func TestAutomationRuntimeRepositoryBeginsPublishedRunIdempotently(t *testing.T) {
	db := &fakeSalesDB{queryRow: fakeRow{scan: func(destinations ...any) {
		*(destinations[0].(*bool)) = true
		*(destinations[1].(*string)) = "run-id"
		*(destinations[2].(*automation.RunState)) = automation.RunRunning
		*(destinations[3].(*[]byte)) = []byte(`[]`)
	}}}
	repository := NewAutomationRuntimeRepository(
		db, func() string { return "step-id" },
	)
	startedAt := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)

	existing, err := repository.Begin(context.Background(), automation.Run{
		ID: "run-id", AutomationID: "automation-id", AutomationVersion: 2,
		EventID: "event-id", MSPID: "msp-id", ClientID: "client-id",
		IdempotencyKey: "event-id|automation-id|2", CausationID: "cause-id",
		Depth: 1, Attempt: 1, MaxAttempts: 3,
		State: automation.RunRunning, StartedAt: startedAt,
		InputSnapshot: map[string]string{"priority": "critical"},
	})

	if err != nil || existing != nil ||
		!strings.Contains(db.query, "ON CONFLICT (msp_id, idempotency_key)") ||
		!strings.Contains(db.query, "version.state = 'published'") ||
		!strings.Contains(db.query, "definition.enabled") ||
		!strings.Contains(db.query, "max_attempts") {
		t.Fatalf("Begin() existing=%+v error=%v query=%s", existing, err, db.query)
	}
}

func TestAutomationRuntimeRepositoryReturnsCompletedIdempotentRun(t *testing.T) {
	db := &fakeSalesDB{queryRow: fakeRow{scan: func(destinations ...any) {
		*(destinations[0].(*bool)) = false
		*(destinations[1].(*string)) = "existing-run-id"
		*(destinations[2].(*automation.RunState)) = automation.RunSucceeded
		*(destinations[3].(*[]byte)) = []byte(`["work-id"]`)
	}}}
	repository := NewAutomationRuntimeRepository(db, func() string { return "id" })

	existing, err := repository.Begin(context.Background(), automation.Run{
		ID: "new-run-id", AutomationID: "automation-id", AutomationVersion: 2,
		EventID: "event-id", MSPID: "msp-id", ClientID: "client-id",
		IdempotencyKey: "same-key", Depth: 1, Attempt: 1, MaxAttempts: 3,
		State: automation.RunRunning, StartedAt: time.Now(),
	})

	if err != nil || existing == nil ||
		existing.ID != "existing-run-id" ||
		existing.State != automation.RunSucceeded ||
		len(existing.ChangedObjectIDs) != 1 ||
		existing.ChangedObjectIDs[0] != "work-id" {
		t.Fatalf("Begin() existing=%+v error=%v", existing, err)
	}
}

func TestAutomationRuntimeRepositorySuspendsWaitAtomically(t *testing.T) {
	tx := &fakeSalesTx{}
	repository := NewAutomationRuntimeRepository(
		&fakeSalesDB{tx: tx}, sequenceRepositoryIDs("step-run-id", "suspension-id"),
	)
	at := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)

	err := repository.Suspend(context.Background(), automation.Suspension{
		RunID: "run-id", StepID: "wait-step", StartedAt: at, CompletedAt: at,
		ResumeAt:      at.Add(time.Minute),
		InputSnapshot: map[string]string{"priority": "critical"},
	})

	if err != nil {
		t.Fatalf("Suspend() error=%v", err)
	}
	assertQueryOrder(
		t, tx.queries,
		"INSERT INTO automation_step_runs",
		"INSERT INTO automation_suspensions",
		"UPDATE automation_runs",
	)
	if !strings.Contains(tx.queries[2], "state = 'waiting'") ||
		!strings.Contains(tx.queries[0], "attempt") ||
		!strings.Contains(tx.queries[2], "input_snapshot") ||
		!tx.committed || tx.rolledBack {
		t.Fatalf(
			"Suspend() queries=%v committed=%v rolledBack=%v",
			tx.queries, tx.committed, tx.rolledBack,
		)
	}
}

func TestAutomationRuntimeRepositoryRecordsStepAndCurrentSnapshotAtomically(t *testing.T) {
	tx := &fakeSalesTx{}
	repository := NewAutomationRuntimeRepository(
		&fakeSalesDB{tx: tx}, func() string { return "step-run-id" },
	)
	at := time.Now().UTC()
	err := repository.RecordStep(context.Background(), automation.StepRecord{
		RunID: "run-id", StepID: "assign", Attempt: 2,
		ActionKind: automation.ActionAssign, State: automation.RunSucceeded,
		StartedAt: at, CompletedAt: at,
		ChangedObjectIDs: []string{"work-id"},
		InputSnapshot:    map[string]string{"_subject_version": "2"},
	})
	if err != nil {
		t.Fatalf("RecordStep() error=%v", err)
	}
	assertQueryOrder(
		t, tx.queries,
		"INSERT INTO automation_step_runs",
		"UPDATE automation_runs",
	)
	if !strings.Contains(tx.queries[0], "attempt") ||
		!strings.Contains(tx.queries[1], "input_snapshot") ||
		!tx.committed || tx.rolledBack {
		t.Fatalf(
			"RecordStep() queries=%v committed=%v rolledBack=%v",
			tx.queries, tx.committed, tx.rolledBack,
		)
	}
}

func TestAutomationRuntimeRepositoryDeadLettersTerminalFailureAtomically(t *testing.T) {
	tx := &fakeSalesTx{}
	repository := NewAutomationRuntimeRepository(
		&fakeSalesDB{tx: tx}, func() string { return "id" },
	)
	at := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)

	err := repository.DeadLetter(
		context.Background(),
		automation.RunFailure{
			RunID: "run-id", FailedAt: at, ErrorCode: "action_failed",
			Attempt: 3, MaxAttempts: 3,
		},
		automation.DeadLetter{
			ID: "dead-id", RunID: "run-id", AutomationID: "automation-id",
			AutomationVersion: 2, EventID: "event-id", MSPID: "msp-id",
			ClientID: "client-id", CreatedAt: at, ErrorCode: "action_failed",
			SafeMessage: "automation action failed", State: "open",
		},
	)

	if err != nil {
		t.Fatalf("DeadLetter() error=%v", err)
	}
	assertQueryOrder(
		t, tx.queries,
		"UPDATE automation_runs",
		"INSERT INTO automation_dead_letters",
	)
	if !strings.Contains(tx.queries[0], "state = 'failed'") ||
		!tx.committed || tx.rolledBack {
		t.Fatalf(
			"DeadLetter() queries=%v committed=%v rolledBack=%v",
			tx.queries, tx.committed, tx.rolledBack,
		)
	}
}

func TestAutomationRuntimeRepositoryClaimsDueContinuationWithImmutableDefinition(t *testing.T) {
	definitionJSON := []byte(`{
	  "steps":[
	    {"id":"wait","kind":"wait","wait_seconds":60},
	    {"id":"assign","kind":"action","action":{
	      "kind":"assign","parameters":{"owner_id":"owner-id"}
	    }}
	  ]
	}`)
	db := &fakeSalesDB{queryRows: &fakeRows{scans: []func(...any){
		func(destinations ...any) {
			*(destinations[0].(*string)) = "run-id"
			*(destinations[1].(*string)) = "automation-id"
			*(destinations[2].(*string)) = "msp-id"
			*(destinations[3].(*int64)) = 2
			*(destinations[4].(*[]string)) = []string{"client-id"}
			*(destinations[5].(*[]string)) = []string{"work_record.assign"}
			*(destinations[6].(*string)) = "work_record.created"
			*(destinations[7].(*[]byte)) = definitionJSON
			*(destinations[8].(*string)) = "event-id"
			*(destinations[9].(*string)) = "client-id"
			*(destinations[10].(*string)) = "cause-id"
			*(destinations[11].(*int)) = 1
			*(destinations[12].(*[]byte)) = []byte(
				`{"_subject_id":"work-id","_subject_version":"2"}`,
			)
			*(destinations[13].(*[]byte)) = []byte(`["work-id"]`)
			*(destinations[14].(*int)) = 1
			*(destinations[15].(*int)) = 3
			*(destinations[16].(*automation.ContinuationMode)) =
				automation.ContinuationAfterWait
			*(destinations[17].(*string)) = "wait"
		},
	}}}
	repository := NewAutomationRuntimeRepository(db, func() string { return "id" })

	jobs, err := repository.ClaimContinuations(
		context.Background(), 25, time.Now().UTC(), 5*time.Minute,
	)

	if err != nil || len(jobs) != 1 ||
		jobs[0].Command.Run.ID != "run-id" ||
		jobs[0].Command.Mode != automation.ContinuationAfterWait ||
		jobs[0].Command.StepID != "wait" ||
		jobs[0].Command.Definition.Version != 2 ||
		jobs[0].Command.Run.InputSnapshot["_subject_id"] != "work-id" ||
		!strings.Contains(db.query, "state IN ('waiting', 'retrying', 'running')") ||
		!strings.Contains(db.query, "FOR UPDATE OF run SKIP LOCKED") ||
		!strings.Contains(db.query, "execution_lease_until") {
		t.Fatalf("ClaimContinuations() jobs=%+v error=%v query=%s", jobs, err, db.query)
	}
}

func TestAutomationRuntimeRepositoryReleasesContinuationWithBackoffAtomically(t *testing.T) {
	tx := &fakeSalesTx{}
	repository := NewAutomationRuntimeRepository(
		&fakeSalesDB{tx: tx}, func() string { return "id" },
	)
	now := time.Now().UTC()
	err := repository.ReleaseContinuation(
		context.Background(),
		automation.ContinuationRelease{
			RunID: "run-id", ReleasedAt: now,
			RetryAt:   now.Add(time.Minute),
			ErrorCode: "continuation_failed",
		},
	)
	if err != nil {
		t.Fatalf("ReleaseContinuation() error=%v", err)
	}
	assertQueryOrder(
		t, tx.queries,
		"UPDATE automation_suspensions",
		"UPDATE automation_runs",
	)
	if !strings.Contains(tx.queries[1], "execution_lease_until = $3") ||
		!strings.Contains(tx.queries[1], "continuation_mode") ||
		!tx.committed || tx.rolledBack {
		t.Fatalf(
			"ReleaseContinuation() queries=%v committed=%v rolledBack=%v",
			tx.queries, tx.committed, tx.rolledBack,
		)
	}
}

func sequenceRepositoryIDs(values ...string) func() string {
	index := 0
	return func() string {
		value := values[index]
		index++
		return value
	}
}
