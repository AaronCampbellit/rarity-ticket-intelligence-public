package migrations_test

import (
	"os"
	"strings"
	"testing"
)

func TestTaggingProjectionEffectsRetainObjectIdentityAndSignedDeltas(t *testing.T) {
	contents, err := os.ReadFile("000082_tagging_projection_effects.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	sql := string(contents)
	for _, required := range []string{
		"CREATE TABLE tag_projection_effects",
		"source_event_id", "object_type", "object_id", "inherited",
		"DROP CONSTRAINT tag_usage_daily_active_count_check",
		"DROP CONSTRAINT tag_cooccurrence_daily_object_count_check",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("projection migration missing %q", required)
		}
	}
}
