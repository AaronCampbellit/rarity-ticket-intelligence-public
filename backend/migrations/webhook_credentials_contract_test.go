package migrations

import (
	"strings"
	"testing"
)

func TestWebhookCredentialMigrationSupportsSealedAndLegacySecrets(t *testing.T) {
	body, err := FS.ReadFile("000058_webhook_credentials.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(body)
	for _, fragment := range []string{
		"ALTER COLUMN secret_ref DROP NOT NULL",
		"ADD COLUMN secret_version integer",
		"ADD COLUMN secret_nonce bytea",
		"ADD COLUMN secret_ciphertext bytea",
		"secret_ref LIKE 'env://RARITY_WEBHOOK_SECRET_%'",
	} {
		if !strings.Contains(sql, fragment) {
			t.Errorf("credential migration missing %q", fragment)
		}
	}
}
