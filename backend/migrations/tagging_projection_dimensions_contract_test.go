package migrations_test

import (
	"os"
	"strings"
	"testing"
)

func TestTaggingProjectionDimensionsMigrationContract(t *testing.T) {
	contents, err := os.ReadFile("000083_tagging_projection_dimensions.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	sql := string(contents)
	for _, required := range []string{"ADD COLUMN dimensions jsonb", "USING gin (dimensions)"} {
		if !strings.Contains(sql, required) {
			t.Fatalf("projection dimensions migration missing %q", required)
		}
	}
}
