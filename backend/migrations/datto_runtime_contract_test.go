package migrations

import (
	"strings"
	"testing"
)

func TestDattoRuntimeMigrationEncryptsCursorAndPersistsReviewCandidates(t *testing.T) {
	body, err := FS.ReadFile("000042_datto_runtime.sql")
	if err != nil {
		t.Fatalf("read Datto runtime migration: %v", err)
	}
	sql := string(body)
	for _, required := range []string{
		"cursor_key_version integer",
		"cursor_nonce bytea",
		"cursor_ciphertext bytea",
		"lease_until timestamptz",
		"CREATE TABLE datto_reconciliation_candidates",
		"evidence jsonb NOT NULL",
		"UNIQUE (snapshot_id, rarity_asset_id)",
		"datto_connections_due_idx",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("Datto runtime migration missing %q", required)
		}
	}
}

func TestDattoManagementMigrationSupportsManualQueueAndScopedProgress(t *testing.T) {
	body, err := FS.ReadFile("000043_datto_management_runtime.sql")
	if err != nil {
		t.Fatalf("read Datto management runtime migration: %v", err)
	}
	sql := string(body)
	for _, required := range []string{
		"manual_requested_at timestamptz",
		"manual_requested_by uuid",
		"manual_request_id uuid",
		"UNIQUE (manual_request_id)",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("Datto management migration missing %q", required)
		}
	}
}

func TestDattoAlertQueueMigrationDefinesDurableLeasedIngestion(t *testing.T) {
	body, err := FS.ReadFile("000044_datto_alert_queue.sql")
	if err != nil {
		t.Fatalf("read Datto alert queue migration: %v", err)
	}
	sql := string(body)
	for _, required := range []string{
		"CREATE TABLE datto_alert_queue",
		"processing_state text NOT NULL DEFAULT 'pending'",
		"'blocked_mapping'",
		"lease_until timestamptz",
		"connection_id, external_alert_id, alert_state, observed_at",
		"DROP TABLE datto_alert_queue",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("Datto alert queue migration missing %q", required)
		}
	}
}
