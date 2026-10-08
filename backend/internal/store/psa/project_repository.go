package psa

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar"
	internalid "github.com/rarity-ticket-intelligence/rarity/backend/internal/id"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/projects"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
)

type ProjectRepository struct {
	db database
}

var _ projects.Repository = (*ProjectRepository)(nil)
var _ projects.ProjectQueryRepository = (*ProjectRepository)(nil)
var _ projects.ResourceRepository = (*ProjectRepository)(nil)
var _ projects.CostActualRepository = (*ProjectRepository)(nil)
var _ projects.FinancialRepository = (*ProjectRepository)(nil)
var _ projects.AvailabilityRepository = (*ProjectRepository)(nil)
var _ projects.CapacityRepository = (*ProjectRepository)(nil)
var _ projects.FinancialInputRepository = (*ProjectRepository)(nil)
var _ projects.PhaseFinancialRepository = (*ProjectRepository)(nil)
var _ projects.MilestoneRepository = (*ProjectRepository)(nil)

func NewProjectRepository(db database) *ProjectRepository {
	return &ProjectRepository{db: db}
}

func (r *ProjectRepository) FindMilestoneProject(ctx context.Context, target scope.Target, id projects.ProjectID) (projects.Project, error) {
	if !validTargetUUIDs(target) || !internalid.ValidCanonical(string(id)) {
		return projects.Project{}, projects.ErrInvalidMilestone
	}
	var found projects.Project
	err := r.db.QueryRow(ctx, `SELECT id::text,msp_id::text,client_id::text,name,version FROM projects WHERE id=$1::uuid AND msp_id=$2::uuid AND (NULLIF($3,'')::uuid IS NULL OR client_id=$3::uuid)`, id, target.MSPID, target.ClientID).Scan((*string)(&found.ID), &found.MSPID, &found.ClientID, &found.Name, &found.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return projects.Project{}, scope.ErrNotFound
	}
	return found, err
}

func (r *ProjectRepository) FindMilestonePhase(ctx context.Context, target scope.Target, projectID projects.ProjectID, id projects.PhaseID) (projects.Phase, error) {
	if !validTargetUUIDs(target) || !internalid.ValidCanonical(string(projectID)) || !internalid.ValidCanonical(string(id)) {
		return projects.Phase{}, projects.ErrInvalidMilestone
	}
	var found projects.Phase
	err := r.db.QueryRow(ctx, `SELECT id::text,project_id::text,msp_id::text,client_id::text,name,version FROM phases WHERE id=$1::uuid AND project_id=$2::uuid AND msp_id=$3::uuid AND client_id=$4::uuid`, id, projectID, target.MSPID, target.ClientID).Scan((*string)(&found.ID), (*string)(&found.ProjectID), &found.MSPID, &found.ClientID, &found.Name, &found.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return projects.Phase{}, scope.ErrNotFound
	}
	return found, err
}

func (r *ProjectRepository) FindMilestone(ctx context.Context, target scope.Target, id string) (projects.Milestone, error) {
	if !validTargetUUIDs(target) || !internalid.ValidCanonical(id) {
		return projects.Milestone{}, projects.ErrInvalidMilestone
	}
	var m projects.Milestone
	var recurrence []byte
	err := r.db.QueryRow(ctx, `SELECT id::text,msp_id::text,client_id::text,project_id::text,COALESCE(phase_id::text,''),name,description,priority,due_on,all_day,starts_on,ends_on,starts_at,ends_at,COALESCE(timezone,''),recurrence_rule,status,COALESCE(owner_id::text,''),version,created_at,created_by::text,updated_at,updated_by::text FROM project_milestones WHERE id=$1::uuid AND msp_id=$2::uuid AND (NULLIF($3,'')::uuid IS NULL OR client_id=$3::uuid)`, id, target.MSPID, target.ClientID).Scan(&m.ID, &m.MSPID, &m.ClientID, (*string)(&m.ProjectID), (*string)(&m.PhaseID), &m.Name, &m.Description, &m.Priority, &m.DueOn, &m.AllDay, &m.StartsOn, &m.EndsOn, &m.StartsAt, &m.EndsAt, &m.Timezone, &recurrence, &m.Status, &m.OwnerID, &m.Version, &m.CreatedAt, &m.CreatedBy, &m.UpdatedAt, &m.UpdatedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return projects.Milestone{}, scope.ErrNotFound
	}
	if err == nil && len(recurrence) > 0 {
		err = json.Unmarshal(recurrence, &m.Recurrence)
	}
	return m, err
}

func (r *ProjectRepository) CreateMilestoneAtomic(ctx context.Context, accepted projects.MilestoneMutation) error {
	return r.withTransaction(ctx, func(tx transaction) error {
		m := accepted.Milestone
		if !validMilestoneMutationUUIDs(accepted) {
			return projects.ErrInvalidMilestone
		}
		if err := claimCalendarDomainRequest(ctx, tx, accepted.RequestID, m.MSPID, m.ClientID, "milestone.create", accepted.IdempotencyKey, accepted.RequestFingerprint, m, m.CreatedAt); err != nil {
			return err
		}
		recurrence, err := marshalOptionalJSON(m.Recurrence)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `INSERT INTO project_milestones(id,msp_id,client_id,project_id,phase_id,name,description,priority,due_on,all_day,starts_on,ends_on,starts_at,ends_at,timezone,recurrence_rule,status,owner_id,version,created_at,created_by,updated_at,updated_by)
SELECT $1::uuid,project.msp_id,project.client_id,project.id,NULLIF($5,'')::uuid,$6,$7,$8,$9,$10,$11,$12,$13,$14,NULLIF($15,''),$16::jsonb,$17,NULLIF($18,'')::uuid,1,$19,$20::uuid,$21,$22::uuid
FROM projects project
WHERE project.id = $3::uuid AND project.msp_id = $2::uuid AND project.client_id = $4::uuid
AND (NULLIF($5,'')::uuid IS NULL OR EXISTS(SELECT 1 FROM phases phase WHERE phase.id=$5::uuid AND phase.project_id=project.id AND phase.msp_id=project.msp_id AND phase.client_id=project.client_id))
AND (NULLIF($18,'')::uuid IS NULL OR EXISTS(SELECT 1 FROM technicians technician WHERE technician.id=$18::uuid AND technician.msp_id=project.msp_id AND technician.lifecycle_state='active'))
AND EXISTS(SELECT 1 FROM technicians actor WHERE actor.id=$20::uuid AND actor.msp_id=project.msp_id AND actor.lifecycle_state='active')`, m.ID, m.MSPID, m.ProjectID, m.ClientID, m.PhaseID, m.Name, m.Description, m.Priority, m.DueOn, m.AllDay, m.StartsOn, m.EndsOn, m.StartsAt, m.EndsAt, m.Timezone, recurrence, m.Status, m.OwnerID, m.CreatedAt, m.CreatedBy, m.UpdatedAt, m.UpdatedBy)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return scope.ErrNotFound
		}
		if err = writeAuditOnly(ctx, tx, accepted.Audit); err != nil {
			return err
		}
		return writeEventOnly(ctx, tx, accepted.Event)
	})
}

