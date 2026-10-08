package psa

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sales"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type ProposalRepository struct {
	db    database
	newID func() string
}

var _ sales.ProposalRepository = (*ProposalRepository)(nil)

func NewProposalRepository(db database, newID func() string) *ProposalRepository {
	return &ProposalRepository{db: db, newID: newID}
}

func (r *ProposalRepository) FindProposal(
	ctx context.Context,
	target scope.Target,
	id string,
) (sales.Proposal, error) {
	var proposal sales.Proposal
	err := r.db.QueryRow(ctx, `
SELECT id::text, msp_id::text, COALESCE(client_id::text, ''),
       COALESCE(prospect_id::text, ''), opportunity_id::text,
       display_id, current_version,
       COALESCE((SELECT pv.id::text FROM proposal_versions pv
                 WHERE pv.proposal_id = proposals.id
                   AND pv.msp_id = proposals.msp_id
                   AND pv.version = proposals.current_version), ''),
       state, version, updated_at
FROM proposals
WHERE id = $1 AND msp_id = $2
  AND (
    client_id = NULLIF($3, '')::uuid
    OR (NULLIF($3, '')::uuid IS NULL AND client_id IS NULL)
  )
`, id, target.MSPID, target.ClientID).Scan(
		&proposal.ID, &proposal.MSPID, &proposal.ClientID, &proposal.ProspectID,
		&proposal.OpportunityID, &proposal.DisplayID,
		&proposal.CurrentVersion, &proposal.CurrentVersionID, &proposal.State,
		&proposal.Version, &proposal.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return sales.Proposal{}, scope.ErrNotFound
	}
	return proposal, err
}

func (r *ProposalRepository) ListProposals(
	ctx context.Context,
	target scope.Target,
	filter sales.ProposalListFilter,
) ([]sales.Proposal, error) {
	found, err := r.db.Query(ctx, `
SELECT id::text, msp_id::text, COALESCE(client_id::text, ''),
       COALESCE(prospect_id::text, ''), opportunity_id::text,
       display_id, current_version,
       COALESCE((SELECT pv.id::text FROM proposal_versions pv
                 WHERE pv.proposal_id = proposals.id
                   AND pv.msp_id = proposals.msp_id
                   AND pv.version = proposals.current_version), ''),
       state, version, updated_at
FROM proposals
WHERE msp_id = $1
  AND client_id IS NOT DISTINCT FROM NULLIF($2, '')::uuid
  AND ($3 = '' OR state = $3)
  AND ($4 = '' OR opportunity_id = $4::uuid)
  AND (
    $5::timestamptz IS NULL
    OR (updated_at, id) < ($5::timestamptz, $6::uuid)
  )
ORDER BY updated_at DESC, id DESC
LIMIT $7
`, target.MSPID, target.ClientID, filter.State, filter.OpportunityID,
		nullableTime(filter.BeforeUpdatedAt), nullableID(filter.BeforeID),
		filter.Limit)
	if err != nil {
		return nil, err
	}
	defer found.Close()
	result := make([]sales.Proposal, 0)
	for found.Next() {
		var proposal sales.Proposal
		if err := found.Scan(
			&proposal.ID, &proposal.MSPID, &proposal.ClientID,
			&proposal.ProspectID, &proposal.OpportunityID, &proposal.DisplayID,
			&proposal.CurrentVersion, &proposal.CurrentVersionID,
			&proposal.State, &proposal.Version,
			&proposal.UpdatedAt,
		); err != nil {
			return nil, err
		}
		result = append(result, proposal)
	}
	if err := found.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *ProposalRepository) FindProposalsByReference(
	ctx context.Context,
	target scope.Target,
	reference string,
	limit int,
) ([]sales.Proposal, error) {
	if limit < 1 || limit > 2 {
		limit = 2
	}
	normalized := normalizedReference(reference)
	found, err := r.db.Query(ctx, `
SELECT id::text, msp_id::text, client_id::text,
       COALESCE(prospect_id::text, ''), opportunity_id::text,
       display_id, current_version,
       COALESCE((SELECT pv.id::text FROM proposal_versions pv
                 WHERE pv.proposal_id = proposals.id
                   AND pv.msp_id = proposals.msp_id
                   AND pv.version = proposals.current_version), ''),
       state, version, updated_at
FROM proposals
WHERE msp_id = $1 AND client_id = $2::uuid
  AND lower(btrim(regexp_replace(
    translate(display_id, $4, repeat(' ', char_length($4))),
    '[[:space:]]+', ' ', 'g'
  ))) = $3
ORDER BY id
LIMIT $5
`, target.MSPID, target.ClientID, normalized, unicodeReferenceWhitespace, limit)
	if err != nil {
		return nil, err
	}
	defer found.Close()
	result := make([]sales.Proposal, 0, limit)
	for found.Next() {
		var proposal sales.Proposal
		if err := found.Scan(
			&proposal.ID, &proposal.MSPID, &proposal.ClientID,
			&proposal.ProspectID, &proposal.OpportunityID, &proposal.DisplayID,
			&proposal.CurrentVersion, &proposal.CurrentVersionID,
			&proposal.State, &proposal.Version, &proposal.UpdatedAt,
		); err != nil {
			return nil, err
		}
		result = append(result, proposal)
	}
	if err := found.Err(); err != nil {
		return nil, err
	}
	return exactReferenceMatches(
		result,
		reference,
		func(proposal sales.Proposal) string { return proposal.DisplayID },
		func(sales.Proposal) string { return "" },
	), nil
}

