package migrations

import (
	"os"
	"strings"
	"testing"
)

func TestSetupCenterConfigurationMigrationAddsOptimisticVersion(t *testing.T) {
	content, err := os.ReadFile("000074_setup_center_configuration.sql")
	if err != nil {
		t.Fatalf("read setup center configuration migration: %v", err)
	}
	for _, required := range []string{
		"ADD COLUMN setup_version bigint NOT NULL DEFAULT 1",
		"DROP COLUMN setup_version",
	} {
		if !strings.Contains(string(content), required) {
			t.Errorf("setup center configuration migration missing %q", required)
		}
	}
}
