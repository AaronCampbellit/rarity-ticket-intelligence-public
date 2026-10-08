package migrations

import (
	"strings"
	"testing"
)

func TestClientResourceLifecycleCapabilitiesSeedOnlyGlobalAdministrator(t *testing.T) {
	sql, err := FS.ReadFile("000098_client_resource_lifecycle_capabilities.sql")
	if err != nil {
		t.Fatal(err)
	}
	body := string(sql)
	for _, kind := range []string{"location", "contact", "asset", "service", "contract"} {
		for _, action := range []string{"update", "lifecycle"} {
			if !strings.Contains(body, "'"+kind+"."+action+"'") {
				t.Errorf("migration omitted %s.%s", kind, action)
			}
		}
	}
	if !strings.Contains(body, "role.key = 'global-admin'") || !strings.Contains(body, "ON CONFLICT DO NOTHING") ||
		strings.Contains(body, "WHERE role.key IN") {
		t.Fatalf("migration must grant only the global administrator role: %s", body)
	}
}
