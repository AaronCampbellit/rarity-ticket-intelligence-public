package migrations

import (
	"strings"
	"testing"
)

func TestTeamsAndIntegrationHealthMigrationDefinesEncryptedDeliveryEvidence(t *testing.T) {
	body, err := FS.ReadFile("000017_teams_health.sql")
	if err != nil {
		t.Fatalf("read Teams health migration: %v", err)
	}
	required := []string{
		"CREATE TABLE teams_connections",
		"webhook_secret_ref text NOT NULL",
		"CREATE TABLE teams_delivery_attempts",
		"first_attempt_at timestamptz NOT NULL",
		"CHECK (state IN ('delivered', 'retrying', 'failed'))",
		"CREATE VIEW integration_health_signals",
		"pending_failures",
		"last_success_at",
		"last_error_code",
	}
	for _, fragment := range required {
		if !strings.Contains(string(body), fragment) {
			t.Errorf("Teams/health migration missing %q", fragment)
		}
	}
}

func TestTeamsConnectionLifecycleMigrationDefinesProtectedGUISecrets(t *testing.T) {
	body, err := FS.ReadFile("000050_teams_connection_lifecycle.sql")
	if err != nil {
		t.Fatalf("read Teams connection lifecycle migration: %v", err)
	}
	for _, fragment := range []string{
		"ALTER COLUMN webhook_secret_ref DROP NOT NULL",
		"webhook_secret_version integer",
		"webhook_secret_nonce bytea",
		"webhook_secret_ciphertext bytea",
		"last_tested_at timestamptz",
		"ADD COLUMN connection_version bigint NOT NULL DEFAULT 1",
		"CHECK (connection_version > 0)",
		"RARITY_TEAMS_WEBHOOK_",
		"cannot roll back Teams connection lifecycle while encrypted GUI credentials exist",
		"DROP CONSTRAINT teams_connections_secret_source_check",
		"DROP COLUMN connection_version",
	} {
		if !strings.Contains(string(body), fragment) {
			t.Errorf("Teams lifecycle migration missing %q", fragment)
		}
	}
}
