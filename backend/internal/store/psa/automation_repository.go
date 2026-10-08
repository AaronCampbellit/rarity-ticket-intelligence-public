package psa

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/automation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type AutomationRepository struct {
	db database
}

var _ automation.DeadLetterRepository = (*AutomationRepository)(nil)
var _ automation.ManagementRepository = (*AutomationRepository)(nil)

func NewAutomationRepository(db database) *AutomationRepository {
	return &AutomationRepository{db: db}
}

func (r *AutomationRepository) ListDefinitions(
	ctx context.Context,
	target scope.Target,
) ([]automation.ManagedDefinition, error) {
	rows, err := r.db.Query(ctx, `
SELECT definition.id::text, definition.msp_id::text,
       definition.name, definition.version,
       version.id::text, version.version, version.state,
       version.trigger_event_type, version.client_scopes::text[],
       version.capabilities::text[], version.definition_json
FROM automation_definitions definition
JOIN LATERAL (
  SELECT candidate.*
  FROM automation_versions candidate
  WHERE candidate.automation_id = definition.id
    AND candidate.msp_id = definition.msp_id
    AND $2::uuid = ANY(candidate.client_scopes)
  ORDER BY candidate.version DESC
  LIMIT 1
) version ON true
WHERE definition.msp_id = $1
ORDER BY definition.name, definition.id
`, target.MSPID, target.ClientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]automation.ManagedDefinition, 0)
	for rows.Next() {
		var managed automation.ManagedDefinition
		var definitionJSON []byte
		if err := rows.Scan(
			&managed.ID, &managed.MSPID, &managed.Name,
			&managed.RecordVersion, &managed.VersionID,
			&managed.Version, &managed.State,
			&managed.Trigger.EventType, &managed.ClientScopes,
			&managed.Capabilities, &definitionJSON,
		); err != nil {
			return nil, err
		}
		var stored automation.Definition
		if err := json.Unmarshal(definitionJSON, &stored); err != nil {
			return nil, err
		}
		managed.Steps = stored.Steps
		result = append(result, managed)
	}
	return result, rows.Err()
}

func (r *AutomationRepository) FindDefinition(
	ctx context.Context,
	target scope.Target,
	id string,
	versionNumber int64,
) (automation.ManagedDefinition, error) {
	var (
		managed        automation.ManagedDefinition
		definitionJSON []byte
	)
	err := r.db.QueryRow(ctx, `
SELECT definition.id::text, definition.msp_id::text,
       definition.name, definition.version,
       version.id::text, version.version, version.state,
       version.trigger_event_type, version.client_scopes::text[],
       version.capabilities::text[], version.definition_json
FROM automation_definitions definition
JOIN automation_versions version
  ON version.automation_id = definition.id
 AND version.msp_id = definition.msp_id
WHERE definition.id = $1 AND definition.msp_id = $2
  AND $3::uuid = ANY(version.client_scopes)
  AND version.version = $4
`, id, target.MSPID, target.ClientID, versionNumber).Scan(
		&managed.ID, &managed.MSPID, &managed.Name,
		&managed.RecordVersion, &managed.VersionID,
		&managed.Version, &managed.State,
		&managed.Trigger.EventType, &managed.ClientScopes,
		&managed.Capabilities, &definitionJSON,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return automation.ManagedDefinition{}, scope.ErrNotFound
	}
	if err != nil {
		return automation.ManagedDefinition{}, err
	}
	var stored automation.Definition
	if err := json.Unmarshal(definitionJSON, &stored); err != nil {
		return automation.ManagedDefinition{}, err
	}
	managed.Steps = stored.Steps
	if automation.Validate(managed.Definition) != nil {
		return automation.ManagedDefinition{}, automation.ErrInvalidDefinition
	}
	return managed, nil
}

