package migrations

import (
	"strings"
	"testing"
)

func TestWorkforceTimeReviewMigrationDefinesTrustedCaptureAndSnapshots(t *testing.T) {
	body, err := FS.ReadFile("000072_workforce_time_review.sql")
	if err != nil {
		t.Fatalf("read workforce time review migration: %v", err)
	}
	sql := string(body)
	for _, fragment := range []string{
		"CREATE TABLE labor_roles",
		"CREATE TABLE labor_role_versions",
		"CREATE TABLE ticket_timer_sessions",
		"state IN ('running', 'stopped', 'consumed', 'discarded')",
		"CREATE UNIQUE INDEX ticket_timer_one_running_per_ticket_technician",
		"ADD COLUMN labor_role_version_id",
		"ADD COLUMN internal_cost_minor",
		"ADD COLUMN bill_rate_minor",
		"ADD COLUMN rate_currency",
		"CREATE TABLE time_entry_amendments",
		"CREATE TRIGGER time_entry_amendments_immutable",
		"ADD COLUMN reversed_at",
		"ADD COLUMN replacement_time_entry_id",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("workforce time review migration missing %q", fragment)
		}
	}
}
