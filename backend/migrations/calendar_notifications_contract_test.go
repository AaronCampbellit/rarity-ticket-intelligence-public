package migrations

import (
	"strings"
	"testing"
)

func TestCalendarNotificationsMigrationContract(t *testing.T) {
	body := calendarMigrationBody(t, "000095_calendar_notifications.sql")
	for _, fragment := range []string{
		"CREATE TABLE calendar_notification_preference_sets",
		"PRIMARY KEY (msp_id, technician_id)",
		"FOREIGN KEY (technician_id, msp_id) REFERENCES technicians(id, msp_id)",
		"version bigint NOT NULL DEFAULT 1 CHECK (version > 0)",
		"CREATE TABLE calendar_notification_preference_rules",
		"CHECK (event_class IN ('calendar.schedule_changed'))",
		"CHECK (change_class IN ('schedule', 'pto', 'conflict', 'cancellation', 'reminder'))",
		"CHECK (urgency IN ('routine', 'important', 'urgent'))",
		"CHECK (channel IN ('in_app', 'email'))",
		"PRIMARY KEY (msp_id, technician_id, event_class, change_class, urgency, channel)",
		"ADD COLUMN source_revision bigint",
		"ADD COLUMN threshold text",
		"SET source_revision = projection.source_revision",
		"THEN '24h'",
		"ELSE fact.reminder_kind || ':' ||",
		"ALTER COLUMN source_revision SET NOT NULL",
		"ALTER COLUMN threshold SET NOT NULL",
		"CREATE UNIQUE INDEX calendar_reminder_facts_revision_dedupe_idx",
		"(msp_id, projection_id, occurrence_key, threshold, source_revision)",
	} {
		if !strings.Contains(body, fragment) {
			t.Errorf("calendar notifications migration missing %q", fragment)
		}
	}
}
