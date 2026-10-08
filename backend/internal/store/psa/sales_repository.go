package psa

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/objectidentity"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sales"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type row interface {
	Scan(...any) error
}

type rows interface {
	Next() bool
	Scan(...any) error
	Err() error
	Close()
}

type transaction interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (rows, error)
	QueryRow(context.Context, string, ...any) row
	Commit(context.Context) error
	Rollback(context.Context) error
}

type database interface {
	Begin(context.Context) (transaction, error)
	Query(context.Context, string, ...any) (rows, error)
	QueryRow(context.Context, string, ...any) row
}

// executionSession pins session-scoped advisory locks to one physical database
// connection until Release. It deliberately has no transaction lifecycle.
type executionSession interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) row
	Release()
}

type executionSessionDatabase interface {
	Acquire(context.Context) (executionSession, error)
}

type SalesRepository struct {
	db database
}

var _ sales.Repository = (*SalesRepository)(nil)

func NewSalesRepository(db database) *SalesRepository {
	return &SalesRepository{db: db}
}

func (r *SalesRepository) FindOpportunity(
	ctx context.Context,
	target scope.Target,
	id sales.OpportunityID,
) (sales.Opportunity, error) {
	var (
		opportunity                      sales.Opportunity
		fields, customFields, contactIDs []byte
		currency                         string
	)
	err := r.db.QueryRow(ctx, opportunitySelect+`
WHERE o.id = $1 AND o.msp_id = $2
  AND (
    o.client_id = NULLIF($3, '')::uuid
    OR (NULLIF($3, '')::uuid IS NULL AND o.client_id IS NULL)
  )
`, id, target.MSPID, target.ClientID).Scan(
		(*string)(&opportunity.ID), &opportunity.MSPID, &opportunity.ClientID,
		&opportunity.ProspectID, &opportunity.PipelineID,
		(*string)(&opportunity.StageID), &opportunity.DisplayID,
		&opportunity.Name, &opportunity.Amount.Minor, &currency,
		&opportunity.Version, &opportunity.UpdatedAt, &opportunity.UpdatedBy,
		&fields, &customFields, &opportunity.TeamID, &contactIDs,
		&opportunity.ProposalIssued, &opportunity.ApprovalGranted,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return sales.Opportunity{}, scope.ErrNotFound
	}
	if err != nil {
		return sales.Opportunity{}, err
	}
	opportunity.Amount.Currency = currency
	if err := json.Unmarshal(fields, &opportunity.Fields); err != nil {
		return sales.Opportunity{}, err
	}
	if err := json.Unmarshal(customFields, &opportunity.CustomFields); err != nil {
		return sales.Opportunity{}, err
	}
	if err := json.Unmarshal(contactIDs, &opportunity.ContactIDs); err != nil {
		return sales.Opportunity{}, err
	}
	return opportunity, nil
}

const opportunitySelect = `
SELECT
  o.id::text, o.msp_id::text, COALESCE(o.client_id::text, ''),
  COALESCE(o.prospect_id::text, ''), o.pipeline_id::text, o.stage_id::text,
  o.display_id, o.name, o.amount_minor, o.currency, o.version,
  o.updated_at, o.updated_by::text,
  o.custom_fields || jsonb_build_object(
    'display_id', o.display_id,
    'name', o.name,
    'description', o.description,
    'expected_close_on', COALESCE(to_char(o.expected_close_on, 'YYYY-MM-DD'), ''),
    'owner_id', COALESCE(o.owner_id::text, '')
  ),
  o.custom_fields,
  COALESCE(o.team_id::text, ''),
  COALESCE((
    SELECT jsonb_agg(oc.contact_id::text ORDER BY oc.position)
    FROM opportunity_contacts oc
    WHERE oc.opportunity_id = o.id AND oc.msp_id = o.msp_id
      AND oc.client_id = o.client_id
  ), '[]'::jsonb),
  EXISTS (
    SELECT 1 FROM proposals p
    WHERE p.opportunity_id = o.id AND p.msp_id = o.msp_id
      AND p.state IN ('issued', 'accepted')
  ),
  EXISTS (
    SELECT 1
    FROM proposals p
    JOIN proposal_versions pv
      ON pv.proposal_id = p.id AND pv.msp_id = p.msp_id
      AND pv.version = p.current_version
    JOIN approvals a
      ON a.proposal_version_id = pv.id AND a.msp_id = pv.msp_id
    WHERE p.opportunity_id = o.id AND p.msp_id = o.msp_id
      AND a.approval_type = 'internal' AND a.state = 'approved'
  )
FROM opportunities o
`

