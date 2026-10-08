package migrations

import (
	"strings"
	"testing"
)

func TestGlobalAdministratorAssetCapabilityMigration(t *testing.T) {
	sql, err := FS.ReadFile("000069_global_admin_asset_capability.sql")
	if err != nil {
		t.Fatalf("read global administrator asset capability migration: %v", err)
	}
	body := string(sql)
	for _, fragment := range []string{
		"INSERT INTO role_capabilities",
		"'global-admin'",
		"'asset.create'",
		"ON CONFLICT DO NOTHING",
	} {
		if !strings.Contains(body, fragment) {
			t.Errorf("global administrator asset capability migration missing %q", fragment)
		}
	}
}
