package mutation

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSafeDiffRedactsContactValuesButNamesChangedFields(t *testing.T) {
	diff := BuildSafeDiff(
		map[string]any{"display_name": "Alex", "email": "old@example.com", "phone": "555-0100"},
		map[string]any{"display_name": "Alexandra", "email": "new@example.com", "phone": "555-0199"},
		"email", "phone",
	)
	body, err := json.Marshal(diff)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	encoded := string(body)
	for _, secret := range []string{"old@example.com", "new@example.com", "555-0100", "555-0199"} {
		if strings.Contains(encoded, secret) {
			t.Fatalf("safe diff leaked contact value %q: %s", secret, encoded)
		}
	}
	if !strings.Contains(encoded, `"email":{"changed":true,"redacted":true}`) ||
		!strings.Contains(encoded, `"phone":{"changed":true,"redacted":true}`) ||
		!strings.Contains(encoded, `"display_name":{"after":"Alexandra","before":"Alex"}`) {
		t.Fatalf("safe diff lost safe field evidence: %s", encoded)
	}
}

func TestSafeDiffOmitsUnchangedFieldsAndPreservesExplicitClear(t *testing.T) {
	diff := BuildSafeDiff(
		map[string]any{"name": "North", "location_id": "location-1"},
		map[string]any{"name": "North", "location_id": nil},
	)
	if _, exists := diff["name"]; exists {
		t.Fatalf("unchanged name appeared in safe diff: %+v", diff)
	}
	change, exists := diff["location_id"]
	if !exists || change["before"] != "location-1" {
		t.Fatalf("explicit clear missing from safe diff: %+v", diff)
	}
	if after, exists := change["after"]; !exists || after != nil {
		t.Fatalf("explicit clear does not retain a null after value: %+v", change)
	}
}