func (r *SalesRepository) ListOpportunities(
	ctx context.Context,
	target scope.Target,
	filter sales.OpportunityListFilter,
) ([]sales.Opportunity, error) {
	found, err := r.db.Query(ctx, opportunitySelect+`
WHERE o.msp_id = $1
  AND o.client_id IS NOT DISTINCT FROM NULLIF($2, '')::uuid
  AND ($3 = '' OR o.pipeline_id = $3::uuid)
  AND ($4 = '' OR o.stage_id = $4::uuid)
  AND (
    $5::timestamptz IS NULL
    OR (o.updated_at, o.id) < ($5::timestamptz, $6::uuid)
  )
  AND o.lifecycle_state = 'active'
ORDER BY o.updated_at DESC, o.id DESC
LIMIT $7
`, target.MSPID, target.ClientID, filter.PipelineID, filter.StageID,
		nullableTime(filter.BeforeUpdatedAt), nullableID(string(filter.BeforeID)),
		filter.Limit)
	if err != nil {
		return nil, err
	}
	defer found.Close()
	result := make([]sales.Opportunity, 0)
	for found.Next() {
		var opportunity sales.Opportunity
		var fields, customFields, contactIDs []byte
		var currency string
		if err := found.Scan(
			(*string)(&opportunity.ID), &opportunity.MSPID, &opportunity.ClientID,
			&opportunity.ProspectID, &opportunity.PipelineID,
			(*string)(&opportunity.StageID), &opportunity.DisplayID,
			&opportunity.Name, &opportunity.Amount.Minor, &currency,
			&opportunity.Version, &opportunity.UpdatedAt, &opportunity.UpdatedBy,
			&fields, &customFields, &opportunity.TeamID, &contactIDs,
			&opportunity.ProposalIssued, &opportunity.ApprovalGranted,
		); err != nil {
			return nil, err
		}
		opportunity.Amount.Currency = currency
		if err := json.Unmarshal(fields, &opportunity.Fields); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(customFields, &opportunity.CustomFields); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(contactIDs, &opportunity.ContactIDs); err != nil {
			return nil, err
		}
		result = append(result, opportunity)
	}
	if err := found.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *SalesRepository) FindOpportunitiesByReference(
	ctx context.Context,
	target scope.Target,
	reference string,
	limit int,
) ([]sales.Opportunity, error) {
	if limit < 1 || limit > 2 {
		limit = 2
	}
	normalized := normalizedReference(reference)
	found, err := r.db.Query(ctx, opportunitySelect+`
WHERE o.msp_id = $1 AND o.client_id = $2::uuid
  AND o.lifecycle_state = 'active'
  AND (
    lower(btrim(regexp_replace(
      translate(o.display_id, $4, repeat(' ', char_length($4))),
      '[[:space:]]+', ' ', 'g'
    ))) = $3
    OR lower(btrim(regexp_replace(
      translate(o.name, $4, repeat(' ', char_length($4))),
      '[[:space:]]+', ' ', 'g'
    ))) = $3
  )
ORDER BY CASE WHEN lower(btrim(regexp_replace(
  translate(o.display_id, $4, repeat(' ', char_length($4))),
  '[[:space:]]+', ' ', 'g'
))) = $3 THEN 0 ELSE 1 END, o.id
LIMIT $5
`, target.MSPID, target.ClientID, normalized, unicodeReferenceWhitespace, limit)
	if err != nil {
		return nil, err
	}
	defer found.Close()
	result := make([]sales.Opportunity, 0, limit)
	for found.Next() {
		opportunity, err := scanOpportunity(found)
		if err != nil {
			return nil, err
		}
		result = append(result, opportunity)
	}
	if err := found.Err(); err != nil {
		return nil, err
	}
	return exactReferenceMatches(
		result,
		reference,
		func(opportunity sales.Opportunity) string { return opportunity.DisplayID },
		func(opportunity sales.Opportunity) string { return opportunity.Name },
	), nil
}

func scanOpportunity(scanner interface{ Scan(...any) error }) (sales.Opportunity, error) {
	var (
		opportunity                      sales.Opportunity
		fields, customFields, contactIDs []byte
		currency                         string
	)
	if err := scanner.Scan(
		(*string)(&opportunity.ID), &opportunity.MSPID, &opportunity.ClientID,
		&opportunity.ProspectID, &opportunity.PipelineID,
		(*string)(&opportunity.StageID), &opportunity.DisplayID,
		&opportunity.Name, &opportunity.Amount.Minor, &currency,
		&opportunity.Version, &opportunity.UpdatedAt, &opportunity.UpdatedBy,
		&fields, &customFields, &opportunity.TeamID, &contactIDs,
		&opportunity.ProposalIssued, &opportunity.ApprovalGranted,
	); err != nil {
		return sales.Opportunity{}, err
	}
	opportunity.Amount.Currency = currency
	if err := json.Unmarshal(fields, &opportunity.Fields); err != nil {
		return sales.Opportunity{}, err
	}
	if err := json.Unmarshal(customFields, &opportunity.CustomFields); err != nil {
		return sales.Opportunity{}, err
	}
	if err := json.Unmarshal(contactIDs, &opportunity.ContactIDs); err != nil {
		return sales.Opportunity{}, err
	}
	return opportunity, nil
}

