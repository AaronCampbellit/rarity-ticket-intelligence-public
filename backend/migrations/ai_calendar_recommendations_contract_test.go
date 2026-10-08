package migrations

import (
	"os"
	"strings"
	"testing"
)

func TestAICalendarRecommendationMigrationKeepsFeatureAndModelFences(t *testing.T) {
	body, err := os.ReadFile("000096_ai_calendar_recommendations.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(body)
	for _, required := range []string{
		"calendar_recommendation_model_profile_id",
		"calendar_recommendation",
		"ai_policies_calendar_recommendation_model_profile_msp_fkey",
		"validate_ai_policy_model_profiles",
		"protect_ai_model_profile_references",
		"protect_ai_provider_connection_references",
		"selected_feature <> 'calendar_recommendation' OR profile.zero_cost",
		"NEW.enabled AND NEW.zero_cost",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("migration missing %q", required)
		}
	}
}
