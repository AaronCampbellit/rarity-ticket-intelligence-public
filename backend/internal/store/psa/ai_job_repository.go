package psa

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/secrets"
)

const aiJobMaxAttempts = 3

var errClassificationCompletionMiss = errors.New("classification completion fence rejected")

type AIJobRepository struct {
	db      database
	secrets secrets.Provider
}

var _ aiassist.JobStore = (*AIJobRepository)(nil)
var _ aiassist.JobQueue = (*AIJobRepository)(nil)

func NewAIJobRepository(db database, provider secrets.Provider) *AIJobRepository {
	return &AIJobRepository{db: db, secrets: provider}
}

func (r *AIJobRepository) Submit(ctx context.Context, command aiassist.SubmitCommand, now time.Time, ids aiassist.JobIDs) (aiassist.GenerationJob, error) {
	if r == nil || r.db == nil || strings.TrimSpace(ids.JobID) == "" || strings.TrimSpace(ids.AuditID) == "" || strings.TrimSpace(ids.EventID) == "" || strings.TrimSpace(ids.CorrelationID) == "" {
		return aiassist.GenerationJob{}, aiassist.ErrAIDenied
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return aiassist.GenerationJob{}, err
	}
	idempotencyKey := scopedAIJobIdempotencyKey(command)
	job, inserted, err := scanGenerationJobInserted(tx.QueryRow(ctx, `
WITH selected AS (
  SELECT work.id, work.msp_id, work.client_id,
         CASE $3::text
           WHEN 'summary' THEN policy.summary_model_profile_id
           WHEN 'reply_draft' THEN policy.reply_draft_model_profile_id
           WHEN 'similar_suggestions' THEN policy.similar_suggestions_model_profile_id
         END AS model_profile_id,
         ARRAY_REMOVE(ARRAY[
           CASE WHEN length(btrim(work.title)) > 0 THEN 'title' END,
           CASE WHEN length(btrim(work.description)) > 0 THEN 'description' END
         ], NULL) AS relevant_input_names
  FROM work_records work
  JOIN ai_policies policy ON policy.msp_id = work.msp_id
  WHERE work.id = $1 AND work.msp_id = $2 AND work.client_id = $4
    AND work.deleted_at IS NULL AND work.lifecycle_state = 'active'
    AND policy.enabled AND $3::text = ANY(policy.allowed_features)
), valid AS (
  SELECT selected.*
  FROM selected
  JOIN ai_model_profiles model
    ON model.id = selected.model_profile_id AND model.msp_id = selected.msp_id
   AND model.enabled AND $3::text = ANY(model.supported_features)
  JOIN ai_provider_connections connection
    ON connection.id = model.connection_id AND connection.msp_id = model.msp_id
   AND connection.enabled
   AND connection.disclosure_accepted_at IS NOT NULL
   AND connection.disclosure_accepted_by IS NOT NULL
  WHERE cardinality(selected.relevant_input_names) > 0
), inserted AS (
  INSERT INTO ai_generation_jobs (
    id, msp_id, client_id, work_record_id, subject_type, subject_id, requested_by, feature,
    model_profile_id, relevant_input_names, state, attempt, max_attempts,
    idempotency_key, created_at, updated_at
  )
  SELECT $5::uuid, msp_id, client_id, id, 'work_record', id, $6::uuid, $3, model_profile_id,
         relevant_input_names, 'queued', 0, $7, $8, $9, $9
  FROM valid
  ON CONFLICT (idempotency_key) DO UPDATE SET id = ai_generation_jobs.id
  RETURNING *, (xmax = 0) AS inserted
)
SELECT id::text, msp_id::text, COALESCE(client_id::text, ''), COALESCE(work_record_id::text, ''),
       subject_type, subject_id::text,
       requested_by::text, feature, model_profile_id::text,
       relevant_input_names, state, attempt, max_attempts,
       COALESCE(lease_token::text, ''), lease_until,
       cancellation_requested_at, COALESCE(cancellation_requested_by::text, ''),
       cancelled_at, COALESCE(safe_error_code, ''), COALESCE(recommendation_id::text, ''),
       idempotency_key, created_at, updated_at, completed_at, version,
       inserted
FROM inserted
`, command.WorkRecordID, command.Principal.Scope.MSPID, command.Feature,
		command.Principal.Scope.ClientID, ids.JobID, command.Principal.ID,
		aiJobMaxAttempts, idempotencyKey, now))
	if err != nil {
		_ = tx.Rollback(ctx)
		if errors.Is(err, pgx.ErrNoRows) {
			return aiassist.GenerationJob{}, scope.ErrNotFound
		}
		return aiassist.GenerationJob{}, err
	}
	// A replay has already produced immutable submission evidence. A newly
	// inserted job and its audit/outbox facts must commit or roll back together.
	if inserted {
		transitionIDs := aiassist.JobTransitionIDs{AuditID: ids.AuditID, EventID: ids.EventID, CorrelationID: ids.CorrelationID}
		if err := writeMutationFacts(ctx, tx,
			jobTransitionAudit(job, command.Principal.ID, "ai.generation_job.submitted", "queued", now, transitionIDs),
			jobTransitionEvent(job, command.Principal.ID, "ai.generation_job.submitted", "queued", now, transitionIDs),
		); err != nil {
			_ = tx.Rollback(ctx)
			return aiassist.GenerationJob{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return aiassist.GenerationJob{}, err
	}
	return job, nil
}

func scopedAIJobIdempotencyKey(command aiassist.SubmitCommand) string {
	return command.Principal.Scope.MSPID + "|" + command.Principal.Scope.ClientID + "|" +
		command.Principal.ID + "|" + command.WorkRecordID + "|" + string(command.Feature) + "|" + command.IdempotencyKey
}

func (r *AIJobRepository) Get(ctx context.Context, target scope.Target, id string) (aiassist.GenerationJob, error) {
	job, err := scanGenerationJob(r.db.QueryRow(ctx, generationJobSelect+`
WHERE job.id = $1 AND job.msp_id = $2 AND job.client_id = $3
`, id, target.MSPID, target.ClientID))
	if errors.Is(err, pgx.ErrNoRows) {
		return aiassist.GenerationJob{}, scope.ErrNotFound
	}
	return job, err
}

func (r *AIJobRepository) Cancel(ctx context.Context, target scope.Target, id, actorID, reason string, expectedVersion int64, now time.Time, ids aiassist.JobTransitionIDs) (aiassist.GenerationJob, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return aiassist.GenerationJob{}, err
	}
	job, err := scanGenerationJob(tx.QueryRow(ctx, `
UPDATE ai_generation_jobs job
SET cancellation_requested_at = $4,
    cancellation_requested_by = $5::uuid,
    state = CASE WHEN job.state = 'queued' THEN 'cancelled' ELSE job.state END,
    cancelled_at = CASE WHEN job.state = 'queued' THEN $4 ELSE job.cancelled_at END,
    lease_token = CASE WHEN job.state = 'queued' THEN NULL ELSE job.lease_token END,
    lease_until = CASE WHEN job.state = 'queued' THEN NULL ELSE job.lease_until END,
    updated_at = $4, version = job.version + 1
WHERE job.id = $1 AND job.msp_id = $2 AND job.client_id = $3
  AND job.state IN ('queued', 'running') AND job.version = $6
RETURNING `+generationJobColumns, id, target.MSPID, target.ClientID, now, actorID, expectedVersion))
	if errors.Is(err, pgx.ErrNoRows) {
		_ = tx.Rollback(ctx)
		return aiassist.GenerationJob{}, scope.ErrNotFound
	}
	if err != nil {
		_ = tx.Rollback(ctx)
		return aiassist.GenerationJob{}, err
	}
	action := "ai.generation_job.cancellation_requested"
	if job.State == aiassist.JobCancelled {
		action = "ai.generation_job.cancelled"
	}
	if err := writeMutationFacts(ctx, tx, jobTransitionAudit(job, actorID, action, reason, now, ids), jobTransitionEvent(job, actorID, action, reason, now, ids)); err != nil {
		_ = tx.Rollback(ctx)
		return aiassist.GenerationJob{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return aiassist.GenerationJob{}, err
	}
	return job, nil
}

func (r *AIJobRepository) Retry(ctx context.Context, target scope.Target, id, reason string, expectedVersion int64, now time.Time, ids aiassist.JobTransitionIDs) (aiassist.GenerationJob, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return aiassist.GenerationJob{}, err
	}
	job, err := scanGenerationJob(tx.QueryRow(ctx, `
UPDATE ai_generation_jobs job
SET state = 'queued', lease_token = NULL, lease_until = NULL,
    cancellation_requested_at = NULL, cancellation_requested_by = NULL,
	    safe_error_code = NULL, completed_at = NULL, updated_at = $4, version = job.version + 1
WHERE job.id = $1 AND job.msp_id = $2 AND job.client_id = $3
  AND job.state = 'failed' AND job.attempt < job.max_attempts
  AND job.safe_error_code IN ('provider_timeout', 'provider_unavailable') AND job.version = $5
RETURNING `+generationJobColumns, id, target.MSPID, target.ClientID, now, expectedVersion))
	if errors.Is(err, pgx.ErrNoRows) {
		_ = tx.Rollback(ctx)
		return aiassist.GenerationJob{}, aiassist.ErrJobNotRetryable
	}
	if err != nil {
		_ = tx.Rollback(ctx)
		return aiassist.GenerationJob{}, err
	}
	if err := writeMutationFacts(ctx, tx, jobTransitionAudit(job, "", "ai.generation_job.requeued", reason, now, ids), jobTransitionEvent(job, "", "ai.generation_job.requeued", reason, now, ids)); err != nil {
		_ = tx.Rollback(ctx)
		return aiassist.GenerationJob{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return aiassist.GenerationJob{}, err
	}
	return job, nil
}

func (r *AIJobRepository) Claim(ctx context.Context, limit int, lease time.Duration, ids []aiassist.JobTransitionIDs) ([]aiassist.ClaimedGenerationJob, error) {
	if r == nil || r.db == nil || limit < 1 || lease <= 0 {
		return []aiassist.ClaimedGenerationJob{}, nil
	}
	if len(ids) < limit {
		return nil, aiassist.ErrAIDenied
	}
	for _, id := range ids[:limit] {
		if strings.TrimSpace(id.AuditID) == "" || strings.TrimSpace(id.EventID) == "" || strings.TrimSpace(id.CorrelationID) == "" {
			return nil, aiassist.ErrAIDenied
		}
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]aiassist.ClaimedGenerationJob, 0)
	for i := 0; i < limit; i++ {
		job, err := scanGenerationJob(tx.QueryRow(ctx, `
WITH candidate AS (
  SELECT job.id
  FROM ai_generation_jobs job
  WHERE (job.state = 'queued' AND job.next_attempt_at <= now() AND job.cancellation_requested_at IS NULL)
     OR (job.state = 'running' AND job.lease_until < now() AND job.cancellation_requested_at IS NULL AND job.attempt < job.max_attempts)
  ORDER BY job.created_at, job.id
  FOR UPDATE SKIP LOCKED
  LIMIT 1
)
UPDATE ai_generation_jobs job
SET state = 'running', attempt = job.attempt + 1,
    lease_token = md5(random()::text || clock_timestamp()::text)::uuid,
    lease_until = now() + ($1 * interval '1 microsecond'),
    safe_error_code = NULL, updated_at = now(), version = job.version + 1
FROM candidate
WHERE job.id = candidate.id AND job.attempt < job.max_attempts
RETURNING `+generationJobColumns, leaseMicroseconds(lease)))
		if errors.Is(err, pgx.ErrNoRows) {
			break
		}
		if err != nil {
			_ = tx.Rollback(ctx)
			return nil, err
		}
		if err := writeMutationFacts(ctx, tx,
			workerJobTransitionAudit(job, "ai.generation_job.claimed", "lease_acquired", job.UpdatedAt, ids[i]),
			workerJobTransitionEvent(job, "ai.generation_job.claimed", "lease_acquired", job.UpdatedAt, ids[i]),
		); err != nil {
			_ = tx.Rollback(ctx)
			return nil, err
		}
		result = append(result, aiassist.ClaimedGenerationJob{Job: job, LeaseToken: job.LeaseToken})
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	return result, nil
}

// SweepStranded terminalizes every expired running job that cannot legally be
// reclaimed. Cancellation wins over attempt exhaustion. Each transition, its
// reservation release, audit row, and outbox event are one transaction, so a
// worker crash cannot leave a permanently-running job or stranded budget.
func (r *AIJobRepository) SweepStranded(ctx context.Context) (int, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	var swept int
	err = tx.QueryRow(ctx, `
WITH stranded AS (
  UPDATE ai_generation_jobs job
  SET state = CASE WHEN job.cancellation_requested_at IS NOT NULL THEN 'cancelled' ELSE 'failed' END,
      cancelled_at = CASE WHEN job.cancellation_requested_at IS NOT NULL THEN now() ELSE job.cancelled_at END,
      completed_at = CASE WHEN job.cancellation_requested_at IS NULL THEN now() ELSE job.completed_at END,
      safe_error_code = CASE WHEN job.cancellation_requested_at IS NOT NULL THEN 'cancelled' ELSE 'lease_expired' END,
      lease_token = NULL, lease_until = NULL, reserved_cost_minor = 0,
      updated_at = now(), version = job.version + 1
  WHERE job.state = 'running' AND job.lease_until < now()
    AND (job.cancellation_requested_at IS NOT NULL OR job.attempt >= job.max_attempts)
  RETURNING job.id, job.msp_id, job.client_id, job.requested_by, job.version, job.state, job.safe_error_code
), facts AS (
  SELECT stranded.*, md5(random()::text || clock_timestamp()::text)::uuid AS audit_id,
         md5(random()::text || clock_timestamp()::text)::uuid AS event_id,
         md5(random()::text || clock_timestamp()::text)::uuid AS correlation_id
  FROM stranded
), audit AS (
  INSERT INTO audit_ledger (id, occurred_at, msp_id, client_id, actor_type, actor_id, action, subject_type, subject_id, subject_version, source, reason, correlation_id)
  SELECT audit_id, now(), msp_id, client_id, 'system', requested_by,
         'ai.generation_job.' || state, 'ai_generation_job', id, version, 'system', safe_error_code, correlation_id
  FROM facts
), event AS (
  INSERT INTO event_outbox (event_id, event_type, schema_version, occurred_at, msp_id, client_id, actor_type, actor_id, subject_type, subject_id, subject_version, correlation_id, source, data)
  SELECT event_id, 'ai.generation_job.' || state, 1, now(), msp_id, client_id, 'system', requested_by,
         'ai_generation_job', id, version, correlation_id, 'system', jsonb_build_object('reason', safe_error_code)
  FROM facts
)
SELECT count(*) FROM facts
`).Scan(&swept)
	if err != nil {
		_ = tx.Rollback(ctx)
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return 0, err
	}
	return swept, nil
}

func (r *AIJobRepository) LoadExecution(ctx context.Context, id, leaseToken string) (aiassist.JobExecution, error) {
	return r.loadExecution(ctx, r.db, id, leaseToken)
}

type jobExecutionQueryer interface {
	QueryRow(context.Context, string, ...any) row
}

func (r *AIJobRepository) loadExecution(ctx context.Context, source jobExecutionQueryer, id, leaseToken string) (aiassist.JobExecution, error) {
	var execution aiassist.JobExecution
	var features, modelFeatures []string
	var timeoutSeconds int64
	var currentMonthlyCost int64
	var title, description string
	var adapter, network, health string
	err := source.QueryRow(ctx, `
WITH subject AS (
  SELECT 'work_record'::text subject_type, id, msp_id, client_id, title, description, updated_at FROM work_records WHERE deleted_at IS NULL AND lifecycle_state = 'active'
  UNION ALL SELECT 'task', id, msp_id, client_id, title, ''::text, updated_at FROM tasks WHERE status <> 'cancelled'
  UNION ALL SELECT 'project', id, msp_id, client_id, name, ''::text, updated_at FROM projects WHERE lifecycle_state <> 'cancelled'
  UNION ALL SELECT 'asset', id, msp_id, client_id, name, ''::text, updated_at FROM assets WHERE lifecycle_state <> 'retired'
  UNION ALL SELECT 'time_entry', id, msp_id, client_id, note, ''::text, created_at FROM time_entries
  UNION ALL SELECT 'knowledge_article', article.id, article.msp_id, article.client_id, article.title,
                   version.body, article.updated_at
            FROM knowledge_articles article
            JOIN knowledge_article_versions version
              ON version.article_id = article.id AND version.msp_id = article.msp_id
             AND version.version = article.current_version
            WHERE article.state <> 'archived'
)
SELECT job.id::text, job.msp_id::text, COALESCE(job.client_id::text, ''), COALESCE(job.work_record_id::text, ''),
       job.subject_type, job.subject_id::text,
       job.requested_by::text, job.feature, job.model_profile_id::text,
       job.relevant_input_names, job.state, job.attempt, job.max_attempts,
       COALESCE(job.lease_token::text, ''), job.lease_until,
       job.cancellation_requested_at, COALESCE(job.cancellation_requested_by::text, ''),
       job.cancelled_at, COALESCE(job.safe_error_code, ''), COALESCE(job.recommendation_id::text, ''),
       job.idempotency_key, job.created_at, job.updated_at, job.completed_at, job.version,
       policy.enabled, policy.allowed_features, policy.cost_limit_enabled, policy.allow_unmetered_unknown,
       policy.monthly_cost_limit_minor, COALESCE((SELECT sum(usage.cost_minor) FROM ai_usage_records usage
          WHERE usage.msp_id = job.msp_id
            AND usage.recorded_at >= (date_trunc('month', now() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC')
            AND usage.recorded_at < ((date_trunc('month', now() AT TIME ZONE 'UTC') + interval '1 month') AT TIME ZONE 'UTC')), 0), policy.version,
       model.id::text, model.connection_id::text, model.provider_model_id, model.display_name,
       model.supported_features, model.context_limit, model.output_limit, model.zero_cost,
       model.input_cost_per_million_minor, model.output_cost_per_million_minor, model.enabled, model.version,
       connection.id::text, connection.name, connection.adapter_type, connection.network_mode, connection.base_url,
       (connection.credential_version IS NOT NULL), connection.enabled, connection.timeout_seconds,
       connection.request_limit_bytes, connection.response_limit_bytes, connection.local_network_acknowledged_at,
       connection.health_state, connection.version,
       work.title, work.description, work.updated_at,
       CASE WHEN job.feature = 'classification' THEN COALESCE((SELECT array_agg(tag.id::text ORDER BY tag.id) FROM tags tag WHERE tag.msp_id = job.msp_id AND tag.lifecycle_state = 'active'), ARRAY[]::text[])
       ELSE COALESCE((SELECT array_agg(candidate.id::text ORDER BY candidate.id)
                 FROM work_records candidate
                 WHERE candidate.msp_id = job.msp_id AND candidate.client_id = job.client_id
                   AND candidate.deleted_at IS NULL AND candidate.lifecycle_state = 'active'
                   AND candidate.id <> job.work_record_id), ARRAY[]::text[]) END,
       CASE WHEN job.feature = 'classification' THEN md5(COALESCE((SELECT string_agg(tag.id::text || ':' || tag.version::text, ',' ORDER BY tag.id) FROM tags tag WHERE tag.msp_id = job.msp_id AND tag.lifecycle_state = 'active'), ''))
       ELSE md5(COALESCE((SELECT string_agg(candidate.id::text || ':' || candidate.updated_at::text, ',' ORDER BY candidate.id)
                     FROM work_records candidate
                     WHERE candidate.msp_id = job.msp_id AND candidate.client_id = job.client_id
                       AND candidate.deleted_at IS NULL AND candidate.lifecycle_state = 'active'
                       AND candidate.id <> job.work_record_id), '')) END
FROM ai_generation_jobs job
JOIN ai_policies policy ON policy.msp_id = job.msp_id
JOIN ai_model_profiles model ON model.id = job.model_profile_id AND model.msp_id = job.msp_id
JOIN ai_provider_connections connection ON connection.id = model.connection_id AND connection.msp_id = model.msp_id
JOIN subject work ON work.subject_type = job.subject_type AND work.id = job.subject_id AND work.msp_id = job.msp_id AND work.client_id IS NOT DISTINCT FROM job.client_id
LEFT JOIN client_organizations client ON client.id = job.client_id AND client.msp_id = job.msp_id
JOIN technicians requester ON requester.id = job.requested_by AND requester.msp_id = job.msp_id AND requester.lifecycle_state = 'active'
WHERE job.id = $1 AND job.state = 'running' AND job.lease_token = $2::uuid
  AND job.lease_until > now() AND job.cancellation_requested_at IS NULL
  AND policy.enabled AND job.feature::text = ANY(policy.allowed_features) AND model.enabled AND connection.enabled
  AND connection.disclosure_accepted_at IS NOT NULL
  AND connection.disclosure_accepted_by IS NOT NULL
  AND (job.client_id IS NULL OR (client.lifecycle_state = 'active' AND client.deleted_at IS NULL))
  AND job.feature::text = ANY(model.supported_features)
  AND model.id = CASE job.feature WHEN 'summary' THEN policy.summary_model_profile_id
                                  WHEN 'reply_draft' THEN policy.reply_draft_model_profile_id
                                  WHEN 'similar_suggestions' THEN policy.similar_suggestions_model_profile_id
                                  ELSE policy.classification_model_profile_id END
  AND EXISTS (
    SELECT 1 FROM role_assignments assignment
    JOIN role_capabilities capability ON capability.role_id = assignment.role_id AND capability.msp_id = assignment.msp_id
    WHERE assignment.msp_id = job.msp_id AND assignment.technician_id = job.requested_by
      AND (assignment.client_id IS NULL OR assignment.client_id = job.client_id)
      AND (assignment.expires_at IS NULL OR assignment.expires_at > now())
      AND capability.capability = CASE WHEN job.feature = 'classification' THEN 'classification.apply' ELSE 'ai.assist' END
  )
`, id, leaseToken).Scan(
		&execution.Job.ID, &execution.Job.MSPID, &execution.Job.ClientID, &execution.Job.WorkRecordID, &execution.Job.Subject.Type, &execution.Job.Subject.ID,
		&execution.Job.RequestedBy, &execution.Job.Feature, &execution.Job.ModelProfileID,
		&execution.Job.RelevantInputs, &execution.Job.State, &execution.Job.Attempt, &execution.Job.MaxAttempts,
		&execution.Job.LeaseToken, &execution.Job.LeaseUntil, &execution.Job.CancellationRequestedAt,
		&execution.Job.CancellationRequestedBy, &execution.Job.CancelledAt, &execution.Job.SafeErrorCode,
		&execution.Job.RecommendationID, &execution.Job.IdempotencyKey, &execution.Job.CreatedAt,
		&execution.Job.UpdatedAt, &execution.Job.CompletedAt, &execution.Job.Version,
		&execution.Policy.Enabled, &features, &execution.Policy.CostLimitEnabled, &execution.Policy.AllowUnmeteredUnknown,
		&execution.Policy.MonthlyCostLimitMinor, &currentMonthlyCost, &execution.Policy.Version,
		&execution.Model.ID, &execution.Model.ConnectionID, &execution.Model.ProviderModelID,
		&execution.Model.DisplayName, &modelFeatures, &execution.Model.ContextLimit,
		&execution.Model.OutputLimit, &execution.Model.ZeroCost, &execution.Model.InputCostPerMillionMinor,
		&execution.Model.OutputCostPerMillionMinor, &execution.Model.Enabled, &execution.Model.Version,
		&execution.Connection.ID, &execution.Connection.Name, &adapter, &network,
		&execution.Connection.BaseURL, &execution.Connection.CredentialConfigured, &execution.Connection.Enabled,
		&timeoutSeconds, &execution.Connection.RequestLimitBytes, &execution.Connection.ResponseLimitBytes,
		&execution.Connection.LocalNetworkAcknowledgedAt, &health, &execution.Connection.Version,
		&title, &description, &execution.Snapshot.WorkUpdatedAt, &execution.Context.AuthorizedCandidateIDs,
		&execution.Snapshot.CandidateFingerprint,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return aiassist.JobExecution{}, aiassist.ErrJobLeaseLost
	}
	if err != nil {
		return aiassist.JobExecution{}, err
	}
	execution.Policy.MSPID = execution.Job.MSPID
	execution.Job.Subject.MSPID = execution.Job.MSPID
	execution.Job.Subject.ClientID = execution.Job.ClientID
	execution.Policy.CurrentMonthlyCostMinor = &currentMonthlyCost
	execution.Policy.PromptVersion = "ai-v1"
	execution.Policy.AllowedFeatures = featuresFromStrings(features)
	execution.Model.MSPID = execution.Job.MSPID
	execution.Model.SupportedFeatures = featuresFromStrings(modelFeatures)
	execution.Connection.MSPID = execution.Job.MSPID
	execution.Connection.Timeout = time.Duration(timeoutSeconds) * time.Second
	execution.Connection.Adapter = aiassist.AdapterType(adapter)
	execution.Connection.Network = aiassist.NetworkMode(network)
	execution.Connection.Health = aiassist.HealthState(health)
	for _, name := range execution.Job.RelevantInputs {
		switch name {
		case "title":
			execution.Context.Fields = append(execution.Context.Fields, aiassist.ContextField{Name: name, Value: title, Classification: aiassist.ContextStandard})
		case "description":
			execution.Context.Fields = append(execution.Context.Fields, aiassist.ContextField{Name: name, Value: description, Classification: aiassist.ContextStandard})
		}
	}
	return execution, nil
}

// AuthorizeAndReserve is the final disclosure boundary. The advisory xact lock
// serializes every paid job for an MSP so two workers cannot independently
// observe remaining monthly budget and both call a provider. It deliberately
// reloads the authorization graph after the lock: initial worker context is
// only a preparation snapshot, never authority to open a credential.
func (r *AIJobRepository) AuthorizeAndReserve(ctx context.Context, id, leaseToken, fingerprint string, inputUnits, outputUnits int64) (aiassist.JobExecution, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return aiassist.JobExecution{}, err
	}
	fail := func(err error) (aiassist.JobExecution, error) {
		_ = tx.Rollback(ctx)
		return aiassist.JobExecution{}, err
	}
	if _, err := tx.Exec(ctx, `
SELECT pg_advisory_xact_lock(hashtextextended((SELECT msp_id::text FROM ai_generation_jobs WHERE id = $1::uuid), 0))
`, id); err != nil {
		return fail(err)
	}
	execution, err := r.loadExecution(ctx, tx, id, leaseToken)
	if err != nil {
		return fail(err)
	}
	reserved := int64(0)
	if execution.Policy.CostLimitEnabled && !execution.Model.ZeroCost {
		reserved, err = aiassist.EstimateReservedCostMinor(execution.Model, inputUnits, outputUnits)
		if errors.Is(err, aiassist.ErrUnknownCost) && execution.Policy.AllowUnmeteredUnknown {
			reserved, err = 0, nil
		}
		if err != nil {
			return fail(err)
		}
		var activeReservations int64
		if err := tx.QueryRow(ctx, `
SELECT COALESCE(sum(reserved_cost_minor), 0)
FROM ai_generation_jobs
WHERE msp_id = $1::uuid AND state = 'running' AND id <> $2::uuid
`, execution.Job.MSPID, execution.Job.ID).Scan(&activeReservations); err != nil {
			return fail(err)
		}
		actual := int64(0)
		if execution.Policy.CurrentMonthlyCostMinor != nil {
			actual = *execution.Policy.CurrentMonthlyCostMinor
		}
		if actual < 0 || activeReservations < 0 || reserved > execution.Policy.MonthlyCostLimitMinor ||
			actual > execution.Policy.MonthlyCostLimitMinor-reserved || activeReservations > execution.Policy.MonthlyCostLimitMinor-actual-reserved {
			return fail(aiassist.ErrCostLimitExceeded)
		}
	}
	if strings.TrimSpace(fingerprint) == "" {
		return fail(aiassist.ErrJobLeaseLost)
	}
	tag, err := tx.Exec(ctx, `
WITH subject AS (
  SELECT 'work_record'::text subject_type,id,msp_id,client_id,updated_at FROM work_records WHERE deleted_at IS NULL AND lifecycle_state='active'
  UNION ALL SELECT 'task',id,msp_id,client_id,updated_at FROM tasks WHERE status <> 'cancelled'
  UNION ALL SELECT 'project',id,msp_id,client_id,updated_at FROM projects WHERE lifecycle_state <> 'cancelled'
  UNION ALL SELECT 'asset',id,msp_id,client_id,updated_at FROM assets WHERE lifecycle_state <> 'retired'
  UNION ALL SELECT 'time_entry',id,msp_id,client_id,created_at FROM time_entries
  UNION ALL SELECT 'knowledge_article',id,msp_id,client_id,updated_at FROM knowledge_articles WHERE state <> 'archived'
)
UPDATE ai_generation_jobs
SET reserved_cost_minor = $3, execution_fingerprint = $4, execution_policy_version = $5,
    execution_model_version = $6, execution_connection_version = $7,
    execution_work_updated_at = $8, execution_candidate_fingerprint = $9, updated_at = now()
WHERE id = $1::uuid AND state = 'running' AND lease_token = $2::uuid
  AND lease_until > now() AND cancellation_requested_at IS NULL
	AND EXISTS (
		SELECT 1
		FROM ai_policies policy
		JOIN ai_model_profiles model ON model.id = ai_generation_jobs.model_profile_id AND model.msp_id = ai_generation_jobs.msp_id
		JOIN ai_provider_connections connection ON connection.id = model.connection_id AND connection.msp_id = model.msp_id
		JOIN subject current_subject ON current_subject.subject_type=ai_generation_jobs.subject_type
		  AND current_subject.id=ai_generation_jobs.subject_id AND current_subject.msp_id=ai_generation_jobs.msp_id
		  AND current_subject.client_id IS NOT DISTINCT FROM ai_generation_jobs.client_id
		WHERE policy.msp_id = ai_generation_jobs.msp_id AND policy.version = $5 AND model.version = $6
		  AND connection.version = $7 AND current_subject.updated_at = $8
		  AND policy.enabled AND model.enabled AND connection.enabled
		  AND connection.disclosure_accepted_at IS NOT NULL
		  AND connection.disclosure_accepted_by IS NOT NULL
		  AND ai_generation_jobs.feature::text = ANY(policy.allowed_features)
		  AND ai_generation_jobs.feature::text = ANY(model.supported_features)
		  AND model.id = CASE ai_generation_jobs.feature WHEN 'summary' THEN policy.summary_model_profile_id
														 WHEN 'reply_draft' THEN policy.reply_draft_model_profile_id
														 WHEN 'similar_suggestions' THEN policy.similar_suggestions_model_profile_id
														 ELSE policy.classification_model_profile_id END
		  AND (CASE WHEN ai_generation_jobs.feature = 'classification' THEN $9 = md5(COALESCE((SELECT string_agg(tag.id::text || ':' || tag.version::text, ',' ORDER BY tag.id) FROM tags tag WHERE tag.msp_id = ai_generation_jobs.msp_id AND tag.lifecycle_state = 'active'), '')) ELSE $9 = md5(COALESCE((
			SELECT string_agg(candidate.id::text || ':' || candidate.updated_at::text, ',' ORDER BY candidate.id)
			FROM work_records candidate
			WHERE candidate.msp_id = ai_generation_jobs.msp_id AND candidate.client_id = ai_generation_jobs.client_id
			  AND candidate.deleted_at IS NULL AND candidate.lifecycle_state = 'active'
			  AND candidate.id <> ai_generation_jobs.work_record_id
		  ), '')) END)
	)
`, id, leaseToken, reserved, fingerprint, execution.Policy.Version, execution.Model.Version,
		execution.Connection.Version, execution.Snapshot.WorkUpdatedAt, execution.Snapshot.CandidateFingerprint)
	if err != nil {
		return fail(err)
	}
	if tag.RowsAffected() != 1 {
		return fail(aiassist.ErrJobLeaseLost)
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return aiassist.JobExecution{}, err
	}
	return execution, nil
}

func (r *AIJobRepository) CancellationRequested(ctx context.Context, id, leaseToken string) (bool, error) {
	var cancelled bool
	err := r.db.QueryRow(ctx, `
SELECT cancellation_requested_at IS NOT NULL
FROM ai_generation_jobs
WHERE id = $1 AND state = 'running' AND lease_token = $2::uuid
  AND lease_until > now()
`, id, leaseToken).Scan(&cancelled)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	return cancelled, err
}

func (r *AIJobRepository) MaintainLease(ctx context.Context, id, leaseToken string, lease time.Duration) (aiassist.LeaseMaintenance, error) {
	var status aiassist.LeaseMaintenance
	err := r.db.QueryRow(ctx, `
UPDATE ai_generation_jobs
SET lease_until = now() + ($3 * interval '1 microsecond'), updated_at = now()
WHERE id = $1 AND state = 'running' AND lease_token = $2::uuid
  AND lease_until > now() AND cancellation_requested_at IS NULL
RETURNING true, cancellation_requested_at IS NOT NULL
`, id, leaseToken, leaseMicroseconds(lease)).Scan(&status.Owned, &status.Cancelled)
	if errors.Is(err, pgx.ErrNoRows) {
		cancelled, cancelErr := r.CancellationRequested(ctx, id, leaseToken)
		if cancelErr == nil && cancelled {
			return aiassist.LeaseMaintenance{Cancelled: true}, nil
		}
		return aiassist.LeaseMaintenance{}, nil
	}
	return status, err
}

func (r *AIJobRepository) classifyCompletionMiss(ctx context.Context, id, leaseToken string) error {
	var state string
	var cancelled bool
	err := r.db.QueryRow(ctx, `
SELECT state, cancellation_requested_at IS NOT NULL
FROM ai_generation_jobs
WHERE id = $1 AND lease_token = $2::uuid AND lease_until > now()
`, id, leaseToken).Scan(&state, &cancelled)
	if err != nil || state != string(aiassist.JobRunning) {
		return aiassist.ErrJobLeaseLost
	}
	if cancelled {
		return aiassist.ErrJobCancelled
	}
	return aiassist.ErrCostLimitExceeded
}

func (r *AIJobRepository) ExecuteWithCredential(ctx context.Context, reference aiassist.CredentialReference, use func([]byte) error) (err error) {
	if strings.TrimSpace(reference.ConnectionID) == "" || strings.TrimSpace(reference.MSPID) == "" || reference.ConnectionVersion < 1 || strings.TrimSpace(reference.ConnectionBaseURL) == "" || (reference.ConnectionNetwork != aiassist.NetworkLocal && reference.ConnectionNetwork != aiassist.NetworkRemote) || strings.TrimSpace(reference.JobID) == "" || strings.TrimSpace(reference.LeaseToken) == "" || strings.TrimSpace(reference.ExecutionFingerprint) == "" || use == nil {
		return aiassist.ErrProviderCredentialUnavailable
	}
	sessionDB, ok := r.db.(executionSessionDatabase)
	if !ok {
		return aiassist.ErrProviderCredentialUnavailable
	}
	session, err := sessionDB.Acquire(ctx)
	if err != nil {
		return aiassist.ErrProviderCredentialUnavailable
	}
	locked := false
	defer func() {
		// A session advisory lock is owned by this physical connection, not the
		// pool. Always unlock on that connection before returning it, even after
		// context cancellation or a provider panic (defer then re-panics).
		if locked {
			if _, unlockErr := session.Exec(context.WithoutCancel(ctx), providerConnectionAdvisoryUnlockSQL, reference.MSPID, reference.ConnectionID); unlockErr != nil && err == nil {
				err = unlockErr
			}
		}
		session.Release()
	}()
	if _, err = session.Exec(ctx, providerConnectionAdvisoryLockSQL, reference.MSPID, reference.ConnectionID); err != nil {
		return aiassist.ErrProviderCredentialUnavailable
	}
	locked = true
	var sealed secrets.SealedValue
	err = session.QueryRow(ctx, `
WITH subject AS (
  SELECT 'work_record'::text subject_type,id,msp_id,client_id,updated_at FROM work_records WHERE deleted_at IS NULL AND lifecycle_state='active'
  UNION ALL SELECT 'task',id,msp_id,client_id,updated_at FROM tasks WHERE status <> 'cancelled'
  UNION ALL SELECT 'project',id,msp_id,client_id,updated_at FROM projects WHERE lifecycle_state <> 'cancelled'
  UNION ALL SELECT 'asset',id,msp_id,client_id,updated_at FROM assets WHERE lifecycle_state <> 'retired'
  UNION ALL SELECT 'time_entry',id,msp_id,client_id,created_at FROM time_entries
  UNION ALL SELECT 'knowledge_article',id,msp_id,client_id,updated_at FROM knowledge_articles WHERE state <> 'archived'
)
SELECT COALESCE(credential_version, 0), COALESCE(credential_nonce, ''::bytea), COALESCE(credential_ciphertext, ''::bytea)
FROM ai_provider_connections connection
JOIN ai_generation_jobs job ON job.id = $4::uuid AND job.msp_id = connection.msp_id
JOIN subject current_subject ON current_subject.subject_type=job.subject_type
  AND current_subject.id=job.subject_id AND current_subject.msp_id=job.msp_id
  AND current_subject.client_id IS NOT DISTINCT FROM job.client_id
WHERE connection.id = $1 AND connection.msp_id = $2 AND connection.version = $3
  AND connection.enabled
  AND connection.disclosure_accepted_at IS NOT NULL
  AND connection.disclosure_accepted_by IS NOT NULL
  AND job.state = 'running' AND job.lease_token = $5::uuid
  AND job.lease_until > now() AND job.cancellation_requested_at IS NULL
  AND connection.base_url = $6 AND connection.network_mode = $7
  AND job.execution_fingerprint = $8
  AND job.execution_connection_version = connection.version
  AND job.execution_policy_version = (SELECT version FROM ai_policies WHERE msp_id = job.msp_id)
  AND job.execution_model_version = (SELECT version FROM ai_model_profiles WHERE id = job.model_profile_id AND msp_id = job.msp_id)
  AND job.execution_work_updated_at = current_subject.updated_at
  AND (CASE WHEN job.feature = 'classification' THEN job.execution_candidate_fingerprint = md5(COALESCE((SELECT string_agg(tag.id::text || ':' || tag.version::text, ',' ORDER BY tag.id) FROM tags tag WHERE tag.msp_id = job.msp_id AND tag.lifecycle_state = 'active'), '')) ELSE job.execution_candidate_fingerprint = md5(COALESCE((
    SELECT string_agg(candidate.id::text || ':' || candidate.updated_at::text, ',' ORDER BY candidate.id)
    FROM work_records candidate
    WHERE candidate.msp_id = job.msp_id AND candidate.client_id = job.client_id
      AND candidate.deleted_at IS NULL AND candidate.lifecycle_state = 'active'
      AND candidate.id <> job.work_record_id
  ), '')) END)
`, reference.ConnectionID, reference.MSPID, reference.ConnectionVersion, reference.JobID, reference.LeaseToken,
		reference.ConnectionBaseURL, reference.ConnectionNetwork, reference.ExecutionFingerprint).Scan(&sealed.Version, &sealed.Nonce, &sealed.Ciphertext)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return aiassist.ErrExecutionFenceLost
		}
		return aiassist.ErrProviderCredentialUnavailable
	}
	if sealed.Version == 0 || len(sealed.Nonce) == 0 || len(sealed.Ciphertext) == 0 {
		return use(nil)
	}
	if r.secrets == nil {
		return aiassist.ErrProviderCredentialUnavailable
	}
	credential, err := r.secrets.Open(ctx, "ai.provider."+reference.ConnectionID, sealed)
	if err != nil {
		return aiassist.ErrProviderCredentialUnavailable
	}
	defer func() {
		for i := range credential {
			credential[i] = 0
		}
	}()
	return use(credential)
}