func (r *SalesRepository) FindStagesByReference(
	ctx context.Context,
	target scope.Target,
	opportunityID sales.OpportunityID,
	reference string,
	limit int,
) ([]sales.PipelineStage, error) {
	if limit < 1 || limit > 2 {
		limit = 2
	}
	rawReference := strings.TrimSpace(reference)
	normalized := normalizedReference(reference)
	found, err := r.db.Query(ctx, `
SELECT stage.id::text, stage.pipeline_id::text, stage.key, stage.name,
       stage.position, stage.probability, stage.forecast_category,
       stage.required_fields, stage.allowed_next_stage_ids,
       stage.requires_proposal, stage.requires_approval, stage.version
FROM pipeline_stages stage
JOIN opportunities o
  ON stage.pipeline_id = o.pipeline_id
WHERE o.msp_id = $1 AND o.client_id = $2::uuid AND o.id = $3
  AND o.lifecycle_state = 'active'
  AND (
    stage.id::text = $4
    OR lower(btrim(regexp_replace(
      translate(stage.key, $6, repeat(' ', char_length($6))),
      '[[:space:]]+', ' ', 'g'
    ))) = $5
    OR lower(btrim(regexp_replace(
      translate(stage.name, $6, repeat(' ', char_length($6))),
      '[[:space:]]+', ' ', 'g'
    ))) = $5
  )
ORDER BY CASE
  WHEN stage.id::text = $4 THEN 0
  WHEN lower(btrim(regexp_replace(
    translate(stage.key, $6, repeat(' ', char_length($6))),
    '[[:space:]]+', ' ', 'g'
  ))) = $5 THEN 1
  ELSE 2
END, stage.position, stage.id
LIMIT $7
`, target.MSPID, target.ClientID, opportunityID, rawReference, normalized,
		unicodeReferenceWhitespace, limit)
	if err != nil {
		return nil, err
	}
	defer found.Close()
	result := make([]sales.PipelineStage, 0, limit)
	for found.Next() {
		stage, err := scanPipelineStage(found)
		if err != nil {
			return nil, err
		}
		result = append(result, stage)
	}
	if err := found.Err(); err != nil {
		return nil, err
	}
	return exactStageReferenceMatches(result, rawReference), nil
}

func scanPipelineStage(scanner interface{ Scan(...any) error }) (sales.PipelineStage, error) {
	var (
		stage                       sales.PipelineStage
		category                    string
		position, probability       int32
		requiredFields, allowedNext []byte
	)
	if err := scanner.Scan(
		(*string)(&stage.ID), &stage.PipelineID, &stage.Key, &stage.Name,
		&position, &probability, &category, &requiredFields, &allowedNext,
		&stage.RequiresProposal, &stage.RequiresApproval,
		&stage.Version,
	); err != nil {
		return sales.PipelineStage{}, err
	}
	if position < 1 || probability < 0 || probability > 100 {
		return sales.PipelineStage{}, sales.ErrInvalidSalesRecord
	}
	stage.Position = int(position)
	stage.Probability = uint8(probability)
	stage.Category = sales.ForecastCategory(category)
	if err := json.Unmarshal(requiredFields, &stage.RequiredFields); err != nil {
		return sales.PipelineStage{}, err
	}
	if err := json.Unmarshal(allowedNext, &stage.AllowedNext); err != nil {
		return sales.PipelineStage{}, err
	}
	return stage, nil
}

func exactStageReferenceMatches(
	candidates []sales.PipelineStage,
	reference string,
) []sales.PipelineStage {
	normalized := normalizedReference(reference)
	byID := make([]sales.PipelineStage, 0, len(candidates))
	byKey := make([]sales.PipelineStage, 0, len(candidates))
	byName := make([]sales.PipelineStage, 0, len(candidates))
	for _, candidate := range candidates {
		if strings.TrimSpace(string(candidate.ID)) == strings.TrimSpace(reference) {
			byID = append(byID, candidate)
			continue
		}
		if normalizedReference(candidate.Key) == normalized {
			byKey = append(byKey, candidate)
			continue
		}
		if normalizedReference(candidate.Name) == normalized {
			byName = append(byName, candidate)
		}
	}
	if len(byID) != 0 {
		return byID
	}
	if len(byKey) != 0 {
		return byKey
	}
	return byName
}

func (r *SalesRepository) FindStage(
	ctx context.Context,
	pipelineID string,
	stageID sales.PipelineStageID,
) (sales.PipelineStage, error) {
	var (
		stage                       sales.PipelineStage
		category                    string
		position, probability       int32
		requiredFields, allowedNext []byte
	)
	err := r.db.QueryRow(ctx, `
SELECT id::text, pipeline_id::text, key, name, position, probability,
       forecast_category, required_fields, allowed_next_stage_ids,
       requires_proposal, requires_approval, version
FROM pipeline_stages
WHERE id = $1 AND pipeline_id = $2
`, stageID, pipelineID).Scan(
		(*string)(&stage.ID), &stage.PipelineID, &stage.Key, &stage.Name,
		&position, &probability, &category, &requiredFields, &allowedNext,
		&stage.RequiresProposal, &stage.RequiresApproval,
		&stage.Version,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return sales.PipelineStage{}, sales.ErrStageNotFound
	}
	if err != nil {
		return sales.PipelineStage{}, err
	}
	if position < 1 || probability < 0 || probability > 100 {
		return sales.PipelineStage{}, sales.ErrInvalidSalesRecord
	}
	stage.Position = int(position)
	stage.Probability = uint8(probability)
	stage.Category = sales.ForecastCategory(category)
	if err := json.Unmarshal(requiredFields, &stage.RequiredFields); err != nil {
		return sales.PipelineStage{}, err
	}
	if err := json.Unmarshal(allowedNext, &stage.AllowedNext); err != nil {
		return sales.PipelineStage{}, err
	}
	return stage, nil
}

