package psa

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/clientidentity"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/projects"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sales"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tasks"
)

type ConversionRepository struct {
	db        database
	newID     func() string
	sales     *SalesRepository
	proposals *ProposalRepository
}

var _ projects.ConversionRepository = (*ConversionRepository)(nil)

func NewConversionRepository(db database, newID func() string) *ConversionRepository {
	return &ConversionRepository{
		db: db, newID: newID,
		sales: NewSalesRepository(db), proposals: NewProposalRepository(db, newID),
	}
}

func (r *ConversionRepository) LoadConversionSource(
	ctx context.Context,
	target scope.Target,
	opportunityID sales.OpportunityID,
	proposalVersionID string,
	selectedTaskIDs []tasks.ID,
) (projects.ConversionSource, error) {
	opportunity, err := r.sales.FindOpportunity(ctx, target, opportunityID)
	if err != nil {
		return projects.ConversionSource{}, err
	}
	version, err := r.proposals.FindProposalVersion(ctx, target, proposalVersionID)
	if err != nil {
		return projects.ConversionSource{}, err
	}
	proposal, err := r.proposals.FindProposal(ctx, target, version.ProposalID)
	if err != nil {
		return projects.ConversionSource{}, err
	}
	acceptance, err := r.findAcceptance(ctx, target, version)
	if err != nil {
		return projects.ConversionSource{}, err
	}
	closedWon, err := r.findClosedWonStage(ctx, opportunity)
	if err != nil {
		return projects.ConversionSource{}, err
	}
	selected, err := r.findSelectedTasks(ctx, target, opportunityID, selectedTaskIDs)
	if err != nil {
		return projects.ConversionSource{}, err
	}
	source := projects.ConversionSource{
		Opportunity: opportunity, ProposalRecord: proposal,
		ProposalVersion: version, Acceptance: acceptance,
		ClosedWonStageID: closedWon, SelectedTasks: selected,
	}
	if opportunity.ProspectID != "" {
		prospect, err := r.findProspect(ctx, target.MSPID, opportunity.ProspectID)
		if err != nil {
			return projects.ConversionSource{}, err
		}
		source.Prospect = &prospect
		candidates, err := r.findCandidateClients(ctx, target.MSPID, prospect)
		if err != nil {
			return projects.ConversionSource{}, err
		}
		source.CandidateClientIDs = candidates
	}
	return source, nil
}

func (r *ConversionRepository) FindOpportunityConversion(
	ctx context.Context,
	target scope.Target,
	opportunityID sales.OpportunityID,
) (projects.ConversionRecord, bool, error) {
	var record projects.ConversionRecord
	var snapshot []byte
	err := r.db.QueryRow(ctx, `
SELECT id::text, opportunity_id::text, proposal_version_id::text,
       project_id::text, msp_id::text, client_id::text, request_key,
       encode(preview_hash, 'hex'), conversion_snapshot,
       converted_at, converted_by::text
FROM opportunity_conversions
WHERE opportunity_id = $1 AND msp_id = $2
  AND (NULLIF($3, '')::uuid IS NULL OR client_id = NULLIF($3, '')::uuid)
`, opportunityID, target.MSPID, target.ClientID).Scan(
		&record.ID, (*string)(&record.OpportunityID), &record.ProposalVersionID,
		(*string)(&record.ProjectID), &record.MSPID, &record.ClientID,
		&record.RequestKey, &record.PreviewHash, &snapshot,
		&record.ConvertedAt, &record.ConvertedBy,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return projects.ConversionRecord{}, false, nil
	}
	if err != nil {
		return projects.ConversionRecord{}, false, err
	}
	if err := json.Unmarshal(snapshot, &record.Snapshot); err != nil {
		return projects.ConversionRecord{}, false, err
	}
	return record, true, nil
}