func (r *AIJobRepository) Complete(ctx context.Context, completion aiassist.JobCompletion) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	// Completion uses the same MSP serialization key as reservation. The
	// reservation remains held until the usage row and terminal state commit.
	if _, err := tx.Exec(ctx, `
SELECT pg_advisory_xact_lock(hashtextextended((SELECT msp_id::text FROM ai_generation_jobs WHERE id = $1::uuid), 0))
`, completion.JobID); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if completion.Record.ClassificationCandidates != nil {
		if err := r.completeClassification(ctx, tx, completion); err != nil {
			_ = tx.Rollback(ctx)
			if errors.Is(err, errClassificationCompletionMiss) {
				return r.classifyCompletionMiss(ctx, completion.JobID, completion.LeaseToken)
			}
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		return nil
	}
	if err := insertAIRecommendation(ctx, tx, completion.Record); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	tag, err := tx.Exec(ctx, `
UPDATE ai_generation_jobs
SET state = 'completed', recommendation_id = $3::uuid, completed_at = $4,
    updated_at = $4, lease_token = NULL, lease_until = NULL, reserved_cost_minor = 0,
    safe_error_code = NULL, version = version + 1
WHERE id = $1
  AND state = 'running' AND lease_token = $2::uuid
  AND lease_until > now() AND cancellation_requested_at IS NULL
  AND EXISTS (
    SELECT 1
    FROM ai_policies policy
    JOIN ai_model_profiles model ON model.id = ai_generation_jobs.model_profile_id AND model.msp_id = ai_generation_jobs.msp_id
    JOIN ai_provider_connections connection ON connection.id = model.connection_id AND connection.msp_id = model.msp_id
    WHERE policy.msp_id = ai_generation_jobs.msp_id AND policy.enabled
      AND ai_generation_jobs.feature::text = ANY(policy.allowed_features)
      AND ai_generation_jobs.feature::text = ANY(model.supported_features)
      AND model.enabled AND connection.enabled
      AND connection.disclosure_accepted_at IS NOT NULL
      AND connection.disclosure_accepted_by IS NOT NULL
      AND model.id = CASE ai_generation_jobs.feature WHEN 'summary' THEN policy.summary_model_profile_id
                                                     WHEN 'reply_draft' THEN policy.reply_draft_model_profile_id
                                                     ELSE policy.similar_suggestions_model_profile_id END
  )
  AND NOT EXISTS (
    SELECT 1 FROM ai_policies policy JOIN ai_model_profiles model ON model.id = ai_generation_jobs.model_profile_id AND model.msp_id = ai_generation_jobs.msp_id
    WHERE policy.msp_id = ai_generation_jobs.msp_id AND ai_generation_jobs.feature::text = ANY(policy.allowed_features)
      AND model.id = CASE ai_generation_jobs.feature WHEN 'summary' THEN policy.summary_model_profile_id
                                                     WHEN 'reply_draft' THEN policy.reply_draft_model_profile_id
                                                     ELSE policy.similar_suggestions_model_profile_id END
      AND policy.cost_limit_enabled AND NOT model.zero_cost
      AND ((NOT policy.allow_unmetered_unknown AND $5::bigint IS NULL) OR
           ($5::bigint IS NOT NULL AND (
             (SELECT COALESCE(sum(cost_minor), 0) FROM ai_usage_records WHERE msp_id = ai_generation_jobs.msp_id
               AND recorded_at >= (date_trunc('month', now() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC')
               AND recorded_at < ((date_trunc('month', now() AT TIME ZONE 'UTC') + interval '1 month') AT TIME ZONE 'UTC'))
             + (SELECT COALESCE(sum(reserved_cost_minor), 0) FROM ai_generation_jobs other WHERE other.msp_id = ai_generation_jobs.msp_id AND other.state = 'running' AND other.id <> ai_generation_jobs.id)
             + $5 > policy.monthly_cost_limit_minor)))
  )
`, completion.JobID, completion.LeaseToken, completion.Record.Recommendation.ID, completion.Record.Recommendation.GeneratedAt, completion.Record.Usage.CostMinor)
	if err != nil || tag.RowsAffected() != 1 {
		_ = tx.Rollback(ctx)
		if err != nil {
			return err
		}
		return r.classifyCompletionMiss(ctx, completion.JobID, completion.LeaseToken)
	}
	if err := writeMutationFacts(ctx, tx, completion.Record.Audit, completion.Record.Event); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}