func (r *SalesRepository) CreateProspectAtomic(
	ctx context.Context,
	accepted sales.CreateProspectMutation,
) error {
	return r.withTransaction(ctx, func(tx transaction) error {
		prospect := accepted.Prospect
		err := objectidentity.Enforce(
			ctx,
			"prospect_identity:"+prospect.MSPID,
			prospect.Name,
			prospect.DisplayID,
			`SELECT name, display_id
FROM prospects
WHERE msp_id = $1`,
			[]any{prospect.MSPID},
			func(ctx context.Context, query string, args ...any) error {
				_, err := tx.Exec(ctx, query, args...)
				return err
			},
			func(ctx context.Context, query string, args ...any) (objectidentity.Rows, error) {
				return tx.Query(ctx, query, args...)
			},
		)
		if errors.Is(err, objectidentity.ErrConflict) {
			return sales.ErrProspectIdentityConflict
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO prospects (
  id, msp_id, display_id, name, email, phone, lifecycle_state,
  version, created_at, created_by, updated_at, updated_by
) VALUES ($1, $2, $3, $4, $5, $6, 'active', $7, $8, $9, $8, $9)
`, prospect.ID, prospect.MSPID, prospect.DisplayID, prospect.Name,
			nullableText(prospect.Email), nullableText(prospect.Phone),
			prospect.Version, prospect.CreatedAt, prospect.CreatedBy); err != nil {
			return err
		}
		return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
	})
}

func (r *SalesRepository) ListProspects(
	ctx context.Context,
	mspID string,
	limit int,
) ([]sales.Prospect, error) {
	rows, err := r.db.Query(ctx, `
SELECT id::text, msp_id::text, display_id, name,
       COALESCE(email, ''), COALESCE(phone, ''),
       version, created_at, created_by::text
FROM prospects
WHERE msp_id = $1 AND lifecycle_state = 'active'
ORDER BY created_at DESC, id DESC
LIMIT $2
`, mspID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]sales.Prospect, 0)
	for rows.Next() {
		var prospect sales.Prospect
		if err := rows.Scan(
			&prospect.ID, &prospect.MSPID, &prospect.DisplayID, &prospect.Name,
			&prospect.Email, &prospect.Phone, &prospect.Version,
			&prospect.CreatedAt, &prospect.CreatedBy,
		); err != nil {
			return nil, err
		}
		result = append(result, prospect)
	}
	return result, rows.Err()
}

func (r *SalesRepository) FindProspectsByReference(
	ctx context.Context,
	mspID string,
	reference string,
	limit int,
) ([]sales.Prospect, error) {
	if limit < 1 || limit > 2 {
		limit = 2
	}
	normalized := strings.ToLower(strings.Join(strings.Fields(reference), " "))
	rows, err := r.db.Query(ctx, `
SELECT id::text, msp_id::text, display_id, name,
       COALESCE(email, ''), COALESCE(phone, ''),
       version, created_at, created_by::text
FROM prospects
WHERE msp_id = $1 AND lifecycle_state = 'active'
  AND (
    lower(regexp_replace(btrim(display_id), '\s+', ' ', 'g')) = $2
    OR lower(regexp_replace(btrim(name), '\s+', ' ', 'g')) = $2
  )
ORDER BY id
LIMIT $3
`, mspID, normalized, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]sales.Prospect, 0, limit)
	for rows.Next() {
		var prospect sales.Prospect
		if err := rows.Scan(
			&prospect.ID, &prospect.MSPID, &prospect.DisplayID, &prospect.Name,
			&prospect.Email, &prospect.Phone, &prospect.Version,
			&prospect.CreatedAt, &prospect.CreatedBy,
		); err != nil {
			return nil, err
		}
		result = append(result, prospect)
	}
	return result, rows.Err()
}

func (r *SalesRepository) CreatePipelineAtomic(
	ctx context.Context,
	accepted sales.CreatePipelineMutation,
) error {
	return r.withTransaction(ctx, func(tx transaction) error {
		pipeline := accepted.Pipeline
		if _, err := tx.Exec(ctx, `
INSERT INTO pipelines (
  id, msp_id, key, name, enabled, version,
  created_at, created_by, updated_at, updated_by
) VALUES ($1, $2, $3, $4, true, $5, $6, $7, $6, $7)
`, pipeline.ID, pipeline.MSPID, pipeline.Key, pipeline.Name,
			pipeline.Version, pipeline.CreatedAt, pipeline.CreatedBy); err != nil {
			return err
		}
		for _, stage := range pipeline.Stages {
			requiredFields, err := json.Marshal(stage.RequiredFields)
			if err != nil {
				return err
			}
			allowedNext, err := json.Marshal(stage.AllowedNext)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
INSERT INTO pipeline_stages (
  id, pipeline_id, msp_id, key, name, position, probability,
  forecast_category, required_fields, allowed_next_stage_ids,
  requires_proposal, requires_approval, version
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
`, stage.ID, pipeline.ID, pipeline.MSPID, stage.Key, stage.Name,
				stage.Position, stage.Probability, stage.Category,
				requiredFields, allowedNext, stage.RequiresProposal,
				stage.RequiresApproval, stage.Version); err != nil {
				return err
			}
		}
		return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
	})
}

