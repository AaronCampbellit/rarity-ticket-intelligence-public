package psa

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type AIRepository struct {
	db database
}

var _ aiassist.DecisionStore = (*AIRepository)(nil)
var _ aiassist.RecommendationReadStore = (*AIRepository)(nil)
var _ aiassist.Store = (*AIRepository)(nil)

func NewAIRepository(db database) *AIRepository {
	return &AIRepository{db: db}
}

func (r *AIRepository) Get(
	ctx context.Context,
	target scope.Target,
	id string,
) (aiassist.Recommendation, error) {
	var (
		recommendation aiassist.Recommendation
		relevantInputs []byte
		candidateIDs   []byte
	)
	err := r.db.QueryRow(ctx, `
SELECT id::text, feature, msp_id::text, client_id::text,
       work_record_id::text, output_ref, candidate_ids, confidence,
       provider, model, prompt_version, state, generated_at, relevant_inputs
FROM ai_recommendations
WHERE id = $1 AND msp_id = $2 AND client_id = $3
`, id, target.MSPID, target.ClientID).Scan(
		&recommendation.ID, &recommendation.Feature, &recommendation.MSPID,
		&recommendation.ClientID, &recommendation.WorkRecordID,
		&recommendation.Text, &candidateIDs, &recommendation.Confidence,
		&recommendation.Provider, &recommendation.Model,
		&recommendation.PromptVersion, &recommendation.State,
		&recommendation.GeneratedAt, &relevantInputs,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return aiassist.Recommendation{}, scope.ErrNotFound
	}
	if err != nil {
		return aiassist.Recommendation{}, err
	}
	if err := json.Unmarshal(candidateIDs, &recommendation.CandidateIDs); err != nil {
		return aiassist.Recommendation{}, err
	}
	if err := json.Unmarshal(relevantInputs, &recommendation.RelevantInputs); err != nil {
		return aiassist.Recommendation{}, err
	}
	return recommendation, nil
}

func (r *AIRepository) GetForReview(
	ctx context.Context,
	target scope.Target,
	id string,
) (aiassist.RecommendationView, error) {
	var (
		recommendation aiassist.RecommendationView
		candidateIDs   []byte
	)
	err := r.db.QueryRow(ctx, `
SELECT recommendation.id::text, job.id::text, recommendation.client_id::text,
       recommendation.work_record_id::text, recommendation.feature,
       recommendation.output_ref, recommendation.candidate_ids,
       recommendation.confidence, recommendation.state,
       recommendation.generated_at, recommendation.version,
       COALESCE(job.relevant_input_names, ARRAY[]::text[])
FROM ai_recommendations recommendation
JOIN ai_generation_jobs job
  ON job.recommendation_id = recommendation.id
 AND job.msp_id = recommendation.msp_id
 AND job.client_id = recommendation.client_id
WHERE recommendation.id = $1 AND recommendation.msp_id = $2 AND recommendation.client_id = $3
`, id, target.MSPID, target.ClientID).Scan(
		&recommendation.ID, &recommendation.JobID, &recommendation.ClientID,
		&recommendation.WorkRecordID, &recommendation.Feature, &recommendation.Text,
		&candidateIDs, &recommendation.Confidence, &recommendation.State,
		&recommendation.GeneratedAt, &recommendation.Version, &recommendation.RelevantInputs,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return aiassist.RecommendationView{}, scope.ErrNotFound
	}
	if err != nil {
		return aiassist.RecommendationView{}, err
	}
	if err := json.Unmarshal(candidateIDs, &recommendation.CandidateIDs); err != nil {
		return aiassist.RecommendationView{}, err
	}
	return recommendation, nil
}

func (r *AIRepository) Decide(
	ctx context.Context,
	accepted aiassist.DecisionMutation,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	if err := decideAIRecommendation(ctx, tx, accepted); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}

// Record preserves the original synchronous recommendation boundary while the
// durable worker uses the same insert helper inside its lease-fenced commit.
func (r *AIRepository) Record(ctx context.Context, record aiassist.RecommendationRecord) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	if err := insertAIRecommendation(ctx, tx, record); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := writeMutationFacts(ctx, tx, record.Audit, record.Event); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}

func insertAIRecommendation(ctx context.Context, tx transaction, record aiassist.RecommendationRecord) error {
	recommendation := record.Recommendation
	candidateIDs, err := json.Marshal(recommendation.CandidateIDs)
	if err != nil {
		return err
	}
	relevantInputs, err := json.Marshal(recommendation.RelevantInputs)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO ai_recommendations (
  id, msp_id, client_id, work_record_id, feature, provider, model,
  prompt_version, relevant_inputs, output_ref, candidate_ids, confidence,
  state, generated_at, generated_by
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb, $10, $11::jsonb,
          $12, 'pending_human', $13, $14)
`, recommendation.ID, recommendation.MSPID, recommendation.ClientID,
		recommendation.WorkRecordID, recommendation.Feature, recommendation.Provider,
		recommendation.Model, recommendation.PromptVersion, relevantInputs,
		recommendation.Text, candidateIDs, recommendation.Confidence,
		recommendation.GeneratedAt, record.Audit.ActorID); err != nil {
		return err
	}
	usage := record.Usage
	if _, err := tx.Exec(ctx, `
INSERT INTO ai_usage_records (
  id, recommendation_id, msp_id, provider, model, input_units, output_units,
  cost_minor, recorded_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
`, usage.ID, recommendation.ID, recommendation.MSPID, usage.Provider,
		usage.Model, usage.InputUnits, usage.OutputUnits, usage.CostMinor,
		usage.RecordedAt); err != nil {
		return err
	}
	return nil
}

func decideAIRecommendation(
	ctx context.Context,
	tx transaction,
	accepted aiassist.DecisionMutation,
) error {
	recommendation := accepted.Recommendation
	tag, err := tx.Exec(ctx, `
UPDATE ai_recommendations
SET state = $4, version = version + 1
WHERE id = $1 AND msp_id = $2 AND client_id = $3
  AND state = 'pending_human' AND version = 1
`, recommendation.ID, recommendation.MSPID, recommendation.ClientID,
		recommendation.State)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return object.ErrVersionConflict
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO ai_recommendation_decisions (
  id, recommendation_id, msp_id, client_id, decision, reason,
  decided_at, decided_by, applied, sent
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
`, accepted.DecisionID, recommendation.ID, recommendation.MSPID,
		recommendation.ClientID, accepted.Decision, accepted.Reason,
		accepted.DecidedAt, accepted.DecidedBy, accepted.Applied,
		accepted.Sent); err != nil {
		return err
	}
	return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
}
