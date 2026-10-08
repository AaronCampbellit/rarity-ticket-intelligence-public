package migrations

import (
	"strings"
	"testing"
)

func TestOptionalEntraMigrationPreservesLocalAdministration(t *testing.T) {
	sql, err := FS.ReadFile("000067_optional_entra_local_administrators.sql")
	if err != nil {
		t.Fatalf("read optional Entra migration: %v", err)
	}
	body := string(sql)
	for _, fragment := range []string{
		"ALTER COLUMN entra_tenant_id DROP NOT NULL",
		"ALTER COLUMN entra_client_id DROP NOT NULL",
		"ALTER COLUMN entra_secret_version DROP NOT NULL",
		"ADD COLUMN entra_state text NOT NULL DEFAULT 'not_connected'",
		"ADD COLUMN entra_version bigint NOT NULL DEFAULT 1",
		"installation_setup_entra_group_check",
		"UPDATE installation_setup",
		"SET entra_state = 'connected'",
		"-- +goose StatementBegin\nDO $$",
		"$$;\n-- +goose StatementEnd",
	} {
		if !strings.Contains(body, fragment) {
			t.Errorf("optional Entra migration missing %q", fragment)
		}
	}
	if strings.Contains(body, "UPDATE break_glass_accounts") {
		t.Error("migration must not rewrite local administrator credentials")
	}
}
