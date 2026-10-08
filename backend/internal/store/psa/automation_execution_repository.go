package psa

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/automation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

const automationMaxAttempts = 3

type AutomationExecutionRepository struct {
	db database
}

var _ automation.ExecutionQueue = (*AutomationExecutionRepository)(nil)
var _ automation.ExternalConnectionRepository = (*AutomationExecutionRepository)(nil)

func NewAutomationExecutionRepository(
	db database,
) *AutomationExecutionRepository {
	return &AutomationExecutionRepository{db: db}
}

func (r *AutomationExecutionRepository) Load(
	ctx context.Context,
	ref string,
	mspID string,
	clientID string,
) (automation.ExternalConnection, error) {
	var connection automation.ExternalConnection
	err := r.db.QueryRow(ctx, `
SELECT id::text, msp_id::text, $3::text, endpoint_url,
       secret_refs->>'signing_secret'
FROM connections
WHERE id = $1 AND msp_id = $2
  AND kind = 'external_http' AND enabled
  AND $3::uuid = ANY(client_scopes)
  AND 'automation.call_http' = ANY(capabilities)
  AND 'automation.input.safe' = ANY(data_scopes)
  AND endpoint_url IS NOT NULL
  AND length(btrim(COALESCE(secret_refs->>'signing_secret', ''))) > 0
`, ref, mspID, clientID).Scan(
		&connection.ID, &connection.MSPID, &connection.ClientID,
		&connection.Endpoint, &connection.SigningSecretRef,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return automation.ExternalConnection{}, scope.ErrNotFound
	}
	return connection, err
}

func (r *AutomationExecutionRepository) Plan(
	ctx context.Context,
	limit int,
	now time.Time,
) (int, error) {
	var planned int
	err := r.db.QueryRow(ctx, `
WITH candidate_events AS MATERIALIZED (
  SELECT event.event_id, event.event_type, event.msp_id,
         event.client_id, event.occurred_at
  FROM event_outbox event
  WHERE event.client_id IS NOT NULL
    AND NOT EXISTS (
      SELECT 1
      FROM automation_event_plans plan
      WHERE plan.event_id = event.event_id
    )
  ORDER BY event.occurred_at, event.event_id
  LIMIT $1
),
planned_jobs AS (
  INSERT INTO automation_execution_jobs (
    id, event_id, automation_version_id, msp_id, client_id,
    replay_generation, state, attempt_count, next_attempt_at, created_at
  )
  SELECT md5(
           event.event_id::text || ':' || version.id::text
         )::uuid,
         event.event_id, version.id, event.msp_id, event.client_id,
         0, 'pending', 0, $2, $2
  FROM candidate_events event
  JOIN automation_definitions definition
    ON definition.msp_id = event.msp_id AND definition.enabled
  JOIN automation_versions version
    ON version.automation_id = definition.id
   AND version.msp_id = definition.msp_id
   AND version.version = definition.current_version
   AND version.state = 'published'
  WHERE event.event_type = version.trigger_event_type
    AND event.client_id = ANY(version.client_scopes)
  ON CONFLICT (event_id, automation_version_id, replay_generation)
  DO NOTHING
  RETURNING id
),
planned_events AS (
  INSERT INTO automation_event_plans (event_id, planned_at)
  SELECT event_id, $2
  FROM candidate_events
  ON CONFLICT (event_id) DO NOTHING
  RETURNING event_id
)
SELECT count(*)::integer
FROM planned_events
`, limit, now).Scan(&planned)
	return planned, err
}

