package migrations

import (
	"strings"
	"testing"
)

func TestAIMigrationDefinesOptInTraceableHumanControlledRecommendations(t *testing.T) {
	body, err := FS.ReadFile("000020_ai_assistance.sql")
	if err != nil {
		t.Fatalf("read AI assistance migration: %v", err)
	}
	required := []string{
		"CREATE TABLE ai_policies",
		"provider_disclosure_accepted_at timestamptz",
		"allowed_features text[] NOT NULL",
		"CREATE TABLE ai_recommendations",
		"relevant_inputs jsonb NOT NULL",
		"prompt_version text NOT NULL",
		"state text NOT NULL DEFAULT 'pending_human'",
		"CREATE TABLE ai_recommendation_decisions",
		"applied boolean NOT NULL DEFAULT false",
		"sent boolean NOT NULL DEFAULT false",
		"CREATE TABLE ai_usage_records",
	}
	for _, fragment := range required {
		if !strings.Contains(string(body), fragment) {
			t.Errorf("AI migration missing %q", fragment)
		}
	}
}

func TestProviderAgnosticAIRuntimeMigrationDefinesEncryptedDurableControlPlane(t *testing.T) {
	body, err := FS.ReadFile("000049_provider_agnostic_ai_runtime.sql")
	if err != nil {
		t.Fatalf("read provider AI migration: %v", err)
	}
	sql := string(body)
	for _, fragment := range []string{
		"CREATE TABLE ai_provider_connections",
		"adapter_type text NOT NULL",
		"network_mode text NOT NULL",
		"credential_ciphertext bytea",
		"disclosure_accepted_at timestamptz",
		"(disclosure_accepted_at IS NULL AND disclosure_accepted_by IS NULL)",
		"local_network_acknowledged_at timestamptz",
		"response_limit_bytes bigint NOT NULL DEFAULT 5242880",
		"CREATE TABLE ai_policy_legacy_provider_configurations",
		"UPDATE ai_policies SET enabled = false WHERE enabled",
		"pg_get_constraintdef",
		"provider_connection_id IS NOT NULL",
		"ADD CONSTRAINT ai_policies_allowed_features_check",
		"restore legacy provider policy configuration",
		"CREATE FUNCTION default_ai_provider_connection_timeout()",
		"WHEN 'local' THEN 900",
		"WHEN 'remote' THEN 300",
		"CREATE TRIGGER ai_provider_connections_default_timeout",
		"CREATE TABLE ai_model_profiles",
		"input_cost_per_million_minor bigint",
		"output_cost_per_million_minor bigint",
		"CHECK (zero_cost OR (input_cost_per_million_minor IS NULL OR input_cost_per_million_minor >= 0))",
		"CREATE FUNCTION validate_ai_policy_model_profiles()",
		"connection.disclosure_accepted_at IS NOT NULL",
		"CREATE CONSTRAINT TRIGGER ai_policies_model_profiles_valid",
		"CREATE TABLE ai_generation_jobs",
		"CREATE FUNCTION validate_ai_generation_job_model_profile()",
		"CREATE CONSTRAINT TRIGGER ai_generation_jobs_model_profile_valid",
		"CREATE FUNCTION protect_ai_model_profile_references()",
		"CREATE CONSTRAINT TRIGGER ai_model_profiles_references_valid",
		"CREATE FUNCTION protect_ai_provider_connection_references()",
		"CREATE FUNCTION reset_ai_provider_disclosure_on_identity_change()",
		"CREATE TRIGGER ai_provider_connections_reset_disclosure",
		"CREATE CONSTRAINT TRIGGER ai_provider_connections_references_valid",
		"AFTER UPDATE OF enabled, adapter_type, network_mode, base_url",
		"lease_until timestamptz",
		"reserved_cost_minor bigint NOT NULL DEFAULT 0",
		"execution_fingerprint text",
		"execution_policy_version bigint",
		"execution_candidate_fingerprint text",
		"UNIQUE (idempotency_key)",
		"ALTER TABLE ai_usage_records ALTER COLUMN cost_minor DROP NOT NULL",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("provider AI migration missing %q", fragment)
		}
	}
	down := strings.Split(sql, "-- +goose Down")
	if len(down) != 2 {
		t.Fatalf("provider AI migration must have one Down section")
	}
	for _, fragment := range []string{
		"cannot roll back provider-agnostic AI runtime while usage values are unknown",
		"input_units IS NULL",
		"output_units IS NULL",
		"cost_minor IS NULL",
	} {
		if !strings.Contains(down[1], fragment) {
			t.Fatalf("provider AI migration Down missing %q", fragment)
		}
	}
	if preflight, firstMutation := strings.Index(down[1], "cannot roll back provider-agnostic AI runtime while usage values are unknown"), strings.Index(down[1], "ALTER TABLE ai_usage_records"); preflight == -1 || preflight > firstMutation {
		t.Fatal("provider AI migration must check for unknown usage before Down mutations")
	}
}

func TestGlobalAIWorkspaceMigrationDefinesScopedConversationsAndExpiringProposals(t *testing.T) {
	body, err := FS.ReadFile("000077_global_ai_workspace.sql")
	if err != nil {
		t.Fatalf("read global AI workspace migration: %v", err)
	}
	sql := string(body)
	for _, fragment := range []string{
		"CREATE TABLE ai_conversations",
		"CREATE TABLE ai_messages",
		"CREATE TABLE ai_product_documents",
		"search_vector tsvector",
		"CREATE TABLE ai_action_proposals",
		"INSERT INTO ai_product_documents",
		"expires_at timestamptz NOT NULL",
		"state IN ('pending', 'confirmed', 'rejected', 'expired', 'failed')",
		"FOREIGN KEY (principal_id, msp_id) REFERENCES technicians(id, msp_id)",
		"FOREIGN KEY (client_id, msp_id) REFERENCES client_organizations(id, msp_id)",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("global AI workspace migration missing %q", fragment)
		}
	}
}

func TestAIWorkspaceDirectProjectsDoNotInventProposalProvenance(t *testing.T) {
	body, err := FS.ReadFile("000078_ai_workspace_direct_projects.sql")
	if err != nil {
		t.Fatalf("read direct AI project migration: %v", err)
	}
	sql := string(body)
	for _, fragment := range []string{
		"ALTER COLUMN original_proposal_version_id DROP NOT NULL",
		"original_proposal_version_id IS NULL",
		"cannot restore required proposal provenance",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("direct AI project migration missing %q", fragment)
		}
	}
}

func TestAIContractProposalTargetScopeIsSeparateAndTenantProtected(t *testing.T) {
	body, err := FS.ReadFile("000079_ai_workspace_proposal_target_scope.sql")
	if err != nil {
		t.Fatalf("read AI proposal target scope migration: %v", err)
	}
	sql := string(body)
	for _, fragment := range []string{
		"ALTER TABLE ai_action_proposals\n  ADD COLUMN target_client_id uuid;",
		"UPDATE ai_action_proposals\nSET target_client_id = client_id\nWHERE client_id IS NOT NULL;",
		"FOREIGN KEY (target_client_id, msp_id)",
		"REFERENCES client_organizations(id, msp_id)",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("AI proposal target scope migration missing %q", fragment)
		}
	}
}
