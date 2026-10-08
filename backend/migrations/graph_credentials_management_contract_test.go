package migrations

import (
	"strings"
	"testing"
)

func TestGraphCredentialMigrationSupportsSealedAndLegacyRuntime(t *testing.T) {
	body, err := FS.ReadFile("000059_graph_credentials.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(body)
	for _, fragment := range []string{
		"ALTER COLUMN credential_secret_ref DROP NOT NULL",
		"credential_secret_ciphertext bytea",
		"client_state_secret_ciphertext bytea",
		"env://RARITY_GRAPH_CREDENTIAL_",
		"env://RARITY_GRAPH_CLIENT_STATE_",
	} {
		if !strings.Contains(sql, fragment) {
			t.Errorf("migration missing %q", fragment)
		}
	}
}
