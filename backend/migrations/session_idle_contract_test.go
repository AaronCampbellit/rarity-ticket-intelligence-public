package migrations

import (
	"strings"
	"testing"
)

func TestSessionIdleMigrationAddsDurableIdleDeadline(t *testing.T) {
	sql, err := FS.ReadFile("000054_session_idle_controls.sql")
	if err != nil {
		t.Fatalf("read session idle migration: %v", err)
	}
	body := string(sql)
	for _, fragment := range []string{
		"ADD COLUMN idle_expires_at timestamptz",
		"ADD COLUMN idle_timeout_seconds integer",
		"idle_expires_at <= expires_at",
	} {
		if !strings.Contains(body, fragment) {
			t.Errorf("session idle migration missing %q", fragment)
		}
	}
}
