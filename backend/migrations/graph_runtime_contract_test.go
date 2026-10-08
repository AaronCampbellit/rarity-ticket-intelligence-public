package migrations

import (
	"strings"
	"testing"
)

func TestGraphRuntimeMigrationProtectsMutableCursorAndLeasesRecoveryWork(t *testing.T) {
	body, err := FS.ReadFile("000040_graph_runtime.sql")
	if err != nil {
		t.Fatalf("read Graph runtime migration: %v", err)
	}
	sql := string(body)
	for _, required := range []string{
		"cursor_key_version integer",
		"cursor_nonce bytea",
		"cursor_ciphertext bytea",
		"lease_until timestamptz",
		"last_attempt_at timestamptz",
		"graph_delta_cursor_ciphertext_complete",
		"graph_delta_cursors_due_idx",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("Graph runtime migration missing %q", required)
		}
	}
}

func TestGraphNotificationRuntimeMigrationQueuesValidatedHintsDurably(t *testing.T) {
	body, err := FS.ReadFile("000041_graph_notification_runtime.sql")
	if err != nil {
		t.Fatalf("read Graph notification runtime migration: %v", err)
	}
	sql := string(body)
	for _, required := range []string{
		"CREATE TABLE graph_notification_hints",
		"UNIQUE (subscription_id, message_id)",
		"state text NOT NULL DEFAULT 'pending'",
		"lease_until timestamptz",
		"graph_notification_hints_due_idx",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("Graph notification migration missing %q", required)
		}
	}
}

func TestGraphSubscriptionRuntimeMigrationLeasesProvisioningAndEnforcesOneSubscription(t *testing.T) {
	body, err := FS.ReadFile("000045_graph_subscription_runtime.sql")
	if err != nil {
		t.Fatalf("read Graph subscription runtime migration: %v", err)
	}
	sql := string(body)
	for _, required := range []string{
		"client_state_secret_ref text",
		"subscription_lease_until timestamptz",
		"last_subscription_attempt_at timestamptz",
		"last_subscription_success_at timestamptz",
		"graph_subscription_lease_valid",
		"graph_subscriptions_connection_unique",
		"GROUP BY connection_id",
		"HAVING count(*) > 1",
		"UPDATE graph_mailbox_connections connection",
		"SET client_state_secret_ref = subscription.client_state_secret_ref",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("Graph subscription migration missing %q", required)
		}
	}
}
