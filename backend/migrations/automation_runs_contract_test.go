package migrations

import (
	"strings"
	"testing"
)

func TestAutomationRunMigrationDefinesIdempotentHistoryWaitsAndDeadLetters(t *testing.T) {
	body, err := FS.ReadFile("000019_automation_runs.sql")
	if err != nil {
		t.Fatalf("read automation run migration: %v", err)
	}
	required := []string{
		"CREATE TABLE automation_runs",
		"UNIQUE (msp_id, idempotency_key)",
		"input_snapshot jsonb NOT NULL",
		"causation_id uuid",
		"depth integer NOT NULL",
		"CREATE TABLE automation_step_runs",
		"CREATE TABLE automation_suspensions",
		"CREATE TABLE automation_dead_letters",
		"safe_message text NOT NULL",
		"CREATE TABLE automation_dead_letter_actions",
	}
	for _, fragment := range required {
		if !strings.Contains(string(body), fragment) {
			t.Errorf("automation run migration missing %q", fragment)
		}
	}
}

func TestAutomationExecutionRuntimeMigrationPlansAndLeasesEventJobs(t *testing.T) {
	body, err := FS.ReadFile("000046_automation_execution_runtime.sql")
	if err != nil {
		t.Fatalf("read automation execution runtime migration: %v", err)
	}
	sql := string(body)
	for _, required := range []string{
		"CREATE TABLE automation_event_plans",
		"event_id uuid PRIMARY KEY REFERENCES event_outbox(event_id)",
		"CREATE TABLE automation_execution_jobs",
		"ALTER TABLE automation_runs",
		"execution_lease_until timestamptz",
		"continuation_mode text",
		"continuation_step_id text",
		"ALTER TABLE automation_step_runs",
		"ADD COLUMN attempt integer",
		"ALTER TABLE automation_suspensions",
		"UNIQUE (event_id, automation_version_id)",
		"state text NOT NULL DEFAULT 'pending'",
		"attempt_count integer NOT NULL DEFAULT 0",
		"next_attempt_at timestamptz NOT NULL",
		"lease_until timestamptz",
		"automation_execution_jobs_due_idx",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("automation execution runtime migration missing %q", required)
		}
	}
}

func TestAutomationReplayRuntimeMigrationVersionsExecutionJobs(t *testing.T) {
	body, err := FS.ReadFile("000047_automation_replay_runtime.sql")
	if err != nil {
		t.Fatalf("read automation replay runtime migration: %v", err)
	}
	sql := string(body)
	for _, required := range []string{
		"ADD COLUMN replay_generation integer",
		"DROP CONSTRAINT automation_execution_jobs_event_id_automation_version_id_key",
		"UNIQUE (event_id, automation_version_id, replay_generation)",
		"CHECK (replay_generation >= 0)",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("automation replay migration missing %q", required)
		}
	}
}
