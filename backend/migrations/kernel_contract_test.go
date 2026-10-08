package migrations

import (
	"strings"
	"testing"
)

func TestSecureKernelMigrationDefinesScopedAtomicMutationRecords(t *testing.T) {
	sql, err := FS.ReadFile("000002_secure_platform_kernel.sql")
	if err != nil {
		t.Fatalf("read secure platform kernel migration: %v", err)
	}
	body := string(sql)

	required := []string{
		"CREATE TABLE msp_organizations",
		"CREATE TABLE client_organizations",
		"CREATE TABLE audit_ledger",
		"CREATE TABLE event_outbox",
		"msp_id uuid NOT NULL",
		"client_id uuid",
		"correlation_id uuid NOT NULL",
		"subject_version bigint NOT NULL",
	}
	for _, fragment := range required {
		if !strings.Contains(body, fragment) {
			t.Errorf("migration missing %q", fragment)
		}
	}

	if !strings.Contains(body, "UNIQUE (msp_id, display_id)") {
		t.Error("client display identifiers must be unique only within their MSP scope")
	}
	if !strings.Contains(body, "CHECK (client_id IS NULL OR msp_id IS NOT NULL)") {
		t.Error("audit and outbox scope must reject client scope without MSP scope")
	}
	if !strings.Contains(body, "CREATE TRIGGER audit_ledger_append_only") {
		t.Error("audit ledger must reject updates and deletes at the database boundary")
	}
}

func TestIdentityMigrationDefinesTenantBoundIdentityRBACAndSessions(t *testing.T) {
	sql, err := FS.ReadFile("000003_identity_access.sql")
	if err != nil {
		t.Fatalf("read identity migration: %v", err)
	}
	body := string(sql)
	required := []string{
		"CREATE TABLE technicians",
		"CREATE TABLE external_identities",
		"CREATE TABLE roles",
		"CREATE TABLE role_capabilities",
		"CREATE TABLE role_assignments",
		"CREATE TABLE sessions",
		"issuer text NOT NULL",
		"subject text NOT NULL",
		"tenant_id text NOT NULL",
		"token_hash bytea NOT NULL",
		"expires_at timestamptz NOT NULL",
		"UNIQUE (msp_id, issuer, subject)",
	}
	for _, fragment := range required {
		if !strings.Contains(body, fragment) {
			t.Errorf("identity migration missing %q", fragment)
		}
	}
	if strings.Contains(body, "token text") {
		t.Error("session storage must not persist plaintext bearer tokens")
	}
}

func TestWorkManagementMigrationDefinesClientScopedCore(t *testing.T) {
	sql, err := FS.ReadFile("000004_work_management.sql")
	if err != nil {
		t.Fatalf("read work management migration: %v", err)
	}
	body := string(sql)
	required := []string{
		"CREATE TABLE locations",
		"CREATE TABLE contacts",
		"CREATE TABLE departments",
		"CREATE TABLE teams",
		"CREATE TABLE queues",
		"CREATE TABLE assets",
		"CREATE TABLE services",
		"CREATE TABLE contracts",
		"CREATE TABLE work_records",
		"CREATE TABLE tasks",
		"CREATE TABLE comments",
		"CREATE TABLE attachments",
		"CREATE TABLE time_entries",
		"CREATE TABLE object_links",
		"version bigint NOT NULL",
		"merged_into_id uuid",
		"UNIQUE (msp_id, client_id, display_id)",
	}
	for _, fragment := range required {
		if !strings.Contains(body, fragment) {
			t.Errorf("work-management migration missing %q", fragment)
		}
	}
	if strings.Contains(body, "task_dependencies") {
		t.Error("task dependency graphs are outside the approved model")
	}
}

func TestClassificationKeepsTypeStatusAndTagsDistinct(t *testing.T) {
	workManagement, err := FS.ReadFile("000004_work_management.sql")
	if err != nil {
		t.Fatalf("read work management migration: %v", err)
	}
	tagging, err := FS.ReadFile("000080_tagging_classification.sql")
	if err != nil {
		t.Fatalf("read tagging migration: %v", err)
	}

	workBody := string(workManagement)
	if !strings.Contains(workBody, "record_type text NOT NULL") {
		t.Error("work-record Type must remain a first-class field")
	}
	if !strings.Contains(workBody, "status text NOT NULL") {
		t.Error("work-record Status must remain a first-class field")
	}

	tagBody := string(tagging)
	if !strings.Contains(tagBody, "CREATE TABLE object_tag_assignments") {
		t.Error("Tags must use their own governed association model")
	}
	if strings.Contains(strings.ToLower(tagBody), "category text") {
		t.Error("the removed generic Category field must not return through tagging")
	}
}

func TestWorkflowExperienceMigrationDefinesVersionedOperations(t *testing.T) {
	sql, err := FS.ReadFile("000005_workflow_experience.sql")
	if err != nil {
		t.Fatalf("read workflow experience migration: %v", err)
	}
	body := string(sql)
	required := []string{
		"CREATE TABLE workflows",
		"CREATE TABLE workflow_versions",
		"CREATE TRIGGER workflow_versions_immutable",
		"CREATE TABLE work_record_workflows",
		"CREATE TABLE business_calendars",
		"CREATE TABLE sla_policies",
		"CREATE TABLE work_record_slas",
		"CREATE TABLE notification_policies",
		"CREATE TABLE notification_deliveries",
		"CREATE TABLE saved_searches",
		"CREATE TABLE dashboards",
		"CREATE TABLE knowledge_articles",
		"definition jsonb NOT NULL",
		"matched_trace jsonb NOT NULL",
	}
	for _, fragment := range required {
		if !strings.Contains(body, fragment) {
			t.Errorf("workflow migration missing %q", fragment)
		}
	}
}

