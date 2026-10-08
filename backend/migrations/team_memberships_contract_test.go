package migrations

import (
	"strings"
	"testing"
)

func migrationBody(t *testing.T, name string) string {
	t.Helper()
	body, err := FS.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(body)
}

func TestTeamMembershipMigrationContract(t *testing.T) {
	body := migrationBody(t, "000088_team_memberships.sql")
	for _, fragment := range []string{
		"CREATE TABLE team_memberships",
		"FOREIGN KEY (team_id, msp_id) REFERENCES teams",
		"FOREIGN KEY (technician_id, msp_id) REFERENCES technicians",
		"CHECK (lifecycle_state IN ('active', 'inactive'))",
		"'mention.create'",
		"'mention.read'",
	} {
		if !strings.Contains(body, fragment) {
			t.Errorf("team membership migration missing %q", fragment)
		}
	}
}
