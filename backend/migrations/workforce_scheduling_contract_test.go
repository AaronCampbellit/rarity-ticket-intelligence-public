package migrations

import (
	"strings"
	"testing"
)

func TestWorkforceSchedulingMigrationContract(t *testing.T) {
	body := calendarMigrationBody(t, "000092_workforce_scheduling.sql")
	for _, fragment := range []string{
		"CREATE TABLE technician_schedule_versions",
		"CREATE TABLE technician_schedule_windows",
		"CREATE TABLE technician_schedule_exceptions",
		"CREATE TABLE pto_requests",
		"CHECK (state IN ('requested', 'approved', 'rejected', 'cancelled'))",
		"CREATE TABLE calendar_conflict_policies",
		"CHECK (approved_pto_rule IN ('informational', 'warning', 'overrideable_block', 'hard_block'))",
		"ordinary_overbooking_rule text NOT NULL DEFAULT 'overrideable_block'",
		"workforce_manager_id",
		"FOREIGN KEY (technician_id, msp_id) REFERENCES technicians(id, msp_id)",
		"CHECK (effective_through IS NULL OR effective_through >= effective_from)",
		"UNIQUE NULLS NOT DISTINCT",
		"(schedule_version_id, msp_id, technician_id, exception_on, starts_local, ends_local)",
		"calendar_timezone_is_valid(timezone)",
	} {
		if !strings.Contains(body, fragment) {
			t.Errorf("workforce migration missing %q", fragment)
		}
	}
}
