package migrations

import (
	"strings"
	"testing"
)

func TestGlobalAdministratorDattoReconciliationCapabilityMigration(t *testing.T) {
	sql, err := FS.ReadFile("000071_global_admin_datto_reconciliation.sql")
	if err != nil {
		t.Fatalf("read global administrator Datto capability migration: %v", err)
	}
	body := string(sql)
	for _, fragment := range []string{
		"INSERT INTO role_capabilities",
		"'global-admin'",
		"'integration.datto.reconcile'",
		"ON CONFLICT DO NOTHING",
	} {
		if !strings.Contains(body, fragment) {
			t.Errorf(
				"global administrator Datto capability migration missing %q",
				fragment,
			)
		}
	}
}
