package migrations

import (
	"strings"
	"testing"
)

func TestAIClassificationSubjectsMigrationMakesJobsTypedAndClassificationOptIn(t *testing.T) {
	body, err := FS.ReadFile("000081_ai_classification_subjects.sql")
	if err != nil {
		t.Fatalf("read classification subject migration: %v", err)
	}
	sql := string(body)
	for _, fragment := range []string{
		"ADD COLUMN subject_type text",
		"ADD COLUMN subject_id uuid",
		"ALTER COLUMN work_record_id DROP NOT NULL",
		"CHECK (subject_type IN ('work_record', 'task', 'project', 'asset', 'knowledge_article', 'time_entry'))",
		"'classification'",
		"classification_model_profile_id",
		"ai_generation_job_id",
		"provider_evidence",
		"application_lease_token",
		"application_lease_until",
		"ai_generation_jobs_completed_result_check",
		"tag_ai_suggestion_decisions",
		"tag_ai_suggestion_decisions_append_only",
	} {
		if !strings.Contains(sql, fragment) {
			t.Errorf("classification migration missing %q", fragment)
		}
	}
}
