package migrations

import (
	"strings"
	"testing"
)

func TestRoutingMigrationVersionsRulesAndPersistsDecisions(t *testing.T) {
	sql, err := FS.ReadFile("000029_routing_rules.sql")
	if err != nil {
		t.Fatalf("read routing migration: %v", err)
	}
	body := string(sql)
	for _, fragment := range []string{
		"CREATE TABLE routing_rule_sets",
		"CREATE TABLE routing_rule_set_versions",
		"CREATE TABLE routing_rule_versions",
		"CREATE TABLE work_record_routing",
		"REFERENCES queues(id, msp_id)",
		"REFERENCES client_organizations(id, msp_id)",
		"routing_rule_versions_immutable",
		"UNIQUE (rule_set_id, version, position)",
	} {
		if !strings.Contains(body, fragment) {
			t.Errorf("routing migration missing %q", fragment)
		}
	}
}
