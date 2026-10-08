package psa

import (
	"context"
	"encoding/json"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
)

// insertInitialTagAssignments persists direct tags and their append-only
// history in the caller's object transaction. Inherited Project evidence is
// deliberately derived at read time and never copied to a Task.
func insertInitialTagAssignments(
	ctx context.Context,
	tx transaction,
	target tagging.TargetRef,
	version int64,
	initial tagging.InitialAssignmentSet,
) error {
	for _, assignment := range initial.Direct {
		actorType, actorID := initial.ActorType, initial.ActorID
		occurredAt := initial.OccurredAt
		correlationID := initial.CorrelationID
		evidence := initial.Evidence
		if assignment.Source == tagging.SourceSystemFallback {
			actorType, actorID = "system", ""
		}
		if actorType == "" {
			actorType = "system"
		}
		if occurredAt.IsZero() {
			occurredAt = time.Now().UTC()
		}
		if evidence == nil {
			evidence = map[string]any{}
		}
		evidence = cloneInitialEvidence(evidence)
		evidence["initial_assignment"] = true
		evidenceJSON, err := json.Marshal(evidence)
		if err != nil {
			return err
		}
		seed := target.MSPID + ":initial-tag:" + target.ObjectType.String() + ":" + target.ObjectID + ":" + assignment.Tag.ID
		tag, err := tx.Exec(ctx, `
INSERT INTO object_tag_assignments (
  id, msp_id, client_id, object_type, object_id, object_version,
  tag_id, assignment_source, assigned_at, assigned_by, evidence, version
)
SELECT md5($1)::uuid, $2::uuid, NULLIF($3, '')::uuid, $4, $5::uuid, $6,
       tag.id, $7, $9, NULLIF($10, '')::uuid, $11::jsonb, 1
FROM tags tag
WHERE tag.id = $8::uuid AND tag.msp_id = $2::uuid
  AND tag.lifecycle_state = 'active'
`, seed, target.MSPID, target.ClientID, target.ObjectType.String(), target.ObjectID,
			version, assignment.Source.String(), assignment.Tag.ID, occurredAt, actorID, evidenceJSON)
		if err != nil {
			return classifyTaggingWriteError(err)
		}
		if tag.RowsAffected() != 1 {
			return scope.ErrNotFound
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO tag_assignment_events (
  id, msp_id, client_id, assignment_id, object_type, object_id,
  target_version, tag_id, operation, assignment_source,
  actor_type, actor_id, occurred_at, evidence, idempotency_key, correlation_id
) VALUES (
  md5($1 || ':event')::uuid, $2::uuid, NULLIF($3, '')::uuid,
  md5($1)::uuid, $4, $5::uuid, $6, $7::uuid, 'added', $8,
  $9, NULLIF($10, '')::uuid, $11, $12::jsonb,
  'initial-tag:' || $1, NULLIF($13, '')::uuid
)
`, seed, target.MSPID, target.ClientID, target.ObjectType.String(), target.ObjectID,
			version, assignment.Tag.ID, assignment.Source.String(), actorType, actorID,
			occurredAt, evidenceJSON, correlationID); err != nil {
			return err
		}
	}
	return nil
}

func cloneInitialEvidence(input map[string]any) map[string]any {
	cloned := make(map[string]any, len(input)+1)
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}