func (r *ProposalRepository) FindOpportunityForProposal(
	ctx context.Context,
	target scope.Target,
	id string,
) (sales.Opportunity, error) {
	var opportunity sales.Opportunity
	err := r.db.QueryRow(ctx, `
	SELECT id::text, msp_id::text, COALESCE(client_id::text, ''),
	       COALESCE(prospect_id::text, ''), pipeline_id::text, stage_id::text,
	       display_id, name, version
FROM opportunities
WHERE id = $1 AND msp_id = $2
  AND client_id IS NOT DISTINCT FROM NULLIF($3, '')::uuid
  AND lifecycle_state = 'active'
	`, id, target.MSPID, target.ClientID).Scan(
		&opportunity.ID, &opportunity.MSPID,
		&opportunity.ClientID, &opportunity.ProspectID, &opportunity.PipelineID,
		&opportunity.StageID, &opportunity.DisplayID, &opportunity.Name, &opportunity.Version,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return sales.Opportunity{}, scope.ErrNotFound
	}
	return opportunity, err
}

func (r *ProposalRepository) ProposalDisplayIDAvailable(
	ctx context.Context,
	target scope.Target,
	displayID string,
) (bool, error) {
	var available bool
	err := r.db.QueryRow(ctx, `
SELECT NOT EXISTS (
  SELECT 1
  FROM proposals
  WHERE msp_id = $1 AND display_id = $2
)
`, target.MSPID, displayID).Scan(&available)
	return available, err
}

