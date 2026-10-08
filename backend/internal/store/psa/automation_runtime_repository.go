package psa

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/automation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
)

type AutomationRuntimeRepository struct {
	db    database
	newID func() string
}

var _ automation.RunStore = (*AutomationRuntimeRepository)(nil)
var _ automation.ContinuationQueue = (*AutomationRuntimeRepository)(nil)

func NewAutomationRuntimeRepository(
	db database,
	newID func() string,
) *AutomationRuntimeRepository {
	return &AutomationRuntimeRepository{db: db, newID: newID}
}

func (r *AutomationRuntimeRepository) Begin(
	ctx context.Context,
	run automation.Run,
) (*automation.Run, error) {
	inputSnapshot, err := json.Marshal(run.InputSnapshot)
	if err != nil {
		return nil, automation.ErrExecutionDenied
	}
	var (
		created          bool
		runID            string
		state            automation.RunState
		changedObjectIDs []byte
	)
	err = r.db.QueryRow(ctx, `
WITH eligible AS (
  SELECT version.id
  FROM automation_definitions definition
  JOIN automation_versions version
    ON version.automation_id = definition.id
   AND version.msp_id = definition.msp_id
  WHERE definition.id = $2 AND definition.msp_id = $5
    AND definition.enabled
    AND version.version = $3
    AND version.state = 'published'
    AND $6::uuid = ANY(version.client_scopes)
),
inserted AS (
  INSERT INTO automation_runs (
    id, automation_version_id, msp_id, client_id,
    trigger_event_id, idempotency_key, causation_id,
    depth, attempt, max_attempts, state, input_snapshot,
    started_at
  )
  SELECT $1, eligible.id, $5, $6, $4, $7,
         NULLIF($8, '')::uuid, $9, $10, $11, 'running',
         $12::jsonb, $13
  FROM eligible
  ON CONFLICT (msp_id, idempotency_key) DO NOTHING
  RETURNING id, state, changed_object_ids
),
selected AS (
  SELECT true AS created, id, state, changed_object_ids
  FROM inserted
  UNION ALL
  SELECT false, run.id, run.state, run.changed_object_ids
  FROM automation_runs run
  WHERE run.msp_id = $5 AND run.idempotency_key = $7
    AND NOT EXISTS (SELECT 1 FROM inserted)
)
SELECT created, id::text, state, changed_object_ids
FROM selected
`, run.ID, run.AutomationID, run.AutomationVersion, run.EventID,
		run.MSPID, run.ClientID, run.IdempotencyKey, run.CausationID,
		run.Depth, run.Attempt, run.MaxAttempts, inputSnapshot,
		run.StartedAt).Scan(
		&created, &runID, &state, &changedObjectIDs,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, automation.ErrExecutionDenied
	}
	if err != nil {
		return nil, err
	}
	if created {
		return nil, nil
	}
	var changed []string
	if err := json.Unmarshal(changedObjectIDs, &changed); err != nil {
		return nil, err
	}
	return &automation.Run{
		ID: runID, State: state, ChangedObjectIDs: changed,
	}, nil
}

func (r *AutomationRuntimeRepository) RecordStep(
	ctx context.Context,
	step automation.StepRecord,
) error {
	changedObjectIDs, err := json.Marshal(step.ChangedObjectIDs)
	if err != nil {
		return automation.ErrExecutionDenied
	}
	inputSnapshot, err := json.Marshal(step.InputSnapshot)
	if err != nil {
		return automation.ErrExecutionDenied
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `
INSERT INTO automation_step_runs (
  id, run_id, msp_id, client_id, step_id, attempt, action_kind,
  state, started_at, completed_at, error_code, changed_object_ids
)
SELECT $1, run.id, run.msp_id, run.client_id, $3, $4,
       NULLIF($5, ''), $6, $7, $8, NULLIF($9, ''), $10::jsonb
FROM automation_runs run
WHERE run.id = $2 AND run.state = 'running' AND run.attempt = $4
`, r.newID(), step.RunID, step.StepID, step.Attempt,
		step.ActionKind, step.State,
		step.StartedAt, step.CompletedAt, step.ErrorCode, changedObjectIDs)
	if err != nil || tag.RowsAffected() != 1 {
		_ = tx.Rollback(ctx)
		if err != nil {
			return err
		}
		return object.ErrVersionConflict
	}
	tag, err = tx.Exec(ctx, `
UPDATE automation_runs
SET input_snapshot = $3::jsonb
WHERE id = $1 AND state = 'running' AND attempt = $2
`, step.RunID, step.Attempt, inputSnapshot)
	if err != nil || tag.RowsAffected() != 1 {
		_ = tx.Rollback(ctx)
		if err != nil {
			return err
		}
		return object.ErrVersionConflict
	}
	return commitAutomationRuntime(ctx, tx)
}

