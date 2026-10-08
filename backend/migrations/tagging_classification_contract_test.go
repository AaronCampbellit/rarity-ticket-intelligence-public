package migrations

import (
	"strings"
	"testing"
)

func taggingMigrationBody(t *testing.T, name string) string {
	t.Helper()
	body, err := FS.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(body)
}

func TestTaggingClassificationMigrationDefinesGovernedScopedFabric(t *testing.T) {
	body := taggingMigrationBody(t, "000080_tagging_classification.sql")
	for _, fragment := range []string{
		"CREATE TABLE tag_groups",
		"CREATE TABLE tags",
		"CREATE UNIQUE INDEX tags_label_unique",
		"CREATE TABLE object_tag_assignments",
		"CHECK (object_type IN ('work_record', 'task', 'project', 'asset', 'knowledge_article', 'time_entry'))",
		"CREATE TABLE tag_assignment_events",
		"CREATE TRIGGER tag_assignment_events_append_only",
		"CREATE TABLE tag_ai_policies",
		"CREATE TABLE tag_ai_suggestion_items",
		"CREATE TABLE tag_projection_cursors",
		"CREATE TABLE classification_migration_runs",
	} {
		if !strings.Contains(body, fragment) {
			t.Errorf("tagging migration missing %q", fragment)
		}
	}
	if strings.Contains(body, "category text") {
		t.Fatal("generic Category storage must not be introduced")
	}
}

func TestTaggingClassificationMigrationEnforcesIdentityHistoryAndCutover(t *testing.T) {
	body := taggingMigrationBody(t, "000080_tagging_classification.sql")
	for _, fragment := range []string{
		"CREATE TRIGGER tags_internal_key_immutable",
		"CREATE TRIGGER tag_terms_unambiguous",
		"target_version bigint NOT NULL",
		"UNIQUE (idempotency_key)",
		"CREATE TABLE tag_usage_daily",
		"CREATE TABLE tag_cooccurrence_daily",
		"CREATE TABLE classification_migration_mappings",
		"'taxonomy.system'",
		"'taxonomy.system.unclassified'",
		"'system_fallback'",
		"category_source_present",
		"'full_cutover'",
	} {
		if !strings.Contains(body, fragment) {
			t.Errorf("tagging migration missing invariant %q", fragment)
		}
	}
}

func TestTaggingAssociationRuntimeKeepsKnowledgeContentVersionIndependent(t *testing.T) {
	body := taggingMigrationBody(t, "000080_tagging_classification.sql")
	for _, fragment := range []string{
		"CREATE TABLE classification_object_versions",
		"CREATE TABLE classification_tag_operations",
		"UNIQUE NULLS NOT DISTINCT (msp_id, client_id, object_type, object_id, idempotency_key)",
	} {
		if !strings.Contains(body, fragment) {
			t.Errorf("association runtime migration missing %q", fragment)
		}
	}
}

func TestTaggingClassificationMigrationUpgradesExistingGlobalAdministrators(t *testing.T) {
	body := taggingMigrationBody(t, "000080_tagging_classification.sql")
	for _, fragment := range []string{
		"INSERT INTO role_capabilities",
		"'global-admin'",
		"'classification.manage'",
		"'classification.apply'",
		"'classification.report'",
		"'classification.ai.manage'",
		"ON CONFLICT DO NOTHING",
	} {
		if !strings.Contains(body, fragment) {
			t.Errorf("classification capability upgrade missing %q", fragment)
		}
	}
}