func (r *AutomationRepository) CreateDefinition(
	ctx context.Context,
	accepted automation.DefinitionMutation,
) error {
	definitionJSON, err := json.Marshal(accepted.Managed.Definition)
	if err != nil {
		return automation.ErrInvalidDefinition
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	managed := accepted.Managed
	if _, err := tx.Exec(ctx, `
INSERT INTO automation_definitions (
  id, msp_id, name, current_version, enabled, version,
  created_at, created_by, updated_at, updated_by
) VALUES ($1, $2, $3, 0, false, 1, $4, $5, $4, $5)
`, managed.ID, managed.MSPID, managed.Name,
		accepted.Audit.OccurredAt, accepted.Audit.ActorID); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO automation_versions (
  id, automation_id, msp_id, version, state,
  trigger_event_type, client_scopes, capabilities,
  definition_json, max_depth
) VALUES (
  $1, $2, $3, $4, 'draft', $5, $6::uuid[], $7::text[],
  $8::jsonb, 8
)
`, accepted.VersionID, managed.ID, managed.MSPID, managed.Version,
		managed.Trigger.EventType, managed.ClientScopes,
		managed.Capabilities, definitionJSON); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := writeMutationFacts(
		ctx, tx, accepted.Audit, accepted.Event,
	); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return commitAutomationRuntime(ctx, tx)
}

func (r *AutomationRepository) ReviseDefinition(
	ctx context.Context,
	accepted automation.DefinitionMutation,
) error {
	definitionJSON, err := json.Marshal(accepted.Managed.Definition)
	if err != nil {
		return automation.ErrInvalidDefinition
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	managed := accepted.Managed
	tag, err := tx.Exec(ctx, `
UPDATE automation_definitions
SET version = version + 1, updated_at = $4, updated_by = $5
WHERE id = $1 AND msp_id = $2 AND version = $3
`, managed.ID, managed.MSPID, accepted.ExpectedRecordVersion,
		accepted.Audit.OccurredAt, accepted.Audit.ActorID)
	if err != nil || tag.RowsAffected() != 1 {
		_ = tx.Rollback(ctx)
		if err != nil {
			return err
		}
		return object.ErrVersionConflict
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO automation_versions (
  id, automation_id, msp_id, version, state,
  trigger_event_type, client_scopes, capabilities,
  definition_json, max_depth
) VALUES (
  $1, $2, $3, $4, 'draft', $5, $6::uuid[], $7::text[],
  $8::jsonb, 8
)
`, accepted.VersionID, managed.ID, managed.MSPID, managed.Version,
		managed.Trigger.EventType, managed.ClientScopes,
		managed.Capabilities, definitionJSON); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := writeMutationFacts(
		ctx, tx, accepted.Audit, accepted.Event,
	); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return commitAutomationRuntime(ctx, tx)
}

func (r *AutomationRepository) PublishDefinition(
	ctx context.Context,
	accepted automation.DefinitionMutation,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	managed := accepted.Managed
	tagIDs, groupIDs := automation.ReferencedCatalogIDs(managed.Definition)
	var invalidReferences int
	if err := tx.QueryRow(ctx, `
SELECT count(*)
FROM (
  SELECT referenced.id
  FROM unnest($2::uuid[]) AS referenced(id)
  WHERE NOT EXISTS (
    SELECT 1 FROM tags catalog
    WHERE catalog.id = referenced.id AND catalog.msp_id = $1
      AND catalog.lifecycle_state = 'active'
  )
  UNION ALL
  SELECT referenced.id
  FROM unnest($3::uuid[]) AS referenced(id)
  WHERE NOT EXISTS (
    SELECT 1 FROM tag_groups catalog
    WHERE catalog.id = referenced.id AND catalog.msp_id = $1
      AND catalog.lifecycle_state = 'active'
  )
) invalid
`, managed.MSPID, tagIDs, groupIDs).Scan(&invalidReferences); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if invalidReferences != 0 {
		_ = tx.Rollback(ctx)
		return automation.ErrInactiveCatalogReference
	}
	tag, err := tx.Exec(ctx, `
UPDATE automation_versions
SET state = 'published', published_at = $5, published_by = $6
WHERE id = $1 AND automation_id = $2 AND msp_id = $3
  AND version = $4 AND state = 'draft'
`, accepted.VersionID, managed.ID, managed.MSPID, managed.Version,
		accepted.Audit.OccurredAt, accepted.Audit.ActorID)
	if err != nil || tag.RowsAffected() != 1 {
		_ = tx.Rollback(ctx)
		if err != nil {
			return err
		}
		return object.ErrVersionConflict
	}
	tag, err = tx.Exec(ctx, `
UPDATE automation_definitions
SET current_version = $4, enabled = true,
    version = version + 1, updated_at = $5, updated_by = $6
WHERE id = $1 AND msp_id = $2 AND version = $3
`, managed.ID, managed.MSPID, accepted.ExpectedRecordVersion,
		managed.Version, accepted.Audit.OccurredAt,
		accepted.Audit.ActorID)
	if err != nil || tag.RowsAffected() != 1 {
		_ = tx.Rollback(ctx)
		if err != nil {
			return err
		}
		return object.ErrVersionConflict
	}
	if err := writeMutationFacts(
		ctx, tx, accepted.Audit, accepted.Event,
	); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return commitAutomationRuntime(ctx, tx)
}

