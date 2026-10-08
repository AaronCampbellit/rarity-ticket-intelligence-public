package migrations

import (
	"strings"
	"testing"
)

func TestGraphIntakeMigrationDefinesDurableRecoveryAndThreadingState(t *testing.T) {
	body, err := FS.ReadFile("000014_graph_intake.sql")
	if err != nil {
		t.Fatalf("read Graph intake migration: %v", err)
	}
	required := []string{
		"CREATE TABLE graph_mailbox_connections",
		"CREATE TABLE graph_subscriptions",
		"CREATE TABLE graph_delta_cursors",
		"UNIQUE (connection_id, folder_id)",
		"CREATE TABLE inbound_email_messages",
		"UNIQUE (connection_id, external_message_id)",
		"raw_mime_ref text NOT NULL",
		"attachment_refs jsonb NOT NULL",
		"retention_until timestamptz NOT NULL",
		"graph_conversation_id text",
		"internet_message_id text",
		"thread_match_method text NOT NULL",
	}
	for _, fragment := range required {
		if !strings.Contains(string(body), fragment) {
			t.Errorf("Graph intake migration missing %q", fragment)
		}
	}
}
