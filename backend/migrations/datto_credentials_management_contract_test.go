package migrations_test

import (
	"os"
	"strings"
	"testing"
)

func TestDattoCredentialManagementMigrationPreservesLegacyAndSealedShapes(t *testing.T) {
	content, err := os.ReadFile("000060_datto_credentials.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(content)
	for _, fragment := range []string{
		"configuration_generation bigint NOT NULL DEFAULT 1",
		"credential_secret_version integer",
		"credential_secret_nonce bytea",
		"credential_secret_ciphertext bytea",
		"credential_secret_ref LIKE 'env://RARITY_DATTO_CREDENTIAL_%'",
		"credential_secret_ref IS NULL",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration missing %q", fragment)
		}
	}
}
