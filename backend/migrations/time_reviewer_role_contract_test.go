package migrations

import (
	"strings"
	"testing"
)

func TestTimeReviewerRoleMigrationSeedsScopedReviewCapabilities(t *testing.T) {
	sql, err := FS.ReadFile("000073_time_reviewer_role.sql")
	if err != nil {
		t.Fatalf("read time reviewer role migration: %v", err)
	}
	body := string(sql)
	for _, fragment := range []string{
		"'time_reviewer'",
		"'Time reviewer'",
		"'time_entry.read_scoped'",
		"'time_entry.amend'",
		"'time_entry.approve'",
		"'timesheet.review'",
		"'global_admin'",
		"'time_entry.update_own'",
		"'timesheet.read_own'",
		"ON CONFLICT DO NOTHING",
	} {
		if !strings.Contains(body, fragment) {
			t.Errorf("time reviewer migration missing %q", fragment)
		}
	}
	if !strings.Contains(
		body,
		"WHERE role.key = 'time_reviewer'\n  AND role.system_role\n  AND role.id = md5(role.msp_id::text || ':time_reviewer')::uuid",
	) {
		t.Error("capabilities must only attach to the deterministic system role")
	}
}