func (r *AutomationExecutionRepository) Claim(
	ctx context.Context,
	limit int,
	now time.Time,
	lease time.Duration,
) ([]automation.ExecutionJob, error) {
	rows, err := r.db.Query(ctx, `
WITH candidates AS (
  SELECT job.id
  FROM automation_execution_jobs job
  WHERE (
      job.state = 'pending' AND job.next_attempt_at <= $1
    )
    OR (
      job.state = 'processing' AND job.lease_until <= $1
    )
  ORDER BY job.next_attempt_at, job.created_at, job.id
  LIMIT $2
  FOR UPDATE OF job SKIP LOCKED
),
leased AS (
  UPDATE automation_execution_jobs job
  SET state = 'processing',
      attempt_count = attempt_count + 1,
      lease_until = $1 + ($3 * interval '1 microsecond'),
      last_error_code = NULL
  FROM candidates
  WHERE job.id = candidates.id
  RETURNING job.*
)
SELECT job.id::text, definition.id::text, version.msp_id::text,
       version.version, version.client_scopes::text[],
       version.capabilities::text[], version.trigger_event_type,
       version.definition_json,
       event.event_id::text, event.client_id::text,
       COALESCE(event.causation_id::text, ''),
       COALESCE((
         SELECT parent.depth
         FROM automation_runs parent
         WHERE parent.id = event.causation_id
       ), 0),
       event.data,
       event.subject_type, event.subject_id::text,
       event.subject_version, job.replay_generation
FROM leased job
JOIN automation_versions version
  ON version.id = job.automation_version_id
 AND version.msp_id = job.msp_id
JOIN automation_definitions definition
  ON definition.id = version.automation_id
 AND definition.msp_id = version.msp_id
JOIN event_outbox event ON event.event_id = job.event_id
ORDER BY job.created_at, job.id
`, now, limit, leaseMicroseconds(lease))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]automation.ExecutionJob, 0)
	for rows.Next() {
		var (
			jobID, automationID, mspID, triggerType string
			eventID, clientID, causationID          string
			subjectType, subjectID                  string
			version, subjectVersion                 int64
			clientScopes, capabilities              []string
			definitionJSON, eventData               []byte
			depth, replayGeneration                 int
		)
		if err := rows.Scan(
			&jobID, &automationID, &mspID, &version,
			&clientScopes, &capabilities, &triggerType,
			&definitionJSON, &eventID, &clientID, &causationID,
			&depth, &eventData, &subjectType, &subjectID,
			&subjectVersion, &replayGeneration,
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
		snapshot, err := automationEventSnapshot(eventData)
		if err != nil {
			return nil, err
		}
		if triggerType == "tag.added" || triggerType == "tag.removed" {
			snapshot = authoritativeTagSnapshot(snapshot)
		}
		snapshot["_subject_type"] = subjectType
		snapshot["_subject_id"] = subjectID
		snapshot["_subject_version"] = strconv.FormatInt(subjectVersion, 10)
		idempotencyKey := fmt.Sprintf(
			"%s|%s|%d", eventID, automationID, version,
		)
		if replayGeneration > 0 {
			idempotencyKey += "|replay:" + strconv.Itoa(replayGeneration)
		}
		result = append(result, automation.ExecutionJob{
			ID: jobID,
			Command: automation.RunCommand{
				Definition: definition,
				Event: automation.TriggerEvent{
					ID: eventID, Type: triggerType, MSPID: mspID,
					ClientID: clientID, CausationID: causationID,
					Depth: depth, InputSnapshot: snapshot,
				},
				IdempotencyKey: idempotencyKey,
				Attempt:        1, MaxAttempts: automationMaxAttempts,
			},
		})
	}
	return result, rows.Err()
}

func (r *AutomationExecutionRepository) Complete(
	ctx context.Context,
	completion automation.ExecutionCompletion,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `
UPDATE automation_execution_jobs
SET state = 'processed', run_id = NULLIF($2, '')::uuid,
    processed_at = $3, lease_until = NULL, last_error_code = NULL
WHERE id = $1
  AND state = 'processing' AND lease_until IS NOT NULL
`, completion.JobID, completion.RunID, completion.CompletedAt)
	if err != nil || tag.RowsAffected() != 1 {
		_ = tx.Rollback(ctx)
		if err != nil {
			return err
		}
		return object.ErrVersionConflict
	}
	return commitAutomationRuntime(ctx, tx)
}

func (r *AutomationExecutionRepository) Fail(
	ctx context.Context,
	failure automation.ExecutionFailure,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	query := `
UPDATE automation_execution_jobs
SET state = 'failed', failed_at = $2, lease_until = NULL,
    last_error_code = $3
WHERE id = $1
  AND state = 'processing' AND lease_until IS NOT NULL
`
	args := []any{failure.JobID, failure.FailedAt, failure.ErrorCode}
	if failure.Retry {
		query = `
UPDATE automation_execution_jobs
SET state = 'pending', next_attempt_at = $4,
    lease_until = NULL, last_error_code = $3
WHERE id = $1
  AND state = 'processing' AND lease_until IS NOT NULL
`
		args = append(args, failure.NextAttemptAt)
	}
	tag, err := tx.Exec(ctx, query, args...)
	if err != nil || tag.RowsAffected() != 1 {
		_ = tx.Rollback(ctx)
		if err != nil {
			return err
		}
		return object.ErrVersionConflict
	}
	return commitAutomationRuntime(ctx, tx)
}

func automationEventSnapshot(payload []byte) (map[string]string, error) {
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.UseNumber()
	var values map[string]any
	if err := decoder.Decode(&values); err != nil || values == nil {
		return nil, automation.ErrExecutionDenied
	}
	result := make(map[string]string, len(values)+3)
	for key, value := range values {
		switch typed := value.(type) {
		case string:
			result[key] = typed
		case bool:
			result[key] = strconv.FormatBool(typed)
		case json.Number:
			result[key] = typed.String()
		case []any:
			stringsOnly := make([]string, 0, len(typed))
			for _, item := range typed {
				value, ok := item.(string)
				if !ok {
					stringsOnly = nil
					break
				}
				stringsOnly = append(stringsOnly, value)
			}
			if stringsOnly != nil {
				encoded, _ := json.Marshal(stringsOnly)
				result[key] = string(encoded)
			}
		case nil:
			result[key] = ""
		}
	}
	return result, nil
}

func authoritativeTagSnapshot(snapshot map[string]string) map[string]string {
	result := make(map[string]string, 5)
	for _, key := range []string{"tag_id", "direct_tag_ids", "effective_tag_ids", "group_ids", "assignment_source"} {
		if value, ok := snapshot[key]; ok {
			result[key] = value
		}
	}
	return result
}
