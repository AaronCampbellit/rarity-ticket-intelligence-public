package psa

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/knowledge"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/routing"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sales"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sla"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/store"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workflow"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workrecords"
)

// TestAISecondWavePostgres catches transaction gaps that allow a confirmed
// mutation to commit after one of its versioned or lifecycle-scoped inputs
// changes. Every concurrency case uses an observed PostgreSQL blocking edge;
// timing alone is never treated as proof.
func TestAISecondWavePostgres(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for AI second-wave PostgreSQL concurrency and rollback proof")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	t.Cleanup(cancel)
	if err := store.Migrate(ctx, databaseURL); err != nil {
		t.Fatalf("migrate isolated PostgreSQL database: %v", err)
	}
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	if config.MaxConns < 12 {
		config.MaxConns = 12
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("open isolated PostgreSQL database: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping isolated PostgreSQL database: %v", err)
	}

	t.Run("ticket create serializes every accepted dependency", func(t *testing.T) {
		testAISecondWaveTicketCreateConcurrency(t, ctx, pool)
	})
	t.Run("ticket assignment serializes target owner and scope", func(t *testing.T) {
		testAISecondWaveAssignmentConcurrency(t, ctx, pool)
	})
	t.Run("opportunity mutations serialize target and Client", func(t *testing.T) {
		testAISecondWaveOpportunityConcurrency(t, ctx, pool)
	})
	t.Run("proposal create serializes Opportunity and Client", func(t *testing.T) {
		testAISecondWaveProposalConcurrency(t, ctx, pool)
	})
	t.Run("knowledge publish serializes revision and Client", func(t *testing.T) {
		testAISecondWaveKnowledgeConcurrency(t, ctx, pool)
	})
	t.Run("domain audit and outbox failures roll back", func(t *testing.T) {
		testAISecondWaveRollback(t, ctx, pool)
	})

	var residue int
	if err := pool.QueryRow(ctx, `
SELECT count(*)
FROM msp_organizations
WHERE display_id LIKE 'RTI-AI2-T11-%'
`).Scan(&residue); err != nil {
		t.Fatalf("count AI second-wave fixture residue: %v", err)
	}
	if residue != 0 {
		t.Fatalf("AI second-wave fixture MSP residue=%d, want zero", residue)
	}
}

type aiSecondWaveFixture struct {
	mspID, clientID, actorID                                    string
	serviceID, contractID, queueID                              string
	ruleSetID, routingRuleID                                    string
	workflowID, calendarID, policyID                            string
	technicianOneID, technicianTwoID, technicianThreeID         string
	roleID, assignmentOneID, assignmentTwoID, assignmentThreeID string
	workRecordID                                                string
	pipelineID, currentStageID, destinationStageID              string
	opportunityID                                               string
	articleID                                                   string
	now                                                         time.Time
}

func withAISecondWaveFixture(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	run func(aiSecondWaveFixture),
) {
	t.Helper()
	fixture := seedAISecondWaveFixture(t, ctx, pool)
	defer cleanupAISecondWaveFixture(t, pool, fixture)
	run(fixture)
}

var aiSecondWavePlaceholder = regexp.MustCompile(`\$(\d+)`)

func execAISecondWaveSeedBatch(
	ctx context.Context,
	pool *pgxpool.Pool,
	batch string,
	args ...any,
) error {
	for _, rawStatement := range strings.Split(batch, ";") {
		statement := strings.TrimSpace(rawStatement)
		if statement == "" {
			continue
		}
		positions := make(map[int]int)
		statementArgs := make([]any, 0)
		var replacementError error
		statement = aiSecondWavePlaceholder.ReplaceAllStringFunc(
			statement,
			func(token string) string {
				original, err := strconv.Atoi(strings.TrimPrefix(token, "$"))
				if err != nil || original < 1 || original > len(args) {
					replacementError = fmt.Errorf("invalid fixture placeholder %q", token)
					return token
				}
				if current, ok := positions[original]; ok {
					return fmt.Sprintf("$%d", current)
				}
				current := len(statementArgs) + 1
				positions[original] = current
				statementArgs = append(statementArgs, args[original-1])
				return fmt.Sprintf("$%d", current)
			},
		)
		if replacementError != nil {
			return replacementError
		}
		if _, err := pool.Exec(ctx, statement, statementArgs...); err != nil {
			return err
		}
	}
	return nil
}