func (r *ProjectRepository) UpdateMilestoneAtomic(ctx context.Context, accepted projects.MilestoneMutation, expected int64) error {
	return r.withTransaction(ctx, func(tx transaction) error {
		m := accepted.Milestone
		if !validMilestoneMutationUUIDs(accepted) {
			return projects.ErrInvalidMilestone
		}
		if err := claimCalendarDomainRequest(ctx, tx, accepted.RequestID, m.MSPID, m.ClientID, "milestone.update", accepted.IdempotencyKey, accepted.RequestFingerprint, m, m.UpdatedAt); err != nil {
			return err
		}
		recurrence, err := marshalOptionalJSON(m.Recurrence)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE project_milestones milestone SET name=$6,description=$7,priority=$8,due_on=$9,all_day=$10,starts_on=$11,ends_on=$12,starts_at=$13,ends_at=$14,timezone=NULLIF($15,''),recurrence_rule=$16::jsonb,status=$17,owner_id=NULLIF($18,'')::uuid,version=version+1,updated_at=$19,updated_by=$20::uuid
WHERE milestone.id=$1::uuid AND milestone.msp_id=$2::uuid AND milestone.client_id=$3::uuid AND milestone.project_id=$4::uuid AND milestone.version=$5
AND (NULLIF($18,'')::uuid IS NULL OR EXISTS(SELECT 1 FROM technicians technician WHERE technician.id=$18::uuid AND technician.msp_id=milestone.msp_id AND technician.lifecycle_state='active'))`, m.ID, m.MSPID, m.ClientID, m.ProjectID, expected, m.Name, m.Description, m.Priority, m.DueOn, m.AllDay, m.StartsOn, m.EndsOn, m.StartsAt, m.EndsAt, m.Timezone, recurrence, m.Status, m.OwnerID, m.UpdatedAt, m.UpdatedBy)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return classifyScopedVersion(ctx, tx, "project_milestones", m.ID, m.MSPID, m.ClientID, expected, projects.ErrVersionConflict)
		}
		if err = writeAuditOnly(ctx, tx, accepted.Audit); err != nil {
			return err
		}
		return writeEventOnly(ctx, tx, accepted.Event)
	})
}

func validTargetUUIDs(target scope.Target) bool {
	return internalid.ValidCanonical(target.MSPID) && (target.ClientID == "" || internalid.ValidCanonical(target.ClientID))
}
func validMilestoneMutationUUIDs(accepted projects.MilestoneMutation) bool {
	m := accepted.Milestone
	return internalid.ValidCanonical(m.ID) && internalid.ValidCanonical(m.MSPID) && internalid.ValidCanonical(m.ClientID) && internalid.ValidCanonical(string(m.ProjectID)) &&
		(m.PhaseID == "" || internalid.ValidCanonical(string(m.PhaseID))) && (m.OwnerID == "" || internalid.ValidCanonical(m.OwnerID)) && internalid.ValidCanonical(m.CreatedBy) && internalid.ValidCanonical(m.UpdatedBy) && validCalendarFactUUIDs(accepted.RequestID, accepted.Audit, accepted.Event)
}

func (r *ProjectRepository) LoadPhaseFinancialInputs(
	ctx context.Context,
	target scope.Target,
	id projects.ProjectID,
) (map[projects.PhaseID]projects.FinancialInputs, error) {
	rows, err := r.db.Query(ctx, `
SELECT
  phase.id::text,
  COALESCE(original.currency, current.currency, phase.budget_currency, ''),
  COALESCE(original.revenue_minor, phase.budget_minor, 0),
  COALESCE(current.revenue_minor, phase.budget_minor, 0),
  COALESCE(current.labor_cost_minor, 0),
  labor.actual_labor,
  cost.actuals,
  cost.commitments,
  recognized.billable,
  labor.complete,
  (original.currency IS NOT NULL AND current.currency IS NOT NULL
    AND original.currency <> current.currency) OR
    cost.currency_count > 1 OR
    (cost.currency_count = 1 AND cost.currency IS DISTINCT FROM
      COALESCE(original.currency, current.currency, phase.budget_currency)) OR
    labor.currency_count > 1 OR
    (labor.currency_count = 1 AND labor.currency IS DISTINCT FROM
      COALESCE(original.currency, current.currency, phase.budget_currency)) OR
    recognized.currency_count > 1 OR
    (recognized.currency_count = 1 AND recognized.currency IS DISTINCT FROM
      COALESCE(original.currency, current.currency, phase.budget_currency))
FROM phases phase
LEFT JOIN project_budgets original
  ON original.project_id = phase.project_id
  AND original.phase_id = phase.id
  AND original.msp_id = phase.msp_id
  AND original.client_id = phase.client_id
  AND original.budget_type = 'original'
LEFT JOIN project_budgets current
  ON current.project_id = phase.project_id
  AND current.phase_id = phase.id
  AND current.msp_id = phase.msp_id
  AND current.client_id = phase.client_id
  AND current.budget_type = 'current'
LEFT JOIN LATERAL (
  SELECT
    COALESCE(SUM(
      ROUND(te.duration_seconds::numeric * rate.hourly_rate_minor / 3600)
    ) FILTER (WHERE rate.id IS NOT NULL), 0)::bigint AS actual_labor,
    COUNT(*) FILTER (WHERE rate.id IS NULL) = 0 AS complete,
    COUNT(DISTINCT rate.currency) AS currency_count,
    MAX(rate.currency) AS currency
  FROM tasks task
  JOIN time_entries te
    ON te.task_id = task.id AND te.msp_id = task.msp_id
    AND te.client_id = task.client_id
  LEFT JOIN LATERAL (
    SELECT candidate.id, candidate.currency, candidate.hourly_rate_minor
    FROM technician_labor_cost_rates candidate
    WHERE candidate.msp_id = te.msp_id
      AND candidate.technician_id = te.technician_id
      AND candidate.effective_at <= te.started_at
    ORDER BY candidate.effective_at DESC, candidate.id DESC
    LIMIT 1
  ) rate ON true
  WHERE task.parent_type = 'phase' AND task.parent_id = phase.id
    AND task.msp_id = phase.msp_id AND task.client_id = phase.client_id
) labor ON true
LEFT JOIN LATERAL (
  SELECT
    COALESCE(SUM(cost.amount_minor) FILTER (WHERE NOT cost.committed), 0)::bigint AS actuals,
    COALESCE(SUM(cost.amount_minor) FILTER (WHERE cost.committed), 0)::bigint AS commitments,
    COUNT(DISTINCT cost.currency) AS currency_count,
    MAX(cost.currency) AS currency
  FROM cost_actuals cost
  WHERE cost.phase_id = phase.id AND cost.project_id = phase.project_id
    AND cost.msp_id = phase.msp_id AND cost.client_id = phase.client_id
) cost ON true
LEFT JOIN LATERAL (
  SELECT
    COALESCE(SUM(recognized.amount_minor), 0)::bigint AS billable,
    COUNT(DISTINCT recognized.currency) AS currency_count,
    MAX(recognized.currency) AS currency
  FROM recognized_billable_work recognized
  WHERE recognized.phase_id = phase.id
    AND recognized.project_id = phase.project_id
    AND recognized.msp_id = phase.msp_id
    AND recognized.client_id = phase.client_id
) recognized ON true
WHERE phase.project_id = $1 AND phase.msp_id = $2 AND phase.client_id = $3
ORDER BY phase.position, phase.id
`, id, target.MSPID, target.ClientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	found := make(map[projects.PhaseID]projects.FinancialInputs)
	for rows.Next() {
		var (
			phaseID  projects.PhaseID
			currency string
			originalBudget, currentBudget, plannedLabor, actualLabor,
			costActuals, committedCost, billableWork int64
			actualLaborComplete, mixedCurrencies bool
		)
		if err := rows.Scan(
			&phaseID, &currency, &originalBudget, &currentBudget,
			&plannedLabor, &actualLabor, &costActuals, &committedCost,
			&billableWork, &actualLaborComplete, &mixedCurrencies,
		); err != nil {
			return nil, err
		}
		if mixedCurrencies || len(currency) != 3 {
			return nil, projects.ErrInvalidFinancialInputs
		}
		found[phaseID] = projects.FinancialInputs{
			OriginalBudget:      projects.Money{Minor: originalBudget, Currency: currency},
			CurrentBudget:       projects.Money{Minor: currentBudget, Currency: currency},
			PlannedLabor:        projects.Money{Minor: plannedLabor, Currency: currency},
			ActualLabor:         projects.Money{Minor: actualLabor, Currency: currency},
			CostActuals:         projects.Money{Minor: costActuals, Currency: currency},
			CommittedCost:       projects.Money{Minor: committedCost, Currency: currency},
			BillableWork:        projects.Money{Minor: billableWork, Currency: currency},
			ActualLaborComplete: actualLaborComplete,
		}
	}
	return found, rows.Err()
}

func (r *ProjectRepository) CreateLaborCostRateAtomic(
	ctx context.Context,
	accepted projects.LaborCostRateMutation,
) error {
	return r.withTransaction(ctx, func(tx transaction) error {
		rate := accepted.Rate
		var technicianID string
		err := tx.QueryRow(ctx, `
SELECT id::text
FROM technicians
WHERE id = $1 AND msp_id = $2 AND lifecycle_state = 'active'
FOR UPDATE
`, rate.TechnicianID, rate.MSPID).Scan(&technicianID)
		if errors.Is(err, pgx.ErrNoRows) {
			return scope.ErrNotFound
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO technician_labor_cost_rates (
  id, msp_id, technician_id, currency, hourly_rate_minor,
  effective_at, version, created_by
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
`, rate.ID, rate.MSPID, rate.TechnicianID, rate.HourlyRate.Currency,
			rate.HourlyRate.Minor, rate.EffectiveAt, rate.Version,
			accepted.Audit.ActorID,
		); err != nil {
			return err
		}
		return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
	})
}

