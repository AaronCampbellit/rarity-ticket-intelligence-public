-- name: CreateProspect :one
INSERT INTO prospects (
  id, msp_id, display_id, name, email, phone, created_by, updated_by
) VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
RETURNING *;

-- name: GetProspectScoped :one
SELECT * FROM prospects
WHERE id = $1 AND msp_id = $2;

-- name: CreateOpportunity :one
INSERT INTO opportunities (
  id, msp_id, client_id, prospect_id, pipeline_id, stage_id,
  display_id, name, description, amount_minor, currency, owner_id,
  expected_close_on, committed, created_by, updated_by
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $15
)
RETURNING *;

-- name: GetOpportunityScoped :one
SELECT * FROM opportunities
WHERE id = $1
  AND msp_id = $2
  AND (client_id = $3 OR ($3::uuid IS NULL AND client_id IS NULL));

-- name: TransitionOpportunityVersioned :one
UPDATE opportunities
SET stage_id = $4, version = version + 1, updated_at = now(), updated_by = $5
WHERE id = $1 AND msp_id = $2 AND version = $3
RETURNING *;

-- name: ListOpportunitiesByPipeline :many
SELECT * FROM opportunities
WHERE msp_id = $1 AND pipeline_id = $2
ORDER BY updated_at DESC, id;

-- name: CreateProposal :one
INSERT INTO proposals (
  id, msp_id, client_id, prospect_id, opportunity_id, display_id,
  created_by, updated_by
) VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
RETURNING *;

-- name: InsertProposalVersion :one
INSERT INTO proposal_versions (
  id, proposal_id, msp_id, version, currency, subtotal_minor,
  tax_minor, total_minor, cost_minor, margin_minor, terms,
  issued_by, expires_at, pdf_snapshot_id
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14
)
RETURNING *;

-- name: GetProposalVersionScoped :one
SELECT pv.* FROM proposal_versions pv
JOIN proposals p ON p.id = pv.proposal_id AND p.msp_id = pv.msp_id
WHERE pv.id = $1
  AND pv.msp_id = $2
  AND (p.client_id = $3 OR ($3::uuid IS NULL AND p.client_id IS NULL));

-- name: InsertApproval :one
INSERT INTO approvals (
  id, msp_id, client_id, proposal_version_id, approval_type,
  state, approver_id, signer_name, signer_email, decision_at,
  recorded_by, evidence, pdf_snapshot_id
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
)
RETURNING *;
