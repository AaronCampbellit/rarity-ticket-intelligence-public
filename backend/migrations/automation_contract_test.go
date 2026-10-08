package migrations

import (
	"strings"
	"testing"
)

func TestAutomationMigrationDefinesImmutableTypedVersionsAndConnectionReferences(t *testing.T) {
	body, err := FS.ReadFile("000018_automation.sql")
	if err != nil {
		t.Fatalf("read automation migration: %v", err)
	}
	required := []string{
		"CREATE TABLE connections",
		"secret_refs jsonb NOT NULL",
		"CREATE TABLE automation_definitions",
		"CREATE TABLE automation_versions",
		"definition_json jsonb NOT NULL",
		"CREATE TRIGGER automation_versions_immutable",
		"client_scopes uuid[] NOT NULL",
		"capabilities text[] NOT NULL",
		"CHECK (max_depth BETWEEN 1 AND 8)",
	}
	for _, fragment := range required {
		if !strings.Contains(string(body), fragment) {
			t.Errorf("automation migration missing %q", fragment)
		}
	}
}

func TestAutomationManagementRuntimeAllowsOnlyDraftPublicationMutation(t *testing.T) {
	body, err := FS.ReadFile("000048_automation_management_runtime.sql")
	if err != nil {
		t.Fatalf("read automation management runtime migration: %v", err)
	}
	sql := string(body)
	for _, required := range []string{
		"CREATE FUNCTION protect_automation_version_mutation()",
		"OLD.state = 'draft'",
		"NEW.state = 'published'",
		"OLD.definition_json IS NOT DISTINCT FROM NEW.definition_json",
		"DROP TRIGGER automation_versions_immutable",
		"CREATE TRIGGER automation_versions_immutable",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("automation management migration missing %q", required)
		}
	}
}
