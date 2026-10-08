-- name: CreateProject :one
INSERT INTO projects (
  id, msp_id, client_id, display_id, name, original_proposal_version_id,
  lifecycle_state, planned_start, planned_end, created_by, updated_by
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10)
RETURNING *;

-- name: GetProjectScoped :one
SELECT * FROM projects
WHERE id = $1 AND msp_id = $2 AND client_id = $3;

-- name: UpdateProjectVersioned :one
UPDATE projects
SET name = $5, lifecycle_state = $6, version = version + 1,
    updated_at = now(), updated_by = $7
WHERE id = $1 AND msp_id = $2 AND client_id = $3 AND version = $4
RETURNING *;

-- name: InsertPhase :one
INSERT INTO phases (
  id, project_id, msp_id, client_id, name, position, state,
  planned_start, planned_end
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: ListProjectPhases :many
SELECT * FROM phases
WHERE project_id = $1 AND msp_id = $2 AND client_id = $3
ORDER BY position;

-- name: InsertResourcePlan :one
INSERT INTO resource_plans (
  id, project_id, phase_id, msp_id, client_id, role_id, team_id,
  starts_on, ends_on, planned_minutes
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: InsertProjectBudget :one
INSERT INTO project_budgets (
  id, project_id, phase_id, msp_id, client_id, budget_type,
  currency, labor_minutes, labor_cost_minor, nonlabor_cost_minor,
  revenue_minor
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: InsertCostActual :one
INSERT INTO cost_actuals (
  id, project_id, phase_id, msp_id, client_id, cost_type,
  description, currency, amount_minor, committed, incurred_at, source_ref
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING *;

-- name: InsertOpportunityConversion :one
INSERT INTO opportunity_conversions (
  id, opportunity_id, proposal_version_id, project_id, msp_id, client_id,
  request_key, preview_hash, conversion_snapshot, converted_by
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (opportunity_id) DO UPDATE
SET opportunity_id = EXCLUDED.opportunity_id
WHERE opportunity_conversions.request_key = EXCLUDED.request_key
  AND opportunity_conversions.preview_hash = EXCLUDED.preview_hash
RETURNING *;

-- name: GetOpportunityConversion :one
SELECT * FROM opportunity_conversions
WHERE opportunity_id = $1 AND msp_id = $2;