func seedAISecondWaveFixture(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) aiSecondWaveFixture {
	t.Helper()
	fixture := aiSecondWaveFixture{
		mspID: uuid.NewString(), clientID: uuid.NewString(), actorID: uuid.NewString(),
		serviceID: uuid.NewString(), contractID: uuid.NewString(), queueID: uuid.NewString(),
		ruleSetID: uuid.NewString(), routingRuleID: uuid.NewString(),
		workflowID: uuid.NewString(), calendarID: uuid.NewString(), policyID: uuid.NewString(),
		technicianOneID: uuid.NewString(), technicianTwoID: uuid.NewString(),
		technicianThreeID: uuid.NewString(), roleID: uuid.NewString(),
		assignmentOneID: uuid.NewString(), assignmentTwoID: uuid.NewString(),
		assignmentThreeID: uuid.NewString(), workRecordID: uuid.NewString(),
		pipelineID: uuid.NewString(), currentStageID: uuid.NewString(),
		destinationStageID: uuid.NewString(), opportunityID: uuid.NewString(),
		articleID: uuid.NewString(),
		now:       time.Date(2026, time.August, 7, 12, 0, 0, 0, time.UTC),
	}
	displaySuffix := strings.ToUpper(strings.ReplaceAll(fixture.mspID, "-", ""))
	if err := execAISecondWaveSeedBatch(ctx, pool, `
INSERT INTO msp_organizations (
  id, display_id, name, lifecycle_state, version,
  created_at, created_by, updated_at, updated_by
) VALUES ($1, $2, 'AI second-wave transaction proof', 'active', 1, $3, $4, $3, $4);

INSERT INTO client_organizations (
  id, msp_id, display_id, name, lifecycle_state, version,
  created_at, created_by, updated_at, updated_by
) VALUES ($5, $1, $6, 'AI second-wave Client', 'active', 1, $3, $4, $3, $4);
`, fixture.mspID, "RTI-AI2-T11-"+displaySuffix, fixture.now, fixture.actorID,
		fixture.clientID, "AI2-CLIENT-"+displaySuffix); err != nil {
		t.Fatalf("seed AI second-wave organization scope: %v", err)
	}

	if err := execAISecondWaveSeedBatch(ctx, pool, `
INSERT INTO services (
  id, msp_id, client_id, display_id, name, lifecycle_state, version,
  created_at, created_by, updated_at, updated_by
) VALUES ($1, $2, $3, $4, 'Managed Service', 'active', 1, $5, $6, $5, $6);

INSERT INTO contracts (
  id, msp_id, client_id, display_id, name, starts_on, ends_on,
  lifecycle_state, version, created_at, created_by, updated_at, updated_by
) VALUES (
  $7, $2, $3, $8, 'Managed Contract', $9::date, NULL,
  'active', 1, $5, $6, $5, $6
);

INSERT INTO queues (id, msp_id, client_id, key, name, version)
VALUES ($10, $2, $3, $11, 'AI second-wave queue', 1);
`, fixture.serviceID, fixture.mspID, fixture.clientID,
		"AI2-SVC-"+displaySuffix, fixture.now, fixture.actorID,
		fixture.contractID, "AI2-CON-"+displaySuffix,
		fixture.now.Add(-24*time.Hour), fixture.queueID,
		"ai2-"+strings.ToLower(displaySuffix)); err != nil {
		t.Fatalf("seed AI second-wave ticket dependencies: %v", err)
	}

	if err := execAISecondWaveSeedBatch(ctx, pool, `
INSERT INTO routing_rule_sets (
  id, msp_id, current_version, created_at, created_by, updated_at, updated_by
) VALUES ($1, $2, 1, $3, $4, $3, $4);
INSERT INTO routing_rule_set_versions (
  rule_set_id, msp_id, version, published_at, published_by
) VALUES ($1, $2, 1, $3, $4);
INSERT INTO routing_rule_versions (
  rule_set_id, msp_id, version, rule_id, position,
  client_id, record_type, priority, queue_id
) VALUES ($1, $2, 1, $5, 1, $6, 'incident', 'normal', $7);
`, fixture.ruleSetID, fixture.mspID, fixture.now, fixture.actorID,
		fixture.routingRuleID, fixture.clientID, fixture.queueID); err != nil {
		t.Fatalf("seed AI second-wave routing: %v", err)
	}

	if err := execAISecondWaveSeedBatch(ctx, pool, `
INSERT INTO workflows (
  id, msp_id, client_id, key, name, enabled, priority, stable_order,
  fallback, conditions, current_version, created_at, created_by,
  updated_at, updated_by
) VALUES (
  $1, $2, $3, $4, 'AI second-wave workflow', true, 1, 1,
  true, '{}'::jsonb, 1, $5, $6, $5, $6
);
INSERT INTO workflow_versions (
  workflow_id, msp_id, version, definition, published_at, published_by
) VALUES ($1, $2, 1, $7::jsonb, $5, $6);
`, fixture.workflowID, fixture.mspID, fixture.clientID,
		"ai2-"+strings.ToLower(displaySuffix), fixture.now, fixture.actorID,
		`{"initial_state":"new","states":[{"key":"new"}]}`); err != nil {
		t.Fatalf("seed AI second-wave workflow: %v", err)
	}

	if err := execAISecondWaveSeedBatch(ctx, pool, `
INSERT INTO business_calendars (
  id, msp_id, client_id, key, name, timezone, weekly_schedule, holidays, version
) VALUES (
  $1, $2, $3, $4, 'AI second-wave calendar', 'UTC', '{}'::jsonb, '[]'::jsonb, 1
);
INSERT INTO business_calendar_versions (
  calendar_id, msp_id, version, timezone, weekly_schedule, holidays,
  published_at, published_by
) VALUES ($1, $2, 1, 'UTC', '{}'::jsonb, '[]'::jsonb, $5, $6);
INSERT INTO sla_policies (
  id, msp_id, client_id, key, name, calendar_id, conditions,
  response_target_seconds, resolution_target_seconds, warning_percent,
  pause_states, enabled, priority, version, stable_order, fallback
) VALUES (
  $7, $2, $3, $8, 'AI second-wave SLA', $1, '{}'::jsonb,
  3600, 14400, 80, '[]'::jsonb, true, 1, 1, 1, true
);
INSERT INTO sla_policy_versions (
  policy_id, msp_id, version, calendar_id, calendar_version, conditions,
  response_target_seconds, resolution_target_seconds, warning_percent,
  pause_states, enabled, priority, stable_order, fallback,
  published_at, published_by
) VALUES (
  $7, $2, 1, $1, 1, '{}'::jsonb,
  3600, 14400, 80, '[]'::jsonb, true, 1, 1, true, $5, $6
);
`, fixture.calendarID, fixture.mspID, fixture.clientID,
		"ai2-calendar-"+strings.ToLower(displaySuffix), fixture.now, fixture.actorID,
		fixture.policyID, "ai2-sla-"+strings.ToLower(displaySuffix)); err != nil {
		t.Fatalf("seed AI second-wave SLA: %v", err)
	}

	if err := execAISecondWaveSeedBatch(ctx, pool, `
INSERT INTO technicians (
  id, msp_id, email, display_name, lifecycle_state, version, created_at, updated_at
) VALUES
  ($1, $4, $5, 'AI Two One', 'active', 1, $6, $6),
  ($2, $4, $7, 'AI Two Two', 'active', 1, $6, $6),
  ($3, $4, $8, 'AI Two Three', 'active', 1, $6, $6);
INSERT INTO roles (
  id, msp_id, key, name, system_role, version, created_at, updated_at
) VALUES ($9, $4, $10, 'AI second-wave role', false, 1, $6, $6);
INSERT INTO role_assignments (
  id, msp_id, client_id, technician_id, role_id, granted_at, granted_by, expires_at
) VALUES
  ($11, $4, $12, $1, $9, $6, $13, $14),
  ($15, $4, $12, $2, $9, $6, $13, $14),
  ($16, $4, $12, $3, $9, $6, $13, $14);
`, fixture.technicianOneID, fixture.technicianTwoID, fixture.technicianThreeID,
		fixture.mspID,
		"ai2-one-"+strings.ToLower(displaySuffix)+"@example.test",
		fixture.now,
		"ai2-two-"+strings.ToLower(displaySuffix)+"@example.test",
		"ai2-three-"+strings.ToLower(displaySuffix)+"@example.test",
		fixture.roleID, "ai2-"+strings.ToLower(displaySuffix),
		fixture.assignmentOneID, fixture.clientID, fixture.actorID,
		fixture.now.Add(24*time.Hour), fixture.assignmentTwoID,
		fixture.assignmentThreeID); err != nil {
		t.Fatalf("seed AI second-wave assignment scope: %v", err)
	}

	if err := execAISecondWaveSeedBatch(ctx, pool, `
INSERT INTO work_records (
  id, msp_id, client_id, display_id, record_type, title, description,
  status, priority, queue_id, primary_owner_id, service_id, contract_id,
  lifecycle_state, version, created_at, created_by, updated_at, updated_by
) VALUES (
  $1, $2, $3, $4, 'incident', 'Existing AI second-wave ticket', '',
  'new', 'normal', $5, $6, $7, $8,
  'active', 1, $9, $10, $9, $10
);
`, fixture.workRecordID, fixture.mspID, fixture.clientID,
		"AI2-EXISTING-"+displaySuffix, fixture.queueID,
		fixture.technicianOneID, fixture.serviceID, fixture.contractID,
		fixture.now, fixture.actorID); err != nil {
		t.Fatalf("seed AI second-wave ticket: %v", err)
	}

	allowedNext := fmt.Sprintf(`["%s"]`, fixture.destinationStageID)
	if err := execAISecondWaveSeedBatch(ctx, pool, `
INSERT INTO pipelines (
  id, msp_id, key, name, enabled, version, created_at, created_by, updated_at, updated_by
) VALUES ($1, $2, $3, 'AI second-wave pipeline', true, 1, $4, $5, $4, $5);
INSERT INTO pipeline_stages (
  id, pipeline_id, msp_id, key, name, position, probability,
  forecast_category, required_fields, allowed_next_stage_ids,
  requires_proposal, requires_approval, version
) VALUES
  ($6, $1, $2, 'qualified', 'Qualified', 1, 40, 'weighted',
   '[]'::jsonb, $7::jsonb, false, false, 1),
  ($8, $1, $2, 'committed', 'Committed', 2, 80, 'committed',
   '[]'::jsonb, '[]'::jsonb, false, false, 1);
INSERT INTO opportunities (
  id, msp_id, client_id, pipeline_id, stage_id, display_id, name,
  description, amount_minor, currency, lifecycle_state, version,
  created_at, created_by, updated_at, updated_by
) VALUES (
  $9, $2, $10, $1, $6, $11, 'AI second-wave opportunity',
  '', 50000, 'USD', 'active', 1, $4, $5, $4, $5
);
`, fixture.pipelineID, fixture.mspID,
		"ai2-"+strings.ToLower(displaySuffix), fixture.now, fixture.actorID,
		fixture.currentStageID, allowedNext, fixture.destinationStageID,
		fixture.opportunityID, fixture.clientID,
		"AI2-OPP-"+displaySuffix); err != nil {
		t.Fatalf("seed AI second-wave sales scope: %v", err)
	}

	if err := execAISecondWaveSeedBatch(ctx, pool, `
INSERT INTO knowledge_articles (
  id, msp_id, client_id, display_id, title, state, current_version,
  client_visible, created_at, created_by, updated_at, updated_by
) VALUES (
  $1, $2, $3, $4, 'AI second-wave article', 'draft', 1,
  false, $5, $6, $5, $6
);
INSERT INTO knowledge_article_versions (
  article_id, msp_id, version, body, created_at, created_by
) VALUES ($1, $2, 1, 'AI second-wave body', $5, $6);
`, fixture.articleID, fixture.mspID, fixture.clientID,
		"AI2-KB-"+displaySuffix, fixture.now, fixture.actorID); err != nil {
		t.Fatalf("seed AI second-wave knowledge: %v", err)
	}
	return fixture
}