func TestNotificationRuntimeMigrationDefinesImmutablePoliciesAndIdempotentPlanning(t *testing.T) {
	sql, err := FS.ReadFile("000032_notification_runtime.sql")
	if err != nil {
		t.Fatalf("read notification runtime migration: %v", err)
	}
	body := string(sql)
	required := []string{
		"CREATE TABLE notification_policy_versions",
		"CREATE TRIGGER notification_policy_versions_immutable",
		"ALTER COLUMN policy_version SET NOT NULL",
		"suppression_reason text",
		"CONSTRAINT notification_delivery_dedupe_uniq",
		"CREATE TABLE notification_event_plans",
		"jsonb_build_object(",
		"COALESCE((",
	}
	for _, fragment := range required {
		if !strings.Contains(body, fragment) {
			t.Errorf("notification runtime migration missing %q", fragment)
		}
	}
}

func TestKnowledgeRuntimeMigrationAllowsOneWayPublicationAndBlocksClientVisibility(t *testing.T) {
	sql, err := FS.ReadFile("000033_knowledge_runtime.sql")
	if err != nil {
		t.Fatalf("read knowledge runtime migration: %v", err)
	}
	body := string(sql)
	required := []string{
		"ADD COLUMN client_visible boolean NOT NULL DEFAULT false",
		"CHECK (NOT client_visible)",
		"CREATE FUNCTION protect_knowledge_article_version",
		"OLD.published_at IS NOT NULL",
		"NEW.body IS DISTINCT FROM OLD.body",
		"NEW.published_at IS NULL",
		"CREATE TRIGGER knowledge_article_versions_protected",
	}
	for _, fragment := range required {
		if !strings.Contains(body, fragment) {
			t.Errorf("knowledge runtime migration missing %q", fragment)
		}
	}
}

func TestBillingExportRuntimeMigrationDefinesApprovalAndImmutableExportEvidence(t *testing.T) {
	sql, err := FS.ReadFile("000034_billing_export_runtime.sql")
	if err != nil {
		t.Fatalf("read billing export runtime migration: %v", err)
	}
	body := string(sql)
	required := []string{
		"ADD COLUMN approval_state text NOT NULL DEFAULT 'pending'",
		"CREATE TABLE time_entry_approval_decisions",
		"UNIQUE (time_entry_id, time_entry_version)",
		"CREATE TRIGGER time_entry_approval_decisions_immutable",
		"CREATE TABLE billing_exports",
		"CHECK (octet_length(csv_sha256) = 32)",
		"CREATE TABLE billing_export_entries",
		"time_entry_version bigint NOT NULL",
		"duration_seconds integer NOT NULL",
		"CREATE TRIGGER billing_exports_immutable",
		"CREATE TRIGGER billing_export_entries_immutable",
	}
	for _, fragment := range required {
		if !strings.Contains(body, fragment) {
			t.Errorf("billing export runtime migration missing %q", fragment)
		}
	}
}

func TestIntegrationHealthRuntimeMigrationRestoresMSPScopeToUnifiedView(t *testing.T) {
	sql, err := FS.ReadFile("000035_integration_health_scope.sql")
	if err != nil {
		t.Fatalf("read integration health scope migration: %v", err)
	}
	body := string(sql)
	required := []string{
		"DROP VIEW integration_health_signals",
		"CREATE VIEW integration_health_signals AS",
		"msp_id",
		"GROUP BY connection.id, connection.msp_id",
	}
	for _, fragment := range required {
		if !strings.Contains(body, fragment) {
			t.Errorf("integration health migration missing %q", fragment)
		}
	}
}

func TestDirectIntakeRuntimeMigrationMakesIdempotencyClientScoped(t *testing.T) {
	sql, err := FS.ReadFile("000036_direct_intake_scope.sql")
	if err != nil {
		t.Fatalf("read direct intake scope migration: %v", err)
	}
	body := string(sql)
	required := []string{
		"DROP CONSTRAINT inbound_events_msp_id_source_external_id_key",
		"CREATE UNIQUE INDEX inbound_events_client_external_id_idx",
		"(msp_id, client_id, source, external_id)",
		"WHERE client_id IS NOT NULL",
		"CREATE UNIQUE INDEX inbound_events_global_external_id_idx",
		"WHERE client_id IS NULL",
	}
	for _, fragment := range required {
		if !strings.Contains(body, fragment) {
			t.Errorf("direct intake scope migration missing %q", fragment)
		}
	}
}

func TestInboundWebhookRuntimeMigrationMakesIdempotencyConnectionScoped(t *testing.T) {
	sql, err := FS.ReadFile("000037_inbound_webhook_scope.sql")
	if err != nil {
		t.Fatalf("read inbound webhook scope migration: %v", err)
	}
	body := string(sql)
	required := []string{
		"ADD COLUMN connection_id uuid",
		"REFERENCES webhook_connections(id, msp_id)",
		"DROP INDEX inbound_events_client_external_id_idx",
		"source <> 'inbound_webhook'",
		"CREATE UNIQUE INDEX inbound_events_webhook_external_id_idx",
		"(connection_id, external_id)",
		"source = 'inbound_webhook'",
	}
	for _, fragment := range required {
		if !strings.Contains(body, fragment) {
			t.Errorf("inbound webhook scope migration missing %q", fragment)
		}
	}
}