func (r *AutomationRuntimeRepository) Complete(
	ctx context.Context,
	completion automation.RunCompletion,
) error {
	changedObjectIDs, err := json.Marshal(completion.ChangedObjectIDs)
	if err != nil {
		return automation.ErrExecutionDenied
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `
UPDATE automation_runs
SET state = 'succeeded', completed_at = $2,
    changed_object_ids = $3::jsonb, error_code = NULL,
    retry_at = NULL, execution_lease_until = NULL,
    continuation_mode = NULL, continuation_step_id = NULL
WHERE id = $1 AND state = 'running'
`, completion.RunID, completion.CompletedAt, changedObjectIDs)
	if err != nil || tag.RowsAffected() != 1 {
		_ = tx.Rollback(ctx)
		if err != nil {
			return err
		}
		return object.ErrVersionConflict
	}
	return commitAutomationRuntime(ctx, tx)
}

func (r *AutomationRuntimeRepository) Fail(
	ctx context.Context,
	failure automation.RunFailure,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `
UPDATE automation_runs
SET state = 'retrying', completed_at = $2,
    error_code = $3, retry_at = $4,
    execution_lease_until = NULL,
    continuation_mode = 'retry_step',
    continuation_step_id = $5
WHERE id = $1 AND state = 'running'
  AND attempt = $6 AND max_attempts = $7
  AND $4::timestamptz IS NOT NULL
`, failure.RunID, failure.FailedAt, failure.ErrorCode,
		nullableTime(failure.RetryAt), failure.StepID,
		failure.Attempt, failure.MaxAttempts)
	if err != nil || tag.RowsAffected() != 1 {
		_ = tx.Rollback(ctx)
		if err != nil {
			return err
		}
		return object.ErrVersionConflict
	}
	return commitAutomationRuntime(ctx, tx)
}

func (r *AutomationRuntimeRepository) DeadLetter(
	ctx context.Context,
	failure automation.RunFailure,
	letter automation.DeadLetter,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `
UPDATE automation_runs
SET state = 'failed', completed_at = $2,
    error_code = $3, retry_at = NULL,
    execution_lease_until = NULL,
    continuation_mode = NULL, continuation_step_id = NULL
WHERE id = $1 AND msp_id = $4 AND client_id = $5
  AND state = 'running' AND attempt = $6 AND max_attempts = $7
`, failure.RunID, failure.FailedAt, failure.ErrorCode,
		letter.MSPID, letter.ClientID, failure.Attempt, failure.MaxAttempts)
	if err != nil || tag.RowsAffected() != 1 {
		_ = tx.Rollback(ctx)
		if err != nil {
			return err
		}
		return object.ErrVersionConflict
	}
	tag, err = tx.Exec(ctx, `
INSERT INTO automation_dead_letters (
  id, run_id, automation_version_id, msp_id, client_id,
  trigger_event_id, created_at, error_code, safe_message, state
)
SELECT $1, run.id, run.automation_version_id, run.msp_id,
       run.client_id, run.trigger_event_id, $3, $4, $5, 'open'
FROM automation_runs run
WHERE run.id = $2 AND run.msp_id = $6 AND run.client_id = $7
`, letter.ID, letter.RunID, letter.CreatedAt, letter.ErrorCode,
		letter.SafeMessage, letter.MSPID, letter.ClientID)
	if err != nil || tag.RowsAffected() != 1 {
		_ = tx.Rollback(ctx)
		if err != nil {
			return err
		}
		return object.ErrVersionConflict
	}
	return commitAutomationRuntime(ctx, tx)
}