func (r *SalesRepository) ListPipelines(
	ctx context.Context,
	mspID string,
) ([]sales.Pipeline, error) {
	rows, err := r.db.Query(ctx, `
SELECT pipeline.id::text, pipeline.msp_id::text, pipeline.key, pipeline.name,
       pipeline.version, pipeline.created_at, pipeline.created_by::text,
       stage.id::text, stage.key, stage.name, stage.position,
       stage.probability::integer, stage.forecast_category,
       stage.required_fields, stage.allowed_next_stage_ids,
       stage.requires_proposal, stage.requires_approval, stage.version
FROM pipelines pipeline
JOIN pipeline_stages stage
  ON stage.pipeline_id = pipeline.id AND stage.msp_id = pipeline.msp_id
WHERE pipeline.msp_id = $1 AND pipeline.enabled
ORDER BY pipeline.name, pipeline.id, stage.position, stage.id
`, mspID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]sales.Pipeline, 0)
	index := map[string]int{}
	for rows.Next() {
		var pipeline sales.Pipeline
		var stage sales.PipelineStage
		var probability int
		var requiredFields, allowedNext []byte
		if err := rows.Scan(
			&pipeline.ID, &pipeline.MSPID, &pipeline.Key, &pipeline.Name,
			&pipeline.Version, &pipeline.CreatedAt, &pipeline.CreatedBy,
			&stage.ID, &stage.Key, &stage.Name, &stage.Position,
			&probability, &stage.Category, &requiredFields, &allowedNext,
			&stage.RequiresProposal, &stage.RequiresApproval,
			&stage.Version,
		); err != nil {
			return nil, err
		}
		stage.PipelineID = pipeline.ID
		stage.Probability = uint8(probability)
		if err := json.Unmarshal(requiredFields, &stage.RequiredFields); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(allowedNext, &stage.AllowedNext); err != nil {
			return nil, err
		}
		position, exists := index[pipeline.ID]
		if !exists {
			position = len(result)
			index[pipeline.ID] = position
			pipeline.Stages = []sales.PipelineStage{}
			result = append(result, pipeline)
		}
		result[position].Stages = append(result[position].Stages, stage)
	}
	return result, rows.Err()
}

func (r *SalesRepository) CreateOpportunityAtomic(
	ctx context.Context,
	accepted sales.CreateOpportunityMutation,
) error {
	return r.withTransaction(ctx, func(tx transaction) error {
		opportunity := accepted.Opportunity
		description := opportunity.Fields["description"]
		expectedCloseOn := opportunity.Fields["expected_close_on"]
		ownerID := opportunity.Fields["owner_id"]
		tag, err := tx.Exec(ctx, `
INSERT INTO opportunities (
  id, msp_id, client_id, prospect_id, pipeline_id, stage_id,
  display_id, name, description, amount_minor, currency, owner_id,
  expected_close_on, committed, lifecycle_state, version,
  created_at, created_by, updated_at, updated_by
)
SELECT $1, $2, NULLIF($3, '')::uuid, NULLIF($4, '')::uuid, $5, $6,
       $7, $8, $9, $10, $11, NULLIF($12, '')::uuid,
       NULLIF($13, '')::date, false, 'active', $14, $15, $16, $15, $16
WHERE EXISTS (
  SELECT 1
  FROM pipelines p
  JOIN pipeline_stages ps
    ON ps.pipeline_id = p.id AND ps.msp_id = p.msp_id
  WHERE p.id = $5 AND p.msp_id = $2 AND p.enabled = true AND ps.id = $6
) AND (
  ($3 <> '' AND $4 = '' AND EXISTS (
    SELECT 1 FROM client_organizations
    WHERE id = $3::uuid AND msp_id = $2 AND lifecycle_state = 'active'
  ))
  OR ($3 = '' AND $4 <> '' AND EXISTS (
    SELECT 1 FROM prospects
    WHERE id = $4::uuid AND msp_id = $2 AND lifecycle_state = 'active'
  ))
) AND (
  $12 = '' OR EXISTS (
    SELECT 1 FROM technicians
    WHERE id = $12::uuid AND msp_id = $2 AND lifecycle_state = 'active'
  )
)
`, opportunity.ID, opportunity.MSPID, opportunity.ClientID,
			opportunity.ProspectID, opportunity.PipelineID, opportunity.StageID,
			opportunity.DisplayID, opportunity.Name, description,
			opportunity.Amount.Minor, opportunity.Amount.Currency, ownerID,
			expectedCloseOn, opportunity.Version, opportunity.UpdatedAt,
			opportunity.UpdatedBy)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return scope.ErrNotFound
		}
		return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
	})
}

