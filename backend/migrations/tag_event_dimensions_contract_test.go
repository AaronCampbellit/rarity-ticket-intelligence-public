package migrations_test

import (
	"os"
	"strings"
	"testing"
)

func TestTagEventsStampProjectionDimensionsAtMutationTime(t *testing.T) {
	contents, err := os.ReadFile("000084_tag_event_dimensions.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"BEFORE INSERT ON tag_assignment_events", "projection_dimensions", "project_task_dimensions"} {
		if !strings.Contains(string(contents), required) {
			t.Fatalf("missing %q", required)
		}
	}
}
