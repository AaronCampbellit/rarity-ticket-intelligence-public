package migrations

import (
	"strings"
	"testing"
)

func TestWebhookMigrationDefinesScopedConnectionsReplayAndHistory(t *testing.T) {
	body, err := FS.ReadFile("000013_webhooks.sql")
	if err != nil {
		t.Fatalf("read webhook migration: %v", err)
	}
	required := []string{
		"CREATE TABLE webhook_connections",
		"secret_ref text NOT NULL",
		"CREATE TABLE webhook_replay_claims",
		"PRIMARY KEY (connection_id, event_id)",
		"CREATE TABLE webhook_delivery_attempts",
		"CHECK (state IN ('succeeded', 'failed'))",
		"CHECK (attempt > 0)",
		"FOREIGN KEY (client_id, msp_id)",
	}
	for _, fragment := range required {
		if !strings.Contains(string(body), fragment) {
			t.Errorf("webhook migration missing %q", fragment)
		}
	}
}

func TestOutboundWebhookRuntimeMigrationDefinesDurablePlanLeaseAndRetry(t *testing.T) {
	body, err := FS.ReadFile("000038_outbound_webhook_runtime.sql")
	if err != nil {
		t.Fatalf("read outbound webhook runtime migration: %v", err)
	}
	required := []string{
		"CREATE TABLE webhook_event_plans",
		"event_id uuid PRIMARY KEY REFERENCES event_outbox(event_id)",
		"CREATE TABLE webhook_event_deliveries",
		"PRIMARY KEY (connection_id, event_id)",
		"retry_window_seconds integer NOT NULL",
		"attempt_count integer NOT NULL DEFAULT 0",
		"next_attempt_at timestamptz NOT NULL",
		"lease_until timestamptz",
		"CHECK (state IN ('pending', 'delivered', 'failed'))",
		"CREATE INDEX webhook_event_deliveries_pending_idx",
	}
	for _, fragment := range required {
		if !strings.Contains(string(body), fragment) {
			t.Errorf("outbound webhook runtime migration missing %q", fragment)
		}
	}
}