func (r *ConversionRepository) ConvertAtomic(
	ctx context.Context,
	accepted projects.ConversionMutation,
) error {
	return r.withTransaction(ctx, func(tx transaction) error {
		if accepted.Client != nil {
			client := *accepted.Client
			if err := clientidentity.Enforce(
				ctx,
				client.MSPID,
				client.Name,
				client.DisplayID,
				func(ctx context.Context, query string, args ...any) error {
					_, err := tx.Exec(ctx, query, args...)
					return err
				},
				func(
					ctx context.Context,
					query string,
					args ...any,
				) (clientidentity.Rows, error) {
					return tx.Query(ctx, query, args...)
				},
			); err != nil {
				return err
			}
			if err := r.insertClientFromProspect(ctx, tx, client); err != nil {
				return err
			}
		}
		if err := insertConvertedProject(ctx, tx, accepted.Project); err != nil {
			return err
		}
		if err := insertInitialTagAssignments(ctx, tx, tagging.TargetRef{MSPID: accepted.Project.MSPID, ClientID: accepted.Project.ClientID, ObjectType: tagging.ObjectProject, ObjectID: string(accepted.Project.ID)}, accepted.Project.Version, accepted.InitialTags); err != nil {
			return err
		}
		for _, phase := range accepted.Project.Phases {
			if err := insertPhase(ctx, tx, phase); err != nil {
				return err
			}
			if err := insertPhaseTeams(ctx, tx, phase); err != nil {
				return err
			}
		}
		if err := r.insertBaseline(ctx, tx, accepted.Project, "original", accepted.OriginalBudget); err != nil {
			return err
		}
		if err := r.insertBaseline(ctx, tx, accepted.Project, "current", accepted.CurrentBudget); err != nil {
			return err
		}
		opportunity := accepted.Opportunity
		tag, err := tx.Exec(ctx, `
UPDATE opportunities
SET client_id = $4, prospect_id = NULL, stage_id = $5,
    version = version + 1, updated_at = $6, updated_by = $7
WHERE id = $1 AND msp_id = $2 AND version = $3
`, opportunity.ID, opportunity.MSPID, opportunity.Version-1,
			opportunity.ClientID, opportunity.StageID,
			opportunity.UpdatedAt, opportunity.UpdatedBy)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return object.ErrVersionConflict
		}
		if _, err := tx.Exec(ctx, `
UPDATE proposals
SET client_id = $3, prospect_id = NULL, updated_at = $4, updated_by = $5
WHERE opportunity_id = $1 AND msp_id = $2 AND state = 'accepted'
  AND (client_id IS NULL OR client_id = $3)
`, opportunity.ID, opportunity.MSPID, opportunity.ClientID,
			opportunity.UpdatedAt, opportunity.UpdatedBy); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
UPDATE approvals a
SET client_id = $3
WHERE a.msp_id = $2
  AND EXISTS (
    SELECT 1 FROM proposal_versions pv
    JOIN proposals p ON p.id = pv.proposal_id AND p.msp_id = pv.msp_id
    WHERE pv.id = a.proposal_version_id
      AND p.opportunity_id = $1 AND p.msp_id = $2
  )
`, opportunity.ID, opportunity.MSPID, opportunity.ClientID); err != nil {
			return err
		}
		for _, move := range accepted.TaskMoves {
			task := move.Task
			tag, err := tx.Exec(ctx, `
UPDATE tasks
SET client_id = $4, parent_type = $5, parent_id = $6,
    work_record_id = NULL, version = version + 1,
    updated_at = $7, updated_by = $8
WHERE id = $1 AND msp_id = $2 AND version = $3
`, task.ID, task.MSPID, move.PreviousVersion, task.ClientID,
				task.Parent.Type, task.Parent.ID, move.History.MovedAt,
				move.History.MovedBy)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return tasks.ErrStaleTask
			}
			if _, err := tx.Exec(ctx, `
INSERT INTO task_movement_history (
  id, task_id, msp_id, client_id, from_parent_type, from_parent_id,
  to_parent_type, to_parent_id, previous_version, accepted_version,
  moved_at, moved_by, correlation_id
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
`, r.newID(), task.ID, task.MSPID, task.ClientID,
				move.History.From.Type, move.History.From.ID,
				move.History.To.Type, move.History.To.ID,
				move.History.PreviousVersion, move.History.AcceptedVersion,
				move.History.MovedAt, move.History.MovedBy,
				accepted.Audit.CorrelationID); err != nil {
				return err
			}
		}
		hash, err := hex.DecodeString(accepted.Conversion.PreviewHash)
		if err != nil || len(hash) != 32 {
			return projects.ErrInvalidConversion
		}
		snapshot, err := json.Marshal(accepted.Conversion.Snapshot)
		if err != nil {
			return err
		}
		conversion := accepted.Conversion
		if _, err := tx.Exec(ctx, `
INSERT INTO opportunity_conversions (
  id, opportunity_id, proposal_version_id, project_id, msp_id, client_id,
  request_key, preview_hash, conversion_snapshot, converted_at, converted_by
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
`, conversion.ID, conversion.OpportunityID, conversion.ProposalVersionID,
			conversion.ProjectID, conversion.MSPID, conversion.ClientID,
			conversion.RequestKey, hash, snapshot, conversion.ConvertedAt,
			conversion.ConvertedBy); err != nil {
			return err
		}
		return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
	})
}

func (r *ConversionRepository) insertClientFromProspect(
	ctx context.Context,
	tx transaction,
	client projects.ClientSeed,
) error {
	if _, err := tx.Exec(ctx, `
INSERT INTO client_organizations (
  id, msp_id, display_id, name, lifecycle_state,
  originating_prospect_id, version, created_at, created_by,
  updated_at, updated_by
) VALUES ($1, $2, $3, $4, 'active', $5, 1, $6, $7, $6, $7)
`, client.ID, client.MSPID, client.DisplayID, client.Name,
		client.ProspectID, client.CreatedAt, client.CreatedBy); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
INSERT INTO contacts (
  id, msp_id, client_id, display_id, display_name, email, phone,
  lifecycle_state, version, created_at, created_by, updated_at, updated_by
) VALUES ($1, $2, $3, $4, $5, $6, $7, 'active', 1, $8, $9, $8, $9)
`, r.newID(), client.MSPID, client.ID, client.DisplayID, client.Name,
		nullableText(client.Email), nullableText(client.Phone),
		client.CreatedAt, client.CreatedBy)
	return err
}

func insertConvertedProject(ctx context.Context, tx transaction, project projects.Project) error {
	_, err := tx.Exec(ctx, `
INSERT INTO projects (
  id, msp_id, client_id, display_id, name, original_proposal_version_id,
  lifecycle_state, planned_start, planned_end, version,
  created_at, created_by, updated_at, updated_by
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $11, $12)
`, project.ID, project.MSPID, project.ClientID, project.DisplayID, project.Name,
		project.OriginalProposalVersionID, project.LifecycleState,
		nullableTime(project.PlannedStart), nullableTime(project.PlannedEnd),
		project.Version, project.CreatedAt, project.CreatedBy)
	return err
}

func (r *ConversionRepository) insertBaseline(
	ctx context.Context,
	tx transaction,
	project projects.Project,
	budgetType string,
	baseline projects.BudgetBaseline,
) error {
	_, err := tx.Exec(ctx, `
INSERT INTO project_budgets (
  id, project_id, phase_id, msp_id, client_id, budget_type, currency,
  labor_minutes, labor_cost_minor, nonlabor_cost_minor, revenue_minor, version
) VALUES ($1, $2, NULL, $3, $4, $5, $6, $7, $8, 0, $9, 1)
`, r.newID(), project.ID, project.MSPID, project.ClientID, budgetType,
		baseline.Currency, baseline.PlannedMinutes, baseline.CostMinor,
		baseline.RevenueMinor)
	return err
}

func (r *ConversionRepository) findAcceptance(
	ctx context.Context,
	target scope.Target,
	version sales.ProposalVersion,
) (sales.Acceptance, error) {
	var acceptance sales.Acceptance
	var approvalType string
	var evidence []byte
	err := r.db.QueryRow(ctx, `
SELECT a.id::text, pv.proposal_id::text, a.proposal_version_id::text,
       a.msp_id::text, COALESCE(a.client_id::text, ''), a.approval_type,
       a.signer_name, COALESCE(a.signer_email, ''), a.decision_at,
       COALESCE(a.recorded_by::text, ''), a.evidence,
       a.pdf_snapshot_id::text
FROM approvals a
JOIN proposal_versions pv ON pv.id = a.proposal_version_id AND pv.msp_id = a.msp_id
WHERE a.proposal_version_id = $1 AND a.msp_id = $2
  AND (
    a.client_id = NULLIF($3, '')::uuid
    OR (NULLIF($3, '')::uuid IS NULL AND a.client_id IS NULL)
  )
  AND a.approval_type IN ('customer_electronic', 'customer_offline')
  AND a.state = 'approved'
`, version.ID, target.MSPID, target.ClientID).Scan(
		&acceptance.ID, &acceptance.ProposalID, &acceptance.ProposalVersionID,
		&acceptance.MSPID, &acceptance.ClientID, &approvalType,
		&acceptance.SignerName, &acceptance.SignerEmail,
		&acceptance.AcceptedAt, &acceptance.RecordedBy, &evidence,
		&acceptance.PDFSnapshotID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return sales.Acceptance{}, projects.ErrAcceptedProposalRequired
	}
	if err != nil {
		return sales.Acceptance{}, err
	}
	acceptance.Method = sales.AcceptanceElectronic
	if approvalType == "customer_offline" {
		acceptance.Method = sales.AcceptanceOffline
	}
	if err := json.Unmarshal(evidence, &acceptance.Evidence); err != nil {
		return sales.Acceptance{}, err
	}
	return acceptance, nil
}

func (r *ConversionRepository) findClosedWonStage(
	ctx context.Context,
	opportunity sales.Opportunity,
) (sales.PipelineStageID, error) {
	var id string
	err := r.db.QueryRow(ctx, `
SELECT id::text
FROM pipeline_stages
WHERE pipeline_id = $1 AND msp_id = $2 AND forecast_category = 'closed_won'
ORDER BY position
LIMIT 1
`, opportunity.PipelineID, opportunity.MSPID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", projects.ErrInvalidConversion
	}
	return sales.PipelineStageID(id), err
}

func (r *ConversionRepository) findProspect(
	ctx context.Context,
	mspID, prospectID string,
) (sales.Prospect, error) {
	var prospect sales.Prospect
	err := r.db.QueryRow(ctx, `
SELECT id::text, msp_id::text, display_id, name, COALESCE(email, ''),
       COALESCE(phone, ''), version, created_at, created_by::text
FROM prospects WHERE id = $1 AND msp_id = $2
`, prospectID, mspID).Scan(
		&prospect.ID, &prospect.MSPID, &prospect.DisplayID, &prospect.Name,
		&prospect.Email, &prospect.Phone, &prospect.Version,
		&prospect.CreatedAt, &prospect.CreatedBy,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return sales.Prospect{}, scope.ErrNotFound
	}
	return prospect, err
}

func (r *ConversionRepository) findCandidateClients(
	ctx context.Context,
	mspID string,
	prospect sales.Prospect,
) ([]string, error) {
	result, err := r.db.Query(ctx, `
SELECT DISTINCT c.id::text
FROM client_organizations c
LEFT JOIN contacts ct ON ct.client_id = c.id AND ct.msp_id = c.msp_id
WHERE c.msp_id = $1 AND c.lifecycle_state = 'active'
  AND (
    lower(c.name) = lower($2)
    OR ($3 <> '' AND lower(COALESCE(ct.email, '')) = lower($3))
    OR c.originating_prospect_id = $4
  )
ORDER BY c.id::text
`, mspID, prospect.Name, prospect.Email, prospect.ID)
	if err != nil {
		return nil, err
	}
	defer result.Close()
	var ids []string
	for result.Next() {
		var id string
		if err := result.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, result.Err()
}

func (r *ConversionRepository) findSelectedTasks(
	ctx context.Context,
	target scope.Target,
	opportunityID sales.OpportunityID,
	selected []tasks.ID,
) ([]tasks.Task, error) {
	if len(selected) == 0 {
		return nil, nil
	}
	result, err := r.db.Query(ctx, `
SELECT id::text, msp_id::text, client_id::text, parent_type,
       parent_id::text, COALESCE(work_record_id::text, ''),
       COALESCE(parent_task_id::text, ''), title, status, position,
       version, created_by::text, COALESCE(owner_id::text, ''),
       COALESCE(estimate_minutes, 0)
FROM tasks
WHERE msp_id = $1 AND parent_type = 'opportunity' AND parent_id = $2
  AND ($3 = '' OR client_id = $3::uuid)
  AND id::text = ANY($4::text[])
ORDER BY position
`, target.MSPID, opportunityID, target.ClientID, selected)
	if err != nil {
		return nil, err
	}
	defer result.Close()
	var loaded []tasks.Task
	for result.Next() {
		var task tasks.Task
		if err := result.Scan(
			&task.ID, &task.MSPID, &task.ClientID, &task.Parent.Type,
			&task.Parent.ID, &task.WorkRecordID, &task.ParentTaskID,
			&task.Title, &task.Status, &task.Position, &task.Version,
			&task.CreatedBy, &task.OwnerID, &task.EstimateMinutes,
		); err != nil {
			return nil, err
		}
		task.Parent.MSPID, task.Parent.ClientID = task.MSPID, task.ClientID
		loaded = append(loaded, task)
	}
	if err := result.Err(); err != nil {
		return nil, err
	}
	if len(loaded) != len(selected) {
		return nil, fmt.Errorf("%w: selected task missing", scope.ErrNotFound)
	}
	return loaded, nil
}

func (r *ConversionRepository) withTransaction(
	ctx context.Context,
	fn func(transaction) error,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}
