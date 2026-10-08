package migrations

import (
	"strings"
	"testing"
)

func calendarMigrationBody(t *testing.T, name string) string {
	t.Helper()
	body, err := FS.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(body)
}

func TestUnifiedCalendarCoreMigrationContract(t *testing.T) {
	body := calendarMigrationBody(t, "000091_unified_calendar_core.sql")
	for _, fragment := range []string{
		"CREATE TABLE calendar_event_projections",
		"UNIQUE (msp_id, source_type, source_id, event_role, source_role_key)",
		"CREATE TABLE calendar_recurrence_exceptions",
		"CREATE TABLE calendar_dependencies",
		"CHECK (relationship_type IN ('finish_to_start', 'start_to_start', 'finish_to_finish'))",
		"CREATE TABLE calendar_scheduling_proposals",
		"CREATE TABLE calendar_proposal_changes",
		"CREATE TABLE calendar_custom_date_fields",
		"CREATE TABLE object_custom_date_values",
		"CREATE TABLE calendar_projection_cursors",
		"CREATE TABLE calendar_live_changes",
		"health_state text NOT NULL DEFAULT 'on_track'",
		"health_reasons jsonb NOT NULL DEFAULT '[]'::jsonb",
		"health_rule_version bigint NOT NULL DEFAULT 2",
		"health_evaluated_at timestamptz",
		"CREATE TABLE calendar_health_event_claims",
		"PRIMARY KEY (source_event_id, rule_version)",
		"CREATE INDEX calendar_live_changes_source_revision_idx",
		"CREATE TABLE calendar_reminder_facts",
		"CREATE TABLE calendar_domain_requests",
		"ALTER TABLE work_records",
		"ADD COLUMN scheduled_starts_at",
		"ALTER TABLE tasks",
		"source_revision bigint NOT NULL CHECK (source_revision > 0)",
		"authorization_context_hash bytea NOT NULL",
		"conflict_policy_version bigint NOT NULL CHECK (conflict_policy_version > 0)",
		"capacity_bearing boolean NOT NULL DEFAULT false",
		"CREATE FUNCTION calendar_timezone_is_valid",
		"CREATE FUNCTION calendar_recurrence_rule_is_valid",
		"CREATE FUNCTION calendar_recurrence_until_is_valid",
		"client_scope_key text GENERATED ALWAYS AS",
		"WHEN client_id IS NULL THEN 'global'",
		"ELSE 'client:' || client_id::text",
		"FOREIGN KEY (projection_id, msp_id, client_scope_key)",
		"FOREIGN KEY (proposal_id, msp_id, client_scope_key)",
		"NOT (rule ? 'interval')",
		"CHECK (object_type IN ('work_record', 'task', 'project', 'asset', 'knowledge_article', 'time_entry'))",
		"UNIQUE (id, msp_id, object_type)",
		"FOREIGN KEY (field_id, msp_id, object_type)",
		"event_role text NOT NULL",
		"category text NOT NULL",
		"color_category text NOT NULL",
		"calendar_mode text NOT NULL",
		"timezone_source text",
		"planned_effort_source text",
		"planned_effort_source IS NOT NULL",
		"'calendar.read'",
		"'calendar.schedule'",
		"'calendar.workforce.manage'",
		"'calendar.commitment.manage'",
		"'calendar.policy.manage'",
		"'calendar.ai.recommend'",
		"ON CONFLICT DO NOTHING",
	} {
		if !strings.Contains(body, fragment) {
			t.Errorf("calendar core migration missing %q", fragment)
		}
	}
	for _, sourceType := range []string{
		"'work_record'", "'task'", "'project'", "'phase'", "'milestone'",
		"'technician_schedule'", "'pto'", "'maintenance_window'",
		"'commercial_commitment'", "'custom_date'",
	} {
		if !strings.Contains(body, sourceType) {
			t.Errorf("calendar core migration missing canonical source %q", sourceType)
		}
	}
	for _, forbidden := range []string{
		"start_to_finish",
		"CREATE TABLE calendar_events",
		"00000000-0000-0000-0000-000000000000'::uuid",
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("calendar core migration contains forbidden %q", forbidden)
		}
	}
}

func TestCalendarRecurrenceSplitMigrationAddsResourcePlanSourceConstraints(t *testing.T) {
	body := calendarMigrationBody(t, "000094_calendar_recurrence_series_splits.sql")
	for _, fragment := range []string{
		"DROP CONSTRAINT calendar_event_projections_source_type_check",
		"ADD CONSTRAINT calendar_event_projections_source_type_check",
		"DROP CONSTRAINT calendar_proposal_changes_source_type_check",
		"ADD CONSTRAINT calendar_proposal_changes_source_type_check",
		"'resource_plan'",
	} {
		if !strings.Contains(body, fragment) {
			t.Errorf("calendar recurrence/resource-plan migration missing %q", fragment)
		}
	}
	if count := strings.Count(body, "ADD CONSTRAINT calendar_event_projections_source_type_check"); count != 1 {
		t.Errorf("projection allowlist must not contract on down; ADD count=%d", count)
	}
	if count := strings.Count(body, "ADD CONSTRAINT calendar_proposal_changes_source_type_check"); count != 1 {
		t.Errorf("proposal-change allowlist must not contract on down; ADD count=%d", count)
	}
}