func (r *SalesRepository) CreateOpportunityActivityAtomic(
	ctx context.Context,
	accepted sales.CreateOpportunityActivityMutation,
) error {
	return r.withTransaction(ctx, func(tx transaction) error {
		activity := accepted.Activity
		if err := lockActiveClientAtVersion(
			ctx, tx, activity.MSPID, activity.ClientID,
			accepted.ExpectedClientVersion,
		); err != nil {
			return err
		}
		if err := lockActiveOpportunityAtVersion(
			ctx, tx, activity.MSPID, activity.ClientID,
			activity.OpportunityID, accepted.Pipeline.ID, accepted.Stage.ID,
			accepted.ExpectedOpportunityVersion,
		); err != nil {
			return err
		}
		if err := lockEnabledPipelineAtVersion(
			ctx, tx, activity.MSPID, accepted.Pipeline.ID,
			accepted.Pipeline.Version,
		); err != nil {
			return err
		}
		if err := lockPipelineStageSnapshots(
			ctx, tx, activity.MSPID, accepted.Pipeline.ID,
			[]sales.PipelineStage{accepted.Stage},
		); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO opportunity_activities (
  id, msp_id, client_id, opportunity_id, kind, summary, details,
  occurred_at, created_at, created_by
)
VALUES ($1, $2, NULLIF($3, '')::uuid, $4, $5, $6, $7, $8, $9, $10)
`, activity.ID, activity.MSPID, activity.ClientID, activity.OpportunityID,
			activity.Kind, activity.Summary, activity.Details, activity.OccurredAt,
			activity.CreatedAt, activity.CreatedBy); err != nil {
			return err
		}
		return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
	})
}

func (r *SalesRepository) ListOpportunityActivities(
	ctx context.Context,
	target scope.Target,
	opportunityID sales.OpportunityID,
	limit int,
) ([]sales.OpportunityActivity, error) {
	found, err := r.db.Query(ctx, `
SELECT id::text, msp_id::text, COALESCE(client_id::text, ''),
       opportunity_id::text, kind, summary, details, occurred_at,
       created_at, created_by::text
FROM opportunity_activities
WHERE msp_id = $1 AND client_id IS NOT DISTINCT FROM NULLIF($2, '')::uuid
  AND opportunity_id = $3
ORDER BY occurred_at DESC, id DESC
LIMIT $4
`, target.MSPID, target.ClientID, opportunityID, limit)
	if err != nil {
		return nil, err
	}
	defer found.Close()
	result := make([]sales.OpportunityActivity, 0)
	for found.Next() {
		var activity sales.OpportunityActivity
		if err := found.Scan(
			&activity.ID, &activity.MSPID, &activity.ClientID,
			&activity.OpportunityID, &activity.Kind, &activity.Summary,
			&activity.Details, &activity.OccurredAt, &activity.CreatedAt,
			&activity.CreatedBy,
		); err != nil {
			return nil, err
		}
		result = append(result, activity)
	}
	if err := found.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *SalesRepository) Forecast(
	ctx context.Context,
	target scope.Target,
	pipelineID string,
) ([]sales.ForecastBucket, error) {
	found, err := r.db.Query(ctx, `
SELECT o.pipeline_id::text, o.stage_id::text, s.name, s.forecast_category,
       s.probability, count(*)::bigint, o.currency,
       sum(o.amount_minor)::bigint,
       sum(o.amount_minor * s.probability / 100)::bigint
FROM opportunities o
JOIN pipeline_stages s
  ON s.id = o.stage_id AND s.pipeline_id = o.pipeline_id AND s.msp_id = o.msp_id
WHERE o.msp_id = $1
  AND ($2 = '' OR o.client_id = $2::uuid)
  AND ($3 = '' OR o.pipeline_id = $3::uuid)
  AND o.lifecycle_state = 'active'
GROUP BY o.pipeline_id, o.stage_id, s.name, s.forecast_category,
         s.probability, s.position, o.currency
ORDER BY o.pipeline_id, s.position, o.currency
`, target.MSPID, target.ClientID, pipelineID)
	if err != nil {
		return nil, err
	}
	defer found.Close()
	result := make([]sales.ForecastBucket, 0)
	for found.Next() {
		var bucket sales.ForecastBucket
		var currency string
		if err := found.Scan(
			&bucket.PipelineID, &bucket.StageID, &bucket.StageName,
			&bucket.Category, &bucket.Probability, &bucket.OpportunityCount,
			&currency, &bucket.Amount.Minor, &bucket.WeightedAmount.Minor,
		); err != nil {
			return nil, err
		}
		bucket.Amount.Currency, bucket.WeightedAmount.Currency = currency, currency
		result = append(result, bucket)
	}
	if err := found.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *SalesRepository) TransitionAtomic(
	ctx context.Context,
	accepted sales.TransitionMutation,
) error {
	return r.withTransaction(ctx, func(tx transaction) error {
		opportunity := accepted.Opportunity
		if err := lockActiveClientAtVersion(
			ctx, tx, opportunity.MSPID, opportunity.ClientID,
			accepted.ExpectedClientVersion,
		); err != nil {
			return err
		}
		if err := lockActiveOpportunityForUpdateAtVersion(
			ctx, tx, opportunity.MSPID, opportunity.ClientID,
			opportunity.ID, accepted.Pipeline.ID, accepted.PreviousStage,
			opportunity.Version-1,
		); err != nil {
			return err
		}
		if err := lockEnabledPipelineAtVersion(
			ctx, tx, opportunity.MSPID, accepted.Pipeline.ID,
			accepted.Pipeline.Version,
		); err != nil {
			return err
		}
		if err := lockPipelineStageSnapshots(
			ctx, tx, opportunity.MSPID, accepted.Pipeline.ID,
			[]sales.PipelineStage{accepted.CurrentStage, accepted.DestinationStage},
		); err != nil {
			return err
		}
		if !containsPipelineStage(
			accepted.CurrentStage.AllowedNext, accepted.DestinationStage.ID,
		) {
			return object.ErrVersionConflict
		}
		requiredFields, err := json.Marshal(accepted.DestinationStage.RequiredFields)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `
UPDATE opportunities o
SET stage_id = $6, version = version + 1, updated_at = $7, updated_by = $8
WHERE id = $1 AND msp_id = $2
  AND client_id = $3 AND pipeline_id = $9 AND stage_id = $4 AND version = $5
  AND lifecycle_state = 'active'
  AND NOT EXISTS (
    SELECT 1
    FROM jsonb_array_elements_text($10::jsonb) AS required(field)
    WHERE COALESCE(NULLIF((
      o.custom_fields || jsonb_build_object(
        'display_id', o.display_id,
        'name', o.name,
        'description', o.description,
        'expected_close_on', COALESCE(to_char(o.expected_close_on, 'YYYY-MM-DD'), ''),
        'owner_id', COALESCE(o.owner_id::text, '')
      )
    )->>required.field, ''), '') = ''
  )
  AND (
    NOT $11 OR EXISTS (
      SELECT 1 FROM proposals p
      WHERE p.opportunity_id = o.id AND p.msp_id = o.msp_id
        AND p.state IN ('issued', 'accepted')
    )
  )
  AND (
    NOT $12 OR EXISTS (
      SELECT 1
      FROM proposals p
      JOIN proposal_versions pv
        ON pv.proposal_id = p.id AND pv.msp_id = p.msp_id
        AND pv.version = p.current_version
      JOIN approvals a
        ON a.proposal_version_id = pv.id AND a.msp_id = pv.msp_id
      WHERE p.opportunity_id = o.id AND p.msp_id = o.msp_id
        AND a.approval_type = 'internal' AND a.state = 'approved'
    )
  )
`, opportunity.ID, opportunity.MSPID, opportunity.ClientID,
			accepted.PreviousStage, opportunity.Version-1, opportunity.StageID,
			opportunity.UpdatedAt, opportunity.UpdatedBy, accepted.Pipeline.ID,
			requiredFields, accepted.DestinationStage.RequiresProposal,
			accepted.DestinationStage.RequiresApproval)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return object.ErrVersionConflict
		}
		return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
	})
}

func lockEnabledPipelineAtVersion(
	ctx context.Context,
	tx transaction,
	mspID string,
	pipelineID string,
	expectedVersion int64,
) error {
	tag, err := tx.Exec(ctx, `
SELECT 1
FROM pipelines
WHERE id = $1 AND msp_id = $2 AND enabled
  AND ($3 = 0 OR version = $3)
FOR SHARE
`, pipelineID, mspID, expectedVersion)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return object.ErrVersionConflict
	}
	return nil
}

func lockPipelineStageSnapshots(
	ctx context.Context,
	tx transaction,
	mspID string,
	pipelineID string,
	expected []sales.PipelineStage,
) error {
	if len(expected) < 1 || len(expected) > 2 {
		return object.ErrVersionConflict
	}
	byID := make(map[sales.PipelineStageID]sales.PipelineStage, len(expected))
	for _, stage := range expected {
		if stage.ID == "" || stage.PipelineID != pipelineID {
			return object.ErrVersionConflict
		}
		if _, duplicate := byID[stage.ID]; duplicate {
			return object.ErrVersionConflict
		}
		byID[stage.ID] = stage
	}
	firstID := string(expected[0].ID)
	secondID := ""
	if len(expected) == 2 {
		secondID = string(expected[1].ID)
	}
	found, err := tx.Query(ctx, `
SELECT id::text, pipeline_id::text, key, name, position, probability,
       forecast_category, required_fields, allowed_next_stage_ids,
       requires_proposal, requires_approval, version
FROM pipeline_stages
WHERE msp_id = $1 AND pipeline_id = $2
  AND (id = $3::uuid OR id = NULLIF($4, '')::uuid)
ORDER BY id
FOR SHARE
`, mspID, pipelineID, firstID, secondID)
	if err != nil {
		return err
	}
	defer found.Close()
	seen := 0
	for found.Next() {
		stage, err := scanPipelineStage(found)
		if err != nil {
			return err
		}
		snapshot, ok := byID[stage.ID]
		if !ok || !pipelineStageSnapshotEqual(stage, snapshot) {
			return object.ErrVersionConflict
		}
		seen++
	}
	if err := found.Err(); err != nil {
		return err
	}
	if seen != len(expected) {
		return object.ErrVersionConflict
	}
	return nil
}

func pipelineStageSnapshotEqual(
	current sales.PipelineStage,
	expected sales.PipelineStage,
) bool {
	if expected.Version == 0 {
		current.Version = 0
	}
	return reflect.DeepEqual(current, expected)
}

func lockActiveOpportunityForUpdateAtVersion(
	ctx context.Context,
	tx transaction,
	mspID string,
	clientID string,
	opportunityID sales.OpportunityID,
	pipelineID string,
	stageID sales.PipelineStageID,
	expectedVersion int64,
) error {
	tag, err := tx.Exec(ctx, `
SELECT 1
FROM opportunities
WHERE id = $1 AND msp_id = $2 AND client_id = $3
  AND pipeline_id = $4 AND stage_id = $5
  AND lifecycle_state = 'active' AND version = $6
FOR UPDATE
`, opportunityID, mspID, clientID, pipelineID, stageID, expectedVersion)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return object.ErrVersionConflict
	}
	return nil
}

func lockActiveOpportunityAtVersion(
	ctx context.Context,
	tx transaction,
	mspID string,
	clientID string,
	opportunityID sales.OpportunityID,
	pipelineID string,
	stageID sales.PipelineStageID,
	expectedVersion int64,
) error {
	tag, err := tx.Exec(ctx, `
SELECT 1
FROM opportunities
WHERE id = $1 AND msp_id = $2 AND client_id = $3
  AND pipeline_id = $4 AND stage_id = $5
  AND lifecycle_state = 'active'
  AND ($6 = 0 OR version = $6)
FOR SHARE
`, opportunityID, mspID, clientID, pipelineID, stageID, expectedVersion)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return object.ErrVersionConflict
	}
	return nil
}

func containsPipelineStage(
	allowed []sales.PipelineStageID,
	wanted sales.PipelineStageID,
) bool {
	for _, stage := range allowed {
		if stage == wanted {
			return true
		}
	}
	return false
}

func (r *SalesRepository) ReplaceOpportunityFieldsAtomic(
	ctx context.Context,
	accepted sales.ReplaceOpportunityFieldsMutation,
) error {
	return r.withTransaction(ctx, func(tx transaction) error {
		opportunity := accepted.Opportunity
		fields, err := json.Marshal(opportunity.CustomFields)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `
UPDATE opportunities
SET custom_fields = $5::jsonb, version = version + 1,
    updated_at = $6, updated_by = $7
WHERE id = $1 AND msp_id = $2 AND client_id = $3
  AND version = $4 AND lifecycle_state = 'active'
`, opportunity.ID, opportunity.MSPID, opportunity.ClientID,
			opportunity.Version-1, fields,
			opportunity.UpdatedAt, opportunity.UpdatedBy)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return object.ErrVersionConflict
		}
		return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
	})
}

func (r *SalesRepository) ReplaceOpportunityParticipantsAtomic(
	ctx context.Context,
	accepted sales.ReplaceOpportunityParticipantsMutation,
) error {
	return r.withTransaction(ctx, func(tx transaction) error {
		opportunity := accepted.Opportunity
		if opportunity.TeamID != "" {
			var teamID string
			err := tx.QueryRow(ctx, `
SELECT id::text FROM teams WHERE id = $1 AND msp_id = $2
`, opportunity.TeamID, opportunity.MSPID).Scan(&teamID)
			if errors.Is(err, pgx.ErrNoRows) {
				return scope.ErrNotFound
			}
			if err != nil {
				return err
			}
		}
		tag, err := tx.Exec(ctx, `
UPDATE opportunities
SET team_id = NULLIF($5, '')::uuid, version = version + 1,
    updated_at = $6, updated_by = $7
WHERE id = $1 AND msp_id = $2 AND client_id = $3
  AND version = $4 AND lifecycle_state = 'active'
`, opportunity.ID, opportunity.MSPID, opportunity.ClientID,
			opportunity.Version-1, opportunity.TeamID,
			opportunity.UpdatedAt, opportunity.UpdatedBy)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return object.ErrVersionConflict
		}
		if _, err := tx.Exec(ctx, `
DELETE FROM opportunity_contacts
WHERE opportunity_id = $1 AND msp_id = $2 AND client_id = $3
`, opportunity.ID, opportunity.MSPID, opportunity.ClientID); err != nil {
			return err
		}
		if len(opportunity.ContactIDs) > 0 {
			tag, err = tx.Exec(ctx, `
INSERT INTO opportunity_contacts (
  opportunity_id, contact_id, msp_id, client_id, position
)
SELECT $1, contact.id, $2, $3, requested.position
FROM unnest($4::text[]) WITH ORDINALITY requested(id, position)
JOIN contacts contact
  ON contact.id::text = requested.id AND contact.msp_id = $2
  AND contact.client_id = $3 AND contact.lifecycle_state = 'active'
`, opportunity.ID, opportunity.MSPID, opportunity.ClientID,
				opportunity.ContactIDs)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != int64(len(opportunity.ContactIDs)) {
				return scope.ErrNotFound
			}
		}
		return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
	})
}

func (r *SalesRepository) withTransaction(
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

func writeMutationFacts(
	ctx context.Context,
	tx transaction,
	audit mutation.AuditRecord,
	event mutation.EventRecord,
) error {
	auditSafeDiff := []byte(`{}`)
	if audit.SafeDiff != nil {
		var err error
		auditSafeDiff, err = json.Marshal(audit.SafeDiff)
		if err != nil {
			return err
		}
	}
	eventData := []byte(`{}`)
	if event.Data != nil {
		var err error
		eventData, err = json.Marshal(event.Data)
		if err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO audit_ledger (
  id, occurred_at, msp_id, client_id, actor_type, actor_id, action,
  subject_type, subject_id, subject_version, source, reason, correlation_id,
  safe_diff
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14::jsonb)
`, audit.ID, audit.OccurredAt, audit.MSPID, nullableID(audit.ClientID),
		audit.ActorType, audit.ActorID, audit.Action, audit.SubjectType,
		audit.SubjectID, audit.SubjectVersion, audit.Source,
		nullableText(audit.Reason), audit.CorrelationID, auditSafeDiff); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
INSERT INTO event_outbox (
  event_id, event_type, schema_version, occurred_at, msp_id, client_id,
  actor_type, actor_id, subject_type, subject_id, subject_version,
  correlation_id, causation_id, source, data
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
  NULLIF($13, '')::uuid, $14, $15::jsonb
)
`, event.EventID, event.EventType, event.SchemaVersion, event.OccurredAt,
		event.MSPID, nullableID(event.ClientID), event.ActorType, event.ActorID,
		event.SubjectType, event.SubjectID, event.SubjectVersion,
		event.CorrelationID, event.CausationID, event.Source, eventData)
	return err
}

func nullableID(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableText(value string) any {
	if value == "" {
		return nil
	}
	return value
}
