package migrations

import (
	"strings"
	"testing"
)

func TestSetupVerificationEvidenceContract(t *testing.T) {
	body, err := FS.ReadFile("000075_setup_verification_evidence.sql")
	if err != nil {
		t.Fatalf("read setup verification migration: %v", err)
	}
	required := []string{
		"CREATE TABLE setup_verification_evidence",
		"PRIMARY KEY (msp_id, section)",
		"section IN ('object_storage', 'backups')",
		"safe_code text NOT NULL",
		"valid_until timestamptz NOT NULL",
		"evidence_hash bytea",
		"consumed_nonce text",
		"setup_verification_evidence_nonce_idx",
		"jsonb_typeof(details) = 'object'",
	}
	for _, fragment := range required {
		if !strings.Contains(string(body), fragment) {
			t.Errorf("setup verification migration missing %q", fragment)
		}
	}
	for _, forbidden := range []string{"credential", "raw_output", "secret"} {
		if strings.Contains(string(body), forbidden) {
			t.Errorf("setup verification migration contains forbidden field %q", forbidden)
		}
	}
}
