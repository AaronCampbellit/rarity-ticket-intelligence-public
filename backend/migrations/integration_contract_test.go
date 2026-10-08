package migrations

import (
	"strings"
	"testing"
)

func TestIntegrationMigrationDefinesScopedHashedServiceKeys(t *testing.T) {
	body, err := FS.ReadFile("000012_integrations.sql")
	if err != nil {
		t.Fatalf("read integrations migration: %v", err)
	}
	required := []string{
		"CREATE TABLE service_api_keys",
		"token_hash bytea NOT NULL",
		"UNIQUE (msp_id, key_prefix)",
		"capabilities text[] NOT NULL",
		"data_scopes text[] NOT NULL",
		"CHECK (cardinality(capabilities) > 0)",
		"CHECK (cardinality(data_scopes) > 0)",
		"plaintext",
	}
	for _, fragment := range required {
		if fragment == "plaintext" {
			if strings.Contains(string(body), fragment) {
				t.Error("service key migration must not define plaintext storage")
			}
			continue
		}
		if !strings.Contains(string(body), fragment) {
			t.Errorf("integration migration missing %q", fragment)
		}
	}
}