func (r *ProposalRepository) CreateProposalAtomic(
	ctx context.Context,
	accepted sales.CreateProposalMutation,
) error {
	return r.withTransaction(ctx, func(tx transaction) error {
		proposal := accepted.Proposal
		if proposal.ClientID != "" {
			if err := lockActiveClientAtVersion(
				ctx, tx, proposal.MSPID, proposal.ClientID,
				accepted.ExpectedClientVersion,
			); err != nil {
				return err
			}
		}
		if err := lockActiveProposalOpportunityAtVersion(
			ctx, tx, proposal, accepted.ExpectedOpportunityVersion,
		); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
INSERT INTO proposals (
  id, msp_id, client_id, prospect_id, opportunity_id, display_id,
  current_version, state, version, created_at, created_by, updated_at, updated_by
) VALUES (
  $1, $2, NULLIF($3, '')::uuid, NULLIF($4, '')::uuid, $5, $6,
  0, 'draft', 1, $7, $8, $7, $8
)
`, proposal.ID, proposal.MSPID, proposal.ClientID, proposal.ProspectID,
			proposal.OpportunityID, proposal.DisplayID,
			accepted.Audit.OccurredAt, accepted.Audit.ActorID)
		if err != nil {
			return err
		}
		return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
	})
}

func lockActiveProposalOpportunityAtVersion(
	ctx context.Context,
	tx transaction,
	proposal sales.Proposal,
	expectedVersion int64,
) error {
	var currentVersion int64
	err := tx.QueryRow(ctx, `
SELECT version
FROM opportunities
WHERE id = $1 AND msp_id = $2
  AND client_id IS NOT DISTINCT FROM NULLIF($3, '')::uuid
  AND prospect_id IS NOT DISTINCT FROM NULLIF($4, '')::uuid
  AND lifecycle_state = 'active'
FOR SHARE
`, proposal.OpportunityID, proposal.MSPID, proposal.ClientID,
		proposal.ProspectID).Scan(&currentVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return scope.ErrNotFound
	}
	if err != nil {
		return err
	}
	return object.RequireVersion(currentVersion, expectedVersion)
}

func (r *ProposalRepository) FindProposalVersion(
	ctx context.Context,
	target scope.Target,
	id string,
) (sales.ProposalVersion, error) {
	var version sales.ProposalVersion
	err := r.db.QueryRow(ctx, `
SELECT pv.id::text, pv.proposal_id::text, pv.msp_id::text, p.client_id::text,
       pv.version, p.state, pv.currency, pv.subtotal_minor, pv.tax_minor,
       pv.total_minor, pv.cost_minor, pv.margin_minor,
       pv.requires_internal_approval, pv.issued_at, pv.issued_by::text,
       pv.expires_at, pv.pdf_snapshot_id::text
FROM proposal_versions pv
JOIN proposals p ON p.id = pv.proposal_id AND p.msp_id = pv.msp_id
WHERE pv.id = $1 AND pv.msp_id = $2
  AND (
    p.client_id = NULLIF($3, '')::uuid
    OR (NULLIF($3, '')::uuid IS NULL AND p.client_id IS NULL)
  )
`, id, target.MSPID, target.ClientID).Scan(
		&version.ID, &version.ProposalID, &version.MSPID, &version.ClientID,
		&version.Version, &version.State, &version.Currency,
		&version.Subtotal.Minor, &version.TaxTotal.Minor, &version.Total.Minor,
		&version.Cost.Minor, &version.Margin.Minor,
		&version.RequiresInternalApproval, &version.IssuedAt, &version.IssuedBy,
		&version.ExpiresAt, &version.PDFSnapshotID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return sales.ProposalVersion{}, scope.ErrNotFound
	}
	if err != nil {
		return sales.ProposalVersion{}, err
	}
	version.Subtotal.Currency = version.Currency
	version.TaxTotal.Currency = version.Currency
	version.Total.Currency = version.Currency
	version.Cost.Currency = version.Currency
	version.Margin.Currency = version.Currency
	lines, err := r.findProposalLines(ctx, version)
	if err != nil {
		return sales.ProposalVersion{}, err
	}
	version.Lines = lines
	return version, nil
}

func (r *ProposalRepository) IssueVersionAtomic(
	ctx context.Context,
	accepted sales.IssueMutation,
) error {
	return r.withTransaction(ctx, func(tx transaction) error {
		proposal, version := accepted.Proposal, accepted.Version
		tag, err := tx.Exec(ctx, `
UPDATE proposals
SET current_version = $6, state = $7, version = version + 1,
    updated_at = $8, updated_by = $9
WHERE id = $1 AND msp_id = $2
  AND (
    client_id = NULLIF($3, '')::uuid
    OR (NULLIF($3, '')::uuid IS NULL AND client_id IS NULL)
  )
  AND version = $4 AND current_version = $5
`, proposal.ID, proposal.MSPID, proposal.ClientID, proposal.Version-1,
			version.Version-1, proposal.CurrentVersion, proposal.State,
			version.IssuedAt, version.IssuedBy)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return object.ErrVersionConflict
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO proposal_versions (
  id, proposal_id, msp_id, version, currency, subtotal_minor, tax_minor,
  total_minor, cost_minor, margin_minor, issued_at, issued_by, expires_at,
  pdf_snapshot_id, requires_internal_approval
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
`, version.ID, version.ProposalID, version.MSPID, version.Version,
			version.Currency, version.Subtotal.Minor, version.TaxTotal.Minor,
			version.Total.Minor, version.Cost.Minor, version.Margin.Minor,
			version.IssuedAt, version.IssuedBy, version.ExpiresAt,
			version.PDFSnapshotID, version.RequiresInternalApproval); err != nil {
			return err
		}
		for position, line := range version.Lines {
			if _, err := tx.Exec(ctx, `
INSERT INTO proposal_lines (
  id, proposal_version_id, msp_id, position, line_type, description,
  quantity, unit_price_minor, unit_cost_minor, discount_minor, tax_minor,
  tax_treatment, recurrence, planned_minutes
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
`, line.ID, version.ID, version.MSPID, position+1, line.Type,
				line.Description, line.Quantity, line.UnitPrice.Minor,
				line.UnitCost.Minor, line.Discount.Minor, line.Tax.Minor,
				line.TaxTreatment, nullableText(line.Recurrence),
				line.PlannedMinutes); err != nil {
				return err
			}
		}
		if version.RequiresInternalApproval {
			if _, err := tx.Exec(ctx, `
INSERT INTO approvals (
  id, msp_id, client_id, proposal_version_id, approval_type, state
) VALUES ($1, $2, $3, $4, 'internal', 'pending')
`, r.newID(), version.MSPID, nullableID(version.ClientID), version.ID); err != nil {
				return err
			}
		}
		return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
	})
}

