package migrations

import (
	"strings"
	"testing"
)

func TestInternalMentionsMigrationContract(t *testing.T) {
	body := migrationBody(t, "000089_internal_mentions.sql")
	for _, fragment := range []string{
		"CREATE TABLE internal_collaboration_sources",
		"CHECK (parent_type IN ('work_record', 'task', 'project'))",
		"CHECK (source_kind IN ('details', 'comment', 'note'))",
		"CREATE TABLE mention_occurrences",
		"token_id uuid NOT NULL",
		"authorization_revision bigint NOT NULL",
		"CREATE TABLE mention_access_revisions",
		"CREATE TABLE mention_access_revision_history",
		"CREATE TABLE mention_role_assignment_history",
		"CREATE TABLE mention_role_capability_history",
		"CREATE TABLE mention_access_loss_markers",
		"CREATE FUNCTION begin_mention_authorization_revision",
		"CREATE FUNCTION lock_mention_authorization_revision",
		"CREATE FUNCTION mention_has_effective_access_at_revision",
		"CREATE TRIGGER role_assignments_mention_history",
		"CREATE TRIGGER role_capabilities_mention_history",
		"CREATE TABLE mention_recipient_resolutions",
		"CREATE TABLE mention_items",
		"UNIQUE (msp_id, recipient_id, parent_type, parent_id)",
		"CHECK (state IN ('unread', 'read', 'archived'))",
		"CREATE TABLE notification_recipient_preferences",
		"CREATE TABLE mention_access_invalidations",
		"snapshot_occurrence_id uuid NOT NULL",
		"access_loss_confirmed boolean NOT NULL DEFAULT false",
		"CREATE FUNCTION capture_mention_access_loss_marker",
		"CREATE TRIGGER event_outbox_capture_mention_access_loss_marker",
		"mention_invalidation_sequence bigint GENERATED ALWAYS AS IDENTITY",
		"CREATE TABLE mention_invalidation_event_cursor",
		"CREATE TABLE mention_invalidation_event_claims",
		"CREATE FUNCTION queue_mention_invalidation_event",
		"CREATE TRIGGER event_outbox_queue_mention_invalidation",
		"AFTER INSERT ON event_outbox",
		"CREATE INDEX event_outbox_mention_invalidation_idx",
		"ON event_outbox (mention_invalidation_sequence);",
		"CREATE INDEX mention_items_invalidation_scan_idx",
		"CREATE INDEX mention_access_invalidations_confirmed_item_idx",
		"WHERE access_loss_confirmed",
		"CREATE INDEX mention_access_loss_markers_scope_idx",
		"lease_token uuid",
		"lease_until timestamptz",
		"completed_at timestamptz",
		"UNIQUE (causation_id, mention_item_id)",
		"ON UPDATE CASCADE",
		"CHECK (mention_occurrence_id IS NULL OR client_id IS NOT NULL)",
	} {
		if !strings.Contains(body, fragment) {
			t.Errorf("mention migration missing %q", fragment)
		}
	}
	for _, forbidden := range []string{
		"token jsonb NOT NULL",
		"target_label text NOT NULL",
		"CREATE FUNCTION materialize_mention_access_loss",
		"CREATE TRIGGER event_outbox_materialize_mention_access_loss",
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("mention migration retains content-bearing occurrence field %q", forbidden)
		}
	}
}

func TestInternalMentionLossMarkerDoesNotFanOutItemsInTheEventTransaction(t *testing.T) {
	body := migrationBody(t, "000089_internal_mentions.sql")
	start := strings.Index(body, "CREATE FUNCTION capture_mention_access_loss_marker")
	if start < 0 {
		t.Fatal("compact loss-marker trigger function is missing")
	}
	end := strings.Index(body[start:], "CREATE TRIGGER event_outbox_capture_mention_access_loss_marker")
	if end < 0 {
		t.Fatal("compact loss-marker trigger is missing")
	}
	functionBody := body[start : start+end]
	if strings.Contains(functionBody, "mention_items") || strings.Contains(functionBody, "mention_access_invalidations") {
		t.Fatalf("event transaction still fans out mention items: %s", functionBody)
	}
	for _, fragment := range []string{"INSERT INTO mention_access_loss_markers", "authorization_revision", "ON CONFLICT (event_id) DO NOTHING"} {
		if !strings.Contains(functionBody, fragment) {
			t.Errorf("compact marker function missing %q", fragment)
		}
	}
}
