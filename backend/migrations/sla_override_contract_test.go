package migrations

import (
	"strings"
	"testing"
)

func TestSLAOverrideMigrationPreservesImmutableBeforeAfterEvidence(t *testing.T) {
	sql, err := FS.ReadFile("000031_sla_overrides.sql")
	if err != nil {
		t.Fatalf("read SLA override migration: %v", err)
	}
	body := string(sql)
	for _, fragment := range []string{
		"CREATE TABLE sla_overrides",
		"work_record_id uuid NOT NULL",
		"sla_version_before bigint NOT NULL",
		"sla_version_after bigint NOT NULL",
		"response_due_at_before",
		"response_due_at_after",
		"resolution_due_at_before",
		"resolution_due_at_after",
		"reason text NOT NULL",
		"sla_overrides_immutable",
	} {
		if !strings.Contains(body, fragment) {
			t.Errorf("SLA override migration missing %q", fragment)
		}
	}
}