func (r *AutomationRuntimeRepository) Suspend(
	ctx context.Context,
	suspension automation.Suspension,
) error {
	inputSnapshot, err := json.Marshal(suspension.InputSnapshot)
	if err != nil {
		return automation.ErrExecutionDenied
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `
INSERT INTO automation_step_runs (
  id, run_id, msp_id, client_id, step_id, attempt, state,
  started_at, completed_at
)
SELECT $1, run.id, run.msp_id, run.client_id, $3, $4, 'waiting',
       $5, $6
FROM automation_runs run
WHERE run.id = $2 AND run.state = 'running' AND run.attempt = $4
`, r.newID(), suspension.RunID, suspension.StepID, suspension.Attempt,
		suspension.StartedAt, suspension.CompletedAt)
	if err != nil || tag.RowsAffected() != 1 {
		_ = tx.Rollback(ctx)
		if err != nil {
			return err
		}
		return object.ErrVersionConflict
	}
	tag, err = tx.Exec(ctx, `
INSERT INTO automation_suspensions (
  id, run_id, msp_id, client_id, step_id, attempt, resume_at,
  input_snapshot
)
SELECT $1, run.id, run.msp_id, run.client_id, $3, $4, $5, $6::jsonb
FROM automation_runs run
WHERE run.id = $2 AND run.state = 'running' AND run.attempt = $4
`, r.newID(), suspension.RunID, suspension.StepID, suspension.Attempt,
		suspension.ResumeAt, inputSnapshot)
	if err != nil || tag.RowsAffected() != 1 {
		_ = tx.Rollback(ctx)
		if err != nil {
			return err
		}
		return object.ErrVersionConflict
	}
	tag, err = tx.Exec(ctx, `
UPDATE automation_runs
SET state = 'waiting', completed_at = $2,
    input_snapshot = $4::jsonb,
    execution_lease_until = NULL,
    continuation_mode = 'after_wait',
    continuation_step_id = $5
WHERE id = $1 AND state = 'running' AND attempt = $3
`, suspension.RunID, suspension.CompletedAt, suspension.Attempt,
		inputSnapshot, suspension.StepID)
	if err != nil || tag.RowsAffected() != 1 {
		_ = tx.Rollback(ctx)
		if err != nil {
			return err
		}
		return object.ErrVersionConflict
	}
	return commitAutomationRuntime(ctx, tx)
}

