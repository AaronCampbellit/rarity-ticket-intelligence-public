package migrations

import (
	"strings"
	"testing"
)

func TestClientResourceMetadataMigrationPreservesAuthorityAndCriticality(t *testing.T) {
	body, err := FS.ReadFile("000027_client_resource_metadata.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	sql := string(body)
	for _, fragment := range []string{
		"ADD COLUMN source_system",
		"ADD COLUMN external_id",
		"ADD COLUMN authority",
		"authority IN ('discovered', 'technician_confirmed')",
		"ADD COLUMN criticality",
	} {
		if !strings.Contains(sql, fragment) {
			t.Errorf("client-resource metadata migration missing %q", fragment)
		}
	}
}

func TestClientResourceLifecycleMigrationConstrainsStatesWithoutRebindingHistory(t *testing.T) {
	body, err := FS.ReadFile("000097_client_resource_lifecycle.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	sql := string(body)
	for _, fragment := range []string{
		"locations_lifecycle_state_check",
		"contacts_lifecycle_state_check",
		"assets_lifecycle_state_check",
		"services_lifecycle_state_check",
		"contracts_lifecycle_state_check",
		"CHECK (lifecycle_state IN ('active', 'inactive')) NOT VALID",
		"VALIDATE CONSTRAINT locations_lifecycle_state_check",
		"VALIDATE CONSTRAINT contacts_lifecycle_state_check",
		"VALIDATE CONSTRAINT assets_lifecycle_state_check",
		"VALIDATE CONSTRAINT services_lifecycle_state_check",
		"VALIDATE CONSTRAINT contracts_lifecycle_state_check",
		"contacts_active_location_dependencies_idx",
		"assets_active_location_dependencies_idx",
		"WHERE lifecycle_state = 'active' AND location_id IS NOT NULL",
	} {
		if !strings.Contains(sql, fragment) {
			t.Errorf("client-resource lifecycle migration missing %q", fragment)
		}
	}
	for _, forbidden := range []string{
		"UPDATE work_records",
		"DELETE FROM work_records",
		"ON DELETE CASCADE",
	} {
		if strings.Contains(sql, forbidden) {
			t.Errorf("client-resource lifecycle migration changes historical work bindings with %q", forbidden)
		}
	}
}
