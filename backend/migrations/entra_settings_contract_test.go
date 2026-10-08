package migrations

import (
	"strings"
	"testing"
)

func TestEntraCandidateMigrationPreservesActiveConfigurationDuringVerification(t *testing.T) {
	body, err := FS.ReadFile("000068_entra_configuration_candidates.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(body)
	for _, required := range []string{
		"CREATE TABLE entra_configuration_candidates",
		"msp_id uuid PRIMARY KEY",
		"version bigint NOT NULL",
		"secret_ciphertext bytea NOT NULL",
		"FOREIGN KEY (staged_by, msp_id)",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("migration omitted %q", required)
		}
	}
	if strings.Contains(sql, "UPDATE installation_setup") {
		t.Fatal("candidate migration must not alter an active Entra generation")
	}
}
