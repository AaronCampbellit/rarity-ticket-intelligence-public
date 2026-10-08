package migrations

import (
	"strings"
	"testing"
)

func TestDattoMigrationDefinesReadOnlySyncReconciliationAndAlertState(t *testing.T) {
	body, err := FS.ReadFile("000016_datto.sql")
	if err != nil {
		t.Fatalf("read Datto migration: %v", err)
	}
	required := []string{
		"CREATE TABLE datto_connections",
		"sync_interval interval NOT NULL DEFAULT interval '15 minutes'",
		"CREATE TABLE datto_site_mappings",
		"CREATE TABLE datto_sync_runs",
		"rate_limit_state jsonb NOT NULL",
		"CREATE TABLE datto_asset_snapshots",
		"UNIQUE (connection_id, external_asset_id)",
		"CREATE TABLE datto_reconciliation_decisions",
		"CREATE TABLE datto_alert_incidents",
		"UNIQUE (connection_id, external_alert_id)",
		"CREATE TABLE datto_alert_observations",
		"recovery boolean NOT NULL DEFAULT false",
	}
	for _, fragment := range required {
		if !strings.Contains(string(body), fragment) {
			t.Errorf("Datto migration missing %q", fragment)
		}
	}
	if strings.Contains(string(body), "write_back") {
		t.Fatal("Datto V1 migration exposes write-back state")
	}
}
