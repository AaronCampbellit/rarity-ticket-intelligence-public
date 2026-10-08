package migrations

import (
	"strings"
	"testing"
)

func TestGlobalAdministratorTimeCapabilitiesUpgradeUsesCanonicalRoleKey(t *testing.T) {
	sql, err := FS.ReadFile("000076_global_admin_time_capabilities.sql")
	if err != nil {
		t.Fatalf("read Global Administrator time capability migration: %v", err)
	}
	body := string(sql)
	for _, fragment := range []string{
		"'global-admin'",
		"'time_entry.approve'",
		"'time_entry.create'",
		"'time_entry.export'",
		"'time_entry.read_scoped'",
		"'time_entry.update_own'",
		"'time_entry.amend'",
		"'timesheet.read_own'",
		"'timesheet.review'",
		"ON CONFLICT DO NOTHING",
	} {
		if !strings.Contains(body, fragment) {
			t.Errorf("Global Administrator upgrade missing %q", fragment)
		}
	}
	if strings.Contains(body, "'global_admin'") {
		t.Error("migration uses non-canonical Global Administrator role key")
	}
}