func (r *AutomationRepository) CreateConnection(
	ctx context.Context,
	accepted automation.ConnectionMutation,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	connection := accepted.Connection
	if _, err := tx.Exec(ctx, `
INSERT INTO connections (
  id, msp_id, name, kind, endpoint_url, secret_refs,
  client_scopes, capabilities, data_scopes,
  enabled, health_state, version,
  created_at, created_by, updated_at, updated_by
) VALUES (
  $1, $2, $3, 'external_http', $4,
  jsonb_build_object('signing_secret', $5::text),
  ARRAY[$6::uuid], ARRAY['automation.call_http']::text[],
  ARRAY['automation.input.safe']::text[],
  true, 'pending', 1, $7, $8, $7, $8
)
`, connection.ID, connection.MSPID, connection.Name,
		connection.Endpoint, connection.SigningSecretRef,
		connection.ClientID, accepted.Audit.OccurredAt,
		accepted.Audit.ActorID); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := writeMutationFacts(
		ctx, tx, accepted.Audit, accepted.Event,
	); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return commitAutomationRuntime(ctx, tx)
}

func (r *AutomationRepository) Get(
	ctx context.Context,
	target scope.Target,
	id string,
) (automation.DeadLetter, error) {
	var letter automation.DeadLetter
	err := r.db.QueryRow(ctx, `
SELECT dl.id::text, dl.run_id::text, av.automation_id::text, av.version,
       dl.trigger_event_id::text, dl.msp_id::text, dl.client_id::text,
       dl.created_at, dl.error_code, dl.safe_message, dl.state
FROM automation_dead_letters dl
JOIN automation_versions av
  ON av.id = dl.automation_version_id AND av.msp_id = dl.msp_id
WHERE dl.id = $1 AND dl.msp_id = $2 AND dl.client_id = $3
`, id, target.MSPID, target.ClientID).Scan(
		&letter.ID, &letter.RunID, &letter.AutomationID,
		&letter.AutomationVersion, &letter.EventID, &letter.MSPID,
		&letter.ClientID, &letter.CreatedAt, &letter.ErrorCode,
		&letter.SafeMessage, &letter.State,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return automation.DeadLetter{}, scope.ErrNotFound
	}
	return letter, err
}

func (r *AutomationRepository) List(
	ctx context.Context,
	target scope.Target,
) ([]automation.DeadLetter, error) {
	rows, err := r.db.Query(ctx, `
SELECT dl.id::text, dl.run_id::text, av.automation_id::text, av.version,
       dl.trigger_event_id::text, dl.msp_id::text, dl.client_id::text,
       dl.created_at, dl.error_code, dl.safe_message, dl.state
FROM automation_dead_letters dl
JOIN automation_versions av
  ON av.id = dl.automation_version_id AND av.msp_id = dl.msp_id
WHERE dl.msp_id = $1 AND dl.client_id = $2
ORDER BY (dl.state = 'open') DESC, dl.created_at DESC, dl.id
LIMIT 100
`, target.MSPID, target.ClientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]automation.DeadLetter, 0)
	for rows.Next() {
		var letter automation.DeadLetter
		if err := rows.Scan(
			&letter.ID, &letter.RunID, &letter.AutomationID,
			&letter.AutomationVersion, &letter.EventID, &letter.MSPID,
			&letter.ClientID, &letter.CreatedAt, &letter.ErrorCode,
			&letter.SafeMessage, &letter.State,
		); err != nil {
			return nil, err
		}
		result = append(result, letter)
	}
	return result, rows.Err()
}

