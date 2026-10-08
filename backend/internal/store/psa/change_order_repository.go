package psa

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/projects"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type ChangeOrderRepository struct {
	db database
}

var _ projects.ChangeOrderRepository = (*ChangeOrderRepository)(nil)

func NewChangeOrderRepository(db database) *ChangeOrderRepository {
	return &ChangeOrderRepository{db: db}
}

func (r *ChangeOrderRepository) FindChangeOrder(
	ctx context.Context,
	target scope.Target,
	id string,
) (projects.ChangeOrder, error) {
	var order projects.ChangeOrder
	err := r.db.QueryRow(ctx, `
SELECT id::text, project_id::text, msp_id::text, client_id::text,
       display_id, state, current_version, version,
       created_at, created_by::text, updated_at, updated_by::text
FROM change_orders
WHERE id = $1 AND msp_id = $2 AND client_id = $3
`, id, target.MSPID, target.ClientID).Scan(
		&order.ID, (*string)(&order.ProjectID), &order.MSPID, &order.ClientID,
		&order.DisplayID, &order.State, &order.CurrentVersion, &order.Version,
		&order.CreatedAt, &order.CreatedBy, &order.UpdatedAt, &order.UpdatedBy,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return projects.ChangeOrder{}, scope.ErrNotFound
	}
	return order, err
}

func (r *ChangeOrderRepository) FindChangeOrderVersion(
	ctx context.Context,
	target scope.Target,
	id string,
) (projects.ChangeOrderVersion, error) {
	var version projects.ChangeOrderVersion
	err := r.db.QueryRow(ctx, `
SELECT id::text, change_order_id::text, msp_id::text, client_id::text,
       version, description, currency, revenue_delta_minor,
       cost_delta_minor, labor_delta_minutes, issued_at, issued_by::text
FROM change_order_versions
WHERE id = $1 AND msp_id = $2 AND client_id = $3
`, id, target.MSPID, target.ClientID).Scan(
		&version.ID, &version.ChangeOrderID, &version.MSPID, &version.ClientID,
		&version.Version, &version.Description, &version.Currency,
		&version.RevenueDeltaMinor, &version.CostDeltaMinor,
		&version.LaborDeltaMinutes, &version.IssuedAt, &version.IssuedBy,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return projects.ChangeOrderVersion{}, scope.ErrNotFound
	}
	return version, err
}

func (r *ChangeOrderRepository) FindProjectForChange(
	ctx context.Context,
	target scope.Target,
	id projects.ProjectID,
) (projects.Project, error) {
	var project projects.Project
	err := r.db.QueryRow(ctx, `
SELECT p.id::text, p.msp_id::text, p.client_id::text, p.display_id, p.name,
       p.original_proposal_version_id::text, p.lifecycle_state, p.version,
       p.created_at, p.created_by::text,
       original.currency, original.revenue_minor,
       original.labor_cost_minor + original.nonlabor_cost_minor,
       original.labor_minutes,
       current.currency, current.revenue_minor,
       current.labor_cost_minor + current.nonlabor_cost_minor,
       current.labor_minutes
FROM projects p
JOIN project_budgets original
  ON original.project_id = p.id AND original.msp_id = p.msp_id
  AND original.client_id = p.client_id AND original.phase_id IS NULL
  AND original.budget_type = 'original'
JOIN project_budgets current
  ON current.project_id = p.id AND current.msp_id = p.msp_id
  AND current.client_id = p.client_id AND current.phase_id IS NULL
  AND current.budget_type = 'current'
WHERE p.id = $1 AND p.msp_id = $2 AND p.client_id = $3
`, id, target.MSPID, target.ClientID).Scan(
		(*string)(&project.ID), &project.MSPID, &project.ClientID,
		&project.DisplayID, &project.Name, &project.OriginalProposalVersionID,
		&project.LifecycleState, &project.Version, &project.CreatedAt,
		&project.CreatedBy,
		&project.OriginalBaseline.Currency, &project.OriginalBaseline.RevenueMinor,
		&project.OriginalBaseline.CostMinor, &project.OriginalBaseline.PlannedMinutes,
		&project.CurrentBaseline.Currency, &project.CurrentBaseline.RevenueMinor,
		&project.CurrentBaseline.CostMinor, &project.CurrentBaseline.PlannedMinutes,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return projects.Project{}, scope.ErrNotFound
	}
	return project, err
}

func (r *ChangeOrderRepository) CreateChangeOrderAtomic(
	ctx context.Context,
	accepted projects.CreateChangeOrderMutation,
) error {
	return r.withTransaction(ctx, func(tx transaction) error {
		order := accepted.Order
		if _, err := tx.Exec(ctx, `
INSERT INTO change_orders (
  id, project_id, msp_id, client_id, display_id, state,
  current_version, version, created_at, created_by, updated_at, updated_by
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
`, order.ID, order.ProjectID, order.MSPID, order.ClientID, order.DisplayID,
			order.State, order.CurrentVersion, order.Version, order.CreatedAt,
			order.CreatedBy, order.UpdatedAt, order.UpdatedBy); err != nil {
			return err
		}
		return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
	})
}