func (r *AIJobRepository) completeClassification(ctx context.Context, tx transaction, completion aiassist.JobCompletion) error {
	if strings.TrimSpace(completion.Record.Audit.ID) == "" || strings.TrimSpace(completion.Record.Event.EventID) == "" || strings.TrimSpace(completion.Record.Audit.CorrelationID) == "" {
		return aiassist.ErrAIDenied
	}
	var mspID, clientID, suggestionID, requestedBy string
	var automatic bool
	var threshold float64
	var policyVersion int64
	err := tx.QueryRow(ctx, `
SELECT job.msp_id::text, COALESCE(job.client_id::text, ''), suggestion.id::text, job.requested_by::text,
       policy.automatic_apply_enabled, policy.automatic_apply_threshold::float8, policy.version
FROM ai_generation_jobs job
JOIN tag_ai_suggestions suggestion ON suggestion.ai_generation_job_id = job.id
JOIN tag_ai_policies policy ON policy.msp_id = job.msp_id
WHERE job.id = $1::uuid AND job.feature = 'classification' AND job.state = 'running'
  AND job.lease_token = $2::uuid AND job.lease_until > now() AND job.cancellation_requested_at IS NULL
`, completion.JobID, completion.LeaseToken).Scan(&mspID, &clientID, &suggestionID, &requestedBy, &automatic, &threshold, &policyVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return aiassist.ErrJobLeaseLost
	}
	if err != nil {
		return err
	}
	for rank, candidate := range completion.Record.ClassificationCandidates {
		if _, err := tx.Exec(ctx, `
INSERT INTO tag_ai_suggestion_items (suggestion_id, msp_id, tag_id, confidence, rank, rationale)
SELECT $1::uuid, $2::uuid, tag.id, $4, $5, $6 FROM tags tag
WHERE tag.id = $3::uuid AND tag.msp_id = $2::uuid AND tag.lifecycle_state = 'active'
ON CONFLICT (suggestion_id, tag_id) DO NOTHING
`, suggestionID, mspID, candidate.TagID, candidate.Confidence, rank+1, candidate.Rationale); err != nil {
			return err
		}
	}
	evidence, _ := json.Marshal(map[string]any{"provider": completion.Record.Usage.Provider, "model": completion.Record.Usage.Model, "prompt_version": "classification-v1", "policy_version": policyVersion, "input_units": completion.Record.Usage.InputUnits, "output_units": completion.Record.Usage.OutputUnits})
	state := "not_queued"
	if automatic {
		state = "queued"
	}
	tag, err := tx.Exec(ctx, `
UPDATE ai_generation_jobs job
SET state = 'completed', completed_at = $3, updated_at = $3,
    lease_token = NULL, lease_until = NULL, reserved_cost_minor = 0,
    safe_error_code = NULL, version = version + 1
WHERE job.id = $1::uuid AND job.feature = 'classification'
  AND job.state = 'running' AND job.lease_token = $2::uuid
  AND job.lease_until > now() AND job.cancellation_requested_at IS NULL
  AND EXISTS (
    SELECT 1
    FROM ai_policies ai_policy
    JOIN ai_model_profiles model
      ON model.id = job.model_profile_id AND model.msp_id = job.msp_id
    JOIN ai_provider_connections connection
      ON connection.id = model.connection_id AND connection.msp_id = model.msp_id
    WHERE ai_policy.msp_id = job.msp_id AND ai_policy.enabled
      AND job.feature::text = ANY(ai_policy.allowed_features)
      AND job.feature::text = ANY(model.supported_features)
      AND model.enabled AND connection.enabled
      AND connection.disclosure_accepted_at IS NOT NULL
      AND connection.disclosure_accepted_by IS NOT NULL
      AND model.id = ai_policy.classification_model_profile_id
  )
  AND NOT EXISTS (
    SELECT 1
    FROM ai_policies ai_policy
    JOIN ai_model_profiles model
      ON model.id = job.model_profile_id AND model.msp_id = job.msp_id
    WHERE ai_policy.msp_id = job.msp_id
      AND model.id = ai_policy.classification_model_profile_id
      AND ai_policy.cost_limit_enabled AND NOT model.zero_cost
      AND ((NOT ai_policy.allow_unmetered_unknown AND $4::bigint IS NULL) OR
           ($4::bigint IS NOT NULL AND (
             (SELECT COALESCE(sum(usage.cost_minor), 0)
              FROM ai_usage_records usage
              WHERE usage.msp_id = job.msp_id
                AND usage.recorded_at >= (date_trunc('month', $3 AT TIME ZONE 'UTC') AT TIME ZONE 'UTC')
                AND usage.recorded_at < ((date_trunc('month', $3 AT TIME ZONE 'UTC') + interval '1 month') AT TIME ZONE 'UTC'))
             + (SELECT COALESCE(sum(other.reserved_cost_minor), 0)
                FROM ai_generation_jobs other
                WHERE other.msp_id = job.msp_id AND other.state = 'running' AND other.id <> job.id)
             + $4 > ai_policy.monthly_cost_limit_minor)))
  )
`, completion.JobID, completion.LeaseToken, completion.Record.Usage.RecordedAt, completion.Record.Usage.CostMinor)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errClassificationCompletionMiss
	}
	usage := completion.Record.Usage
	if _, err := tx.Exec(ctx, `
INSERT INTO ai_usage_records (
  id, recommendation_id, generation_job_id, msp_id, provider, model,
  input_units, output_units, cost_minor, recorded_at
) VALUES ($1::uuid, NULL, $2::uuid, $3::uuid, $4, $5, $6, $7, $8, $9)
`, usage.ID, completion.JobID, mspID, usage.Provider, usage.Model,
		usage.InputUnits, usage.OutputUnits, usage.CostMinor, usage.RecordedAt); err != nil {
		return err
	}
	var suggestionVersion int64
	err = tx.QueryRow(ctx, `
UPDATE tag_ai_suggestions SET status = 'completed', completed_at = now(), provider_evidence = $2::jsonb,
  application_state = CASE WHEN $3 AND EXISTS (SELECT 1 FROM tag_ai_suggestion_items item WHERE item.suggestion_id = tag_ai_suggestions.id AND item.confidence >= $4) THEN $5 ELSE 'not_queued' END,
  version = version + 1 WHERE id = $1::uuid
RETURNING version
`, suggestionID, string(evidence), automatic, threshold, state).Scan(&suggestionVersion)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO audit_ledger (id,occurred_at,msp_id,client_id,actor_type,actor_id,action,subject_type,subject_id,subject_version,source,reason,correlation_id,safe_diff)
VALUES ($1::uuid,now(),$2::uuid,NULLIF($3,'')::uuid,'technician',$4::uuid,'classification.suggestion.completed','tag_ai_suggestion',$5::uuid,$6,'worker','provider_result_accepted',$7::uuid,jsonb_build_object('candidate_count',$8::integer))
`, completion.Record.Audit.ID, mspID, clientID, requestedBy, suggestionID, suggestionVersion, completion.Record.Audit.CorrelationID, len(completion.Record.ClassificationCandidates)); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO event_outbox (event_id,event_type,schema_version,occurred_at,msp_id,client_id,actor_type,actor_id,subject_type,subject_id,subject_version,correlation_id,source,data)
VALUES ($1::uuid,'classification.suggestion.completed',1,now(),$2::uuid,NULLIF($3,'')::uuid,'technician',$4::uuid,'tag_ai_suggestion',$5::uuid,$6,$7::uuid,'worker',jsonb_build_object('candidate_count',$8::integer,'application_state',$9::text))
`, completion.Record.Event.EventID, mspID, clientID, requestedBy, suggestionID, suggestionVersion, completion.Record.Event.CorrelationID, len(completion.Record.ClassificationCandidates), state); err != nil {
		return err
	}
	return nil
}

