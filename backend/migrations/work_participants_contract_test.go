package migrations

import (
	"strings"
	"testing"
)

func TestWorkParticipantMigrationPreservesRoleHistory(t *testing.T) {
	sql, err := FS.ReadFile("000028_work_record_participants.sql")
	if err != nil {
		t.Fatalf("read work participant migration: %v", err)
	}
	body := string(sql)
	for _, fragment := range []string{
		"CREATE TABLE work_record_participants",
		"role text NOT NULL",
		"removed_at timestamptz",
		"CREATE UNIQUE INDEX work_record_participants_active_idx",
		"role IN ('collaborator', 'reviewer', 'escalation', 'watcher')",
	} {
		if !strings.Contains(body, fragment) {
			t.Errorf("work participant migration missing %q", fragment)
		}
	}
}