func (r *ChangeOrderRepository) IssueChangeOrderAtomic(
	ctx context.Context,
	accepted projects.IssueChangeOrderMutation,
) error {
	return r.withTransaction(ctx, func(tx transaction) error {
		order, version := accepted.Order, accepted.Version
		tag, err := updateChangeOrder(ctx, tx, order, projects.ChangeOrderDraft)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return object.ErrVersionConflict
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO change_order_versions (
  id, change_order_id, msp_id, client_id, version, description, currency,
  revenue_delta_minor, cost_delta_minor, labor_delta_minutes,
  issued_at, issued_by
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
`, version.ID, version.ChangeOrderID, version.MSPID, version.ClientID,
			version.Version, version.Description, version.Currency,
			version.RevenueDeltaMinor, version.CostDeltaMinor,
			version.LaborDeltaMinutes, version.IssuedAt, version.IssuedBy); err != nil {
			return err
		}
		return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
	})
}

func (r *ChangeOrderRepository) DecideChangeOrderAtomic(
	ctx context.Context,
	accepted projects.DecideChangeOrderMutation,
) error {
	return r.withTransaction(ctx, func(tx transaction) error {
		tag, err := updateChangeOrder(ctx, tx, accepted.Order, projects.ChangeOrderIssued)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return object.ErrVersionConflict
		}
		decision := accepted.Decision
		if _, err := tx.Exec(ctx, `
INSERT INTO change_order_decisions (
  id, change_order_id, change_order_version_id, msp_id, client_id,
  previous_state, decision, override, reason, decided_at, decided_by
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
`, decision.ID, decision.ChangeOrderID, decision.ChangeOrderVersionID,
			decision.MSPID, decision.ClientID, decision.PreviousState,
			decision.Decision, decision.Override, nullableText(decision.Reason),
			decision.DecidedAt, decision.DecidedBy); err != nil {
			return err
		}
		return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
	})
}

func (r *ChangeOrderRepository) ApplyChangeOrderAtomic(
	ctx context.Context,
	accepted projects.ApplyChangeOrderMutation,
) error {
	return r.withTransaction(ctx, func(tx transaction) error {
		tag, err := updateChangeOrder(ctx, tx, accepted.Order, projects.ChangeOrderApproved)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return object.ErrVersionConflict
		}
		project := accepted.Project
		tag, err = tx.Exec(ctx, `
UPDATE projects
SET version = version + 1, updated_at = $5, updated_by = $6
WHERE id = $1 AND msp_id = $2 AND client_id = $3 AND version = $4
`, project.ID, project.MSPID, project.ClientID, project.Version-1,
			accepted.Application.AppliedAt, accepted.Application.AppliedBy)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return object.ErrVersionConflict
		}
		baseline := project.CurrentBaseline
		tag, err = tx.Exec(ctx, `
UPDATE project_budgets
SET currency = $4, labor_minutes = $5, labor_cost_minor = $6,
    nonlabor_cost_minor = 0, revenue_minor = $7, version = version + 1
WHERE project_id = $1 AND msp_id = $2 AND client_id = $3
  AND phase_id IS NULL AND budget_type = 'current'
`, project.ID, project.MSPID, project.ClientID, baseline.Currency,
			baseline.PlannedMinutes, baseline.CostMinor, baseline.RevenueMinor)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return object.ErrVersionConflict
		}
		application := accepted.Application
		if _, err := tx.Exec(ctx, `
INSERT INTO change_order_applications (
  id, change_order_id, change_order_version_id, project_id,
  msp_id, client_id, applied_at, applied_by
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
`, application.ID, application.ChangeOrderID,
			application.ChangeOrderVersionID, application.ProjectID,
			application.MSPID, application.ClientID,
			application.AppliedAt, application.AppliedBy); err != nil {
			return err
		}
		return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
	})
}

func updateChangeOrder(
	ctx context.Context,
	tx transaction,
	order projects.ChangeOrder,
	expectedState projects.ChangeOrderState,
) (interface{ RowsAffected() int64 }, error) {
	if expectedState == projects.ChangeOrderDraft {
		return tx.Exec(ctx, `
UPDATE change_orders
SET state = $5, current_version = $6, version = version + 1,
    updated_at = $7, updated_by = $8
WHERE id = $1 AND msp_id = $2 AND client_id = $3
  AND version = $4 AND state IN ('draft', 'rejected')
`, order.ID, order.MSPID, order.ClientID, order.Version-1,
			order.State, order.CurrentVersion,
			order.UpdatedAt, order.UpdatedBy)
	}
	return tx.Exec(ctx, `
UPDATE change_orders
SET state = $6, current_version = $7, version = version + 1,
    updated_at = $8, updated_by = $9
WHERE id = $1 AND msp_id = $2 AND client_id = $3
  AND version = $4 AND state = $5
`, order.ID, order.MSPID, order.ClientID, order.Version-1,
		expectedState, order.State, order.CurrentVersion,
		order.UpdatedAt, order.UpdatedBy)
}

func (r *ChangeOrderRepository) withTransaction(
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
