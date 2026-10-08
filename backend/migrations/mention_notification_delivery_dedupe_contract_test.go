package migrations

import (
	"strings"
	"testing"
)

func TestMentionNotificationDeliveryDedupeMigrationUsesDisjointPartialIndexes(t *testing.T) {
	body := migrationBody(t, "000090_mention_notification_delivery_dedupe.sql")
	for _, fragment := range []string{
		"DROP CONSTRAINT notification_delivery_dedupe_uniq",
		"CREATE UNIQUE INDEX notification_delivery_legacy_dedupe_uniq",
		"WHERE recipient_technician_id IS NULL",
		"CREATE UNIQUE INDEX notification_delivery_mention_dedupe_uniq",
		"ON notification_deliveries (event_id, recipient_technician_id, channel)",
		"WHERE recipient_technician_id IS NOT NULL",
		"row_number() OVER",
		"PARTITION BY policy_id, event_id, channel, recipient_ref",
		"ADD CONSTRAINT notification_delivery_dedupe_uniq",
	} {
		if !strings.Contains(body, fragment) {
			t.Errorf("dedupe migration missing %q", fragment)
		}
	}
	if strings.Contains(body, "event_id, channel, recipient_ref, recipient_technician_id") {
		t.Error("mention uniqueness must not depend on policy_id or recipient_ref")
	}
}
