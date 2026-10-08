package integration

import (
	"strings"
	"testing"

	"github.com/rarity-ticket-intelligence/rarity/backend/migrations"
)

func TestPSAEventsHaveCanonicalDurableOutboxEnvelope(t *testing.T) {
	body, err := migrations.FS.ReadFile("000002_secure_platform_kernel.sql")
	if err != nil {
		t.Fatalf("read outbox migration: %v", err)
	}
	schema := string(body)
	for _, fragment := range []string{
		"CREATE TABLE event_outbox",
		"event_id uuid PRIMARY KEY",
		"event_type text NOT NULL",
		"schema_version integer NOT NULL CHECK (schema_version > 0)",
		"occurred_at timestamptz NOT NULL",
		"msp_id uuid NOT NULL",
		"client_id uuid",
		"actor_type text NOT NULL",
		"actor_id uuid NOT NULL",
		"subject_type text NOT NULL",
		"subject_id uuid NOT NULL",
		"subject_version bigint NOT NULL CHECK (subject_version > 0)",
		"correlation_id uuid NOT NULL",
		"source text NOT NULL",
		"data jsonb NOT NULL",
		"WHERE published_at IS NULL",
	} {
		if !strings.Contains(schema, fragment) {
			t.Errorf("canonical outbox schema missing %q", fragment)
		}
	}
}
