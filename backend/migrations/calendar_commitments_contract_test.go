package migrations

import (
	"strings"
	"testing"
)

func TestCalendarCommitmentsMigrationContract(t *testing.T) {
	body := calendarMigrationBody(t, "000093_calendar_commitments.sql")
	for _, fragment := range []string{
		"CREATE TABLE project_milestones",
		"CREATE TABLE maintenance_windows",
		"CREATE TABLE maintenance_window_scopes",
		"CREATE TABLE commercial_commitments",
		"CHECK (commitment_type IN ('renewal', 'license'))",
		"CHECK (scope_type IN ('client', 'service', 'asset'))",
		"recurrence_rule jsonb",
		"version bigint NOT NULL DEFAULT 1 CHECK (version > 0)",
		"phase_id uuid",
		"name text NOT NULL CHECK",
		"due_on date NOT NULL",
		"priority text NOT NULL DEFAULT 'normal'",
		"conflict_policy text NOT NULL DEFAULT 'warning'",
		"CHECK (conflict_policy IN ('informational', 'warning', 'overrideable_block', 'hard_block'))",
		"effective_on date NOT NULL",
		"notice_on date",
		"renewal_on date",
		"expiration_on date NOT NULL",
		"quantity numeric(18,4) NOT NULL DEFAULT 0",
		"cost numeric(18,2)",
		"currency char(3)",
		"service_id uuid",
		"asset_id uuid",
		"contract_id uuid",
		"REFERENCES services(id, msp_id, client_id)",
		"REFERENCES assets(id, msp_id, client_id)",
		"REFERENCES contracts(id, msp_id, client_id)",
		"calendar_recurrence_rule_is_valid(recurrence_rule)",
	} {
		if !strings.Contains(body, fragment) {
			t.Errorf("commitment migration missing %q", fragment)
		}
	}
	if strings.Contains(body, "override_with_reason") {
		t.Error("maintenance conflict policy retained non-canonical override_with_reason")
	}
}
