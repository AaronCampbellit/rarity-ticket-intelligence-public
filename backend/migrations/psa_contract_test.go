package migrations

import (
	"strings"
	"testing"
)

func TestPSAMigrationsDefineScopedVersionedConversionContracts(t *testing.T) {
	sales, err := FS.ReadFile("000010_psa_sales.sql")
	if err != nil {
		t.Fatalf("read sales migration: %v", err)
	}
	projects, err := FS.ReadFile("000011_psa_projects.sql")
	if err != nil {
		t.Fatalf("read projects migration: %v", err)
	}
	phaseDelivery, err := FS.ReadFile("000021_phase_delivery_fields.sql")
	if err != nil {
		t.Fatalf("read phase delivery migration: %v", err)
	}
	proposalDelivery, err := FS.ReadFile("000022_proposal_delivery_fields.sql")
	if err != nil {
		t.Fatalf("read proposal delivery migration: %v", err)
	}
	acceptanceGrants, err := FS.ReadFile("000023_proposal_acceptance_grants.sql")
	if err != nil {
		t.Fatalf("read proposal acceptance grants migration: %v", err)
	}
	prospectLineage, err := FS.ReadFile("000024_prospect_client_lineage.sql")
	if err != nil {
		t.Fatalf("read prospect client lineage migration: %v", err)
	}
	prospectAcceptance, err := FS.ReadFile("000025_prospect_acceptance_grants.sql")
	if err != nil {
		t.Fatalf("read prospect acceptance migration: %v", err)
	}
	budgetUniqueness, err := FS.ReadFile("000026_project_budget_uniqueness.sql")
	if err != nil {
		t.Fatalf("read project budget uniqueness migration: %v", err)
	}
	projectTaskTime, err := FS.ReadFile("000061_project_task_time_entries.sql")
	if err != nil {
		t.Fatalf("read Project task time migration: %v", err)
	}
	availability, err := FS.ReadFile("000062_technician_availability.sql")
	if err != nil {
		t.Fatalf("read technician availability migration: %v", err)
	}
	financialInputs, err := FS.ReadFile("000063_project_financial_inputs.sql")
	if err != nil {
		t.Fatalf("read Project financial inputs migration: %v", err)
	}
	opportunityCustomFields, err := FS.ReadFile("000064_opportunity_custom_fields.sql")
	if err != nil {
		t.Fatalf("read Opportunity custom fields migration: %v", err)
	}
	opportunityAttachments, err := FS.ReadFile("000065_opportunity_attachments.sql")
	if err != nil {
		t.Fatalf("read Opportunity attachments migration: %v", err)
	}
	opportunityParticipants, err := FS.ReadFile("000066_opportunity_participants.sql")
	if err != nil {
		t.Fatalf("read Opportunity participants migration: %v", err)
	}
	body := string(sales) + "\n" + string(projects) + "\n" +
		string(phaseDelivery) + "\n" + string(proposalDelivery) + "\n" +
		string(acceptanceGrants) + "\n" + string(prospectLineage) + "\n" +
		string(prospectAcceptance) + "\n" + string(budgetUniqueness)
	body += "\n" + string(projectTaskTime) + "\n" + string(availability) +
		"\n" + string(financialInputs) + "\n" + string(opportunityCustomFields) +
		"\n" + string(opportunityAttachments)
	body += "\n" + string(opportunityParticipants)
	required := []string{
		"CREATE TABLE prospects",
		"CREATE TABLE pipelines",
		"CREATE TABLE pipeline_stages",
		"CREATE TABLE opportunities",
		"CREATE TABLE proposals",
		"CREATE TABLE proposal_versions",
		"ADD COLUMN requires_internal_approval boolean",
		"ADD COLUMN discount_minor bigint",
		"ADD COLUMN tax_minor bigint",
		"ADD COLUMN planned_minutes bigint",
		"CREATE TABLE proposal_pdf_snapshots",
		"CREATE TRIGGER proposal_pdf_snapshots_immutable",
		"CREATE TABLE proposal_acceptance_grants",
		"token_sha256 char(64) NOT NULL UNIQUE",
		"consumed_at timestamptz",
		"ADD COLUMN originating_prospect_id uuid",
		"UNIQUE (msp_id, originating_prospect_id)",
		"ALTER COLUMN client_id DROP NOT NULL",
		"CREATE UNIQUE INDEX project_budgets_project_type_unique",
		"CREATE TRIGGER proposal_versions_immutable",
		"CREATE TABLE proposal_lines",
		"CREATE TABLE approvals",
		"CREATE TABLE projects",
		"original_proposal_version_id uuid NOT NULL",
		"CREATE TABLE phases",
		"ADD COLUMN owner_id uuid",
		"ADD COLUMN planned_minutes bigint",
		"ADD COLUMN budget_minor bigint",
		"ADD COLUMN budget_currency char(3)",
		"ADD COLUMN deliverables jsonb",
		"ADD COLUMN completion_criteria jsonb",
		"CREATE TABLE phase_participating_teams",
		"FOREIGN KEY (phase_id, msp_id, client_id) REFERENCES phases(id, msp_id, client_id)",
		"FOREIGN KEY (team_id, msp_id) REFERENCES teams(id, msp_id)",
		"CREATE TABLE resource_plans",
		"FOREIGN KEY (role_id, msp_id) REFERENCES roles(id, msp_id)",
		"FOREIGN KEY (team_id, msp_id) REFERENCES teams(id, msp_id)",
		"CHECK ((role_id IS NOT NULL)::integer + (team_id IS NOT NULL)::integer = 1)",
		"CREATE TABLE project_budgets",
		"CREATE TABLE cost_actuals",
		"CREATE TABLE change_orders",
		"CREATE TABLE change_order_decisions",
		"CREATE TABLE change_order_applications",
		"CREATE TRIGGER change_order_decisions_immutable",
		"CREATE TRIGGER change_order_applications_immutable",
		"UNIQUE (change_order_version_id)",
		"CREATE TABLE opportunity_conversions",
		"UNIQUE (opportunity_id)",
		"ALTER COLUMN work_record_id DROP NOT NULL",
		"CHECK (work_record_id IS NOT NULL OR task_id IS NOT NULL)",
		"CREATE TABLE technician_availability_windows",
		"available_minutes bigint NOT NULL",
		"FOREIGN KEY (technician_id, msp_id) REFERENCES technicians(id, msp_id)",
		"CREATE TABLE technician_labor_cost_rates",
		"CREATE TABLE recognized_billable_work",
		"CREATE TRIGGER technician_labor_cost_rates_immutable",
		"CREATE TRIGGER recognized_billable_work_immutable",
		"ADD COLUMN custom_fields jsonb NOT NULL",
		"CHECK (jsonb_typeof(custom_fields) = 'object')",
		"ADD COLUMN opportunity_id uuid",
		"num_nonnulls(work_record_id, opportunity_id) = 1",
		"REFERENCES opportunities(id, msp_id, client_id)",
		"ADD COLUMN team_id uuid",
		"CREATE TABLE opportunity_contacts",
		"REFERENCES contacts(id, msp_id, client_id)",
	}
	for _, fragment := range required {
		if !strings.Contains(body, fragment) {
			t.Errorf("PSA migrations missing %q", fragment)
		}
	}
}
