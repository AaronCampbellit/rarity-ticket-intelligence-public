package migrations

import (
	"strings"
	"testing"
)

func TestBreakGlassMigrationStoresOnlyStrongHashMaterialAndNetworkPolicy(t *testing.T) {
	sql, err := FS.ReadFile("000053_break_glass_accounts.sql")
	if err != nil {
		t.Fatalf("read break-glass migration: %v", err)
	}
	body := string(sql)
	for _, fragment := range []string{
		"CREATE TABLE break_glass_accounts",
		"password_hash text NOT NULL",
		"allowed_cidrs cidr[] NOT NULL",
		"last_used_at timestamptz",
		"UNIQUE (msp_id, username)",
	} {
		if !strings.Contains(body, fragment) {
			t.Errorf("break-glass migration missing %q", fragment)
		}
	}
	if strings.Contains(body, "password text") {
		t.Error("break-glass accounts must never persist plaintext passwords")
	}
}

func TestBreakGlassNetworkPolicyFailsClosed(t *testing.T) {
	sql, err := FS.ReadFile("000055_break_glass_network_policy.sql")
	if err != nil {
		t.Fatalf("read break-glass network policy migration: %v", err)
	}
	if !strings.Contains(string(sql), "cardinality(allowed_cidrs) > 0") {
		t.Error("break-glass accounts must require an explicit restricted network")
	}
}