func (r *AutomationRuntimeRepository) ClaimContinuations(
	ctx context.Context,
	limit int,
	now time.Time,
	lease time.Duration,
) ([]automation.ContinuationJob, error) {
	rows, err := r.db.Query(ctx, `
WITH candidates AS (
  SELECT run.id, run.state AS previous_state
  FROM automation_runs run
  LEFT JOIN automation_suspensions suspension
    ON suspension.run_id = run.id
   AND suspension.step_id = run.continuation_step_id
   AND suspension.attempt = run.attempt
  WHERE run.state IN ('waiting', 'retrying', 'running')
    AND run.continuation_mode IS NOT NULL
    AND run.continuation_step_id IS NOT NULL
    AND (
      run.execution_lease_until IS NULL
      OR run.execution_lease_until <= $1
    )
    AND (
      (
        run.state = 'waiting'
        AND run.continuation_mode = 'after_wait'
        AND suspension.resumed_at IS NULL
        AND suspension.resume_at <= $1
      )
      OR (
        run.state = 'retrying'
        AND run.continuation_mode = 'retry_step'
        AND run.retry_at <= $1
      )
      OR (
        run.state = 'running'
        AND run.execution_lease_until <= $1
      )
    )
  ORDER BY COALESCE(
             run.retry_at, suspension.resume_at,
             run.execution_lease_until
           ),
           run.id
  LIMIT $2
  FOR UPDATE OF run SKIP LOCKED
),
resumed AS (
  UPDATE automation_suspensions suspension
  SET resumed_at = $1
  FROM automation_runs run, candidates
  WHERE run.id = candidates.id
    AND suspension.run_id = run.id
    AND suspension.step_id = run.continuation_step_id
    AND suspension.attempt = run.attempt
    AND run.continuation_mode = 'after_wait'
    AND suspension.resumed_at IS NULL
  RETURNING suspension.id
),
leased AS (
  UPDATE automation_runs run
  SET state = 'running',
      attempt = CASE
        WHEN candidates.previous_state = 'retrying'
          THEN run.attempt + 1
        ELSE run.attempt
      END,
      completed_at = NULL,
      retry_at = NULL,
      execution_lease_until =
        $1 + ($3 * interval '1 microsecond')
  FROM candidates
  WHERE run.id = candidates.id
  RETURNING run.*
),
resumed_count AS (
  SELECT count(*) FROM resumed
)
SELECT run.id::text, definition.id::text, version.msp_id::text,
       version.version, version.client_scopes::text[],
       version.capabilities::text[], version.trigger_event_type,
       version.definition_json,
       event.event_id::text, run.client_id::text,
       COALESCE(event.causation_id::text, ''),
       COALESCE((
         SELECT parent.depth
         FROM automation_runs parent
         WHERE parent.id = event.causation_id
       ), 0),
       run.input_snapshot, run.changed_object_ids,
       run.attempt, run.max_attempts,
       run.continuation_mode, run.continuation_step_id
FROM leased run
JOIN automation_versions version
  ON version.id = run.automation_version_id
 AND version.msp_id = run.msp_id
JOIN automation_definitions definition
  ON definition.id = version.automation_id
 AND definition.msp_id = version.msp_id
JOIN event_outbox event ON event.event_id = run.trigger_event_id
CROSS JOIN resumed_count
ORDER BY run.started_at, run.id
`, now, limit, leaseMicroseconds(lease))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]automation.ContinuationJob, 0)
	for rows.Next() {
		var (
			runID, automationID, mspID, triggerType string
			eventID, clientID, causationID          string
			stepID                                  string
			version                                 int64
			clientScopes, capabilities              []string
			definitionJSON, inputJSON, changedJSON  []byte
			depth, attempt, maxAttempts             int
			mode                                    automation.ContinuationMode
		)
		if err := rows.Scan(
			&runID, &automationID, &mspID, &version,
			&clientScopes, &capabilities, &triggerType,
			&definitionJSON, &eventID, &clientID, &causationID,
			&depth, &inputJSON, &changedJSON, &attempt, &maxAttempts,
			&mode, &stepID,
		); err != nil {
			return nil, err
		}
		var definition automation.Definition
		if err := json.Unmarshal(definitionJSON, &definition); err != nil {
			return nil, automation.ErrInvalidDefinition
		}
		definition.ID = automationID
		definition.MSPID = mspID
		definition.Version = version
		definition.State = automation.Published
		definition.ClientScopes = clientScopes
		definition.Capabilities = capabilities
		definition.Trigger = automation.Trigger{EventType: triggerType}
		if err := automation.Validate(definition); err != nil {
			return nil, err
		}
		inputSnapshot, err := automationEventSnapshot(inputJSON)
		if err != nil {
			return nil, err
		}
		var changedObjectIDs []string
		if err := json.Unmarshal(changedJSON, &changedObjectIDs); err != nil {
			return nil, err
		}
		result = append(result, automation.ContinuationJob{
			Command: automation.ContinuationCommand{
				Definition: definition,
				Event: automation.TriggerEvent{
					ID: eventID, Type: triggerType, MSPID: mspID,
					ClientID: clientID, CausationID: causationID,
					Depth: depth, InputSnapshot: inputSnapshot,
				},
				Run: automation.Run{
					ID: runID, AutomationID: automationID,
					AutomationVersion: version, EventID: eventID,
					MSPID: mspID, ClientID: clientID,
					CausationID: causationID, Depth: depth,
					Attempt: attempt, MaxAttempts: maxAttempts,
					State:            automation.RunRunning,
					InputSnapshot:    inputSnapshot,
					ChangedObjectIDs: changedObjectIDs,
				},
				Mode: mode, StepID: stepID,
			},
		})
	}
	return result, rows.Err()
}

func (r *AutomationRuntimeRepository) ReleaseContinuation(
	ctx context.Context,
	release automation.ContinuationRelease,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
UPDATE automation_suspensions suspension
SET resumed_at = NULL
FROM automation_runs run
WHERE run.id = $1 AND run.state = 'running'
  AND run.continuation_mode = 'after_wait'
  AND suspension.run_id = run.id
  AND suspension.step_id = run.continuation_step_id
  AND suspension.attempt = run.attempt
`, release.RunID); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	tag, err := tx.Exec(ctx, `
UPDATE automation_runs
SET state = CASE continuation_mode
      WHEN 'after_wait' THEN 'waiting'
      ELSE 'retrying'
    END,
    completed_at = $2,
    execution_lease_until = $3,
    retry_at = CASE
      WHEN continuation_mode = 'retry_step' THEN $3
      ELSE retry_at
    END,
    error_code = $4
WHERE id = $1 AND state = 'running'
  AND continuation_mode IS NOT NULL
  AND execution_lease_until IS NOT NULL
`, release.RunID, release.ReleasedAt, release.RetryAt,
		release.ErrorCode)
	if err != nil || tag.RowsAffected() != 1 {
		_ = tx.Rollback(ctx)
		if err != nil {
			return err
		}
		return object.ErrVersionConflict
	}
	return commitAutomationRuntime(ctx, tx)
}

func commitAutomationRuntime(
	ctx context.Context,
	tx transaction,
) error {
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}
