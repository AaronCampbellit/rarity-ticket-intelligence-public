package migrations

import (
	"strings"
	"testing"
)

func TestIntakeMigrationDefinesIdempotentQuarantinableEvents(t *testing.T) {
	body, err := FS.ReadFile("000015_inbound_intake.sql")
	if err != nil {
		t.Fatalf("read inbound intake migration: %v", err)
	}
	required := []string{
		"CREATE TABLE forwarding_intake_connections",
		"allowed_sender_domains text[] NOT NULL",
		"CREATE TABLE inbound_events",
		"UNIQUE (msp_id, source, external_id)",
		"raw_payload_ref text NOT NULL",
		"authentication_result text NOT NULL",
		"processing_state text NOT NULL",
		"quarantine_reason text",
		"CREATE TABLE inbound_event_attempts",
	}
	for _, fragment := range required {
		if !strings.Contains(string(body), fragment) {
			t.Errorf("intake migration missing %q", fragment)
		}
	}
}

func TestForwardingRuntimeMigrationDefinesConnectionScopeAndDurableRateLimit(t *testing.T) {
	body, err := FS.ReadFile("000039_forwarding_runtime.sql")
	if err != nil {
		t.Fatalf("read forwarding runtime migration: %v", err)
	}
	required := []string{
		"ADD COLUMN forwarding_connection_id uuid",
		"REFERENCES forwarding_intake_connections(id, msp_id)",
		"CREATE UNIQUE INDEX inbound_events_forwarding_external_id_idx",
		"(forwarding_connection_id, external_id)",
		"source = 'forwarded_email'",
		"CREATE TABLE forwarding_rate_limit_windows",
		"PRIMARY KEY (connection_id, sender_key, window_started_at)",
		"message_count integer NOT NULL",
	}
	for _, fragment := range required {
		if !strings.Contains(string(body), fragment) {
			t.Errorf("forwarding runtime migration missing %q", fragment)
		}
	}
}
