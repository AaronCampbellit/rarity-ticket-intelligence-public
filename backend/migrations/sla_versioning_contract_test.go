package migrations

import (
	"strings"
	"testing"
)

func TestSLAVersioningMigrationPreservesExactAppliedPolicy(t *testing.T) {
	sql, err := FS.ReadFile("000030_sla_versioning.sql")
	if err != nil {
		t.Fatalf("read SLA versioning migration: %v", err)
	}
	body := string(sql)
	for _, fragment := range []string{
		"CREATE TABLE business_calendar_versions",
		"CREATE TABLE sla_policy_versions",
		"business_calendar_versions_immutable",
		"sla_policy_versions_immutable",
		"ADD COLUMN calendar_version",
		"ADD COLUMN response_warning_at",
		"ADD COLUMN resolution_warning_at",
		"ADD COLUMN pause_states",
		"ADD COLUMN response_state",
		"ADD COLUMN resolution_state",
		"ADD COLUMN selection_trace",
		"FOREIGN KEY (policy_id, policy_version)",
		"FOREIGN KEY (calendar_id, calendar_version)",
		"ADD COLUMN stable_order",
		"ADD COLUMN fallback",
	} {
		if !strings.Contains(body, fragment) {
			t.Errorf("SLA versioning migration missing %q", fragment)
		}
	}
}