func (r *ProjectRepository) RecognizeBillableWorkAtomic(
	ctx context.Context,
	accepted projects.BillableWorkMutation,
) error {
	return r.withTransaction(ctx, func(tx transaction) error {
		recognition := accepted.Recognition
		if _, err := tx.Exec(ctx, `
INSERT INTO recognized_billable_work (
  id, project_id, phase_id, msp_id, client_id, description,
  currency, amount_minor, recognized_at, version, created_by
) VALUES (
  $1, $2, NULLIF($3, '')::uuid, $4, $5, $6, $7, $8, $9, $10, $11
)
`, recognition.ID, recognition.ProjectID, recognition.PhaseID,
			recognition.MSPID, recognition.ClientID, recognition.Description,
			recognition.Amount.Currency, recognition.Amount.Minor,
			recognition.RecognizedAt, recognition.Version,
			accepted.Audit.ActorID,
		); err != nil {
			return err
		}
		return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
	})
}

func (r *ProjectRepository) LoadCapacity(
	ctx context.Context,
	target scope.Target,
	window projects.CapacityWindow,
	resourceIDs []string,
) ([]projects.ResourceCapacity, error) {
	principal := authorization.Principal{Scope: scope.Principal{MSPID: target.MSPID}}
	inputs, err := NewCalendarRepository(r.db).LoadCapacityInputs(ctx, principal, calendar.QueryWindow{Start: window.Start, End: window.End}, resourceIDs)
	if err != nil {
		return nil, err
	}
	rows, err := r.db.Query(ctx, `
WITH requested AS (
  SELECT id, display_name FROM technicians
  WHERE msp_id=$1::uuid AND lifecycle_state='active' AND id::text=ANY($2)
), actual AS (
  SELECT entry.technician_id,COALESCE(SUM(EXTRACT(EPOCH FROM (LEAST(entry.ended_at,$4)-GREATEST(entry.started_at,$3)))/60),0)::bigint minutes
  FROM time_entries entry JOIN requested ON requested.id=entry.technician_id
  WHERE entry.msp_id=$1::uuid AND entry.started_at<$4 AND entry.ended_at>$3
  GROUP BY entry.technician_id
)
SELECT requested.id::text,requested.display_name,COALESCE(actual.minutes,0)
FROM requested LEFT JOIN actual ON actual.technician_id=requested.id
ORDER BY requested.display_name,requested.id`, target.MSPID, resourceIDs, window.Start, window.End)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	found := make([]projects.ResourceCapacity, 0, len(resourceIDs))
	for rows.Next() {
		var item projects.ResourceCapacity
		var actual int64
		if err := rows.Scan(&item.ResourceID, &item.Name, &actual); err != nil {
			return nil, err
		}
		summary := calendar.CalculateCapacity(inputs[item.ResourceID])
		item.Available = time.Duration(summary.AvailableMinutes) * time.Minute
		item.Scheduled = time.Duration(summary.CommittedMinutes) * time.Minute
		item.Actual = time.Duration(actual) * time.Minute
		found = append(found, item)
	}
	return found, rows.Err()
}