func cleanupAISecondWaveFixture(
	t *testing.T,
	pool *pgxpool.Pool,
	fixture aiSecondWaveFixture,
) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Errorf("begin AI second-wave fixture cleanup: %v", err)
		return
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	disable := []string{
		"ALTER TABLE audit_ledger DISABLE TRIGGER audit_ledger_append_only",
		"ALTER TABLE knowledge_article_versions DISABLE TRIGGER knowledge_article_versions_protected",
		"ALTER TABLE routing_rule_versions DISABLE TRIGGER routing_rule_versions_immutable",
		"ALTER TABLE routing_rule_set_versions DISABLE TRIGGER routing_rule_set_versions_immutable",
		"ALTER TABLE workflow_versions DISABLE TRIGGER workflow_versions_immutable",
		"ALTER TABLE sla_policy_versions DISABLE TRIGGER sla_policy_versions_immutable",
		"ALTER TABLE business_calendar_versions DISABLE TRIGGER business_calendar_versions_immutable",
	}
	for _, statement := range disable {
		if _, err := tx.Exec(ctx, statement); err != nil {
			t.Errorf("disable fixture cleanup trigger: %v", err)
			return
		}
	}
	deletes := []string{
		"DELETE FROM event_outbox WHERE msp_id = $1",
		"DELETE FROM audit_ledger WHERE msp_id = $1",
		"DELETE FROM opportunity_activities WHERE msp_id = $1",
		"DELETE FROM proposals WHERE msp_id = $1",
		"DELETE FROM opportunities WHERE msp_id = $1",
		"DELETE FROM pipeline_stages WHERE msp_id = $1",
		"DELETE FROM pipelines WHERE msp_id = $1",
		"DELETE FROM knowledge_article_versions WHERE msp_id = $1",
		"DELETE FROM knowledge_articles WHERE msp_id = $1",
		"DELETE FROM work_record_slas WHERE msp_id = $1",
		"DELETE FROM work_record_workflows WHERE msp_id = $1",
		"DELETE FROM work_record_routing WHERE msp_id = $1",
		"DELETE FROM work_records WHERE msp_id = $1",
		"DELETE FROM routing_rule_versions WHERE msp_id = $1",
		"DELETE FROM routing_rule_set_versions WHERE msp_id = $1",
		"DELETE FROM routing_rule_sets WHERE msp_id = $1",
		"DELETE FROM workflow_versions WHERE msp_id = $1",
		"DELETE FROM workflows WHERE msp_id = $1",
		"DELETE FROM sla_policy_versions WHERE msp_id = $1",
		"DELETE FROM sla_policies WHERE msp_id = $1",
		"DELETE FROM business_calendar_versions WHERE msp_id = $1",
		"DELETE FROM business_calendars WHERE msp_id = $1",
		"DELETE FROM role_assignments WHERE msp_id = $1",
		"DELETE FROM roles WHERE msp_id = $1",
		"DELETE FROM technicians WHERE msp_id = $1",
		"DELETE FROM contracts WHERE msp_id = $1",
		"DELETE FROM services WHERE msp_id = $1",
		"DELETE FROM queues WHERE msp_id = $1",
		"DELETE FROM mention_access_revision_history WHERE msp_id = $1",
		"DELETE FROM mention_access_revisions WHERE msp_id = $1",
		"DELETE FROM client_organizations WHERE msp_id = $1",
		"DELETE FROM msp_organizations WHERE id = $1",
	}
	for _, statement := range deletes {
		if _, err := tx.Exec(ctx, statement, fixture.mspID); err != nil {
			t.Errorf("clean AI second-wave fixture with %q: %v", statement, err)
			return
		}
	}
	enable := []string{
		"ALTER TABLE business_calendar_versions ENABLE TRIGGER business_calendar_versions_immutable",
		"ALTER TABLE sla_policy_versions ENABLE TRIGGER sla_policy_versions_immutable",
		"ALTER TABLE workflow_versions ENABLE TRIGGER workflow_versions_immutable",
		"ALTER TABLE routing_rule_set_versions ENABLE TRIGGER routing_rule_set_versions_immutable",
		"ALTER TABLE routing_rule_versions ENABLE TRIGGER routing_rule_versions_immutable",
		"ALTER TABLE knowledge_article_versions ENABLE TRIGGER knowledge_article_versions_protected",
		"ALTER TABLE audit_ledger ENABLE TRIGGER audit_ledger_append_only",
	}
	for _, statement := range enable {
		if _, err := tx.Exec(ctx, statement); err != nil {
			t.Errorf("enable fixture cleanup trigger: %v", err)
			return
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Errorf("commit AI second-wave fixture cleanup: %v", err)
		return
	}
	var residue int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM msp_organizations WHERE id = $1
`, fixture.mspID).Scan(&residue); err != nil {
		t.Errorf("verify AI second-wave fixture cleanup: %v", err)
	} else if residue != 0 {
		t.Errorf("AI second-wave fixture %s residue=%d", fixture.mspID, residue)
	}
}

func aiSecondWaveFacts(
	fixture aiSecondWaveFixture,
	subjectID string,
	action string,
	subjectType string,
	version int64,
) (mutation.AuditRecord, mutation.EventRecord) {
	correlationID := uuid.NewString()
	at := fixture.now.Add(time.Minute)
	audit := mutation.AuditRecord{
		ID: uuid.NewString(), OccurredAt: at,
		MSPID: fixture.mspID, ClientID: fixture.clientID,
		ActorType: "technician", ActorID: fixture.actorID,
		Action: action, SubjectType: subjectType, SubjectID: subjectID,
		SubjectVersion: version, Source: "ai_workspace",
		Reason:        "AI second-wave PostgreSQL transaction proof",
		CorrelationID: correlationID,
	}
	event := mutation.EventRecord{
		EventID: uuid.NewString(), EventType: action, SchemaVersion: 1,
		OccurredAt: at, MSPID: fixture.mspID, ClientID: fixture.clientID,
		ActorType: "technician", ActorID: fixture.actorID,
		SubjectType: subjectType, SubjectID: subjectID,
		SubjectVersion: version, CorrelationID: correlationID,
		Source: "ai_workspace",
	}
	return audit, event
}

func aiSecondWaveTicketCreate(
	fixture aiSecondWaveFixture,
	recordID string,
) workrecords.CreateMutation {
	audit, event := aiSecondWaveFacts(
		fixture, recordID, "work_record.created", "work_record", 1,
	)
	at := fixture.now.Add(time.Minute)
	return workrecords.CreateMutation{
		Record: workrecords.Record{
			Envelope: object.Envelope{
				ID: recordID, ObjectType: "work_record",
				MSPID: fixture.mspID, ClientID: fixture.clientID,
				DisplayID:      "AI2-CREATE-" + strings.ToUpper(strings.ReplaceAll(recordID, "-", "")),
				LifecycleState: "active", Version: 1,
				CreatedAt: at, CreatedBy: fixture.actorID,
				UpdatedAt: at, UpdatedBy: fixture.actorID,
			},
			Type: workrecords.Incident, Title: "AI second-wave create race",
			Description: "PostgreSQL transaction proof", Status: "new",
			Priority: "normal", QueueID: fixture.queueID,
			ServiceID: fixture.serviceID, ContractID: fixture.contractID,
		},
		Routing: workrecords.RoutingSelection{
			RuleSetID: fixture.ruleSetID, RuleSetVersion: 1, DecidedAt: at,
			Decision: routing.Decision{
				RuleID: fixture.routingRuleID, QueueID: fixture.queueID,
				Explanation: "matched AI second-wave fixture",
			},
		},
		Workflow: workflow.Selection{
			WorkflowID: fixture.workflowID, Version: 1, EvaluatedAt: at,
			Trace: []workflow.TraceEntry{{
				WorkflowID: fixture.workflowID, Version: 1, Matched: true,
			}},
		},
		SLA: workrecords.AppliedSLA{
			ID: uuid.NewString(), PolicyID: fixture.policyID, PolicyVersion: 1,
			CalendarID: fixture.calendarID, CalendarVersion: 1,
			ResponseWarningAt:   at.Add(45 * time.Minute),
			ResponseDueAt:       at.Add(time.Hour),
			ResolutionWarningAt: at.Add(3 * time.Hour),
			ResolutionDueAt:     at.Add(4 * time.Hour),
			PauseStates:         []string{}, ResponseState: sla.Running,
			ResolutionState: sla.Running, Version: 1,
			SelectionTrace: []sla.PolicyTraceEntry{{
				PolicyID: fixture.policyID, Version: 1, Matched: true,
			}},
		},
		Audit: audit, Event: event,
	}
}

func aiSecondWaveAssignment(
	fixture aiSecondWaveFixture,
	ownerID string,
) workrecords.AssignmentMutation {
	audit, event := aiSecondWaveFacts(
		fixture, fixture.workRecordID, "work_record.owner.changed",
		"work_record", 2,
	)
	return workrecords.AssignmentMutation{
		Record: workrecords.Record{
			Envelope: object.Envelope{
				ID: fixture.workRecordID, ObjectType: "work_record",
				MSPID: fixture.mspID, ClientID: fixture.clientID,
				Version: 2, UpdatedAt: fixture.now.Add(time.Minute),
				UpdatedBy: fixture.actorID,
			},
			PrimaryOwnerID: ownerID,
		},
		PreviousOwnerID:       fixture.technicianOneID,
		ExpectedClientVersion: 1, ExpectedOwnerVersion: 1,
		Audit: audit, Event: event,
	}
}

func aiSecondWaveCurrentStage(fixture aiSecondWaveFixture) sales.PipelineStage {
	return sales.PipelineStage{
		ID:         sales.PipelineStageID(fixture.currentStageID),
		PipelineID: fixture.pipelineID, Key: "qualified", Name: "Qualified",
		Position: 1, Probability: 40, Category: sales.Weighted,
		RequiredFields: []sales.FieldKey{},
		AllowedNext: []sales.PipelineStageID{
			sales.PipelineStageID(fixture.destinationStageID),
		},
		RequiresProposal: false, RequiresApproval: false, Version: 1,
	}
}

func aiSecondWaveDestinationStage(fixture aiSecondWaveFixture) sales.PipelineStage {
	return sales.PipelineStage{
		ID:         sales.PipelineStageID(fixture.destinationStageID),
		PipelineID: fixture.pipelineID, Key: "committed", Name: "Committed",
		Position: 2, Probability: 80, Category: sales.Committed,
		RequiredFields:   []sales.FieldKey{},
		AllowedNext:      []sales.PipelineStageID{},
		RequiresProposal: false, RequiresApproval: false, Version: 1,
	}
}

func aiSecondWaveTransition(
	fixture aiSecondWaveFixture,
) sales.TransitionMutation {
	audit, event := aiSecondWaveFacts(
		fixture, fixture.opportunityID, "opportunity.stage.changed",
		"opportunity", 2,
	)
	return sales.TransitionMutation{
		Opportunity: sales.Opportunity{
			ID:    sales.OpportunityID(fixture.opportunityID),
			MSPID: fixture.mspID, ClientID: fixture.clientID,
			PipelineID: fixture.pipelineID,
			StageID:    sales.PipelineStageID(fixture.destinationStageID),
			Version:    2, UpdatedAt: fixture.now.Add(time.Minute),
			UpdatedBy: fixture.actorID,
		},
		PreviousStage:         sales.PipelineStageID(fixture.currentStageID),
		ExpectedClientVersion: 1,
		Pipeline: sales.Pipeline{
			ID: fixture.pipelineID, MSPID: fixture.mspID, Version: 1,
		},
		CurrentStage:     aiSecondWaveCurrentStage(fixture),
		DestinationStage: aiSecondWaveDestinationStage(fixture),
		Audit:            audit, Event: event,
	}
}

func aiSecondWaveActivity(
	fixture aiSecondWaveFixture,
	activityID string,
) sales.CreateOpportunityActivityMutation {
	audit, event := aiSecondWaveFacts(
		fixture, activityID, "opportunity.activity.created",
		"opportunity_activity", 1,
	)
	at := fixture.now.Add(time.Minute)
	return sales.CreateOpportunityActivityMutation{
		Activity: sales.OpportunityActivity{
			ID: activityID, MSPID: fixture.mspID, ClientID: fixture.clientID,
			OpportunityID: sales.OpportunityID(fixture.opportunityID),
			Kind:          "note", Summary: "AI second-wave activity",
			Details:    "PostgreSQL transaction proof",
			OccurredAt: at, CreatedAt: at, CreatedBy: fixture.actorID,
		},
		ExpectedClientVersion: 1, ExpectedOpportunityVersion: 1,
		Pipeline: sales.Pipeline{
			ID: fixture.pipelineID, MSPID: fixture.mspID, Version: 1,
		},
		Stage: aiSecondWaveCurrentStage(fixture),
		Audit: audit, Event: event,
	}
}

func aiSecondWaveProposal(
	fixture aiSecondWaveFixture,
	proposalID string,
) sales.CreateProposalMutation {
	audit, event := aiSecondWaveFacts(
		fixture, proposalID, "proposal.created", "proposal", 1,
	)
	return sales.CreateProposalMutation{
		Proposal: sales.Proposal{
			ID: proposalID, MSPID: fixture.mspID, ClientID: fixture.clientID,
			OpportunityID: fixture.opportunityID,
			DisplayID:     "AI2-PROP-" + strings.ToUpper(strings.ReplaceAll(proposalID, "-", "")),
			State:         sales.ProposalDraft, Version: 1,
			UpdatedAt: fixture.now.Add(time.Minute),
		},
		ExpectedClientVersion: 1, ExpectedOpportunityVersion: 1,
		Audit: audit, Event: event,
	}
}

func aiSecondWaveKnowledgePublish(
	fixture aiSecondWaveFixture,
) knowledge.PublishMutation {
	audit, event := aiSecondWaveFacts(
		fixture, fixture.articleID, "knowledge.published",
		"knowledge_article", 1,
	)
	publishedAt := fixture.now.Add(time.Minute)
	return knowledge.PublishMutation{
		Article: knowledge.Article{
			ID: fixture.articleID, MSPID: fixture.mspID,
			ClientID: fixture.clientID, State: knowledge.Published,
			CurrentVersion: 1, UpdatedAt: publishedAt,
			UpdatedBy: fixture.actorID,
		},
		Version: knowledge.Version{
			ArticleID: fixture.articleID, Version: 1,
			State: knowledge.Published, PublishedAt: &publishedAt,
			PublishedBy: fixture.actorID,
		},
		ExpectedClientVersion: 1,
		Audit:                 audit, Event: event,
	}
}

func beginAISecondWaveBlocker(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) (pgx.Tx, int32) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin PostgreSQL race blocker: %v", err)
	}
	var pid int32
	if err := tx.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		_ = tx.Rollback(context.Background())
		t.Fatalf("read PostgreSQL race blocker PID: %v", err)
	}
	return tx, pid
}

func execAISecondWaveBlocker(
	t *testing.T,
	ctx context.Context,
	tx pgx.Tx,
	query string,
	args ...any,
) {
	t.Helper()
	tag, err := tx.Exec(ctx, query, args...)
	if err != nil {
		_ = tx.Rollback(context.Background())
		t.Fatalf("stage PostgreSQL race blocker: %v", err)
	}
	if tag.RowsAffected() != 1 {
		_ = tx.Rollback(context.Background())
		t.Fatalf("stage PostgreSQL race blocker rows=%d, want 1", tag.RowsAffected())
	}
}

func proveAISecondWaveBlockedRace(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	blocker pgx.Tx,
	blockerPID int32,
	waitingQueryFragment string,
	mutate func(context.Context) error,
	want error,
) {
	t.Helper()
	released := false
	defer func() {
		if !released {
			_ = blocker.Rollback(context.Background())
		}
	}()

	writerCtx, cancelWriter := context.WithTimeout(ctx, 20*time.Second)
	defer cancelWriter()
	result := make(chan error, 1)
	go func() {
		result <- mutate(writerCtx)
	}()

	waitCtx, cancelWait := context.WithTimeout(ctx, 8*time.Second)
	defer cancelWait()
	waitAISecondWaveBlockingEdge(
		t, waitCtx, pool, blockerPID, waitingQueryFragment, result,
	)
	if err := blocker.Commit(ctx); err != nil {
		cancelWriter()
		t.Fatalf("commit PostgreSQL race blocker: %v", err)
	}
	released = true
	select {
	case err := <-result:
		if !errors.Is(err, want) {
			t.Fatalf("mutation after serialized race error=%v, want %v", err, want)
		}
	case <-writerCtx.Done():
		t.Fatalf("mutation did not finish after blocker release: %v", writerCtx.Err())
	}
}

func waitAISecondWaveBlockingEdge(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	blockerPID int32,
	queryFragment string,
	result <-chan error,
) {
	t.Helper()
	const query = `
SELECT EXISTS (
  SELECT 1
  FROM pg_stat_activity AS waiting
  WHERE waiting.datname = current_database()
    AND waiting.pid <> $1
    AND $1 = ANY(pg_blocking_pids(waiting.pid))
    AND ($2 = '' OR position($2 in waiting.query) > 0)
)
`
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-result:
			t.Fatalf(
				"mutation finished before PostgreSQL barrier %q was observed: %v",
				queryFragment, err,
			)
		default:
		}
		var blocked bool
		if err := pool.QueryRow(ctx, query, blockerPID, queryFragment).Scan(&blocked); err != nil {
			t.Fatalf("observe PostgreSQL blocking edge: %v", err)
		}
		if blocked {
			return
		}
		select {
		case err := <-result:
			t.Fatalf(
				"mutation finished before PostgreSQL barrier %q was observed: %v",
				queryFragment, err,
			)
		case <-ctx.Done():
			t.Fatalf(
				"PostgreSQL barrier %q was not observed: %v",
				queryFragment, ctx.Err(),
			)
		case <-ticker.C:
		}
	}
}

func assertAISecondWaveCreateAbsent(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	table string,
	id string,
	auditID string,
	eventID string,
) {
	t.Helper()
	for _, check := range []struct {
		table, column, id string
	}{
		{table, "id", id},
		{"audit_ledger", "id", auditID},
		{"event_outbox", "event_id", eventID},
	} {
		var count int
		query := fmt.Sprintf(
			"SELECT count(*) FROM %s WHERE %s = $1",
			check.table, check.column,
		)
		if err := pool.QueryRow(ctx, query, check.id).Scan(&count); err != nil {
			t.Fatalf("count %s rollback residue: %v", check.table, err)
		}
		if count != 0 {
			t.Fatalf("%s retained %d record(s) for %s", check.table, count, check.id)
		}
	}
}

func testAISecondWaveTicketCreateConcurrency(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Run("Client deactivation cannot race a confirmed create", func(t *testing.T) {
		withAISecondWaveFixture(t, ctx, pool, func(fixture aiSecondWaveFixture) {
			accepted := aiSecondWaveTicketCreate(fixture, uuid.NewString())
			blocker, pid := beginAISecondWaveBlocker(t, ctx, pool)
			execAISecondWaveBlocker(t, ctx, blocker, `
UPDATE client_organizations
SET lifecycle_state = 'inactive', version = version + 1,
    updated_at = $3, updated_by = $4
WHERE id = $1 AND msp_id = $2 AND lifecycle_state = 'active'
`, fixture.clientID, fixture.mspID, fixture.now.Add(time.Minute), fixture.actorID)
			proveAISecondWaveBlockedRace(
				t, ctx, pool, blocker, pid, "FROM client_organizations",
				func(runCtx context.Context) error {
					return NewWorkRecordRepositoryFromPool(pool).CreateAtomic(runCtx, accepted)
				},
				scopeNotFoundError(),
			)
			assertAISecondWaveCreateAbsent(
				t, ctx, pool, "work_records", accepted.Record.ID,
				accepted.Audit.ID, accepted.Event.EventID,
			)
		})
	})

	for _, dependency := range []struct {
		name, table, idColumn string
		id                    func(aiSecondWaveFixture) string
	}{
		{
			name:  "Service deactivation cannot race a confirmed create",
			table: "services", idColumn: "id",
			id: func(f aiSecondWaveFixture) string { return f.serviceID },
		},
		{
			name:  "Contract deactivation cannot race a confirmed create",
			table: "contracts", idColumn: "id",
			id: func(f aiSecondWaveFixture) string { return f.contractID },
		},
	} {
		dependency := dependency
		t.Run(dependency.name, func(t *testing.T) {
			withAISecondWaveFixture(t, ctx, pool, func(fixture aiSecondWaveFixture) {
				accepted := aiSecondWaveTicketCreate(fixture, uuid.NewString())
				blocker, pid := beginAISecondWaveBlocker(t, ctx, pool)
				query := fmt.Sprintf(`
UPDATE %s
SET lifecycle_state = 'inactive', version = version + 1,
    updated_at = $4, updated_by = $5
WHERE %s = $1 AND msp_id = $2 AND client_id = $3
  AND lifecycle_state = 'active'
`, dependency.table, dependency.idColumn)
				execAISecondWaveBlocker(
					t, ctx, blocker, query, dependency.id(fixture),
					fixture.mspID, fixture.clientID,
					fixture.now.Add(time.Minute), fixture.actorID,
				)
				proveAISecondWaveBlockedRace(
					t, ctx, pool, blocker, pid, "FROM "+dependency.table,
					func(runCtx context.Context) error {
						return NewWorkRecordRepositoryFromPool(pool).CreateAtomic(runCtx, accepted)
					},
					scopeNotFoundError(),
				)
				assertAISecondWaveCreateAbsent(
					t, ctx, pool, "work_records", accepted.Record.ID,
					accepted.Audit.ID, accepted.Event.EventID,
				)
			})
		})
	}

	t.Run("routing replacement cannot leave a stale create", func(t *testing.T) {
		withAISecondWaveFixture(t, ctx, pool, func(fixture aiSecondWaveFixture) {
			accepted := aiSecondWaveTicketCreate(fixture, uuid.NewString())
			blocker, pid := beginAISecondWaveBlocker(t, ctx, pool)
			replacementRuleID := uuid.NewString()
			execAISecondWaveBlocker(t, ctx, blocker, `
INSERT INTO routing_rule_set_versions (
  rule_set_id, msp_id, version, published_at, published_by
) VALUES ($1, $2, 2, $3, $4)
`, fixture.ruleSetID, fixture.mspID, fixture.now.Add(time.Minute), fixture.actorID)
			execAISecondWaveBlocker(t, ctx, blocker, `
INSERT INTO routing_rule_versions (
  rule_set_id, msp_id, version, rule_id, position,
  client_id, record_type, priority, queue_id
) VALUES ($1, $2, 2, $3, 1, $4, 'incident', 'normal', $5)
`, fixture.ruleSetID, fixture.mspID, replacementRuleID,
				fixture.clientID, fixture.queueID)
			execAISecondWaveBlocker(t, ctx, blocker, `
UPDATE routing_rule_sets
SET current_version = 2, updated_at = $3, updated_by = $4
WHERE id = $1 AND msp_id = $2 AND current_version = 1
`, fixture.ruleSetID, fixture.mspID, fixture.now.Add(time.Minute), fixture.actorID)
			proveAISecondWaveBlockedRace(
				t, ctx, pool, blocker, pid, "FROM routing_rule_sets",
				func(runCtx context.Context) error {
					return NewWorkRecordRepositoryFromPool(pool).CreateAtomic(runCtx, accepted)
				},
				object.ErrVersionConflict,
			)
			assertAISecondWaveCreateAbsent(
				t, ctx, pool, "work_records", accepted.Record.ID,
				accepted.Audit.ID, accepted.Event.EventID,
			)
		})
	})

	t.Run("workflow replacement cannot leave a stale create", func(t *testing.T) {
		withAISecondWaveFixture(t, ctx, pool, func(fixture aiSecondWaveFixture) {
			accepted := aiSecondWaveTicketCreate(fixture, uuid.NewString())
			blocker, pid := beginAISecondWaveBlocker(t, ctx, pool)
			execAISecondWaveBlocker(t, ctx, blocker, `
INSERT INTO workflow_versions (
  workflow_id, msp_id, version, definition, published_at, published_by
) VALUES ($1, $2, 2, $3::jsonb, $4, $5)
`, fixture.workflowID, fixture.mspID,
				`{"initial_state":"new","states":[{"key":"new"},{"key":"triage"}]}`,
				fixture.now.Add(time.Minute), fixture.actorID)
			execAISecondWaveBlocker(t, ctx, blocker, `
UPDATE workflows
SET current_version = 2, updated_at = $3, updated_by = $4
WHERE id = $1 AND msp_id = $2 AND current_version = 1
`, fixture.workflowID, fixture.mspID, fixture.now.Add(time.Minute), fixture.actorID)
			proveAISecondWaveBlockedRace(
				t, ctx, pool, blocker, pid, "FROM workflows",
				func(runCtx context.Context) error {
					return NewWorkRecordRepositoryFromPool(pool).CreateAtomic(runCtx, accepted)
				},
				object.ErrVersionConflict,
			)
			assertAISecondWaveCreateAbsent(
				t, ctx, pool, "work_records", accepted.Record.ID,
				accepted.Audit.ID, accepted.Event.EventID,
			)
		})
	})

	t.Run("SLA replacement cannot leave a stale create", func(t *testing.T) {
		withAISecondWaveFixture(t, ctx, pool, func(fixture aiSecondWaveFixture) {
			accepted := aiSecondWaveTicketCreate(fixture, uuid.NewString())
			blocker, pid := beginAISecondWaveBlocker(t, ctx, pool)
			execAISecondWaveBlocker(t, ctx, blocker, `
INSERT INTO sla_policy_versions (
  policy_id, msp_id, version, calendar_id, calendar_version, conditions,
  response_target_seconds, resolution_target_seconds, warning_percent,
  pause_states, enabled, priority, stable_order, fallback,
  published_at, published_by
) VALUES (
  $1, $2, 2, $3, 1, '{}'::jsonb,
  1800, 7200, 80, '[]'::jsonb, true, 1, 1, true, $4, $5
)
`, fixture.policyID, fixture.mspID, fixture.calendarID,
				fixture.now.Add(time.Minute), fixture.actorID)
			execAISecondWaveBlocker(t, ctx, blocker, `
UPDATE sla_policies
SET version = 2
WHERE id = $1 AND msp_id = $2 AND version = 1
`, fixture.policyID, fixture.mspID)
			proveAISecondWaveBlockedRace(
				t, ctx, pool, blocker, pid, "FROM sla_policies",
				func(runCtx context.Context) error {
					return NewWorkRecordRepositoryFromPool(pool).CreateAtomic(runCtx, accepted)
				},
				object.ErrVersionConflict,
			)
			assertAISecondWaveCreateAbsent(
				t, ctx, pool, "work_records", accepted.Record.ID,
				accepted.Audit.ID, accepted.Event.EventID,
			)
		})
	})
}

// scopeNotFoundError keeps the desired error literal independent of repository
// implementation helpers while avoiding a package-level mutable sentinel.
func scopeNotFoundError() error {
	return scope.ErrNotFound
}

type aiSecondWaveBarrierDatabase struct {
	database
	afterSQL string
	entered  chan int32
	release  <-chan struct{}
	once     sync.Once
}

func (db *aiSecondWaveBarrierDatabase) Begin(
	ctx context.Context,
) (transaction, error) {
	tx, err := db.database.Begin(ctx)
	if err != nil {
		return nil, err
	}
	var pid int32
	if err := tx.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		_ = tx.Rollback(context.Background())
		return nil, err
	}
	return &aiSecondWaveBarrierTransaction{
		transaction: tx, database: db, pid: pid,
	}, nil
}

type aiSecondWaveBarrierTransaction struct {
	transaction
	database *aiSecondWaveBarrierDatabase
	pid      int32
}

func (tx *aiSecondWaveBarrierTransaction) Exec(
	ctx context.Context,
	query string,
	args ...any,
) (pgconn.CommandTag, error) {
	tag, err := tx.transaction.Exec(ctx, query, args...)
	if err != nil || !strings.Contains(query, tx.database.afterSQL) {
		return tag, err
	}
	tx.database.once.Do(func() {
		tx.database.entered <- tx.pid
		select {
		case <-tx.database.release:
		case <-ctx.Done():
		}
	})
	if err := ctx.Err(); err != nil {
		return tag, err
	}
	return tag, nil
}

func testAISecondWaveAssignmentConcurrency(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Run("same-version assignments serialize on the ticket first", func(t *testing.T) {
		withAISecondWaveFixture(t, ctx, pool, func(fixture aiSecondWaveFixture) {
			first := aiSecondWaveAssignment(fixture, fixture.technicianTwoID)
			second := aiSecondWaveAssignment(fixture, fixture.technicianThreeID)
			entered := make(chan int32, 1)
			release := make(chan struct{})
			released := false
			defer func() {
				if !released {
					close(release)
				}
			}()
			barrierDB := &aiSecondWaveBarrierDatabase{
				database: &poolDatabase{pool: pool},
				afterSQL: "SET primary_owner_id",
				entered:  entered, release: release,
			}
			firstResult := make(chan error, 1)
			go func() {
				firstResult <- NewWorkRecordRepository(barrierDB).
					AssignAtomic(ctx, first)
			}()
			var firstPID int32
			select {
			case firstPID = <-entered:
			case <-time.After(10 * time.Second):
				t.Fatal("first assignment did not reach its post-update barrier")
			}

			secondCtx, cancelSecond := context.WithTimeout(ctx, 20*time.Second)
			defer cancelSecond()
			secondResult := make(chan error, 1)
			go func() {
				secondResult <- NewWorkRecordRepositoryFromPool(pool).
					AssignAtomic(secondCtx, second)
			}()
			waitCtx, cancelWait := context.WithTimeout(ctx, 8*time.Second)
			waitAISecondWaveBlockingEdge(
				t, waitCtx, pool, firstPID, "FROM work_records", secondResult,
			)
			cancelWait()
			close(release)
			released = true
			if err := <-firstResult; err != nil {
				t.Fatalf("first same-version assignment: %v", err)
			}
			if err := <-secondResult; !errors.Is(err, object.ErrVersionConflict) {
				t.Fatalf("second same-version assignment error=%v, want version conflict", err)
			}

			var ownerID string
			var version int64
			if err := pool.QueryRow(ctx, `
SELECT primary_owner_id::text, version
FROM work_records
WHERE id = $1 AND msp_id = $2 AND client_id = $3
`, fixture.workRecordID, fixture.mspID, fixture.clientID).Scan(
				&ownerID, &version,
			); err != nil {
				t.Fatalf("read serialized assignment result: %v", err)
			}
			if ownerID != fixture.technicianTwoID || version != 2 {
				t.Fatalf(
					"serialized assignment owner=%s version=%d, want first owner at version 2",
					ownerID, version,
				)
			}
			assertAISecondWaveFactPresence(
				t, ctx, pool, first.Audit.ID, first.Event.EventID, true,
			)
			assertAISecondWaveFactPresence(
				t, ctx, pool, second.Audit.ID, second.Event.EventID, false,
			)
		})
	})

	t.Run("technician deactivation cannot race assignment", func(t *testing.T) {
		withAISecondWaveFixture(t, ctx, pool, func(fixture aiSecondWaveFixture) {
			accepted := aiSecondWaveAssignment(fixture, fixture.technicianTwoID)
			blocker, pid := beginAISecondWaveBlocker(t, ctx, pool)
			execAISecondWaveBlocker(t, ctx, blocker, `
UPDATE technicians
SET lifecycle_state = 'inactive', version = version + 1, updated_at = $3
WHERE id = $1 AND msp_id = $2 AND lifecycle_state = 'active'
`, fixture.technicianTwoID, fixture.mspID, fixture.now.Add(time.Minute))
			proveAISecondWaveBlockedRace(
				t, ctx, pool, blocker, pid, "FROM technicians",
				func(runCtx context.Context) error {
					return NewWorkRecordRepositoryFromPool(pool).AssignAtomic(runCtx, accepted)
				},
				scope.ErrNotFound,
			)
			assertAISecondWaveAssignmentUnchanged(t, ctx, pool, fixture, accepted)
		})
	})

	t.Run("scope expiry cannot race assignment", func(t *testing.T) {
		withAISecondWaveFixture(t, ctx, pool, func(fixture aiSecondWaveFixture) {
			accepted := aiSecondWaveAssignment(fixture, fixture.technicianTwoID)
			blocker, pid := beginAISecondWaveBlocker(t, ctx, pool)
			execAISecondWaveBlocker(t, ctx, blocker, `
UPDATE role_assignments
SET expires_at = $4
WHERE id = $1 AND msp_id = $2 AND client_id = $3
`, fixture.assignmentTwoID, fixture.mspID, fixture.clientID, fixture.now)
			proveAISecondWaveBlockedRace(
				t, ctx, pool, blocker, pid, "JOIN role_assignments",
				func(runCtx context.Context) error {
					return NewWorkRecordRepositoryFromPool(pool).AssignAtomic(runCtx, accepted)
				},
				scope.ErrNotFound,
			)
			assertAISecondWaveAssignmentUnchanged(t, ctx, pool, fixture, accepted)
		})
	})
}

func assertAISecondWaveAssignmentUnchanged(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	fixture aiSecondWaveFixture,
	accepted workrecords.AssignmentMutation,
) {
	t.Helper()
	var ownerID string
	var version int64
	if err := pool.QueryRow(ctx, `
SELECT primary_owner_id::text, version
FROM work_records
WHERE id = $1 AND msp_id = $2 AND client_id = $3
`, fixture.workRecordID, fixture.mspID, fixture.clientID).Scan(
		&ownerID, &version,
	); err != nil {
		t.Fatalf("read rejected assignment result: %v", err)
	}
	if ownerID != fixture.technicianOneID || version != 1 {
		t.Fatalf(
			"rejected assignment owner=%s version=%d, want original owner at version 1",
			ownerID, version,
		)
	}
	assertAISecondWaveFactPresence(
		t, ctx, pool, accepted.Audit.ID, accepted.Event.EventID, false,
	)
}

func assertAISecondWaveFactPresence(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	auditID string,
	eventID string,
	want bool,
) {
	t.Helper()
	wantCount := 0
	if want {
		wantCount = 1
	}
	for _, check := range []struct {
		table, column, id string
	}{
		{"audit_ledger", "id", auditID},
		{"event_outbox", "event_id", eventID},
	} {
		var count int
		query := fmt.Sprintf(
			"SELECT count(*) FROM %s WHERE %s = $1",
			check.table, check.column,
		)
		if err := pool.QueryRow(ctx, query, check.id).Scan(&count); err != nil {
			t.Fatalf("count %s facts: %v", check.table, err)
		}
		if count != wantCount {
			t.Fatalf(
				"%s fact %s count=%d, want %d",
				check.table, check.id, count, wantCount,
			)
		}
	}
}

func testAISecondWaveOpportunityConcurrency(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	for _, operation := range []struct {
		name   string
		mutate func(context.Context, *pgxpool.Pool, aiSecondWaveFixture) error
		facts  func(aiSecondWaveFixture) (string, string)
	}{
		{
			name: "transition versus Opportunity version change",
			mutate: func(
				runCtx context.Context,
				pool *pgxpool.Pool,
				fixture aiSecondWaveFixture,
			) error {
				return NewSalesRepositoryFromPool(pool).TransitionAtomic(
					runCtx, aiSecondWaveTransition(fixture),
				)
			},
			facts: func(f aiSecondWaveFixture) (string, string) {
				accepted := aiSecondWaveTransition(f)
				return accepted.Audit.ID, accepted.Event.EventID
			},
		},
		{
			name: "activity versus Opportunity version change",
			mutate: func(
				runCtx context.Context,
				pool *pgxpool.Pool,
				fixture aiSecondWaveFixture,
			) error {
				return NewSalesRepositoryFromPool(pool).CreateOpportunityActivityAtomic(
					runCtx, aiSecondWaveActivity(fixture, uuid.NewString()),
				)
			},
		},
	} {
		operation := operation
		t.Run(operation.name, func(t *testing.T) {
			withAISecondWaveFixture(t, ctx, pool, func(fixture aiSecondWaveFixture) {
				var auditID, eventID, activityID string
				var mutate func(context.Context) error
				if strings.HasPrefix(operation.name, "transition") {
					accepted := aiSecondWaveTransition(fixture)
					auditID, eventID = accepted.Audit.ID, accepted.Event.EventID
					mutate = func(runCtx context.Context) error {
						return NewSalesRepositoryFromPool(pool).
							TransitionAtomic(runCtx, accepted)
					}
				} else {
					activityID = uuid.NewString()
					accepted := aiSecondWaveActivity(fixture, activityID)
					auditID, eventID = accepted.Audit.ID, accepted.Event.EventID
					mutate = func(runCtx context.Context) error {
						return NewSalesRepositoryFromPool(pool).
							CreateOpportunityActivityAtomic(runCtx, accepted)
					}
				}
				blocker, pid := beginAISecondWaveBlocker(t, ctx, pool)
				execAISecondWaveBlocker(t, ctx, blocker, `
UPDATE opportunities
SET name = name || ' changed', version = version + 1,
    updated_at = $3, updated_by = $4
WHERE id = $1 AND msp_id = $2 AND lifecycle_state = 'active'
`, fixture.opportunityID, fixture.mspID,
					fixture.now.Add(time.Minute), fixture.actorID)
				// Hold the configuration row in the same competing
				// transaction. The mutation must wait on its target before
				// attempting this pipeline lock.
				execAISecondWaveBlocker(t, ctx, blocker, `
UPDATE pipelines
SET version = version + 1, updated_at = $3, updated_by = $4
WHERE id = $1 AND msp_id = $2 AND enabled
`, fixture.pipelineID, fixture.mspID,
					fixture.now.Add(time.Minute), fixture.actorID)
				proveAISecondWaveBlockedRace(
					t, ctx, pool, blocker, pid, "FROM opportunities",
					mutate, object.ErrVersionConflict,
				)
				assertAISecondWaveOpportunityCompetingChange(
					t, ctx, pool, fixture, activityID, auditID, eventID,
				)
			})
		})
	}

	for _, operation := range []string{"transition", "activity"} {
		operation := operation
		t.Run(operation+" versus Client change", func(t *testing.T) {
			withAISecondWaveFixture(t, ctx, pool, func(fixture aiSecondWaveFixture) {
				var auditID, eventID, activityID string
				var mutate func(context.Context) error
				if operation == "transition" {
					accepted := aiSecondWaveTransition(fixture)
					auditID, eventID = accepted.Audit.ID, accepted.Event.EventID
					mutate = func(runCtx context.Context) error {
						return NewSalesRepositoryFromPool(pool).
							TransitionAtomic(runCtx, accepted)
					}
				} else {
					activityID = uuid.NewString()
					accepted := aiSecondWaveActivity(fixture, activityID)
					auditID, eventID = accepted.Audit.ID, accepted.Event.EventID
					mutate = func(runCtx context.Context) error {
						return NewSalesRepositoryFromPool(pool).
							CreateOpportunityActivityAtomic(runCtx, accepted)
					}
				}
				blocker, pid := beginAISecondWaveBlocker(t, ctx, pool)
				execAISecondWaveBlocker(t, ctx, blocker, `
UPDATE client_organizations
SET lifecycle_state = 'inactive', version = version + 1,
    updated_at = $3, updated_by = $4
WHERE id = $1 AND msp_id = $2 AND lifecycle_state = 'active'
`, fixture.clientID, fixture.mspID,
					fixture.now.Add(time.Minute), fixture.actorID)
				proveAISecondWaveBlockedRace(
					t, ctx, pool, blocker, pid, "FROM client_organizations",
					mutate, scope.ErrNotFound,
				)
				var stageID string
				var version int64
				if err := pool.QueryRow(ctx, `
SELECT stage_id::text, version
FROM opportunities
WHERE id = $1 AND msp_id = $2
`, fixture.opportunityID, fixture.mspID).Scan(&stageID, &version); err != nil {
					t.Fatalf("read Opportunity after rejected Client race: %v", err)
				}
				if stageID != fixture.currentStageID || version != 1 {
					t.Fatalf(
						"rejected %s changed Opportunity stage=%s version=%d",
						operation, stageID, version,
					)
				}
				if activityID != "" {
					assertAISecondWaveCreateAbsent(
						t, ctx, pool, "opportunity_activities", activityID,
						auditID, eventID,
					)
				} else {
					assertAISecondWaveFactPresence(
						t, ctx, pool, auditID, eventID, false,
					)
				}
			})
		})
	}
}

func assertAISecondWaveOpportunityCompetingChange(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	fixture aiSecondWaveFixture,
	activityID string,
	auditID string,
	eventID string,
) {
	t.Helper()
	var stageID string
	var version int64
	if err := pool.QueryRow(ctx, `
SELECT stage_id::text, version
FROM opportunities
WHERE id = $1 AND msp_id = $2
`, fixture.opportunityID, fixture.mspID).Scan(&stageID, &version); err != nil {
		t.Fatalf("read Opportunity after competing version change: %v", err)
	}
	if stageID != fixture.currentStageID || version != 2 {
		t.Fatalf(
			"competing change result stage=%s version=%d, want current stage at version 2",
			stageID, version,
		)
	}
	if activityID != "" {
		assertAISecondWaveCreateAbsent(
			t, ctx, pool, "opportunity_activities", activityID,
			auditID, eventID,
		)
	} else {
		assertAISecondWaveFactPresence(
			t, ctx, pool, auditID, eventID, false,
		)
	}
}

func testAISecondWaveProposalConcurrency(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Run("Opportunity change cannot race proposal create", func(t *testing.T) {
		withAISecondWaveFixture(t, ctx, pool, func(fixture aiSecondWaveFixture) {
			accepted := aiSecondWaveProposal(fixture, uuid.NewString())
			blocker, pid := beginAISecondWaveBlocker(t, ctx, pool)
			execAISecondWaveBlocker(t, ctx, blocker, `
UPDATE opportunities
SET name = name || ' changed', version = version + 1,
    updated_at = $3, updated_by = $4
WHERE id = $1 AND msp_id = $2 AND lifecycle_state = 'active'
`, fixture.opportunityID, fixture.mspID,
				fixture.now.Add(time.Minute), fixture.actorID)
			proveAISecondWaveBlockedRace(
				t, ctx, pool, blocker, pid, "FROM opportunities",
				func(runCtx context.Context) error {
					return NewProposalRepositoryFromPool(pool, uuid.NewString).
						CreateProposalAtomic(runCtx, accepted)
				},
				object.ErrVersionConflict,
			)
			assertAISecondWaveCreateAbsent(
				t, ctx, pool, "proposals", accepted.Proposal.ID,
				accepted.Audit.ID, accepted.Event.EventID,
			)
		})
	})

	t.Run("Client change cannot race proposal create", func(t *testing.T) {
		withAISecondWaveFixture(t, ctx, pool, func(fixture aiSecondWaveFixture) {
			accepted := aiSecondWaveProposal(fixture, uuid.NewString())
			blocker, pid := beginAISecondWaveBlocker(t, ctx, pool)
			execAISecondWaveBlocker(t, ctx, blocker, `
UPDATE client_organizations
SET lifecycle_state = 'inactive', version = version + 1,
    updated_at = $3, updated_by = $4
WHERE id = $1 AND msp_id = $2 AND lifecycle_state = 'active'
`, fixture.clientID, fixture.mspID,
				fixture.now.Add(time.Minute), fixture.actorID)
			proveAISecondWaveBlockedRace(
				t, ctx, pool, blocker, pid, "FROM client_organizations",
				func(runCtx context.Context) error {
					return NewProposalRepositoryFromPool(pool, uuid.NewString).
						CreateProposalAtomic(runCtx, accepted)
				},
				scope.ErrNotFound,
			)
			assertAISecondWaveCreateAbsent(
				t, ctx, pool, "proposals", accepted.Proposal.ID,
				accepted.Audit.ID, accepted.Event.EventID,
			)
		})
	})
}

func testAISecondWaveKnowledgeConcurrency(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Run("draft revision cannot race publication", func(t *testing.T) {
		withAISecondWaveFixture(t, ctx, pool, func(fixture aiSecondWaveFixture) {
			accepted := aiSecondWaveKnowledgePublish(fixture)
			blocker, pid := beginAISecondWaveBlocker(t, ctx, pool)
			execAISecondWaveBlocker(t, ctx, blocker, `
INSERT INTO knowledge_article_versions (
  article_id, msp_id, version, body, created_at, created_by
) VALUES ($1, $2, 2, 'competing revision', $3, $4)
`, fixture.articleID, fixture.mspID,
				fixture.now.Add(time.Minute), fixture.actorID)
			execAISecondWaveBlocker(t, ctx, blocker, `
UPDATE knowledge_articles
SET title = title || ' revised', current_version = 2,
    updated_at = $3, updated_by = $4
WHERE id = $1 AND msp_id = $2 AND current_version = 1
  AND state = 'draft' AND NOT client_visible
`, fixture.articleID, fixture.mspID,
				fixture.now.Add(time.Minute), fixture.actorID)
			proveAISecondWaveBlockedRace(
				t, ctx, pool, blocker, pid, "knowledge_articles",
				func(runCtx context.Context) error {
					return NewKnowledgeRepositoryFromPool(pool).
						PublishAtomic(runCtx, accepted)
				},
				object.ErrVersionConflict,
			)
			assertAISecondWaveKnowledgeState(
				t, ctx, pool, fixture, 2, accepted, false,
			)
		})
	})

	t.Run("Client deactivation cannot race publication", func(t *testing.T) {
		withAISecondWaveFixture(t, ctx, pool, func(fixture aiSecondWaveFixture) {
			accepted := aiSecondWaveKnowledgePublish(fixture)
			blocker, pid := beginAISecondWaveBlocker(t, ctx, pool)
			execAISecondWaveBlocker(t, ctx, blocker, `
UPDATE client_organizations
SET lifecycle_state = 'inactive', version = version + 1,
    updated_at = $3, updated_by = $4
WHERE id = $1 AND msp_id = $2 AND lifecycle_state = 'active'
`, fixture.clientID, fixture.mspID,
				fixture.now.Add(time.Minute), fixture.actorID)
			proveAISecondWaveBlockedRace(
				t, ctx, pool, blocker, pid, "FROM client_organizations",
				func(runCtx context.Context) error {
					return NewKnowledgeRepositoryFromPool(pool).
						PublishAtomic(runCtx, accepted)
				},
				scope.ErrNotFound,
			)
			assertAISecondWaveKnowledgeState(
				t, ctx, pool, fixture, 1, accepted, false,
			)
		})
	})
}

func assertAISecondWaveKnowledgeState(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	fixture aiSecondWaveFixture,
	wantCurrentVersion int64,
	accepted knowledge.PublishMutation,
	wantPublished bool,
) {
	t.Helper()
	var state string
	var currentVersion int64
	if err := pool.QueryRow(ctx, `
SELECT state, current_version
FROM knowledge_articles
WHERE id = $1 AND msp_id = $2 AND client_id = $3
`, fixture.articleID, fixture.mspID, fixture.clientID).Scan(
		&state, &currentVersion,
	); err != nil {
		t.Fatalf("read knowledge article state: %v", err)
	}
	if state != "draft" || currentVersion != wantCurrentVersion {
		t.Fatalf(
			"knowledge state=%s current_version=%d, want draft version %d",
			state, currentVersion, wantCurrentVersion,
		)
	}
	var published bool
	if err := pool.QueryRow(ctx, `
SELECT published_at IS NOT NULL
FROM knowledge_article_versions
WHERE article_id = $1 AND msp_id = $2 AND version = 1
`, fixture.articleID, fixture.mspID).Scan(&published); err != nil {
		t.Fatalf("read knowledge publication marker: %v", err)
	}
	if published != wantPublished {
		t.Fatalf("knowledge version published=%t, want %t", published, wantPublished)
	}
	assertAISecondWaveFactPresence(
		t, ctx, pool, accepted.Audit.ID, accepted.Event.EventID, false,
	)
}

func testAISecondWaveRollback(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	for _, failure := range []string{"audit", "outbox"} {
		failure := failure
		t.Run("ticket create rolls back on "+failure+" failure", func(t *testing.T) {
			withAISecondWaveFixture(t, ctx, pool, func(fixture aiSecondWaveFixture) {
				accepted := aiSecondWaveTicketCreate(fixture, uuid.NewString())
				invalidateAISecondWaveFact(failure, &accepted.Audit, &accepted.Event)
				err := NewWorkRecordRepositoryFromPool(pool).
					CreateAtomic(ctx, accepted)
				assertAISecondWaveFactConstraintError(t, err, failure)
				assertAISecondWaveCreateAbsent(
					t, ctx, pool, "work_records", accepted.Record.ID,
					accepted.Audit.ID, accepted.Event.EventID,
				)
			})
		})

		t.Run("ticket assignment rolls back on "+failure+" failure", func(t *testing.T) {
			withAISecondWaveFixture(t, ctx, pool, func(fixture aiSecondWaveFixture) {
				accepted := aiSecondWaveAssignment(fixture, fixture.technicianTwoID)
				invalidateAISecondWaveFact(failure, &accepted.Audit, &accepted.Event)
				err := NewWorkRecordRepositoryFromPool(pool).
					AssignAtomic(ctx, accepted)
				assertAISecondWaveFactConstraintError(t, err, failure)
				assertAISecondWaveAssignmentUnchanged(
					t, ctx, pool, fixture, accepted,
				)
			})
		})

		t.Run("Opportunity transition rolls back on "+failure+" failure", func(t *testing.T) {
			withAISecondWaveFixture(t, ctx, pool, func(fixture aiSecondWaveFixture) {
				accepted := aiSecondWaveTransition(fixture)
				invalidateAISecondWaveFact(failure, &accepted.Audit, &accepted.Event)
				err := NewSalesRepositoryFromPool(pool).
					TransitionAtomic(ctx, accepted)
				assertAISecondWaveFactConstraintError(t, err, failure)
				assertAISecondWaveOpportunityOriginal(
					t, ctx, pool, fixture, accepted.Audit.ID, accepted.Event.EventID,
				)
			})
		})

		t.Run("Opportunity activity rolls back on "+failure+" failure", func(t *testing.T) {
			withAISecondWaveFixture(t, ctx, pool, func(fixture aiSecondWaveFixture) {
				activityID := uuid.NewString()
				accepted := aiSecondWaveActivity(fixture, activityID)
				invalidateAISecondWaveFact(failure, &accepted.Audit, &accepted.Event)
				err := NewSalesRepositoryFromPool(pool).
					CreateOpportunityActivityAtomic(ctx, accepted)
				assertAISecondWaveFactConstraintError(t, err, failure)
				assertAISecondWaveCreateAbsent(
					t, ctx, pool, "opportunity_activities", activityID,
					accepted.Audit.ID, accepted.Event.EventID,
				)
			})
		})

		t.Run("proposal create rolls back on "+failure+" failure", func(t *testing.T) {
			withAISecondWaveFixture(t, ctx, pool, func(fixture aiSecondWaveFixture) {
				accepted := aiSecondWaveProposal(fixture, uuid.NewString())
				invalidateAISecondWaveFact(failure, &accepted.Audit, &accepted.Event)
				err := NewProposalRepositoryFromPool(pool, uuid.NewString).
					CreateProposalAtomic(ctx, accepted)
				assertAISecondWaveFactConstraintError(t, err, failure)
				assertAISecondWaveCreateAbsent(
					t, ctx, pool, "proposals", accepted.Proposal.ID,
					accepted.Audit.ID, accepted.Event.EventID,
				)
			})
		})

		t.Run("knowledge publish rolls back on "+failure+" failure", func(t *testing.T) {
			withAISecondWaveFixture(t, ctx, pool, func(fixture aiSecondWaveFixture) {
				accepted := aiSecondWaveKnowledgePublish(fixture)
				invalidateAISecondWaveFact(failure, &accepted.Audit, &accepted.Event)
				err := NewKnowledgeRepositoryFromPool(pool).
					PublishAtomic(ctx, accepted)
				assertAISecondWaveFactConstraintError(t, err, failure)
				assertAISecondWaveKnowledgeState(
					t, ctx, pool, fixture, 1, accepted, false,
				)
			})
		})
	}
}

func assertAISecondWaveFactConstraintError(
	t *testing.T,
	err error,
	failure string,
) {
	t.Helper()
	wantConstraint := "audit_ledger_subject_version_check"
	wantTable := "audit_ledger"
	if failure == "outbox" {
		wantConstraint = "event_outbox_subject_version_check"
		wantTable = "event_outbox"
	}
	var postgres *pgconn.PgError
	if !errors.As(err, &postgres) {
		t.Fatalf(
			"%s failure error=%T %v, want *pgconn.PgError",
			failure, err, err,
		)
	}
	if postgres.Code != "23514" ||
		postgres.ConstraintName != wantConstraint ||
		postgres.TableName != wantTable {
		t.Fatalf(
			"%s failure SQLSTATE=%q table=%q constraint=%q, want 23514 %q %q",
			failure, postgres.Code, postgres.TableName, postgres.ConstraintName,
			wantTable, wantConstraint,
		)
	}
	t.Logf(
		"observed PostgreSQL SQLSTATE=%s table=%s constraint=%s",
		postgres.Code, postgres.TableName, postgres.ConstraintName,
	)
}

func invalidateAISecondWaveFact(
	failure string,
	audit *mutation.AuditRecord,
	event *mutation.EventRecord,
) {
	if failure == "audit" {
		audit.SubjectVersion = 0
		return
	}
	event.SubjectVersion = 0
}

func assertAISecondWaveOpportunityOriginal(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	fixture aiSecondWaveFixture,
	auditID string,
	eventID string,
) {
	t.Helper()
	var stageID string
	var version int64
	if err := pool.QueryRow(ctx, `
SELECT stage_id::text, version
FROM opportunities
WHERE id = $1 AND msp_id = $2
`, fixture.opportunityID, fixture.mspID).Scan(&stageID, &version); err != nil {
		t.Fatalf("read rolled-back Opportunity: %v", err)
	}
	if stageID != fixture.currentStageID || version != 1 {
		t.Fatalf(
			"rolled-back Opportunity stage=%s version=%d, want original",
			stageID, version,
		)
	}
	assertAISecondWaveFactPresence(t, ctx, pool, auditID, eventID, false)
}