func (r *AutomationRepository) Apply(
	ctx context.Context,
	accepted automation.DeadLetterMutation,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	if err := applyAutomationDeadLetter(ctx, tx, accepted); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}

func applyAutomationDeadLetter(
	ctx context.Context,
	tx transaction,
	accepted automation.DeadLetterMutation,
) error {
	letter := accepted.Letter
	tag, err := tx.Exec(ctx, `
UPDATE automation_dead_letters
SET state = $4, resolved_at = $5, resolved_by = $6, resolution_reason = $7
WHERE id = $1 AND msp_id = $2 AND client_id = $3 AND state = 'open'
`, letter.ID, letter.MSPID, letter.ClientID, letter.State,
		accepted.ActedAt, accepted.ActedBy, accepted.Reason)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return object.ErrVersionConflict
	}
	if err := scheduleAutomationDeadLetter(ctx, tx, accepted); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO automation_dead_letter_actions (
  id, dead_letter_id, action, reason, acted_at, acted_by, correlation_id
) VALUES ($1, $2, $3, $4, $5, $6, $7)
`, accepted.ActionID, letter.ID, accepted.Action, accepted.Reason,
		accepted.ActedAt, accepted.ActedBy, accepted.Audit.CorrelationID); err != nil {
		return err
	}
	return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
}

func scheduleAutomationDeadLetter(
	ctx context.Context,
	tx transaction,
	accepted automation.DeadLetterMutation,
) error {
	letter := accepted.Letter
	switch accepted.Action {
	case automation.DeadLetterRetry:
		tag, err := tx.Exec(ctx, `
UPDATE automation_runs run
SET state = 'retrying',
    max_attempts = run.attempt + 1,
    retry_at = $5,
    execution_lease_until = NULL,
    continuation_mode = 'retry_step',
    continuation_step_id = (
      SELECT step.step_id
      FROM automation_step_runs step
      WHERE step.run_id = run.id AND step.state = 'failed'
      ORDER BY step.attempt DESC, step.completed_at DESC, step.id DESC
      LIMIT 1
    )
FROM automation_dead_letters letter
WHERE letter.id = $1 AND letter.run_id = run.id
  AND letter.msp_id = $2 AND letter.client_id = $3
  AND letter.state = 'retrying'
  AND run.id = $4 AND run.state = 'failed'
  AND EXISTS (
    SELECT 1
    FROM automation_step_runs step
    WHERE step.run_id = run.id AND step.state = 'failed'
  )
`, letter.ID, letter.MSPID, letter.ClientID, letter.RunID,
			accepted.ActedAt)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return object.ErrVersionConflict
		}
	case automation.DeadLetterReplay:
		tag, err := tx.Exec(ctx, `
WITH target AS (
  SELECT run.trigger_event_id AS event_id,
         run.automation_version_id, run.msp_id, run.client_id
  FROM automation_dead_letters letter
  JOIN automation_runs run
    ON run.id = letter.run_id
   AND run.msp_id = letter.msp_id
   AND run.client_id = letter.client_id
  WHERE letter.id = $2 AND letter.run_id = $3
    AND letter.msp_id = $4 AND letter.client_id = $5
    AND letter.state = 'replayed'
),
generation AS (
  SELECT COALESCE(max(existing.replay_generation), -1) + 1 AS value
  FROM automation_execution_jobs existing
  JOIN target
    ON target.event_id = existing.event_id
   AND target.automation_version_id = existing.automation_version_id
)
INSERT INTO automation_execution_jobs (
  id, event_id, automation_version_id, msp_id, client_id,
  replay_generation, state, attempt_count,
  next_attempt_at, created_at
)
SELECT $1, target.event_id, target.automation_version_id,
       target.msp_id, target.client_id, generation.value,
       'pending', 0, $6, $6
FROM target CROSS JOIN generation
`, accepted.ActionID, letter.ID, letter.RunID, letter.MSPID,
			letter.ClientID, accepted.ActedAt)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return object.ErrVersionConflict
		}
	case automation.DeadLetterDismiss:
		return nil
	default:
		return automation.ErrInvalidDeadLetterAction
	}
	return nil
}