func (r *ProposalRepository) RecordAcceptanceAtomic(
	ctx context.Context,
	accepted sales.AcceptanceMutation,
) error {
	return r.withTransaction(ctx, func(tx transaction) error {
		proposal, acceptance := accepted.Proposal, accepted.Acceptance
		tag, err := tx.Exec(ctx, `
UPDATE proposals
SET state = $6, version = version + 1, updated_at = $7,
    updated_by = COALESCE($8::uuid, updated_by)
WHERE id = $1 AND msp_id = $2
  AND (
    client_id = NULLIF($3, '')::uuid
    OR (NULLIF($3, '')::uuid IS NULL AND client_id IS NULL)
  )
  AND version = $4 AND state = $5
`, proposal.ID, proposal.MSPID, proposal.ClientID, proposal.Version-1,
			sales.ProposalIssued, proposal.State, accepted.Audit.OccurredAt,
			nullableID(acceptance.RecordedBy))
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return object.ErrVersionConflict
		}
		evidence, err := json.Marshal(acceptance.Evidence)
		if err != nil {
			return err
		}
		approvalType := "customer_electronic"
		if acceptance.Method == sales.AcceptanceOffline {
			approvalType = "customer_offline"
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO approvals (
  id, msp_id, client_id, proposal_version_id, approval_type, state,
  signer_name, signer_email, decision_at, recorded_by, evidence,
  pdf_snapshot_id
) VALUES ($1, $2, $3, $4, $5, 'approved', $6, $7, $8, $9, $10, $11)
`, acceptance.ID, acceptance.MSPID, nullableID(acceptance.ClientID),
			acceptance.ProposalVersionID, approvalType, acceptance.SignerName,
			nullableText(acceptance.SignerEmail), acceptance.AcceptedAt,
			nullableID(acceptance.RecordedBy), evidence,
			acceptance.PDFSnapshotID); err != nil {
			return err
		}
		return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
	})
}

func (r *ProposalRepository) InternalApprovalSatisfied(
	ctx context.Context,
	target scope.Target,
	versionID string,
) (bool, error) {
	var approved bool
	err := r.db.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1
  FROM approvals a
  JOIN proposal_versions pv ON pv.id = a.proposal_version_id AND pv.msp_id = a.msp_id
  JOIN proposals p ON p.id = pv.proposal_id AND p.msp_id = pv.msp_id
  WHERE a.proposal_version_id = $1 AND a.msp_id = $2 AND p.client_id = $3
    AND a.approval_type = 'internal' AND a.state = 'approved'
)
`, versionID, target.MSPID, target.ClientID).Scan(&approved)
	return approved, err
}

func (r *ProposalRepository) FindInternalApproval(
	ctx context.Context,
	target scope.Target,
	versionID string,
) (sales.InternalApproval, error) {
	var approval sales.InternalApproval
	var evidence []byte
	err := r.db.QueryRow(ctx, `
SELECT a.id::text, a.msp_id::text, COALESCE(a.client_id::text, ''),
       a.proposal_version_id::text, a.state, COALESCE(a.approver_id::text, ''),
       a.evidence, COALESCE(a.decision_at, 'epoch'::timestamptz), a.version
FROM approvals a
JOIN proposal_versions pv ON pv.id = a.proposal_version_id AND pv.msp_id = a.msp_id
JOIN proposals p ON p.id = pv.proposal_id AND p.msp_id = pv.msp_id
WHERE a.proposal_version_id = $1 AND a.msp_id = $2
  AND p.client_id IS NOT DISTINCT FROM NULLIF($3, '')::uuid
  AND a.approval_type = 'internal'
`, versionID, target.MSPID, target.ClientID).Scan(
		&approval.ID, &approval.MSPID, &approval.ClientID,
		&approval.ProposalVersionID, &approval.State, &approval.ApproverID,
		&evidence, &approval.DecisionAt, &approval.Version,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return sales.InternalApproval{}, scope.ErrNotFound
	}
	if err != nil {
		return sales.InternalApproval{}, err
	}
	var decoded map[string]string
	if len(evidence) > 0 {
		if err := json.Unmarshal(evidence, &decoded); err != nil {
			return sales.InternalApproval{}, err
		}
		approval.Reason = decoded["reason"]
	}
	return approval, nil
}

func (r *ProposalRepository) DecideInternalApprovalAtomic(
	ctx context.Context,
	accepted sales.InternalApprovalMutation,
) error {
	return r.withTransaction(ctx, func(tx transaction) error {
		approval := accepted.Approval
		evidence, err := json.Marshal(map[string]string{"reason": approval.Reason})
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `
UPDATE approvals
SET state = $5, approver_id = $6, decision_at = $7, recorded_by = $6,
    evidence = $8, version = $4
WHERE id = $1 AND msp_id = $2
  AND client_id IS NOT DISTINCT FROM NULLIF($3, '')::uuid
  AND approval_type = 'internal' AND state = 'pending' AND version = $4 - 1
`, approval.ID, approval.MSPID, approval.ClientID, approval.Version,
			approval.State, approval.ApproverID, approval.DecisionAt, evidence)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return object.ErrVersionConflict
		}
		return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
	})
}