func (r *AIJobRepository) Fail(ctx context.Context, failure aiassist.JobFailure) error {
	if strings.TrimSpace(failure.IDs.AuditID) == "" || strings.TrimSpace(failure.IDs.EventID) == "" || strings.TrimSpace(failure.IDs.CorrelationID) == "" {
		return aiassist.ErrAIDenied
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	state := "failed"
	if failure.SafeErrorCode == "cancelled" {
		state = "cancelled"
	}
	job, err := scanGenerationJob(tx.QueryRow(ctx, `
UPDATE ai_generation_jobs AS job
SET state = CASE WHEN cancellation_requested_at IS NOT NULL THEN 'cancelled' WHEN $6 AND attempt < max_attempts THEN 'queued' ELSE $3 END,
    next_attempt_at = CASE WHEN cancellation_requested_at IS NULL AND $6 AND attempt < max_attempts THEN $5::timestamptz + interval '30 seconds' ELSE next_attempt_at END,
    safe_error_code = CASE WHEN cancellation_requested_at IS NOT NULL THEN 'cancelled' ELSE NULLIF($4, '') END,
    cancelled_at = CASE WHEN cancellation_requested_at IS NOT NULL OR $3 = 'cancelled' THEN $5 ELSE cancelled_at END,
    completed_at = CASE WHEN cancellation_requested_at IS NULL AND NOT ($6 AND attempt < max_attempts) AND $3 = 'failed' THEN $5 ELSE completed_at END,
    lease_token = NULL, lease_until = NULL, reserved_cost_minor = 0,
    updated_at = $5, version = version + 1
WHERE id = $1 AND state = 'running' AND lease_token = $2::uuid
	  AND lease_until > now()
	RETURNING `+generationJobColumns, failure.JobID, failure.LeaseToken, state, failure.SafeErrorCode, failure.FailedAt, failure.Retryable))
	if err != nil {
		_ = tx.Rollback(ctx)
		if errors.Is(err, pgx.ErrNoRows) {
			return aiassist.ErrJobLeaseLost
		}
		return err
	}
	action := "ai.generation_job." + string(job.State)
	reason := job.SafeErrorCode
	if job.State == aiassist.JobQueued {
		action = "ai.generation_job.requeued"
	}
	if err := writeMutationFacts(ctx, tx,
		workerJobTransitionAudit(job, action, reason, failure.FailedAt, failure.IDs),
		workerJobTransitionEvent(job, action, reason, failure.FailedAt, failure.IDs),
	); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if job.Feature == aiassist.FeatureClassification && job.State != aiassist.JobQueued {
		if _, err := tx.Exec(ctx, `
UPDATE tag_ai_suggestions suggestion
SET status='failed', completed_at=$2, explanation=$3,
    application_state='not_queued', application_lease_token=NULL, application_lease_until=NULL,
    updated_at=$2, version=version+1
WHERE suggestion.ai_generation_job_id=$1::uuid AND suggestion.status='pending'
`, job.ID, failure.FailedAt, job.SafeErrorCode); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}

const generationJobColumns = `job.id::text, job.msp_id::text, COALESCE(job.client_id::text, ''), COALESCE(job.work_record_id::text, ''),
job.subject_type, job.subject_id::text,
job.requested_by::text, job.feature, job.model_profile_id::text, job.relevant_input_names,
job.state, job.attempt, job.max_attempts, COALESCE(job.lease_token::text, ''), job.lease_until,
job.cancellation_requested_at, COALESCE(job.cancellation_requested_by::text, ''), job.cancelled_at,
COALESCE(job.safe_error_code, ''), COALESCE(job.recommendation_id::text, ''), job.idempotency_key,
job.created_at, job.updated_at, job.completed_at, job.version`

const generationJobSelect = `SELECT ` + generationJobColumns + ` FROM ai_generation_jobs job `

func scanGenerationJob(scanner interface{ Scan(...any) error }) (aiassist.GenerationJob, error) {
	var job aiassist.GenerationJob
	err := scanner.Scan(&job.ID, &job.MSPID, &job.ClientID, &job.WorkRecordID, &job.Subject.Type, &job.Subject.ID, &job.RequestedBy,
		&job.Feature, &job.ModelProfileID, &job.RelevantInputs, &job.State, &job.Attempt,
		&job.MaxAttempts, &job.LeaseToken, &job.LeaseUntil, &job.CancellationRequestedAt,
		&job.CancellationRequestedBy, &job.CancelledAt, &job.SafeErrorCode, &job.RecommendationID,
		&job.IdempotencyKey, &job.CreatedAt, &job.UpdatedAt, &job.CompletedAt, &job.Version)
	return job, err
}

func scanGenerationJobInserted(scanner interface{ Scan(...any) error }) (aiassist.GenerationJob, bool, error) {
	var job aiassist.GenerationJob
	var inserted bool
	err := scanner.Scan(&job.ID, &job.MSPID, &job.ClientID, &job.WorkRecordID, &job.Subject.Type, &job.Subject.ID, &job.RequestedBy,
		&job.Feature, &job.ModelProfileID, &job.RelevantInputs, &job.State, &job.Attempt,
		&job.MaxAttempts, &job.LeaseToken, &job.LeaseUntil, &job.CancellationRequestedAt,
		&job.CancellationRequestedBy, &job.CancelledAt, &job.SafeErrorCode, &job.RecommendationID,
		&job.IdempotencyKey, &job.CreatedAt, &job.UpdatedAt, &job.CompletedAt, &job.Version, &inserted)
	return job, inserted, err
}

func jobTransitionAudit(job aiassist.GenerationJob, actorID, action, reason string, now time.Time, ids aiassist.JobTransitionIDs) mutation.AuditRecord {
	if actorID == "" {
		actorID = job.RequestedBy
	}
	return mutation.AuditRecord{ID: ids.AuditID, OccurredAt: now, MSPID: job.MSPID, ClientID: job.ClientID, ActorType: "technician", ActorID: actorID, Action: action, SubjectType: "ai_generation_job", SubjectID: job.ID, SubjectVersion: job.Version, Source: "api", Reason: reason, CorrelationID: ids.CorrelationID}
}

func jobTransitionEvent(job aiassist.GenerationJob, actorID, action, reason string, now time.Time, ids aiassist.JobTransitionIDs) mutation.EventRecord {
	if actorID == "" {
		actorID = job.RequestedBy
	}
	return mutation.EventRecord{EventID: ids.EventID, EventType: action, SchemaVersion: 1, OccurredAt: now, MSPID: job.MSPID, ClientID: job.ClientID, ActorType: "technician", ActorID: actorID, SubjectType: "ai_generation_job", SubjectID: job.ID, SubjectVersion: job.Version, Source: "api", CorrelationID: ids.CorrelationID, Data: map[string]any{"reason": reason}}
}

// Worker-owned transitions are attributed to the authenticated technician who
// submitted the job; the worker supplies the safe system reason and source.
// That preserves a valid UUID-backed audit actor without inventing a system
// principal that the schema cannot verify.
func workerJobTransitionAudit(job aiassist.GenerationJob, action, reason string, now time.Time, ids aiassist.JobTransitionIDs) mutation.AuditRecord {
	return mutation.AuditRecord{ID: ids.AuditID, OccurredAt: now, MSPID: job.MSPID, ClientID: job.ClientID, ActorType: "technician", ActorID: job.RequestedBy, Action: action, SubjectType: "ai_generation_job", SubjectID: job.ID, SubjectVersion: job.Version, Source: "worker", Reason: reason, CorrelationID: ids.CorrelationID}
}

func workerJobTransitionEvent(job aiassist.GenerationJob, action, reason string, now time.Time, ids aiassist.JobTransitionIDs) mutation.EventRecord {
	return mutation.EventRecord{EventID: ids.EventID, EventType: action, SchemaVersion: 1, OccurredAt: now, MSPID: job.MSPID, ClientID: job.ClientID, ActorType: "technician", ActorID: job.RequestedBy, SubjectType: "ai_generation_job", SubjectID: job.ID, SubjectVersion: job.Version, Source: "worker", CorrelationID: ids.CorrelationID, Data: map[string]any{"reason": reason}}
}