func (r *ProjectRepository) CreateAvailabilityAtomic(
	ctx context.Context,
	accepted projects.AvailabilityMutation,
) error {
	return r.withTransaction(ctx, func(tx transaction) error {
		availability := accepted.Availability
		var technicianID string
		err := tx.QueryRow(ctx, `
SELECT id::text
FROM technicians
WHERE id = $1 AND msp_id = $2 AND lifecycle_state = 'active'
FOR UPDATE
`, availability.TechnicianID, availability.MSPID).Scan(&technicianID)
		if errors.Is(err, pgx.ErrNoRows) {
			return scope.ErrNotFound
		}
		if err != nil {
			return err
		}
		var overlappingID string
		err = tx.QueryRow(ctx, `
SELECT id::text
FROM technician_availability_windows
WHERE msp_id = $1 AND technician_id = $2
  AND starts_at < $4 AND ends_at > $3
LIMIT 1
`, availability.MSPID, availability.TechnicianID,
			availability.StartsAt, availability.EndsAt,
		).Scan(&overlappingID)
		if err == nil {
			return projects.ErrAvailabilityOverlap
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO technician_availability_windows (
  id, msp_id, technician_id, starts_at, ends_at, available_minutes,
  version, created_by
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
`, availability.ID, availability.MSPID, availability.TechnicianID,
			availability.StartsAt, availability.EndsAt,
			availability.AvailableMinutes, availability.Version,
			accepted.Audit.ActorID,
		); err != nil {
			return err
		}
		return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
	})
}

func (r *ProjectRepository) LoadFinancialInputs(
	ctx context.Context,
	target scope.Target,
	id projects.ProjectID,
) (projects.FinancialInputs, error) {
	var (
		currency, originalCurrency string
		originalBudget, currentBudget, plannedLabor, actualLabor,
		costActuals, committedCost, billableWork int64
		actualLaborComplete, mixedCurrencies bool
	)
	err := r.db.QueryRow(ctx, `
WITH baselines AS (
  SELECT
    MAX(currency) FILTER (WHERE budget_type = 'original') AS original_currency,
    MAX(revenue_minor) FILTER (WHERE budget_type = 'original') AS original_budget,
    MAX(currency) FILTER (WHERE budget_type = 'current') AS current_currency,
    MAX(revenue_minor) FILTER (WHERE budget_type = 'current') AS current_budget,
    MAX(labor_cost_minor) FILTER (WHERE budget_type = 'current') AS planned_labor
  FROM project_budgets
  WHERE project_id = $1 AND msp_id = $2 AND client_id = $3
),
costs AS (
  SELECT
    COALESCE(SUM(amount_minor) FILTER (WHERE NOT committed), 0) AS actuals,
    COALESCE(SUM(amount_minor) FILTER (WHERE committed), 0) AS commitments,
    COUNT(DISTINCT currency) AS currency_count,
    MAX(currency) AS currency
  FROM cost_actuals
  WHERE project_id = $1 AND msp_id = $2 AND client_id = $3
),
labor AS (
  SELECT
    COALESCE(SUM(
      ROUND(te.duration_seconds::numeric * rate.hourly_rate_minor / 3600)
    ) FILTER (WHERE rate.id IS NOT NULL), 0)::bigint AS actual_labor,
    COUNT(*) FILTER (WHERE rate.id IS NULL) = 0 AS complete,
    COUNT(DISTINCT rate.currency) AS currency_count,
    MAX(rate.currency) AS currency
  FROM time_entries te
  JOIN tasks task
    ON task.id = te.task_id AND task.msp_id = te.msp_id
    AND task.client_id = te.client_id
  LEFT JOIN phases phase
    ON task.parent_type = 'phase' AND phase.id = task.parent_id
    AND phase.msp_id = task.msp_id AND phase.client_id = task.client_id
  LEFT JOIN LATERAL (
    SELECT candidate.id, candidate.currency, candidate.hourly_rate_minor
    FROM technician_labor_cost_rates candidate
    WHERE candidate.msp_id = te.msp_id
      AND candidate.technician_id = te.technician_id
      AND candidate.effective_at <= te.started_at
    ORDER BY candidate.effective_at DESC, candidate.id DESC
    LIMIT 1
  ) rate ON true
  WHERE te.msp_id = $2 AND te.client_id = $3
    AND task.parent_type IN ('project', 'phase')
    AND COALESCE(
      phase.project_id,
      CASE WHEN task.parent_type = 'project' THEN task.parent_id END
    ) = $1
),
billable AS (
  SELECT
    COALESCE(SUM(amount_minor), 0)::bigint AS recognized,
    COUNT(DISTINCT currency) AS currency_count,
    MAX(currency) AS currency
  FROM recognized_billable_work
  WHERE project_id = $1 AND msp_id = $2 AND client_id = $3
)
SELECT
  COALESCE(b.original_currency, ''), COALESCE(b.original_budget, 0),
  COALESCE(b.current_budget, 0), COALESCE(b.planned_labor, 0),
  labor.actual_labor, c.actuals, c.commitments, billable.recognized,
  labor.complete,
  b.current_currency IS DISTINCT FROM b.original_currency OR
    c.currency_count > 1 OR
    (c.currency_count = 1 AND c.currency IS DISTINCT FROM b.original_currency) OR
    labor.currency_count > 1 OR
    (labor.currency_count = 1 AND labor.currency IS DISTINCT FROM b.original_currency) OR
    billable.currency_count > 1 OR
    (billable.currency_count = 1 AND billable.currency IS DISTINCT FROM b.original_currency)
FROM baselines b
CROSS JOIN costs c
CROSS JOIN labor
CROSS JOIN billable
`, id, target.MSPID, target.ClientID).Scan(
		&originalCurrency, &originalBudget, &currentBudget, &plannedLabor,
		&actualLabor, &costActuals, &committedCost, &billableWork,
		&actualLaborComplete, &mixedCurrencies,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return projects.FinancialInputs{}, scope.ErrNotFound
	}
	if err != nil {
		return projects.FinancialInputs{}, err
	}
	if mixedCurrencies || len(originalCurrency) != 3 {
		return projects.FinancialInputs{}, projects.ErrInvalidFinancialInputs
	}
	currency = originalCurrency
	return projects.FinancialInputs{
		OriginalBudget:      projects.Money{Minor: originalBudget, Currency: currency},
		CurrentBudget:       projects.Money{Minor: currentBudget, Currency: currency},
		PlannedLabor:        projects.Money{Minor: plannedLabor, Currency: currency},
		ActualLabor:         projects.Money{Minor: actualLabor, Currency: currency},
		CostActuals:         projects.Money{Minor: costActuals, Currency: currency},
		CommittedCost:       projects.Money{Minor: committedCost, Currency: currency},
		BillableWork:        projects.Money{Minor: billableWork, Currency: currency},
		ActualLaborComplete: actualLaborComplete,
	}, nil
}

func (r *ProjectRepository) CreateCostActualAtomic(
	ctx context.Context,
	accepted projects.CostActualMutation,
) error {
	return r.withTransaction(ctx, func(tx transaction) error {
		cost := accepted.Cost
		if _, err := tx.Exec(ctx, `
INSERT INTO cost_actuals (
  id, project_id, phase_id, msp_id, client_id, cost_type, description,
  currency, amount_minor, committed, incurred_at, source_ref, version
) VALUES (
  $1, $2, NULLIF($3, '')::uuid, $4, $5, $6, $7,
  $8, $9, $10, $11, '{}'::jsonb, $12
)
`, cost.ID, cost.ProjectID, cost.PhaseID, cost.MSPID, cost.ClientID,
			cost.CostType, cost.Description, cost.Amount.Currency,
			cost.Amount.Minor, cost.Committed, cost.IncurredAt,
			cost.Version); err != nil {
			return err
		}
		return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
	})
}

func (r *ProjectRepository) CreateResourcePlanAtomic(
	ctx context.Context,
	accepted projects.ResourcePlanMutation,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	plan := accepted.Plan
	if _, err := tx.Exec(ctx, `
INSERT INTO resource_plans (
  id, project_id, phase_id, msp_id, client_id, role_id, team_id,
  starts_on, ends_on, planned_minutes, version
) VALUES (
  $1, $2, $3, $4, $5, NULLIF($6, '')::uuid, NULLIF($7, '')::uuid,
  $8, $9, $10, $11
)
`, plan.ID, plan.ProjectID, plan.PhaseID, plan.MSPID, plan.ClientID,
		plan.RoleID, plan.TeamID, plan.StartsOn, plan.EndsOn,
		plan.PlannedMinutes, plan.Version); err != nil {
		return err
	}
	if err := writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *ProjectRepository) ListProjects(
	ctx context.Context,
	target scope.Target,
	limit int,
) ([]projects.ProjectSummary, error) {
	rows, err := r.db.Query(ctx, `
SELECT id::text, display_id, name, lifecycle_state,
       planned_start, planned_end, version
FROM projects
WHERE msp_id = $1 AND client_id = $2
ORDER BY created_at DESC, id DESC
LIMIT $3
`, target.MSPID, target.ClientID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	found := make([]projects.ProjectSummary, 0)
	for rows.Next() {
		var item projects.ProjectSummary
		var start, end *time.Time
		if err := rows.Scan(
			(*string)(&item.ID), &item.DisplayID, &item.Name,
			&item.LifecycleState, &start, &end, &item.Version,
		); err != nil {
			return nil, err
		}
		item.PlannedStart, item.PlannedEnd = dateString(start), dateString(end)
		found = append(found, item)
	}
	return found, rows.Err()
}

func (r *ProjectRepository) FindProjectsByReference(
	ctx context.Context,
	target scope.Target,
	reference string,
	limit int,
) ([]projects.ProjectSummary, error) {
	if limit < 1 || limit > 2 {
		limit = 2
	}
	normalized := normalizedReference(reference)
	rows, err := r.db.Query(ctx, `
SELECT id::text, display_id, name
FROM projects
WHERE msp_id = $1 AND client_id = $2
  AND (
    lower(btrim(regexp_replace(
      translate(display_id, $4, repeat(' ', char_length($4))),
      '[[:space:]]+', ' ', 'g'
    ))) = $3
    OR lower(btrim(regexp_replace(
      translate(name, $4, repeat(' ', char_length($4))),
      '[[:space:]]+', ' ', 'g'
    ))) = $3
  )
ORDER BY CASE WHEN lower(btrim(regexp_replace(
  translate(display_id, $4, repeat(' ', char_length($4))),
  '[[:space:]]+', ' ', 'g'
))) = $3 THEN 0 ELSE 1 END, id
LIMIT $5
`, target.MSPID, target.ClientID, normalized, unicodeReferenceWhitespace, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	found := make([]projects.ProjectSummary, 0, limit)
	for rows.Next() {
		var item projects.ProjectSummary
		if err := rows.Scan(
			(*string)(&item.ID), &item.DisplayID, &item.Name,
		); err != nil {
			return nil, err
		}
		found = append(found, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return exactReferenceMatches(
		found, reference,
		func(item projects.ProjectSummary) string { return item.DisplayID },
		func(item projects.ProjectSummary) string { return item.Name },
	), nil
}

func (r *ProjectRepository) LoadProjectWorkspace(
	ctx context.Context,
	target scope.Target,
	id projects.ProjectID,
) (projects.ProjectWorkspace, error) {
	project, err := r.FindProject(ctx, target, id)
	if err != nil {
		return projects.ProjectWorkspace{}, err
	}
	workspace := projects.ProjectWorkspace{
		ID: project.ID, DisplayID: project.DisplayID, Name: project.Name,
		LifecycleState: project.LifecycleState,
		PlannedStart:   dateStringValue(project.PlannedStart),
		PlannedEnd:     dateStringValue(project.PlannedEnd),
		Version:        project.Version,
		Phases:         make([]projects.ProjectPhaseView, 0, len(project.Phases)),
		ProjectTasks:   make([]projects.ProjectTaskView, 0),
		ResourcePlans:  make([]projects.ResourcePlanView, 0),
		CostActuals:    make([]projects.CostActualView, 0),
		ChangeOrders:   make([]projects.ChangeOrderView, 0),
	}
	err = r.db.QueryRow(ctx, `
SELECT c.name, COALESCE(pv.version, 0)
FROM projects p
JOIN client_organizations c ON c.id = p.client_id AND c.msp_id = p.msp_id
LEFT JOIN proposal_versions pv
  ON pv.id = p.original_proposal_version_id AND pv.msp_id = p.msp_id
WHERE p.id = $1 AND p.msp_id = $2 AND p.client_id = $3
`, id, target.MSPID, target.ClientID).Scan(
		&workspace.ClientName, &workspace.OriginalProposalVersion,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return projects.ProjectWorkspace{}, scope.ErrNotFound
	}
	if err != nil {
		return projects.ProjectWorkspace{}, err
	}
	for _, phase := range project.Phases {
		workspace.Phases = append(workspace.Phases, projects.ProjectPhaseView{
			ID: phase.ID, Position: phase.Position, Name: phase.Name,
			State: phase.State, OwnerID: phase.OwnerID,
			ParticipatingTeams: append([]string(nil), phase.ParticipatingTeams...),
			PlannedStart:       dateStringValue(phase.PlannedStart),
			PlannedEnd:         dateStringValue(phase.PlannedEnd),
			PlannedMinutes:     phase.PlannedMinutes, Budget: phase.Budget,
			Deliverables:       append([]string(nil), phase.Deliverables...),
			CompletionCriteria: append([]string(nil), phase.CompletionCriteria...),
			Tasks:              make([]projects.ProjectTaskView, 0),
			Version:            phase.Version,
		})
	}
	if err := r.loadProjectBaselines(ctx, target, id, &workspace); err != nil {
		return projects.ProjectWorkspace{}, err
	}
	if err := r.loadProjectDelivery(ctx, target, id, &workspace); err != nil {
		return projects.ProjectWorkspace{}, err
	}
	changeOrders, err := r.loadProjectChangeOrders(ctx, target, id)
	if err != nil {
		return projects.ProjectWorkspace{}, err
	}
	workspace.ChangeOrders = changeOrders
	return workspace, nil
}

func (r *ProjectRepository) loadProjectDelivery(
	ctx context.Context,
	target scope.Target,
	id projects.ProjectID,
	workspace *projects.ProjectWorkspace,
) error {
	phaseTaskRows, err := r.db.Query(ctx, `
SELECT t.parent_id::text, t.id::text, t.title, t.status,
       COALESCE(t.owner_id::text, ''), COALESCE(owner.display_name, ''),
       COUNT(child.id),
       COALESCE(t.estimate_minutes, 0),
       COALESCE((
         SELECT SUM(te.duration_seconds) / 60
         FROM time_entries te
         WHERE te.task_id = t.id AND te.msp_id = t.msp_id
           AND te.client_id = t.client_id
       ), 0), t.version
FROM tasks t
JOIN phases p
  ON p.id = t.parent_id AND p.msp_id = t.msp_id AND p.client_id = t.client_id
LEFT JOIN technicians owner
  ON owner.id = t.owner_id AND owner.msp_id = t.msp_id
LEFT JOIN tasks child
  ON child.parent_task_id = t.id AND child.msp_id = t.msp_id
  AND child.client_id = t.client_id
WHERE t.msp_id = $1 AND t.client_id = $2
  AND t.parent_type = 'phase' AND p.project_id = $3
  AND t.parent_task_id IS NULL
GROUP BY t.parent_id, t.id, t.title, t.status, owner.display_name,
         t.version, t.position, p.position
ORDER BY p.position, t.position, t.id
`, target.MSPID, target.ClientID, id)
	if err != nil {
		return err
	}
	phaseIndex := make(map[projects.PhaseID]int, len(workspace.Phases))
	for index := range workspace.Phases {
		phaseIndex[workspace.Phases[index].ID] = index
	}
	for phaseTaskRows.Next() {
		var phaseID projects.PhaseID
		var task projects.ProjectTaskView
		if err := phaseTaskRows.Scan(
			&phaseID, &task.ID, &task.Title, &task.Status,
			&task.OwnerID, &task.OwnerName,
			&task.Subtasks, &task.EstimateMinutes, &task.ActualMinutes,
			&task.Version,
		); err != nil {
			phaseTaskRows.Close()
			return err
		}
		index, exists := phaseIndex[phaseID]
		if !exists {
			phaseTaskRows.Close()
			return scope.ErrNotFound
		}
		workspace.Phases[index].Tasks = append(workspace.Phases[index].Tasks, task)
		workspace.Phases[index].ActualMinutes += task.ActualMinutes
	}
	if err := phaseTaskRows.Err(); err != nil {
		phaseTaskRows.Close()
		return err
	}
	phaseTaskRows.Close()

	taskRows, err := r.db.Query(ctx, `
SELECT t.id::text, t.title, t.status,
       COALESCE(t.owner_id::text, ''), COALESCE(owner.display_name, ''),
       COUNT(child.id),
       COALESCE(t.estimate_minutes, 0),
       COALESCE((
         SELECT SUM(te.duration_seconds) / 60
         FROM time_entries te
         WHERE te.task_id = t.id AND te.msp_id = t.msp_id
           AND te.client_id = t.client_id
       ), 0), t.version
FROM tasks t
LEFT JOIN technicians owner
  ON owner.id = t.owner_id AND owner.msp_id = t.msp_id
LEFT JOIN tasks child
  ON child.parent_task_id = t.id AND child.msp_id = t.msp_id
  AND child.client_id = t.client_id
WHERE t.msp_id = $1 AND t.client_id = $2
  AND t.parent_type = 'project' AND t.parent_id = $3
  AND t.parent_task_id IS NULL
GROUP BY t.id, t.title, t.status, owner.display_name, t.version, t.position
ORDER BY t.position, t.id
`, target.MSPID, target.ClientID, id)
	if err != nil {
		return err
	}
	for taskRows.Next() {
		var task projects.ProjectTaskView
		if err := taskRows.Scan(
			&task.ID, &task.Title, &task.Status,
			&task.OwnerID, &task.OwnerName,
			&task.Subtasks, &task.EstimateMinutes, &task.ActualMinutes,
			&task.Version,
		); err != nil {
			taskRows.Close()
			return err
		}
		workspace.ProjectTasks = append(workspace.ProjectTasks, task)
	}
	if err := taskRows.Err(); err != nil {
		taskRows.Close()
		return err
	}
	taskRows.Close()

	planRows, err := r.db.Query(ctx, `
SELECT rp.id::text, COALESCE(rp.phase_id::text, ''),
       CASE WHEN rp.role_id IS NOT NULL THEN 'role' ELSE 'team' END,
       COALESCE(role.name, team.name), rp.starts_on, rp.ends_on,
       rp.planned_minutes, rp.version
FROM resource_plans rp
LEFT JOIN roles role ON role.id = rp.role_id AND role.msp_id = rp.msp_id
LEFT JOIN teams team ON team.id = rp.team_id AND team.msp_id = rp.msp_id
WHERE rp.project_id = $1 AND rp.msp_id = $2 AND rp.client_id = $3
ORDER BY rp.starts_on, rp.id
`, id, target.MSPID, target.ClientID)
	if err != nil {
		return err
	}
	for planRows.Next() {
		var plan projects.ResourcePlanView
		var startsOn, endsOn time.Time
		if err := planRows.Scan(
			&plan.ID, &plan.PhaseID, &plan.ResourceType, &plan.ResourceName,
			&startsOn, &endsOn, &plan.PlannedMinutes, &plan.Version,
		); err != nil {
			planRows.Close()
			return err
		}
		plan.StartsOn = startsOn.Format("2006-01-02")
		plan.EndsOn = endsOn.Format("2006-01-02")
		workspace.ResourcePlans = append(workspace.ResourcePlans, plan)
	}
	if err := planRows.Err(); err != nil {
		planRows.Close()
		return err
	}
	planRows.Close()

	costRows, err := r.db.Query(ctx, `
SELECT id::text, COALESCE(phase_id::text, ''), cost_type, description,
       currency, amount_minor, committed, incurred_at, version
FROM cost_actuals
WHERE project_id = $1 AND msp_id = $2 AND client_id = $3
ORDER BY incurred_at DESC, id DESC
`, id, target.MSPID, target.ClientID)
	if err != nil {
		return err
	}
	defer costRows.Close()
	for costRows.Next() {
		var cost projects.CostActualView
		if err := costRows.Scan(
			&cost.ID, &cost.PhaseID, &cost.CostType, &cost.Description,
			&cost.Amount.Currency, &cost.Amount.Minor, &cost.Committed,
			&cost.IncurredAt, &cost.Version,
		); err != nil {
			return err
		}
		workspace.CostActuals = append(workspace.CostActuals, cost)
	}
	return costRows.Err()
}

func (r *ProjectRepository) loadProjectBaselines(
	ctx context.Context,
	target scope.Target,
	id projects.ProjectID,
	workspace *projects.ProjectWorkspace,
) error {
	rows, err := r.db.Query(ctx, `
SELECT budget_type, currency, revenue_minor,
       labor_cost_minor + nonlabor_cost_minor, labor_minutes
FROM project_budgets
WHERE project_id = $1 AND msp_id = $2 AND client_id = $3
  AND phase_id IS NULL AND budget_type IN ('original', 'current')
`, id, target.MSPID, target.ClientID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var budgetType string
		var baseline projects.BudgetBaseline
		if err := rows.Scan(
			&budgetType, &baseline.Currency, &baseline.RevenueMinor,
			&baseline.CostMinor, &baseline.PlannedMinutes,
		); err != nil {
			return err
		}
		if budgetType == "original" {
			workspace.OriginalBaseline = baseline
		} else {
			workspace.CurrentBaseline = baseline
		}
	}
	return rows.Err()
}

func (r *ProjectRepository) loadProjectChangeOrders(
	ctx context.Context,
	target scope.Target,
	projectID projects.ProjectID,
) ([]projects.ChangeOrderView, error) {
	rows, err := r.db.Query(ctx, `
SELECT id::text, project_id::text, msp_id::text, client_id::text,
       display_id, state, current_version, version,
       created_at, created_by::text, updated_at, updated_by::text
FROM change_orders
WHERE project_id = $1 AND msp_id = $2 AND client_id = $3
ORDER BY created_at DESC, id DESC
`, projectID, target.MSPID, target.ClientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	views := make([]projects.ChangeOrderView, 0)
	for rows.Next() {
		var order projects.ChangeOrder
		if err := rows.Scan(
			&order.ID, (*string)(&order.ProjectID), &order.MSPID, &order.ClientID,
			&order.DisplayID, &order.State, &order.CurrentVersion, &order.Version,
			&order.CreatedAt, &order.CreatedBy, &order.UpdatedAt, &order.UpdatedBy,
		); err != nil {
			return nil, err
		}
		views = append(views, projects.ChangeOrderView{Order: order})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for index := range views {
		if views[index].Order.CurrentVersion == 0 {
			continue
		}
		var version projects.ChangeOrderVersion
		err := r.db.QueryRow(ctx, `
SELECT id::text, change_order_id::text, msp_id::text, client_id::text,
       version, description, currency, revenue_delta_minor,
       cost_delta_minor, labor_delta_minutes, issued_at, issued_by::text
FROM change_order_versions
WHERE change_order_id = $1 AND msp_id = $2 AND client_id = $3
  AND version = $4
`, views[index].Order.ID, target.MSPID, target.ClientID,
			views[index].Order.CurrentVersion).Scan(
			&version.ID, &version.ChangeOrderID, &version.MSPID, &version.ClientID,
			&version.Version, &version.Description, &version.Currency,
			&version.RevenueDeltaMinor, &version.CostDeltaMinor,
			&version.LaborDeltaMinutes, &version.IssuedAt, &version.IssuedBy,
		)
		if err != nil {
			return nil, err
		}
		views[index].Version = &version
		var decision projects.ChangeOrderDecision
		err = r.db.QueryRow(ctx, `
SELECT id::text, change_order_id::text, change_order_version_id::text,
       msp_id::text, client_id::text, previous_state, decision,
       override, COALESCE(reason, ''), decided_at, decided_by::text
FROM change_order_decisions
WHERE change_order_version_id = $1 AND msp_id = $2 AND client_id = $3
`, version.ID, target.MSPID, target.ClientID).Scan(
			&decision.ID, &decision.ChangeOrderID, &decision.ChangeOrderVersionID,
			&decision.MSPID, &decision.ClientID, &decision.PreviousState,
			&decision.Decision, &decision.Override, &decision.Reason,
			&decision.DecidedAt, &decision.DecidedBy,
		)
		if err == nil {
			views[index].Decision = &decision
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
	}
	return views, nil
}

func dateString(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.Format("2006-01-02")
}

func dateStringValue(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format("2006-01-02")
}

func (r *ProjectRepository) CreateAtomic(
	ctx context.Context,
	accepted projects.CreateMutation,
) error {
	return r.withTransaction(ctx, func(tx transaction) error {
		project := accepted.Project
		if _, err := tx.Exec(ctx, `
INSERT INTO projects (
  id, msp_id, client_id, display_id, name, original_proposal_version_id,
  lifecycle_state, planned_start, planned_end, version,
  created_at, created_by, updated_at, updated_by
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $11, $12)
`, project.ID, project.MSPID, project.ClientID, project.DisplayID, project.Name,
			project.OriginalProposalVersionID, project.LifecycleState,
			nullableTime(project.PlannedStart), nullableTime(project.PlannedEnd),
			project.Version, project.CreatedAt, project.CreatedBy); err != nil {
			return err
		}
		for _, phase := range project.Phases {
			if err := insertPhase(ctx, tx, phase); err != nil {
				return err
			}
			if err := insertPhaseTeams(ctx, tx, phase); err != nil {
				return err
			}
		}
		if err := insertInitialTagAssignments(ctx, tx, tagging.TargetRef{MSPID: project.MSPID, ClientID: project.ClientID, ObjectType: tagging.ObjectProject, ObjectID: string(project.ID)}, project.Version, accepted.InitialTags); err != nil {
			return err
		}
		return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
	})
}

func (r *ProjectRepository) CreateAIWorkspaceProjectAtomic(
	ctx context.Context,
	accepted projects.AIWorkspaceCreateMutation,
) error {
	if len(accepted.Audits) != len(accepted.Events) ||
		len(accepted.Audits) != len(accepted.Tasks)+1 {
		return projects.ErrInvalidProject
	}
	return r.withTransaction(ctx, func(tx transaction) error {
		project := accepted.Project
		if err := lockActiveClient(
			ctx, tx, project.MSPID, project.ClientID,
		); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO projects (
  id, msp_id, client_id, display_id, name, original_proposal_version_id,
  lifecycle_state, planned_start, planned_end, version,
  created_at, created_by, updated_at, updated_by
) VALUES (
  $1, $2, $3, $4, $5, NULLIF($6, '')::uuid,
  $7, $8, $9, $10, $11, $12, $11, $12
)
`, project.ID, project.MSPID, project.ClientID, project.DisplayID, project.Name,
			project.OriginalProposalVersionID, project.LifecycleState,
			nullableTime(project.PlannedStart), nullableTime(project.PlannedEnd),
			project.Version, project.CreatedAt, project.CreatedBy); err != nil {
			return err
		}
		for _, task := range accepted.Tasks {
			if _, err := tx.Exec(ctx, `
INSERT INTO tasks (
  id, msp_id, client_id, parent_type, parent_id, work_record_id,
  parent_task_id, title, status, position, owner_id, estimate_minutes,
  version, created_at, created_by, updated_at, updated_by
) VALUES (
  $1, $2, $3, $4, $5::uuid, NULL,
  NULL, $6, $7, $8, NULL, $9,
  $10, $11, $12, $11, $12
)
`, task.ID, task.MSPID, task.ClientID, task.Parent.Type, task.Parent.ID,
				task.Title, task.Status, task.Position, task.EstimateMinutes,
				task.Version, project.CreatedAt, task.CreatedBy); err != nil {
				return err
			}
		}
		if err := insertInitialTagAssignments(ctx, tx, tagging.TargetRef{
			MSPID: project.MSPID, ClientID: project.ClientID, ObjectType: tagging.ObjectProject, ObjectID: string(project.ID),
		}, project.Version, accepted.InitialTags); err != nil {
			return err
		}
		for index := range accepted.Audits {
			if err := writeMutationFacts(
				ctx, tx, accepted.Audits[index], accepted.Events[index],
			); err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *ProjectRepository) FindProject(
	ctx context.Context,
	target scope.Target,
	id projects.ProjectID,
) (projects.Project, error) {
	var project projects.Project
	var plannedStart, plannedEnd *time.Time
	err := r.db.QueryRow(ctx, `
SELECT id::text, msp_id::text, client_id::text, display_id, name,
       COALESCE(original_proposal_version_id::text, ''), lifecycle_state,
       planned_start, planned_end, version, created_at, created_by::text
FROM projects
WHERE id = $1 AND msp_id = $2 AND client_id = $3
`, id, target.MSPID, target.ClientID).Scan(
		(*string)(&project.ID), &project.MSPID, &project.ClientID,
		&project.DisplayID, &project.Name, &project.OriginalProposalVersionID,
		&project.LifecycleState, &plannedStart, &plannedEnd, &project.Version,
		&project.CreatedAt, &project.CreatedBy,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return projects.Project{}, scope.ErrNotFound
	}
	if err != nil {
		return projects.Project{}, err
	}
	project.PlannedStart = timeValue(plannedStart)
	project.PlannedEnd = timeValue(plannedEnd)
	phases, err := r.findProjectPhases(ctx, target, project.ID)
	if err != nil {
		return projects.Project{}, err
	}
	project.Phases = phases
	return project, nil
}

func (r *ProjectRepository) FindPhase(
	ctx context.Context,
	target scope.Target,
	id projects.PhaseID,
) (projects.Phase, error) {
	var phase projects.Phase
	var plannedStart, plannedEnd, actualStart, actualEnd *time.Time
	var currency *string
	var deliverables, criteria []byte
	err := r.db.QueryRow(ctx, phaseSelect+`
WHERE p.id = $1 AND p.msp_id = $2 AND p.client_id = $3
`, id, target.MSPID, target.ClientID).Scan(
		(*string)(&phase.ID), (*string)(&phase.ProjectID), &phase.MSPID,
		&phase.ClientID, &phase.Position, &phase.Name, &phase.State,
		&phase.OwnerID, &plannedStart, &plannedEnd, &actualStart, &actualEnd,
		&phase.PlannedMinutes, &phase.Budget.Minor, &currency,
		&deliverables, &criteria, &phase.Version,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return projects.Phase{}, scope.ErrNotFound
	}
	if err != nil {
		return projects.Phase{}, err
	}
	if err := mapPhaseValues(&phase, plannedStart, plannedEnd, actualStart, actualEnd, currency, deliverables, criteria); err != nil {
		return projects.Phase{}, err
	}
	teams, err := r.findPhaseTeams(ctx, target, phase.ID)
	if err != nil {
		return projects.Phase{}, err
	}
	phase.ParticipatingTeams = teams
	return phase, nil
}

func (r *ProjectRepository) UpdatePhaseAtomic(
	ctx context.Context,
	accepted projects.PhaseMutation,
) error {
	return r.withTransaction(ctx, func(tx transaction) error {
		phase := accepted.Phase
		deliverables, err := json.Marshal(phase.Deliverables)
		if err != nil {
			return err
		}
		criteria, err := json.Marshal(phase.CompletionCriteria)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `
UPDATE phases
SET name = $6, owner_id = $7, planned_start = $8, planned_end = $9,
    actual_start = $10, actual_end = $11, planned_minutes = $12,
    budget_minor = $13, budget_currency = $14, deliverables = $15,
    completion_criteria = $16, version = version + 1
WHERE id = $1 AND project_id = $2 AND msp_id = $3 AND client_id = $4
  AND version = $5
`, phase.ID, phase.ProjectID, phase.MSPID, phase.ClientID, phase.Version-1,
			phase.Name, nullableID(phase.OwnerID), nullableTime(phase.PlannedStart),
			nullableTime(phase.PlannedEnd), phase.ActualStart, phase.ActualEnd,
			phase.PlannedMinutes, phase.Budget.Minor,
			nullableText(phase.Budget.Currency), deliverables, criteria)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return object.ErrVersionConflict
		}
		if _, err := tx.Exec(ctx, `
DELETE FROM phase_participating_teams
WHERE phase_id = $1 AND msp_id = $2 AND client_id = $3
`, phase.ID, phase.MSPID, phase.ClientID); err != nil {
			return err
		}
		if err := insertPhaseTeams(ctx, tx, phase); err != nil {
			return err
		}
		return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
	})
}

const phaseSelect = `
SELECT p.id::text, p.project_id::text, p.msp_id::text, p.client_id::text,
       p.position, p.name, p.state, COALESCE(p.owner_id::text, ''),
       p.planned_start, p.planned_end, p.actual_start, p.actual_end,
       p.planned_minutes, p.budget_minor, p.budget_currency,
       p.deliverables, p.completion_criteria, p.version
FROM phases p
`

func (r *ProjectRepository) findProjectPhases(
	ctx context.Context,
	target scope.Target,
	projectID projects.ProjectID,
) ([]projects.Phase, error) {
	result, err := r.db.Query(ctx, phaseSelect+`
WHERE p.project_id = $1 AND p.msp_id = $2 AND p.client_id = $3
ORDER BY p.position
`, projectID, target.MSPID, target.ClientID)
	if err != nil {
		return nil, err
	}
	defer result.Close()
	var phases []projects.Phase
	for result.Next() {
		var phase projects.Phase
		var plannedStart, plannedEnd, actualStart, actualEnd *time.Time
		var currency *string
		var deliverables, criteria []byte
		if err := result.Scan(
			(*string)(&phase.ID), (*string)(&phase.ProjectID), &phase.MSPID,
			&phase.ClientID, &phase.Position, &phase.Name, &phase.State,
			&phase.OwnerID, &plannedStart, &plannedEnd, &actualStart, &actualEnd,
			&phase.PlannedMinutes, &phase.Budget.Minor, &currency,
			&deliverables, &criteria, &phase.Version,
		); err != nil {
			return nil, err
		}
		if err := mapPhaseValues(&phase, plannedStart, plannedEnd, actualStart, actualEnd, currency, deliverables, criteria); err != nil {
			return nil, err
		}
		phases = append(phases, phase)
	}
	if err := result.Err(); err != nil {
		return nil, err
	}
	for index := range phases {
		teams, err := r.findPhaseTeams(ctx, target, phases[index].ID)
		if err != nil {
			return nil, err
		}
		phases[index].ParticipatingTeams = teams
	}
	return phases, nil
}

func (r *ProjectRepository) findPhaseTeams(
	ctx context.Context,
	target scope.Target,
	phaseID projects.PhaseID,
) ([]string, error) {
	result, err := r.db.Query(ctx, `
SELECT team_id::text
FROM phase_participating_teams
WHERE phase_id = $1 AND msp_id = $2 AND client_id = $3
ORDER BY position
`, phaseID, target.MSPID, target.ClientID)
	if err != nil {
		return nil, err
	}
	defer result.Close()
	teams := make([]string, 0)
	for result.Next() {
		var team string
		if err := result.Scan(&team); err != nil {
			return nil, err
		}
		teams = append(teams, team)
	}
	return teams, result.Err()
}

func insertPhase(ctx context.Context, tx transaction, phase projects.Phase) error {
	if phase.Deliverables == nil {
		phase.Deliverables = []string{}
	}
	if phase.CompletionCriteria == nil {
		phase.CompletionCriteria = []string{}
	}
	deliverables, err := json.Marshal(phase.Deliverables)
	if err != nil {
		return err
	}
	criteria, err := json.Marshal(phase.CompletionCriteria)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
INSERT INTO phases (
  id, project_id, msp_id, client_id, name, position, state,
  owner_id, planned_start, planned_end, actual_start, actual_end,
  planned_minutes, budget_minor, budget_currency, deliverables,
  completion_criteria, version
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
          $13, $14, $15, $16, $17, $18)
`, phase.ID, phase.ProjectID, phase.MSPID, phase.ClientID, phase.Name,
		phase.Position, phase.State, nullableID(phase.OwnerID),
		nullableTime(phase.PlannedStart), nullableTime(phase.PlannedEnd),
		phase.ActualStart, phase.ActualEnd, phase.PlannedMinutes,
		phase.Budget.Minor, nullableText(phase.Budget.Currency),
		deliverables, criteria, phase.Version)
	return err
}

func insertPhaseTeams(ctx context.Context, tx transaction, phase projects.Phase) error {
	for index, teamID := range phase.ParticipatingTeams {
		if _, err := tx.Exec(ctx, `
INSERT INTO phase_participating_teams (
  phase_id, team_id, msp_id, client_id, position
) VALUES ($1, $2, $3, $4, $5)
`, phase.ID, teamID, phase.MSPID, phase.ClientID, index+1); err != nil {
			return err
		}
	}
	return nil
}

func mapPhaseValues(
	phase *projects.Phase,
	plannedStart, plannedEnd, actualStart, actualEnd *time.Time,
	currency *string,
	deliverables, criteria []byte,
) error {
	phase.PlannedStart = timeValue(plannedStart)
	phase.PlannedEnd = timeValue(plannedEnd)
	phase.ActualStart = actualStart
	phase.ActualEnd = actualEnd
	if currency != nil {
		phase.Budget.Currency = *currency
	}
	if err := json.Unmarshal(deliverables, &phase.Deliverables); err != nil {
		return err
	}
	return json.Unmarshal(criteria, &phase.CompletionCriteria)
}

func (r *ProjectRepository) withTransaction(
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

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

func timeValue(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return *value
}