func (r *ProposalRepository) IssueAcceptanceGrantAtomic(
	ctx context.Context,
	accepted sales.AcceptanceGrantMutation,
) error {
	return r.withTransaction(ctx, func(tx transaction) error {
		grant := accepted.Grant
		evidence, err := json.Marshal(grant.Evidence)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `
INSERT INTO proposal_acceptance_grants (
  id, proposal_version_id, msp_id, client_id, token_sha256,
  signer_name, signer_email, evidence, expires_at, issued_at, issued_by
)
SELECT $1, $2, $3, NULLIF($4, '')::uuid, $5, $6, $7, $8, $9, $10, $11
WHERE EXISTS (
  SELECT 1
  FROM proposal_versions pv
  JOIN proposals p ON p.id = pv.proposal_id AND p.msp_id = pv.msp_id
  WHERE pv.id = $2 AND pv.msp_id = $3
    AND p.client_id IS NOT DISTINCT FROM NULLIF($4, '')::uuid
    AND p.state = 'issued' AND pv.pdf_snapshot_id IS NOT NULL
)
`, grant.ID, grant.ProposalVersionID, grant.MSPID, grant.ClientID,
			grant.TokenSHA256, grant.SignerName, grant.SignerEmail, evidence,
			grant.ExpiresAt, grant.IssuedAt, grant.IssuedBy)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return scope.ErrNotFound
		}
		return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
	})
}

func (r *ProposalRepository) FindAcceptedSnapshot(
	ctx context.Context,
	target scope.Target,
	proposalID string,
) (sales.PDFSnapshot, error) {
	var snapshot sales.PDFSnapshot
	err := r.db.QueryRow(ctx, `
SELECT s.id::text, s.proposal_version_id::text, s.sha256, s.stored_at
FROM proposals p
JOIN proposal_versions pv
  ON pv.proposal_id = p.id AND pv.msp_id = p.msp_id
  AND pv.version = p.current_version
JOIN proposal_pdf_snapshots s ON s.id = pv.pdf_snapshot_id
WHERE p.id = $1 AND p.msp_id = $2 AND p.client_id = $3
  AND p.state = 'accepted'
`, proposalID, target.MSPID, target.ClientID).Scan(
		&snapshot.ID, &snapshot.ProposalVersionID, &snapshot.SHA256,
		&snapshot.StoredAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return sales.PDFSnapshot{}, scope.ErrNotFound
	}
	return snapshot, err
}

func (r *ProposalRepository) findProposalLines(
	ctx context.Context,
	version sales.ProposalVersion,
) ([]sales.ProposalLine, error) {
	result, err := r.db.Query(ctx, `
SELECT id::text, line_type, description, quantity, unit_price_minor,
       unit_cost_minor, discount_minor, tax_minor, tax_treatment,
       COALESCE(recurrence, ''), planned_minutes
FROM proposal_lines
WHERE proposal_version_id = $1 AND msp_id = $2
ORDER BY position
`, version.ID, version.MSPID)
	if err != nil {
		return nil, err
	}
	defer result.Close()
	var lines []sales.ProposalLine
	for result.Next() {
		var line sales.ProposalLine
		if err := result.Scan(
			&line.ID, &line.Type, &line.Description, &line.Quantity,
			&line.UnitPrice.Minor, &line.UnitCost.Minor, &line.Discount.Minor,
			&line.Tax.Minor, &line.TaxTreatment, &line.Recurrence,
			&line.PlannedMinutes,
		); err != nil {
			return nil, err
		}
		line.UnitPrice.Currency = version.Currency
		line.UnitCost.Currency = version.Currency
		line.Discount.Currency = version.Currency
		line.Tax.Currency = version.Currency
		lines = append(lines, line)
	}
	return lines, result.Err()
}

func (r *ProposalRepository) withTransaction(
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
