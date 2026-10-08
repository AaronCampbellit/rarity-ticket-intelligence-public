package psa

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
)

type TaggingRepository struct {
	db database
}

var _ tagging.CatalogRepository = (*TaggingRepository)(nil)
var _ tagging.AssociationRepository = (*TaggingRepository)(nil)
var _ tagging.ClassificationRepository = (*TaggingRepository)(nil)
var _ tagging.ClassificationSuggestionStore = (*TaggingRepository)(nil)
var _ tagging.ClassificationApplicationQueue = (*TaggingRepository)(nil)

func NewTaggingRepository(db database) *TaggingRepository {
	return &TaggingRepository{db: db}
}

func (r *TaggingRepository) GetClassificationPolicy(ctx context.Context, mspID string) (tagging.ClassificationPolicy, error) {
	var policy tagging.ClassificationPolicy
	err := r.db.QueryRow(ctx, `
SELECT policy.msp_id::text, policy.automatic_apply_enabled,
       policy.automatic_apply_threshold::float8,
       COALESCE(policy.model_profile_id::text, ''), policy.version
FROM tag_ai_policies policy
WHERE policy.msp_id = $1::uuid
`, mspID).Scan(&policy.MSPID, &policy.Enabled, &policy.Threshold, &policy.ModelProfileID, &policy.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return tagging.ClassificationPolicy{}, scope.ErrNotFound
	}
	if err != nil {
		return tagging.ClassificationPolicy{}, err
	}
	rows, err := r.db.Query(ctx, `SELECT id::text,display_name FROM ai_model_profiles WHERE msp_id=$1::uuid AND enabled AND 'classification'=ANY(supported_features) ORDER BY display_name,id`, mspID)
	if err != nil {
		return tagging.ClassificationPolicy{}, err
	}
	defer rows.Close()
	policy.ModelOptions = []tagging.ClassificationModelOption{}
	for rows.Next() {
		var option tagging.ClassificationModelOption
		if err := rows.Scan(&option.ID, &option.Label); err != nil {
			return tagging.ClassificationPolicy{}, err
		}
		policy.ModelOptions = append(policy.ModelOptions, option)
	}
	if err := rows.Err(); err != nil {
		return tagging.ClassificationPolicy{}, err
	}
	policy.ProviderFailureHealth = "not_configured"
	if policy.ModelProfileID != "" {
		_ = r.db.QueryRow(ctx, `SELECT CASE WHEN connection.last_error_code IS NOT NULL THEN 'failed:'||connection.last_error_code ELSE connection.health_state END FROM ai_model_profiles model JOIN ai_provider_connections connection ON connection.id=model.connection_id AND connection.msp_id=model.msp_id WHERE model.id=$1::uuid AND model.msp_id=$2::uuid`, policy.ModelProfileID, mspID).Scan(&policy.ProviderFailureHealth)
	}
	var retained, changed, total int64
	if scanErr := r.db.QueryRow(ctx, `SELECT count(*) FILTER(WHERE decision.decision='automatically_applied' AND assignment.id IS NOT NULL),count(*) FILTER(WHERE decision.decision IN('accepted','dismissed')),count(*) FROM tag_ai_suggestion_decisions decision LEFT JOIN tag_ai_suggestions suggestion ON suggestion.id=decision.suggestion_id AND suggestion.msp_id=decision.msp_id LEFT JOIN object_tag_assignments assignment ON assignment.msp_id=decision.msp_id AND assignment.client_id IS NOT DISTINCT FROM suggestion.client_id AND assignment.object_type=suggestion.object_type AND assignment.object_id=suggestion.object_id AND assignment.tag_id=decision.tag_id WHERE decision.msp_id=$1::uuid`, mspID).Scan(&retained, &changed, &total); scanErr == nil && total > 0 {
		rr := float64(retained) / float64(total)
		cr := float64(changed) / float64(total)
		policy.RetainedRate = &rr
		policy.ChangeRate = &cr
	}
	return policy, err
}

// SaveClassificationPolicy is intentionally optimistic and checks the model
// still supports the constrained classification feature in the same write.
func (r *TaggingRepository) SaveClassificationPolicy(ctx context.Context, policy tagging.ClassificationPolicy) (tagging.ClassificationPolicy, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return tagging.ClassificationPolicy{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var saved tagging.ClassificationPolicy
	err = tx.QueryRow(ctx, `
UPDATE tag_ai_policies policy
SET automatic_apply_enabled = $2,
    automatic_apply_threshold = $3,
    model_profile_id = NULLIF($4, '')::uuid,
    updated_at = now(), updated_by = $6::uuid,
    version = policy.version + 1
WHERE policy.msp_id = $1::uuid AND policy.version = $5
  AND (
    NULLIF($4, '') IS NULL OR EXISTS (
      SELECT 1 FROM ai_model_profiles profile
      JOIN ai_provider_connections connection ON connection.id = profile.connection_id AND connection.msp_id = profile.msp_id
      WHERE profile.id = NULLIF($4, '')::uuid AND profile.msp_id = policy.msp_id
        AND profile.enabled AND 'classification' = ANY(profile.supported_features)
        AND connection.enabled AND connection.disclosure_accepted_at IS NOT NULL
    )
  )
RETURNING policy.msp_id::text, policy.automatic_apply_enabled,
          policy.automatic_apply_threshold::float8,
          COALESCE(policy.model_profile_id::text, ''), policy.version
`, policy.MSPID, policy.Enabled, policy.Threshold, policy.ModelProfileID, policy.Version, policy.UpdatedBy).Scan(
		&saved.MSPID, &saved.Enabled, &saved.Threshold, &saved.ModelProfileID, &saved.Version,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return tagging.ClassificationPolicy{}, object.ErrVersionConflict
	}
	if err != nil {
		return tagging.ClassificationPolicy{}, err
	}
	var aiVersion int64
	err = tx.QueryRow(ctx, `
UPDATE ai_policies
SET classification_model_profile_id = NULLIF($2, '')::uuid,
    allowed_features = CASE
      WHEN NULLIF($2, '') IS NULL THEN array_remove(allowed_features, 'classification')
      WHEN NOT ('classification' = ANY(allowed_features)) THEN array_append(allowed_features, 'classification')
      ELSE allowed_features END,
    enabled = CASE
      WHEN NULLIF($2, '') IS NULL AND cardinality(array_remove(allowed_features, 'classification')) = 0 THEN false
      ELSE enabled OR NULLIF($2, '') IS NOT NULL END,
    updated_at = now(), updated_by = $3::uuid, version = version + 1
WHERE msp_id = $1::uuid
RETURNING version
`, policy.MSPID, policy.ModelProfileID, policy.UpdatedBy).Scan(&aiVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return tagging.ClassificationPolicy{}, scope.ErrNotFound
	}
	if err != nil {
		return tagging.ClassificationPolicy{}, err
	}
	_, err = tx.Exec(ctx, `
WITH fact AS (
  SELECT md5($1 || ':classification-policy:' || $4::bigint::text)::uuid AS correlation_id
)
INSERT INTO audit_ledger (id, occurred_at, msp_id, actor_type, actor_id, action, subject_type, subject_id, subject_version, source, reason, correlation_id, safe_diff)
SELECT md5($1 || ':classification-policy:audit:' || $4::bigint::text)::uuid, now(), $1::uuid, 'technician', $2::uuid,
       'classification.ai_policy.updated', 'classification_policy', $1::uuid, $4::bigint, 'api', 'Classification AI policy updated', correlation_id,
       jsonb_build_object('automatic_apply_enabled',$3::boolean,'model_profile_id',NULLIF($5::text,''),'ai_policy_version',$6::bigint)
FROM fact
`, policy.MSPID, policy.UpdatedBy, policy.Enabled, saved.Version, policy.ModelProfileID, aiVersion)
	if err != nil {
		return tagging.ClassificationPolicy{}, err
	}
	_, err = tx.Exec(ctx, `WITH fact AS (
  SELECT md5($1 || ':classification-policy:' || $4::bigint::text)::uuid AS correlation_id
)
INSERT INTO event_outbox (event_id,event_type,schema_version,occurred_at,msp_id,actor_type,actor_id,subject_type,subject_id,subject_version,source,correlation_id,data)
SELECT md5($1 || ':classification-policy:event:' || $4::bigint::text)::uuid,'classification.ai_policy.updated',1,now(),$1::uuid,'technician',$2::uuid,
       'classification_policy',$1::uuid,$4::bigint,'api',correlation_id,
       jsonb_build_object('automatic_apply_enabled',$3::boolean,'model_profile_id',NULLIF($5::text,''),'ai_policy_version',$6::bigint)
FROM fact
`, policy.MSPID, policy.UpdatedBy, policy.Enabled, saved.Version, policy.ModelProfileID, aiVersion)
	if err != nil {
		return tagging.ClassificationPolicy{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return tagging.ClassificationPolicy{}, err
	}
	return r.GetClassificationPolicy(ctx, saved.MSPID)
}

func (r *TaggingRepository) CreateClassificationSuggestion(ctx context.Context, record tagging.ClassificationSuggestionRecord) (tagging.ClassificationSuggestionRecord, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return tagging.ClassificationSuggestionRecord{}, err
	}
	var saved tagging.ClassificationSuggestionRecord
	err = tx.QueryRow(ctx, `
INSERT INTO tag_ai_suggestions (
  id, msp_id, client_id, object_type, object_id, object_version, model_profile_id, status, requested_at, correlation_id
)
SELECT $1::uuid, $2::uuid, NULLIF($3, '')::uuid, $4, $5::uuid, $6,
       policy.model_profile_id, 'pending', now(), md5($1 || ':classification-request')::uuid
FROM tag_ai_policies policy
JOIN ai_policies ai_policy ON ai_policy.msp_id = policy.msp_id
JOIN ai_model_profiles model ON model.id = policy.model_profile_id AND model.msp_id = policy.msp_id
JOIN ai_provider_connections connection ON connection.id = model.connection_id AND connection.msp_id = model.msp_id
WHERE policy.msp_id = $2::uuid AND policy.model_profile_id IS NOT NULL
  AND ai_policy.enabled AND 'classification' = ANY(ai_policy.allowed_features)
  AND model.enabled AND 'classification' = ANY(model.supported_features)
  AND connection.enabled AND connection.disclosure_accepted_at IS NOT NULL
RETURNING id::text, msp_id::text, COALESCE(client_id::text, ''), object_type,
          object_id::text, object_version, COALESCE(model_profile_id::text, ''), status, version
`, record.ID, record.Target.MSPID, record.Target.ClientID, record.Target.ObjectType.String(), record.Target.ObjectID, record.ObjectVersion).Scan(
		&saved.ID, &saved.Target.MSPID, &saved.Target.ClientID, &saved.Target.ObjectType,
		&saved.Target.ObjectID, &saved.ObjectVersion, &saved.ModelProfileID, &saved.Status, &saved.Version,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		_ = tx.Rollback(ctx)
		return tagging.ClassificationSuggestionRecord{}, scope.ErrNotFound
	}
	if err != nil {
		_ = tx.Rollback(ctx)
		return tagging.ClassificationSuggestionRecord{}, err
	}
	if _, err = tx.Exec(ctx, `
INSERT INTO ai_generation_jobs (
  id, msp_id, client_id, work_record_id, subject_type, subject_id, requested_by, feature,
  model_profile_id, relevant_input_names, state, attempt, max_attempts, idempotency_key, created_at, updated_at
) VALUES ($1::uuid, $2::uuid, NULLIF($3, '')::uuid, NULL, $4, $5::uuid, $6::uuid, 'classification',
  $7::uuid, ARRAY['title','description'], 'queued', 0, 3, $8, now(), now())
`, record.JobID, saved.Target.MSPID, saved.Target.ClientID, saved.Target.ObjectType.String(), saved.Target.ObjectID, record.RequestedBy, saved.ModelProfileID, "classification|"+record.ID); err != nil {
		_ = tx.Rollback(ctx)
		return tagging.ClassificationSuggestionRecord{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE tag_ai_suggestions SET ai_generation_job_id = $2::uuid WHERE id = $1::uuid`, saved.ID, record.JobID); err != nil {
		_ = tx.Rollback(ctx)
		return tagging.ClassificationSuggestionRecord{}, err
	}
	if _, err = tx.Exec(ctx, `
WITH facts(subject_id,subject_type,action,version,correlation_id) AS (
  VALUES ($1::uuid,'classification_suggestion','classification.suggestion.requested',1::bigint,md5($1 || ':requested')::uuid),
         ($2::uuid,'ai_generation_job','ai.generation_job.submitted',1::bigint,md5($2 || ':submitted')::uuid)
)
INSERT INTO audit_ledger (id,occurred_at,msp_id,client_id,actor_type,actor_id,action,subject_type,subject_id,subject_version,source,reason,correlation_id)
SELECT md5(subject_id::text || ':audit:' || action)::uuid,now(),$3::uuid,NULLIF($4,'')::uuid,'technician',$5::uuid,action,subject_type,subject_id,version,'api','AI classification requested',correlation_id FROM facts
`, saved.ID, record.JobID, saved.Target.MSPID, saved.Target.ClientID, record.RequestedBy); err != nil {
		_ = tx.Rollback(ctx)
		return tagging.ClassificationSuggestionRecord{}, err
	}
	if _, err = tx.Exec(ctx, `WITH facts(subject_id,subject_type,action,version,correlation_id) AS (
  VALUES ($1::uuid,'classification_suggestion','classification.suggestion.requested',1::bigint,md5($1 || ':requested')::uuid),
         ($2::uuid,'ai_generation_job','ai.generation_job.submitted',1::bigint,md5($2 || ':submitted')::uuid)
)
INSERT INTO event_outbox (event_id,event_type,schema_version,occurred_at,msp_id,client_id,actor_type,actor_id,subject_type,subject_id,subject_version,source,correlation_id,data)
SELECT md5(subject_id::text || ':event:' || action)::uuid,action,1,now(),$3::uuid,NULLIF($4,'')::uuid,'technician',$5::uuid,subject_type,subject_id,version,'api',correlation_id,'{}'::jsonb FROM facts
`, saved.ID, record.JobID, saved.Target.MSPID, saved.Target.ClientID, record.RequestedBy); err != nil {
		_ = tx.Rollback(ctx)
		return tagging.ClassificationSuggestionRecord{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return tagging.ClassificationSuggestionRecord{}, err
	}
	saved.JobID = record.JobID
	saved.Items = []tagging.ClassificationSuggestion{}
	return saved, err
}

func (r *TaggingRepository) GetClassificationSuggestion(ctx context.Context, target tagging.TargetRef, id string) (tagging.ClassificationSuggestionRecord, error) {
	var record tagging.ClassificationSuggestionRecord
	err := r.db.QueryRow(ctx, `
SELECT id::text, msp_id::text, COALESCE(client_id::text, ''), object_type,
       object_id::text, object_version, COALESCE(model_profile_id::text, ''), status, version
FROM tag_ai_suggestions
WHERE id = $1::uuid AND msp_id = $2::uuid AND client_id IS NOT DISTINCT FROM NULLIF($3, '')::uuid
  AND object_type = $4 AND object_id = $5::uuid
`, id, target.MSPID, target.ClientID, target.ObjectType.String(), target.ObjectID).Scan(
		&record.ID, &record.Target.MSPID, &record.Target.ClientID, &record.Target.ObjectType,
		&record.Target.ObjectID, &record.ObjectVersion, &record.ModelProfileID, &record.Status, &record.Version,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return tagging.ClassificationSuggestionRecord{}, scope.ErrNotFound
	}
	rows, err := r.db.Query(ctx, `
SELECT item.tag_id::text, item.confidence::float8, item.rationale,
       tag.lifecycle_state = 'active', item.disposition
FROM tag_ai_suggestion_items item JOIN tags tag ON tag.id = item.tag_id AND tag.msp_id = item.msp_id
WHERE item.suggestion_id = $1::uuid AND item.msp_id = $2::uuid ORDER BY item.rank
`, record.ID, record.Target.MSPID)
	if err != nil {
		return tagging.ClassificationSuggestionRecord{}, err
	}
	defer rows.Close()
	record.Items = []tagging.ClassificationSuggestion{}
	for rows.Next() {
		var item tagging.ClassificationSuggestion
		if err := rows.Scan(&item.TagID, &item.Confidence, &item.Rationale, &item.Active, &item.Disposition); err != nil {
			return tagging.ClassificationSuggestionRecord{}, err
		}
		record.Items = append(record.Items, item)
	}
	return record, rows.Err()
}

func (r *TaggingRepository) FindClassificationSuggestion(ctx context.Context, mspID, clientID, id string) (tagging.ClassificationSuggestionRecord, error) {
	var target tagging.TargetRef
	err := r.db.QueryRow(ctx, `
SELECT msp_id::text, COALESCE(client_id::text, ''), object_type, object_id::text
FROM tag_ai_suggestions
WHERE id = $1::uuid AND msp_id = $2::uuid
  AND client_id IS NOT DISTINCT FROM NULLIF($3, '')::uuid
`, id, mspID, clientID).Scan(&target.MSPID, &target.ClientID, &target.ObjectType, &target.ObjectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return tagging.ClassificationSuggestionRecord{}, scope.ErrNotFound
	}
	if err != nil {
		return tagging.ClassificationSuggestionRecord{}, err
	}
	return r.GetClassificationSuggestion(ctx, target, id)
}

func (r *TaggingRepository) DecideClassificationSuggestion(ctx context.Context, record tagging.ClassificationSuggestionRecord, tagID, decision, actorID string) (tagging.ClassificationSuggestionRecord, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return tagging.ClassificationSuggestionRecord{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var status string
	if err = tx.QueryRow(ctx, `SELECT status FROM tag_ai_suggestions WHERE id=$1::uuid AND msp_id=$2::uuid FOR UPDATE`, record.ID, record.Target.MSPID).Scan(&status); errors.Is(err, pgx.ErrNoRows) {
		return tagging.ClassificationSuggestionRecord{}, scope.ErrNotFound
	} else if err != nil {
		return tagging.ClassificationSuggestionRecord{}, err
	}
	var existing string
	err = tx.QueryRow(ctx, `SELECT decision FROM tag_ai_suggestion_decisions WHERE suggestion_id=$1::uuid AND msp_id=$2::uuid AND tag_id=$3::uuid`, record.ID, record.Target.MSPID, tagID).Scan(&existing)
	if err == nil {
		if existing != decision {
			return tagging.ClassificationSuggestionRecord{}, object.ErrVersionConflict
		}
		if err = tx.Commit(ctx); err != nil {
			return tagging.ClassificationSuggestionRecord{}, err
		}
		return r.GetClassificationSuggestion(ctx, record.Target, record.ID)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return tagging.ClassificationSuggestionRecord{}, err
	}
	if status != "completed" || !currentClassificationActorAuthorized(ctx, tx, record.Target, actorID) {
		return tagging.ClassificationSuggestionRecord{}, scope.ErrNotFound
	}
	var active bool
	if err = tx.QueryRow(ctx, `SELECT tag.lifecycle_state='active' FROM tag_ai_suggestion_items item JOIN tags tag ON tag.id=item.tag_id AND tag.msp_id=item.msp_id WHERE item.suggestion_id=$1::uuid AND item.msp_id=$2::uuid AND item.tag_id=$3::uuid FOR UPDATE OF item,tag`, record.ID, record.Target.MSPID, tagID).Scan(&active); errors.Is(err, pgx.ErrNoRows) || !active {
		return tagging.ClassificationSuggestionRecord{}, scope.ErrNotFound
	} else if err != nil {
		return tagging.ClassificationSuggestionRecord{}, err
	}
	correlationID := uuid.NewMD5(uuid.Nil, []byte(record.ID+":"+tagID+":"+decision+":correlation")).String()
	associationAdded := false
	associationVersion := record.ObjectVersion
	if decision == "accepted" {
		var currentVersion int64
		err = tx.QueryRow(ctx, `SELECT version FROM classification_object_versions WHERE msp_id=$1::uuid AND client_id IS NOT DISTINCT FROM NULLIF($2,'')::uuid AND object_type=$3 AND object_id=$4::uuid FOR UPDATE`, record.Target.MSPID, record.Target.ClientID, record.Target.ObjectType.String(), record.Target.ObjectID).Scan(&currentVersion)
		if errors.Is(err, pgx.ErrNoRows) {
			return tagging.ClassificationSuggestionRecord{}, scope.ErrNotFound
		}
		if err != nil {
			return tagging.ClassificationSuggestionRecord{}, err
		}
		if currentVersion != record.ObjectVersion {
			return tagging.ClassificationSuggestionRecord{}, object.ErrVersionConflict
		}
		var already bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM object_tag_assignments WHERE msp_id=$1::uuid AND client_id IS NOT DISTINCT FROM NULLIF($2,'')::uuid AND object_type=$3 AND object_id=$4::uuid AND tag_id=$5::uuid)`, record.Target.MSPID, record.Target.ClientID, record.Target.ObjectType.String(), record.Target.ObjectID, tagID).Scan(&already); err != nil {
			return tagging.ClassificationSuggestionRecord{}, err
		}
		if !already {
			next := currentVersion + 1
			associationAdded = true
			associationVersion = next
			if result, execErr := tx.Exec(ctx, `UPDATE classification_object_versions SET version=$5 WHERE msp_id=$1::uuid AND client_id IS NOT DISTINCT FROM NULLIF($2,'')::uuid AND object_type=$3 AND object_id=$4::uuid AND version=$6`, record.Target.MSPID, record.Target.ClientID, record.Target.ObjectType.String(), record.Target.ObjectID, next, currentVersion); execErr != nil || result.RowsAffected() != 1 {
				if execErr != nil {
					return tagging.ClassificationSuggestionRecord{}, execErr
				}
				return tagging.ClassificationSuggestionRecord{}, object.ErrVersionConflict
			}
			assignmentID := associationAssignmentID(record.Target, tagID)
			if _, err = tx.Exec(ctx, `INSERT INTO object_tag_assignments(id,msp_id,client_id,object_type,object_id,object_version,tag_id,assignment_source,assigned_at,assigned_by,evidence,version) VALUES($1::uuid,$2::uuid,NULLIF($3,'')::uuid,$4,$5::uuid,$6,$7::uuid,'ai_confirmed',now(),$8::uuid,jsonb_build_object('suggestion_id',$9::text,'correlation_id',$10::text),1)`, assignmentID, record.Target.MSPID, record.Target.ClientID, record.Target.ObjectType.String(), record.Target.ObjectID, next, tagID, actorID, record.ID, correlationID); err != nil {
				return tagging.ClassificationSuggestionRecord{}, classifyTaggingWriteError(err)
			}
			if _, err = tx.Exec(ctx, `INSERT INTO tag_assignment_events(id,msp_id,client_id,assignment_id,object_type,object_id,target_version,tag_id,operation,assignment_source,actor_type,actor_id,occurred_at,evidence,idempotency_key,correlation_id) VALUES(md5($1 || ':accepted:event')::uuid,$2::uuid,NULLIF($3,'')::uuid,$1::uuid,$4,$5::uuid,$6,$7::uuid,'added','ai_confirmed','technician',$8::uuid,now(),jsonb_build_object('suggestion_id',$9::text),'classification-suggestion:'||$9::text||':'||$7::text,$10::uuid)`, assignmentID, record.Target.MSPID, record.Target.ClientID, record.Target.ObjectType.String(), record.Target.ObjectID, next, tagID, actorID, record.ID, correlationID); err != nil {
				return tagging.ClassificationSuggestionRecord{}, err
			}
		}
	}
	result, err := tx.Exec(ctx, `INSERT INTO tag_ai_suggestion_decisions(id,suggestion_id,msp_id,tag_id,decision,decided_by,correlation_id) VALUES(md5($1||':'||$2||':'||$3||':'||$4)::uuid,$1::uuid,$2::uuid,$3::uuid,$4,$5::uuid,$6::uuid)`, record.ID, record.Target.MSPID, tagID, decision, actorID, correlationID)
	if err != nil || result.RowsAffected() != 1 {
		if err != nil {
			return tagging.ClassificationSuggestionRecord{}, err
		}
		return tagging.ClassificationSuggestionRecord{}, object.ErrVersionConflict
	}
	result, err = tx.Exec(ctx, `UPDATE tag_ai_suggestion_items SET disposition=CASE $4 WHEN 'accepted' THEN 'accepted' ELSE 'rejected' END WHERE suggestion_id=$1::uuid AND msp_id=$2::uuid AND tag_id=$3::uuid`, record.ID, record.Target.MSPID, tagID, decision)
	if err != nil || result.RowsAffected() != 1 {
		if err != nil {
			return tagging.ClassificationSuggestionRecord{}, err
		}
		return tagging.ClassificationSuggestionRecord{}, scope.ErrNotFound
	}
	result, err = tx.Exec(ctx, `UPDATE tag_ai_suggestions suggestion SET status=CASE WHEN NOT EXISTS(SELECT 1 FROM tag_ai_suggestion_items item WHERE item.suggestion_id=suggestion.id AND item.msp_id=suggestion.msp_id AND item.disposition='suggested') THEN CASE WHEN EXISTS(SELECT 1 FROM tag_ai_suggestion_items item WHERE item.suggestion_id=suggestion.id AND item.msp_id=suggestion.msp_id AND item.disposition='accepted') THEN 'accepted' ELSE 'rejected' END ELSE 'completed' END,object_version=CASE WHEN $4 THEN $5 ELSE object_version END,decided_at=now(),decided_by=$3::uuid,version=version+1 WHERE id=$1::uuid AND msp_id=$2::uuid AND status='completed'`, record.ID, record.Target.MSPID, actorID, associationAdded, associationVersion)
	if err != nil || result.RowsAffected() != 1 {
		if err != nil {
			return tagging.ClassificationSuggestionRecord{}, err
		}
		return tagging.ClassificationSuggestionRecord{}, object.ErrVersionConflict
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_ledger(id,occurred_at,msp_id,client_id,actor_type,actor_id,action,subject_type,subject_id,subject_version,source,reason,correlation_id,safe_diff) VALUES(md5($1||':'||$3||':decision:audit')::uuid,now(),$2::uuid,NULLIF($6,'')::uuid,'technician',$5::uuid,'classification.suggestion.'||$4,'classification_suggestion',$1::uuid,1,'api','Classification suggestion decision',$7::uuid,jsonb_build_object('tag_id',$3,'decision',$4))`, record.ID, record.Target.MSPID, tagID, decision, actorID, record.Target.ClientID, correlationID); err != nil {
		return tagging.ClassificationSuggestionRecord{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO event_outbox(event_id,event_type,schema_version,occurred_at,msp_id,client_id,actor_type,actor_id,subject_type,subject_id,subject_version,source,correlation_id,data) VALUES(md5($1||':'||$3||':decision:event')::uuid,'classification.suggestion.'||$4,1,now(),$2::uuid,NULLIF($6,'')::uuid,'technician',$5::uuid,'classification_suggestion',$1::uuid,1,'api',$7::uuid,jsonb_build_object('tag_id',$3,'decision',$4))`, record.ID, record.Target.MSPID, tagID, decision, actorID, record.Target.ClientID, correlationID); err != nil {
		return tagging.ClassificationSuggestionRecord{}, err
	}
	if associationAdded {
		if _, err = tx.Exec(ctx, `INSERT INTO audit_ledger(id,occurred_at,msp_id,client_id,actor_type,actor_id,action,subject_type,subject_id,subject_version,source,reason,correlation_id,safe_diff) VALUES(md5($1||':'||$3||':accepted-association:audit')::uuid,now(),$2::uuid,NULLIF($5,'')::uuid,'technician',$4::uuid,'classification.tags.replaced',$6,$7::uuid,$8,'ai','Technician accepted AI classification',$9::uuid,jsonb_build_object('tag_id',$3::text,'suggestion_id',$1::text))`, record.ID, record.Target.MSPID, tagID, actorID, record.Target.ClientID, record.Target.ObjectType.String(), record.Target.ObjectID, associationVersion, correlationID); err != nil {
			return tagging.ClassificationSuggestionRecord{}, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO event_outbox(event_id,event_type,schema_version,occurred_at,msp_id,client_id,actor_type,actor_id,subject_type,subject_id,subject_version,source,correlation_id,data) VALUES(md5($1||':'||$3||':accepted-association:event')::uuid,'classification.tags.replaced',1,now(),$2::uuid,NULLIF($5,'')::uuid,'technician',$4::uuid,$6,$7::uuid,$8,'ai',$9::uuid,jsonb_build_object('tag_id',$3::text,'suggestion_id',$1::text))`, record.ID, record.Target.MSPID, tagID, actorID, record.Target.ClientID, record.Target.ObjectType.String(), record.Target.ObjectID, associationVersion, correlationID); err != nil {
			return tagging.ClassificationSuggestionRecord{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return tagging.ClassificationSuggestionRecord{}, err
	}
	return r.GetClassificationSuggestion(ctx, record.Target, record.ID)
}

func currentClassificationActorAuthorized(ctx context.Context, tx transaction, target tagging.TargetRef, actorID string) bool {
	var authorized bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM role_assignments assignment JOIN role_capabilities capability ON capability.role_id=assignment.role_id AND capability.msp_id=assignment.msp_id WHERE assignment.msp_id=$1::uuid AND assignment.technician_id=$2::uuid AND (assignment.client_id IS NULL OR assignment.client_id IS NOT DISTINCT FROM NULLIF($3,'')::uuid) AND (assignment.expires_at IS NULL OR assignment.expires_at>now()) AND capability.capability='classification.apply')`, target.MSPID, actorID, target.ClientID).Scan(&authorized)
	return err == nil && authorized
}

func (r *TaggingRepository) ClaimClassificationApplications(ctx context.Context, limit int, lease time.Duration) ([]tagging.ClassificationSuggestionRecord, error) {
	if limit < 1 || lease <= 0 {
		return []tagging.ClassificationSuggestionRecord{}, nil
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `
WITH candidate AS (
  SELECT suggestion.id FROM tag_ai_suggestions suggestion
  WHERE suggestion.application_state IN ('queued', 'applying')
    AND (suggestion.application_state = 'queued' OR suggestion.application_lease_until < now())
  ORDER BY suggestion.completed_at, suggestion.id FOR UPDATE SKIP LOCKED LIMIT $2
), claimed AS (
  UPDATE tag_ai_suggestions suggestion SET application_state = 'applying',
    application_lease_token = md5(suggestion.id::text || random()::text || clock_timestamp()::text)::uuid,
    application_lease_until = now() + ($1 * interval '1 microsecond'), updated_at = now()
  FROM candidate WHERE suggestion.id = candidate.id
  RETURNING suggestion.id, suggestion.msp_id, suggestion.client_id, suggestion.object_type,
            suggestion.object_id, suggestion.object_version, suggestion.application_lease_token
)
SELECT claimed.id::text, claimed.msp_id::text, COALESCE(claimed.client_id::text, ''), claimed.object_type, claimed.object_id::text, claimed.object_version,
       job.requested_by::text,
       COALESCE((policy.automatic_apply_enabled
        AND suggestion.status = 'completed' AND job.state = 'completed' AND job.feature = 'classification'
        AND suggestion.model_profile_id = policy.model_profile_id AND job.model_profile_id = policy.model_profile_id
        AND ai_policy.enabled AND 'classification' = ANY(ai_policy.allowed_features)
        AND ai_policy.classification_model_profile_id = policy.model_profile_id
        AND suggestion.provider_evidence ? 'provider' AND suggestion.provider_evidence ? 'model'
        AND suggestion.provider_evidence->>'prompt_version' = 'classification-v1'
        AND (suggestion.provider_evidence->>'policy_version')::bigint = policy.version
        AND model.enabled AND 'classification' = ANY(model.supported_features)
        AND connection.enabled AND connection.disclosure_accepted_at IS NOT NULL
        AND connection.disclosure_accepted_by IS NOT NULL
       ), false),
       COALESCE(policy.automatic_apply_threshold::float8, 1),
       claimed.application_lease_token::text
FROM claimed JOIN tag_ai_suggestions suggestion ON suggestion.id = claimed.id
JOIN ai_generation_jobs job ON job.id = suggestion.ai_generation_job_id
LEFT JOIN tag_ai_policies policy ON policy.msp_id = claimed.msp_id
LEFT JOIN ai_policies ai_policy ON ai_policy.msp_id = claimed.msp_id
LEFT JOIN ai_model_profiles model ON model.id = policy.model_profile_id AND model.msp_id = policy.msp_id
LEFT JOIN ai_provider_connections connection ON connection.id = model.connection_id AND connection.msp_id = model.msp_id
`, lease.Microseconds(), limit)
	if err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	defer rows.Close()
	result := []tagging.ClassificationSuggestionRecord{}
	for rows.Next() {
		var item tagging.ClassificationSuggestionRecord
		if err := rows.Scan(&item.ID, &item.Target.MSPID, &item.Target.ClientID, &item.Target.ObjectType, &item.Target.ObjectID, &item.ObjectVersion, &item.RequestedBy, &item.Automatic, &item.Threshold, &item.LeaseToken); err != nil {
			_ = tx.Rollback(ctx)
			return nil, err
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	rows.Close()
	for index := range result {
		itemRows, err := tx.Query(ctx, `
SELECT item.tag_id::text, item.confidence::float8, item.rationale, tag.lifecycle_state = 'active'
FROM tag_ai_suggestion_items item
JOIN tags tag ON tag.id = item.tag_id AND tag.msp_id = item.msp_id
WHERE item.suggestion_id = $1::uuid AND item.msp_id = $2::uuid ORDER BY item.rank
`, result[index].ID, result[index].Target.MSPID)
		if err != nil {
			_ = tx.Rollback(ctx)
			return nil, err
		}
		for itemRows.Next() {
			var item tagging.ClassificationSuggestion
			if err := itemRows.Scan(&item.TagID, &item.Confidence, &item.Rationale, &item.Active); err != nil {
				itemRows.Close()
				_ = tx.Rollback(ctx)
				return nil, err
			}
			result[index].Items = append(result[index].Items, item)
		}
		if err := itemRows.Err(); err != nil {
			itemRows.Close()
			_ = tx.Rollback(ctx)
			return nil, err
		}
		itemRows.Close()
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	return result, nil
}

// ApplyClassificationApplication owns the final authorization fence, tag
// association, decision ledger and terminal lease transition in one commit.
// The browser/claim snapshot is evidence only and never grants authority.
func (r *TaggingRepository) ApplyClassificationApplication(ctx context.Context, claimed tagging.ClassificationSuggestionRecord) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockClassificationApplicationAuthority(ctx, tx, claimed); err != nil {
		return err
	}
	// A newly created object may not have needed an association-version row
	// yet. Establish and lock that durable fence inside the same authority
	// transaction before the final policy/version decision.
	if _, err := lockClassificationObjectVersion(
		ctx, tx, claimed.Target, claimed.ObjectVersion,
	); err != nil {
		return err
	}
	var target tagging.TargetRef
	var actorID string
	var objectVersion int64
	var threshold float64
	var allowed bool
	err = tx.QueryRow(ctx, `
SELECT suggestion.msp_id::text,COALESCE(suggestion.client_id::text,''),suggestion.object_type,suggestion.object_id::text,
       suggestion.object_version,job.requested_by::text,COALESCE(policy.automatic_apply_threshold::float8,1),
       COALESCE(policy.automatic_apply_enabled
         AND suggestion.status='completed' AND job.state='completed' AND job.feature='classification'
         AND suggestion.model_profile_id=policy.model_profile_id AND job.model_profile_id=policy.model_profile_id
         AND ai_policy.enabled AND 'classification'=ANY(ai_policy.allowed_features)
         AND ai_policy.classification_model_profile_id=policy.model_profile_id
         AND suggestion.provider_evidence ? 'provider' AND suggestion.provider_evidence ? 'model'
         AND suggestion.provider_evidence->>'prompt_version'='classification-v1'
         AND (suggestion.provider_evidence->>'policy_version')::bigint=policy.version
         AND model.enabled AND 'classification'=ANY(model.supported_features)
         AND connection.enabled AND connection.disclosure_accepted_at IS NOT NULL AND connection.disclosure_accepted_by IS NOT NULL
         AND object_version.version=suggestion.object_version
         AND EXISTS(SELECT 1 FROM role_assignments assignment JOIN role_capabilities capability ON capability.role_id=assignment.role_id AND capability.msp_id=assignment.msp_id WHERE assignment.msp_id=suggestion.msp_id AND assignment.technician_id=job.requested_by AND (assignment.client_id IS NULL OR assignment.client_id IS NOT DISTINCT FROM suggestion.client_id) AND (assignment.expires_at IS NULL OR assignment.expires_at>now()) AND capability.capability='classification.apply'),false)
FROM tag_ai_suggestions suggestion
JOIN ai_generation_jobs job ON job.id=suggestion.ai_generation_job_id
LEFT JOIN tag_ai_policies policy ON policy.msp_id=suggestion.msp_id
LEFT JOIN ai_policies ai_policy ON ai_policy.msp_id=suggestion.msp_id
LEFT JOIN ai_model_profiles model ON model.id=policy.model_profile_id AND model.msp_id=policy.msp_id
LEFT JOIN ai_provider_connections connection ON connection.id=model.connection_id AND connection.msp_id=model.msp_id
JOIN classification_object_versions object_version ON object_version.msp_id=suggestion.msp_id AND object_version.client_id IS NOT DISTINCT FROM suggestion.client_id AND object_version.object_type=suggestion.object_type AND object_version.object_id=suggestion.object_id
WHERE suggestion.id=$1::uuid AND suggestion.application_state='applying' AND suggestion.application_lease_token=$2::uuid AND suggestion.application_lease_until>now()
FOR UPDATE OF suggestion,object_version
`, claimed.ID, claimed.LeaseToken).Scan(&target.MSPID, &target.ClientID, &target.ObjectType, &target.ObjectID, &objectVersion, &actorID, &threshold, &allowed)
	if errors.Is(err, pgx.ErrNoRows) {
		return scope.ErrNotFound
	}
	if err != nil {
		return err
	}
	state := "skipped"
	correlationID := uuid.NewMD5(uuid.Nil, []byte(claimed.ID+":automatic-application")).String()
	if allowed {
		rows, queryErr := tx.Query(ctx, `SELECT item.tag_id::text FROM tag_ai_suggestion_items item JOIN tags tag ON tag.id=item.tag_id AND tag.msp_id=item.msp_id WHERE item.suggestion_id=$1::uuid AND item.msp_id=$2::uuid AND item.confidence >= $3 AND item.disposition='suggested' AND tag.lifecycle_state='active' ORDER BY item.tag_id FOR UPDATE OF item,tag`, claimed.ID, target.MSPID, threshold)
		if queryErr != nil {
			return queryErr
		}
		eligible := []string{}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			eligible = append(eligible, id)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		added := []string{}
		if len(eligible) > 0 {
			next := objectVersion + 1
			for _, tagID := range eligible {
				assignmentID := associationAssignmentID(target, tagID)
				result, execErr := tx.Exec(ctx, `INSERT INTO object_tag_assignments(id,msp_id,client_id,object_type,object_id,object_version,tag_id,assignment_source,assigned_at,assigned_by,evidence,version) VALUES($1::uuid,$2::uuid,NULLIF($3,'')::uuid,$4,$5::uuid,$6,$7::uuid,'ai_automatic',now(),$8::uuid,jsonb_build_object('suggestion_id',$9::text,'correlation_id',$10::text),1) ON CONFLICT(msp_id,object_type,object_id,tag_id) DO NOTHING`, assignmentID, target.MSPID, target.ClientID, target.ObjectType.String(), target.ObjectID, next, tagID, actorID, claimed.ID, correlationID)
				if execErr != nil {
					return classifyTaggingWriteError(execErr)
				}
				if result.RowsAffected() == 1 {
					added = append(added, tagID)
				}
			}
			if len(added) > 0 {
				result, execErr := tx.Exec(ctx, `UPDATE classification_object_versions SET version=version+1 WHERE msp_id=$1::uuid AND client_id IS NOT DISTINCT FROM NULLIF($2,'')::uuid AND object_type=$3 AND object_id=$4::uuid AND version=$5`, target.MSPID, target.ClientID, target.ObjectType.String(), target.ObjectID, objectVersion)
				if execErr != nil {
					return execErr
				}
				if result.RowsAffected() != 1 {
					return object.ErrVersionConflict
				}
				for _, tagID := range added {
					assignmentID := associationAssignmentID(target, tagID)
					if _, err = tx.Exec(ctx, `INSERT INTO tag_assignment_events(id,msp_id,client_id,assignment_id,object_type,object_id,target_version,tag_id,operation,assignment_source,actor_type,actor_id,occurred_at,evidence,idempotency_key,correlation_id) VALUES(md5($1||':automatic:event')::uuid,$2::uuid,NULLIF($3,'')::uuid,$1::uuid,$4,$5::uuid,$6,$7::uuid,'added','ai_automatic','technician',$8::uuid,now(),jsonb_build_object('suggestion_id',$9::text),'classification-suggestion:'||$9::text||':'||$7::text,$10::uuid)`, assignmentID, target.MSPID, target.ClientID, target.ObjectType.String(), target.ObjectID, next, tagID, actorID, claimed.ID, correlationID); err != nil {
						return err
					}
					if _, err = tx.Exec(ctx, `INSERT INTO tag_ai_suggestion_decisions(id,suggestion_id,msp_id,tag_id,decision,decided_by,reason,correlation_id) VALUES(md5($1||':'||$2||':automatically_applied')::uuid,$1::uuid,$3::uuid,$2::uuid,'automatically_applied',NULL,'High-confidence AI classification',$4::uuid) ON CONFLICT(id) DO NOTHING`, claimed.ID, tagID, target.MSPID, correlationID); err != nil {
						return err
					}
					if _, err = tx.Exec(ctx, `UPDATE tag_ai_suggestion_items SET disposition='automatically_applied' WHERE suggestion_id=$1::uuid AND msp_id=$3::uuid AND tag_id=$2::uuid`, claimed.ID, tagID, target.MSPID); err != nil {
						return err
					}
				}
				state = "applied"
			}
		}
	}
	result, err := tx.Exec(ctx, `UPDATE tag_ai_suggestions SET application_state=$3,application_lease_token=NULL,application_lease_until=NULL,updated_at=now(),version=version+1 WHERE id=$1::uuid AND application_lease_token=$2::uuid`, claimed.ID, claimed.LeaseToken, state)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return scope.ErrNotFound
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_ledger(id,occurred_at,msp_id,client_id,actor_type,actor_id,action,subject_type,subject_id,subject_version,source,reason,correlation_id,safe_diff) VALUES(md5($1||':application:audit')::uuid,now(),$2::uuid,NULLIF($3,'')::uuid,'system',$4::uuid,'classification.suggestion.'||$5,'classification_suggestion',$1::uuid,1,'system','Classification automatic application',$6::uuid,jsonb_build_object('state',$5))`, claimed.ID, target.MSPID, target.ClientID, actorID, state, correlationID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO event_outbox(event_id,event_type,schema_version,occurred_at,msp_id,client_id,actor_type,actor_id,subject_type,subject_id,subject_version,source,correlation_id,data) VALUES(md5($1||':application:event')::uuid,'classification.suggestion.'||$5,1,now(),$2::uuid,NULLIF($3,'')::uuid,'system',$4::uuid,'classification_suggestion',$1::uuid,1,'system',$6::uuid,jsonb_build_object('state',$5))`, claimed.ID, target.MSPID, target.ClientID, actorID, state, correlationID); err != nil {
		return err
	}
	if state == "applied" {
		if _, err = tx.Exec(ctx, `INSERT INTO audit_ledger(id,occurred_at,msp_id,client_id,actor_type,actor_id,action,subject_type,subject_id,subject_version,source,reason,correlation_id,safe_diff) VALUES(md5($1||':association:audit')::uuid,now(),$2::uuid,NULLIF($3,'')::uuid,'system',$4::uuid,'classification.tags.replaced',$5,$6::uuid,$7+1,'ai','High-confidence AI classification',$8::uuid,jsonb_build_object('suggestion_id',$1))`, claimed.ID, target.MSPID, target.ClientID, actorID, target.ObjectType.String(), target.ObjectID, objectVersion, correlationID); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO event_outbox(event_id,event_type,schema_version,occurred_at,msp_id,client_id,actor_type,actor_id,subject_type,subject_id,subject_version,source,correlation_id,data) VALUES(md5($1||':association:event')::uuid,'classification.tags.replaced',1,now(),$2::uuid,NULLIF($3,'')::uuid,'system',$4::uuid,$5,$6::uuid,$7+1,'ai',$8::uuid,jsonb_build_object('suggestion_id',$1))`, claimed.ID, target.MSPID, target.ClientID, actorID, target.ObjectType.String(), target.ObjectID, objectVersion, correlationID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func lockClassificationApplicationAuthority(ctx context.Context, tx transaction, claimed tagging.ClassificationSuggestionRecord) error {
	// Match every other tag mutation's first lock, then take governing rows in
	// a stable order. Updates/deletes of any authority row now wait until the
	// association, decision and terminal state have committed or rolled back.
	if err := lockTagMutationNamespace(ctx, tx, claimed.Target.MSPID); err != nil {
		return err
	}
	var mspID, clientID, modelID, actorID string
	err := tx.QueryRow(ctx, `SELECT suggestion.msp_id::text,COALESCE(suggestion.client_id::text,''),COALESCE(suggestion.model_profile_id::text,''),job.requested_by::text FROM tag_ai_suggestions suggestion JOIN ai_generation_jobs job ON job.id=suggestion.ai_generation_job_id WHERE suggestion.id=$1::uuid AND suggestion.application_state='applying' AND suggestion.application_lease_token=$2::uuid AND suggestion.application_lease_until>now() FOR UPDATE OF suggestion,job`, claimed.ID, claimed.LeaseToken).Scan(&mspID, &clientID, &modelID, &actorID)
	if errors.Is(err, pgx.ErrNoRows) {
		return scope.ErrNotFound
	}
	if err != nil {
		return err
	}
	if mspID != claimed.Target.MSPID || clientID != claimed.Target.ClientID {
		return scope.ErrNotFound
	}
	for _, query := range []string{
		`SELECT msp_id FROM tag_ai_policies WHERE msp_id=$1::uuid FOR UPDATE`,
		`SELECT msp_id FROM ai_policies WHERE msp_id=$1::uuid FOR UPDATE`,
	} {
		var ignored any
		if err := tx.QueryRow(ctx, query, mspID).Scan(&ignored); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	}
	var connectionID string
	if modelID != "" {
		err = tx.QueryRow(ctx, `SELECT connection_id::text FROM ai_model_profiles WHERE id=$1::uuid AND msp_id=$2::uuid FOR UPDATE`, modelID, mspID).Scan(&connectionID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	}
	if connectionID != "" {
		var ignored any
		if err := tx.QueryRow(ctx, `SELECT id FROM ai_provider_connections WHERE id=$1::uuid AND msp_id=$2::uuid FOR UPDATE`, connectionID, mspID).Scan(&ignored); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	}
	rows, err := tx.Query(ctx, `SELECT assignment.id FROM role_assignments assignment JOIN role_capabilities capability ON capability.role_id=assignment.role_id AND capability.msp_id=assignment.msp_id WHERE assignment.msp_id=$1::uuid AND assignment.technician_id=$2::uuid AND (assignment.client_id IS NULL OR assignment.client_id IS NOT DISTINCT FROM NULLIF($3,'')::uuid) AND capability.capability='classification.apply' ORDER BY assignment.id FOR UPDATE OF assignment,capability`, mspID, actorID, clientID)
	if err != nil {
		return err
	}
	rows.Close()
	_, err = getTaggedObject(ctx, tx, claimed.Target, true)
	return err
}

func (r *TaggingRepository) FinishClassificationApplication(ctx context.Context, record tagging.ClassificationSuggestionRecord, state string) error {
	if (state != "applied" && state != "skipped" && state != "failed") || strings.TrimSpace(record.LeaseToken) == "" {
		return tagging.ErrInvalidAssociation
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	var mspID, clientID, requestedBy, actualState string
	var version int64
	err = tx.QueryRow(ctx, `
UPDATE tag_ai_suggestions suggestion SET
  application_state = CASE WHEN $2 = 'skipped' AND EXISTS (
    SELECT 1 FROM classification_tag_operations operation
    WHERE operation.msp_id = suggestion.msp_id
      AND operation.client_id IS NOT DISTINCT FROM suggestion.client_id
      AND operation.object_type = suggestion.object_type AND operation.object_id = suggestion.object_id
      AND operation.idempotency_key = 'classification-suggestion:' || suggestion.id::text
  ) THEN 'applied' ELSE $2 END,
  application_lease_token = NULL, application_lease_until = NULL,
  updated_at = now(), version = suggestion.version + 1
FROM ai_generation_jobs job
WHERE suggestion.id = $1::uuid AND suggestion.application_state = 'applying'
  AND suggestion.application_lease_token = $3::uuid AND suggestion.application_lease_until > now()
  AND job.id = suggestion.ai_generation_job_id
RETURNING suggestion.msp_id::text, COALESCE(suggestion.client_id::text, ''),
          job.requested_by::text, suggestion.application_state, suggestion.version
`, record.ID, state, record.LeaseToken).Scan(&mspID, &clientID, &requestedBy, &actualState, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		_ = tx.Rollback(ctx)
		return scope.ErrNotFound
	}
	if err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if actualState == "applied" {
		if _, err := tx.Exec(ctx, `
INSERT INTO tag_ai_suggestion_decisions (id,suggestion_id,msp_id,tag_id,decision,decided_by,reason,correlation_id)
SELECT md5($1::text || ':' || item.tag_id::text || ':automatically_applied')::uuid,
       $1::uuid,$2::uuid,item.tag_id,'automatically_applied',NULL,
       'High-confidence AI classification',md5($1::text || ':' || item.tag_id::text || ':automatic-correlation')::uuid
FROM tag_ai_suggestion_items item
JOIN tags tag ON tag.id = item.tag_id AND tag.msp_id = item.msp_id AND tag.lifecycle_state = 'active'
JOIN tag_ai_policies policy ON policy.msp_id = item.msp_id
WHERE item.suggestion_id = $1::uuid AND item.msp_id = $2::uuid
  AND item.confidence >= policy.automatic_apply_threshold
ON CONFLICT (id) DO NOTHING
`, record.ID, mspID); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if _, err := tx.Exec(ctx, `
UPDATE tag_ai_suggestion_items item SET disposition='automatically_applied'
FROM tags tag, tag_ai_policies policy
WHERE item.suggestion_id=$1::uuid AND item.msp_id=$2::uuid
  AND tag.id=item.tag_id AND tag.msp_id=item.msp_id AND tag.lifecycle_state='active'
  AND policy.msp_id=item.msp_id AND item.confidence >= policy.automatic_apply_threshold
`, record.ID, mspID); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
	}
	correlationID := record.LeaseToken
	if _, err := tx.Exec(ctx, `
INSERT INTO audit_ledger (id,occurred_at,msp_id,client_id,actor_type,actor_id,action,subject_type,subject_id,subject_version,source,reason,correlation_id)
VALUES (md5($1::text || ':audit:' || $4)::uuid,now(),$2::uuid,NULLIF($3,'')::uuid,'technician',$5::uuid,
        'classification.suggestion.application.' || $4,'tag_ai_suggestion',$1::uuid,$6,'worker',$4,$7::uuid)
`, record.ID, mspID, clientID, actualState, requestedBy, version, correlationID); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO event_outbox (event_id,event_type,schema_version,occurred_at,msp_id,client_id,actor_type,actor_id,subject_type,subject_id,subject_version,correlation_id,source,data)
VALUES (md5($1::text || ':event:' || $4)::uuid,'classification.suggestion.application.' || $4,1,now(),$2::uuid,NULLIF($3,'')::uuid,
        'technician',$5::uuid,'tag_ai_suggestion',$1::uuid,$6,$7::uuid,'worker',jsonb_build_object('application_state',$4))
`, record.ID, mspID, clientID, actualState, requestedBy, version, correlationID); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

type taggingQueryer interface {
	Query(context.Context, string, ...any) (rows, error)
	QueryRow(context.Context, string, ...any) row
}

func getTaggedObject(
	ctx context.Context,
	q taggingQueryer,
	target tagging.TargetRef,
	forUpdate bool,
) (tagging.TaggedObject, error) {
	baseVersion, err := loadTaggingTarget(ctx, q, target, forUpdate)
	if err != nil {
		return tagging.TaggedObject{}, err
	}
	version, err := classificationObjectVersion(ctx, q, target, baseVersion, false)
	if err != nil {
		return tagging.TaggedObject{}, err
	}
	direct, err := loadDirectAssignments(ctx, q, target)
	if err != nil {
		return tagging.TaggedObject{}, err
	}
	inherited, err := loadInheritedAssignments(ctx, q, target)
	if err != nil {
		return tagging.TaggedObject{}, err
	}
	return normalizeTaggingObject(tagging.TaggedObject{
		Target: target, ObjectVersion: version,
		Direct: direct, Inherited: inherited,
	}), nil
}

func classificationObjectVersion(
	ctx context.Context,
	q taggingQueryer,
	target tagging.TargetRef,
	baseVersion int64,
	forUpdate bool,
) (int64, error) {
	lock := ""
	if forUpdate {
		lock = " FOR UPDATE"
	}
	var version int64
	err := q.QueryRow(ctx, `
SELECT version FROM classification_object_versions
WHERE msp_id = $1 AND object_type = $2 AND object_id = $3
  AND (($4 = '' AND client_id IS NULL) OR client_id = NULLIF($4, '')::uuid)`+lock,
		target.MSPID, target.ObjectType.String(), target.ObjectID, target.ClientID,
	).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return baseVersion, nil
	}
	return version, err
}

func lockClassificationObjectVersion(
	ctx context.Context,
	tx transaction,
	target tagging.TargetRef,
	baseVersion int64,
) (int64, error) {
	if _, err := tx.Exec(ctx, `
INSERT INTO classification_object_versions (
  msp_id, client_id, object_type, object_id, version
) VALUES ($1::uuid, NULLIF($2, '')::uuid, $3, $4::uuid, $5)
ON CONFLICT (msp_id, client_id, object_type, object_id) DO NOTHING
`, target.MSPID, target.ClientID, target.ObjectType.String(), target.ObjectID,
		baseVersion); err != nil {
		return 0, err
	}
	return classificationObjectVersion(ctx, tx, target, baseVersion, true)
}

func normalizeTaggingObject(value tagging.TaggedObject) tagging.TaggedObject {
	value.Direct = append([]tagging.Assignment{}, value.Direct...)
	value.Inherited = append([]tagging.Assignment{}, value.Inherited...)
	value.Effective = make([]tagging.Assignment, 0, len(value.Direct)+len(value.Inherited))
	seen := map[string]struct{}{}
	appendAssignment := func(assignment tagging.Assignment) {
		if assignment.Tag.State != tagging.StateActive {
			return
		}
		if _, exists := seen[assignment.Tag.ID]; exists {
			return
		}
		seen[assignment.Tag.ID] = struct{}{}
		value.Effective = append(value.Effective, assignment)
	}
	for _, assignment := range value.Direct {
		appendAssignment(assignment)
	}
	for _, assignment := range value.Inherited {
		appendAssignment(assignment)
	}
	value.ClassificationState = "missing"
	for _, assignment := range value.Effective {
		if assignment.Tag.InternalKey != "taxonomy.system.unclassified" {
			value.ClassificationState = "classified"
			return value
		}
		value.ClassificationState = "unclassified"
	}
	return value
}

func loadTaggingTarget(
	ctx context.Context,
	q taggingQueryer,
	target tagging.TargetRef,
	forUpdate bool,
) (int64, error) {
	lock := ""
	if forUpdate {
		lock = " FOR UPDATE"
	}
	query := ""
	args := []any{target.ObjectID, target.MSPID, target.ClientID}
	switch target.ObjectType {
	case tagging.ObjectWorkRecord:
		query = `SELECT version FROM work_records WHERE id = $1 AND msp_id = $2 AND client_id = $3` + lock
	case tagging.ObjectTask:
		query = `SELECT version FROM tasks WHERE id = $1 AND msp_id = $2 AND client_id = $3` + lock
	case tagging.ObjectProject:
		query = `SELECT version FROM projects WHERE id = $1 AND msp_id = $2 AND client_id = $3` + lock
	case tagging.ObjectAsset:
		query = `SELECT version FROM assets WHERE id = $1 AND msp_id = $2 AND client_id = $3` + lock
	case tagging.ObjectKnowledgeArticle:
		query = `SELECT current_version FROM knowledge_articles WHERE id = $1 AND msp_id = $2 AND (($3 = '' AND client_id IS NULL) OR client_id = NULLIF($3, '')::uuid)` + lock
	case tagging.ObjectTimeEntry:
		query = `SELECT version FROM time_entries WHERE id = $1 AND msp_id = $2 AND client_id = $3` + lock
	default:
		return 0, scope.ErrNotFound
	}
	var version int64
	err := q.QueryRow(ctx, query, args...).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, scope.ErrNotFound
	}
	return version, err
}

func loadDirectAssignments(
	ctx context.Context,
	q taggingQueryer,
	target tagging.TargetRef,
) ([]tagging.Assignment, error) {
	rows, err := q.Query(ctx, `
SELECT assignment.id::text, assignment.assignment_source, assignment.assigned_at,
       COALESCE(assignment.assigned_by::text, ''),
       tag.id::text, tag.msp_id::text, tag.internal_key, tag.label,
       tag.group_id::text, tag.description, COALESCE(tag.color, ''),
       tag.lifecycle_state, tag.system_tag,
       COALESCE(tag.merged_into_id::text, ''), tag.version
FROM object_tag_assignments assignment
JOIN tags tag ON tag.id = assignment.tag_id AND tag.msp_id = assignment.msp_id
WHERE assignment.msp_id = $1 AND assignment.object_type = $2
  AND assignment.object_id = $3
  AND (($4 = '' AND assignment.client_id IS NULL)
       OR assignment.client_id = NULLIF($4, '')::uuid)
ORDER BY assignment.assigned_at, assignment.id
`, target.MSPID, target.ObjectType.String(), target.ObjectID, target.ClientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	assignments := []tagging.Assignment{}
	for rows.Next() {
		assignment, err := scanAssignment(rows)
		if err != nil {
			return nil, err
		}
		assignments = append(assignments, assignment)
	}
	return assignments, rows.Err()
}

func loadInheritedAssignments(
	ctx context.Context,
	q taggingQueryer,
	target tagging.TargetRef,
) ([]tagging.Assignment, error) {
	if target.ObjectType != tagging.ObjectTask {
		return []tagging.Assignment{}, nil
	}
	rows, err := q.Query(ctx, `
SELECT assignment.id::text, assignment.assignment_source, assignment.assigned_at,
       COALESCE(assignment.assigned_by::text, ''),
       tag.id::text, tag.msp_id::text, tag.internal_key, tag.label,
       tag.group_id::text, tag.description, COALESCE(tag.color, ''),
       tag.lifecycle_state, tag.system_tag,
       COALESCE(tag.merged_into_id::text, ''), tag.version,
       project.id::text
FROM tasks task
JOIN projects project
  ON project.msp_id = task.msp_id AND project.client_id = task.client_id
 AND (
   (task.parent_type = 'project' AND task.parent_id = project.id)
   OR (task.parent_type = 'phase' AND EXISTS (
     SELECT 1 FROM phases phase
     WHERE phase.id = task.parent_id AND phase.project_id = project.id
       AND phase.msp_id = task.msp_id AND phase.client_id = task.client_id
   ))
 )
JOIN object_tag_assignments assignment
  ON assignment.msp_id = project.msp_id AND assignment.client_id = project.client_id
 AND assignment.object_type = 'project' AND assignment.object_id = project.id
JOIN tags tag ON tag.id = assignment.tag_id AND tag.msp_id = assignment.msp_id
WHERE task.id = $1 AND task.msp_id = $2 AND task.client_id = $3
ORDER BY assignment.assigned_at, assignment.id
`, target.ObjectID, target.MSPID, target.ClientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	assignments := []tagging.Assignment{}
	for rows.Next() {
		assignment, err := scanInheritedAssignment(rows)
		if err != nil {
			return nil, err
		}
		assignments = append(assignments, assignment)
	}
	return assignments, rows.Err()
}

// lockTaskInheritedSource follows the catalog/project association lock order:
// project first, then its tag rows by ID. Catalog lifecycle mutations acquire
// those tag locks before rewriting assignments, so the following reload is an
// authoritative classification snapshot rather than stale inherited evidence.
func lockTaskInheritedSource(ctx context.Context, tx transaction, target tagging.TargetRef) error {
	if target.ObjectType != tagging.ObjectTask {
		return nil
	}
	var parentType, parentID string
	err := tx.QueryRow(ctx, `SELECT parent_type, parent_id::text FROM tasks WHERE id=$1 AND msp_id=$2 AND client_id=$3`, target.ObjectID, target.MSPID, target.ClientID).Scan(&parentType, &parentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return scope.ErrNotFound
	}
	if err != nil {
		return err
	}
	if parentType == "work_record" || parentType == "opportunity" {
		return nil
	}
	var projectID string
	if parentType == "project" {
		err = tx.QueryRow(ctx, `SELECT id::text FROM projects WHERE id=$1 AND msp_id=$2 AND client_id=$3 FOR UPDATE`, parentID, target.MSPID, target.ClientID).Scan(&projectID)
	} else if parentType == "phase" {
		err = tx.QueryRow(ctx, `SELECT project.id::text FROM phases phase JOIN projects project ON project.id=phase.project_id AND project.msp_id=phase.msp_id AND project.client_id=phase.client_id WHERE phase.id=$1 AND phase.msp_id=$2 AND phase.client_id=$3 FOR UPDATE OF project`, parentID, target.MSPID, target.ClientID).Scan(&projectID)
	} else {
		return scope.ErrNotFound
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return scope.ErrNotFound
	}
	if err != nil {
		return err
	}
	return nil
}

func scanAssignment(scanner interface{ Scan(...any) error }) (tagging.Assignment, error) {
	var assignment tagging.Assignment
	var tag tagging.Tag
	err := scanner.Scan(
		&assignment.ID, &assignment.Source, &assignment.AssignedAt,
		&assignment.AssignedBy, &tag.ID, &tag.MSPID, &tag.InternalKey,
		&tag.Label, &tag.GroupID, &tag.Description, &tag.Color, &tag.State,
		&tag.SystemManaged, &tag.MergedIntoTagID, &tag.Version,
	)
	if err != nil {
		return tagging.Assignment{}, err
	}
	tag.Synonyms = []string{}
	assignment.Tag = tag
	return assignment, nil
}

func scanInheritedAssignment(scanner interface{ Scan(...any) error }) (tagging.Assignment, error) {
	var assignment tagging.Assignment
	var tag tagging.Tag
	var projectID string
	err := scanner.Scan(
		&assignment.ID, &assignment.Source, &assignment.AssignedAt,
		&assignment.AssignedBy, &tag.ID, &tag.MSPID, &tag.InternalKey,
		&tag.Label, &tag.GroupID, &tag.Description, &tag.Color, &tag.State,
		&tag.SystemManaged, &tag.MergedIntoTagID, &tag.Version, &projectID,
	)
	if err != nil {
		return tagging.Assignment{}, err
	}
	tag.Synonyms = []string{}
	assignment.Tag = tag
	assignment.Inherited = true
	assignment.SourceObjectType = tagging.ObjectProject
	assignment.SourceObjectID = projectID
	return assignment, nil
}

func scanHistoryEntry(scanner interface{ Scan(...any) error }) (tagging.HistoryEntry, error) {
	var entry tagging.HistoryEntry
	var assignment tagging.Assignment
	var tag tagging.Tag
	err := scanner.Scan(
		&entry.ID, &entry.Operation, &entry.TargetVersion, &entry.OccurredAt,
		&entry.ActorID, &entry.CorrelationID, &entry.CausationID, &entry.Inherited,
		&entry.SourceObjectType, &entry.SourceObjectID, &assignment.ID,
		&assignment.Source, &assignment.AssignedAt, &assignment.AssignedBy,
		&tag.ID, &tag.MSPID, &tag.InternalKey, &tag.Label, &tag.GroupID,
		&tag.Description, &tag.Color, &tag.State, &tag.SystemManaged,
		&tag.MergedIntoTagID, &tag.Version,
	)
	if err != nil {
		return tagging.HistoryEntry{}, err
	}
	tag.Synonyms = []string{}
	assignment.Tag = tag
	assignment.Inherited = entry.Inherited
	assignment.SourceObjectType = entry.SourceObjectType
	assignment.SourceObjectID = entry.SourceObjectID
	entry.Assignment = assignment
	return entry, nil
}

func scopedAssociationKey(target tagging.TargetRef, key string) string {
	return strings.Join([]string{
		"classification-association", target.MSPID, target.ClientID,
		target.ObjectType.String(), target.ObjectID, key,
	}, ":")
}

func lockTagMutationNamespace(
	ctx context.Context,
	tx transaction,
	mspID string,
) error {
	var locked any
	return tx.QueryRow(
		ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
		"classification-tag-mutation:"+mspID,
	).Scan(&locked)
}

func (r *TaggingRepository) Get(
	ctx context.Context,
	target tagging.TargetRef,
) (tagging.TaggedObject, error) {
	return getTaggedObject(ctx, r.db, target, false)
}

func (r *TaggingRepository) ResolveTaskProject(ctx context.Context, mspID, clientID, parentType, parentID string) (tagging.TargetRef, error) {
	query := ""
	switch parentType {
	case "project":
		query = `SELECT id::text FROM projects WHERE id = $1 AND msp_id = $2 AND client_id = $3`
	case "phase":
		query = `SELECT project_id::text FROM phases WHERE id = $1 AND msp_id = $2 AND client_id = $3`
	default:
		return tagging.TargetRef{}, tagging.ErrInvalidAssociation
	}
	var projectID string
	if err := r.db.QueryRow(ctx, query, parentID, mspID, clientID).Scan(&projectID); errors.Is(err, pgx.ErrNoRows) {
		return tagging.TargetRef{}, scope.ErrNotFound
	} else if err != nil {
		return tagging.TargetRef{}, err
	}
	return tagging.TargetRef{MSPID: mspID, ClientID: clientID, ObjectType: tagging.ObjectProject, ObjectID: projectID}, nil
}

func (r *TaggingRepository) ResolveTags(
	ctx context.Context,
	mspID string,
	ids []string,
) ([]tagging.Tag, error) {
	if len(ids) == 0 {
		return []tagging.Tag{}, nil
	}
	rows, err := r.db.Query(ctx, `
WITH RECURSIVE requested(requested_id) AS (
  SELECT DISTINCT value::uuid FROM unnest($2::text[]) AS value
), chain(requested_id, tag_id, depth) AS (
  SELECT requested_id, requested_id, 0 FROM requested
  UNION ALL
  SELECT chain.requested_id, tag.merged_into_id, chain.depth + 1
  FROM chain
  JOIN tags tag ON tag.id = chain.tag_id AND tag.msp_id = $1
  WHERE tag.lifecycle_state = 'merged'
    AND tag.merged_into_id IS NOT NULL
    AND chain.depth < 32
)
SELECT DISTINCT ON (chain.requested_id)
       chain.requested_id::text,
       tag.id::text, tag.msp_id::text, tag.internal_key, tag.label,
       tag.group_id::text, tag.description, COALESCE(tag.color, ''),
       tag.lifecycle_state, tag.system_tag,
       COALESCE(tag.merged_into_id::text, ''), tag.version
FROM chain
JOIN tags tag ON tag.id = chain.tag_id AND tag.msp_id = $1
WHERE tag.lifecycle_state <> 'merged'
ORDER BY chain.requested_id, chain.depth DESC
`, mspID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]tagging.Tag, 0, len(ids))
	seen := map[string]struct{}{}
	resolved := 0
	for rows.Next() {
		var requested string
		var tag tagging.Tag
		if err := rows.Scan(
			&requested, &tag.ID, &tag.MSPID, &tag.InternalKey, &tag.Label,
			&tag.GroupID, &tag.Description, &tag.Color, &tag.State,
			&tag.SystemManaged, &tag.MergedIntoTagID, &tag.Version,
		); err != nil {
			return nil, err
		}
		resolved++
		if _, exists := seen[tag.ID]; !exists {
			seen[tag.ID] = struct{}{}
			tag.Synonyms = []string{}
			result = append(result, tag)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if resolved != len(ids) {
		return nil, scope.ErrNotFound
	}
	return result, nil
}

func (r *TaggingRepository) Accepted(
	ctx context.Context,
	target tagging.TargetRef,
	idempotencyKey string,
) (tagging.TaggedObject, bool, error) {
	return acceptedAssociationOperation(
		ctx, r.db, target, idempotencyKey, false,
	)
}

func acceptedAssociationOperation(
	ctx context.Context,
	q taggingQueryer,
	target tagging.TargetRef,
	idempotencyKey string,
	forUpdate bool,
) (tagging.TaggedObject, bool, error) {
	lock := ""
	if forUpdate {
		lock = " FOR UPDATE"
	}
	var response []byte
	err := q.QueryRow(ctx, `
SELECT response
FROM classification_tag_operations
WHERE msp_id = $1 AND object_type = $2 AND object_id = $3
  AND (($4 = '' AND client_id IS NULL) OR client_id = NULLIF($4, '')::uuid)
  AND idempotency_key = $5`+lock,
		target.MSPID, target.ObjectType.String(), target.ObjectID, target.ClientID,
		idempotencyKey,
	).Scan(&response)
	if errors.Is(err, pgx.ErrNoRows) {
		return tagging.TaggedObject{}, false, nil
	}
	if err != nil {
		return tagging.TaggedObject{}, false, err
	}
	var accepted tagging.TaggedObject
	if err := json.Unmarshal(response, &accepted); err != nil {
		return tagging.TaggedObject{}, false, err
	}
	return accepted, true, nil
}

func (r *TaggingRepository) History(
	ctx context.Context,
	target tagging.TargetRef,
) ([]tagging.HistoryEntry, error) {
	rows, err := r.db.Query(ctx, `
WITH movement_windows AS (
  SELECT movement.task_id, movement.msp_id, movement.client_id,
         movement.from_parent_type, movement.from_parent_id,
         movement.to_parent_type, movement.to_parent_id, movement.moved_at,
         lead(movement.moved_at) OVER (
           PARTITION BY movement.task_id ORDER BY movement.moved_at, movement.id
         ) AS ends_at,
         row_number() OVER (
           PARTITION BY movement.task_id ORDER BY movement.moved_at, movement.id
         ) AS sequence
  FROM task_movement_history movement
), task_intervals AS (
  SELECT task.id AS task_id, task.msp_id, task.client_id,
         COALESCE(first_move.from_parent_type, task.parent_type) AS parent_type,
         COALESCE(first_move.from_parent_id, task.parent_id) AS parent_id,
         task.created_at AS started_at, first_move.moved_at AS ends_at
  FROM tasks task
  LEFT JOIN movement_windows first_move
    ON first_move.task_id = task.id AND first_move.sequence = 1
  UNION ALL
  SELECT movement.task_id, movement.msp_id, movement.client_id,
         movement.to_parent_type, movement.to_parent_id,
         movement.moved_at, movement.ends_at
  FROM movement_windows movement
)
SELECT event.id::text, event.operation, event.target_version, event.occurred_at,
       COALESCE(event.actor_id::text, ''), event.correlation_id::text,
       COALESCE(event.causation_id::text, ''), false, '', '',
       COALESCE(assignment.id::text, event.assignment_id::text), event.assignment_source,
       COALESCE(assignment.assigned_at, event.occurred_at),
       COALESCE(assignment.assigned_by::text, event.actor_id::text, ''),
       tag.id::text, tag.msp_id::text, tag.internal_key, tag.label,
       tag.group_id::text, tag.description, COALESCE(tag.color, ''),
       tag.lifecycle_state, tag.system_tag,
       COALESCE(tag.merged_into_id::text, ''), tag.version
FROM tag_assignment_events event
LEFT JOIN object_tag_assignments assignment
  ON assignment.id = event.assignment_id AND assignment.msp_id = event.msp_id
JOIN tags tag ON tag.id = event.tag_id AND tag.msp_id = event.msp_id
WHERE event.msp_id = $1 AND event.object_type = $2 AND event.object_id = $3
  AND (($4 = '' AND event.client_id IS NULL)
       OR event.client_id = NULLIF($4, '')::uuid)
UNION ALL
SELECT event.id::text, event.operation, event.target_version, event.occurred_at,
       COALESCE(event.actor_id::text, ''), event.correlation_id::text,
       COALESCE(event.causation_id::text, ''), true, 'project', project.id::text,
       COALESCE(assignment.id::text, event.assignment_id::text), event.assignment_source,
       COALESCE(assignment.assigned_at, event.occurred_at),
       COALESCE(assignment.assigned_by::text, event.actor_id::text, ''),
       tag.id::text, tag.msp_id::text, tag.internal_key, tag.label,
       tag.group_id::text, tag.description, COALESCE(tag.color, ''),
       tag.lifecycle_state, tag.system_tag,
       COALESCE(tag.merged_into_id::text, ''), tag.version
FROM task_intervals interval
JOIN projects project
  ON project.msp_id = interval.msp_id AND project.client_id = interval.client_id
 AND (
   (interval.parent_type = 'project' AND interval.parent_id = project.id)
   OR (interval.parent_type = 'phase' AND EXISTS (
     SELECT 1 FROM phases phase
     WHERE phase.id = interval.parent_id AND phase.project_id = project.id
       AND phase.msp_id = interval.msp_id AND phase.client_id = interval.client_id
   ))
 )
JOIN tag_assignment_events event
  ON event.msp_id = project.msp_id AND event.client_id = project.client_id
 AND event.object_type = 'project' AND event.object_id = project.id
LEFT JOIN object_tag_assignments assignment
  ON assignment.id = event.assignment_id AND assignment.msp_id = event.msp_id
JOIN tags tag ON tag.id = event.tag_id AND tag.msp_id = event.msp_id
WHERE $2 = 'task' AND interval.task_id = $3 AND interval.msp_id = $1
  AND interval.client_id = $4::uuid
  AND event.occurred_at >= interval.started_at
  AND (interval.ends_at IS NULL OR event.occurred_at < interval.ends_at)
	  AND COALESCE((
    SELECT direct_event.operation
    FROM tag_assignment_events direct_event
    WHERE direct_event.msp_id = interval.msp_id AND direct_event.client_id = interval.client_id
      AND direct_event.object_type = 'task' AND direct_event.object_id = interval.task_id
      AND direct_event.tag_id = event.tag_id AND direct_event.occurred_at <= event.occurred_at
    ORDER BY direct_event.occurred_at DESC, direct_event.id DESC
    LIMIT 1
  ), 'removed') <> 'added'
ORDER BY 4, 1
`, target.MSPID, target.ObjectType.String(), target.ObjectID, target.ClientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := []tagging.HistoryEntry{}
	for rows.Next() {
		entry, err := scanHistoryEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (r *TaggingRepository) ReplaceDirect(
	ctx context.Context,
	accepted tagging.AssociationMutation,
) (tagging.TaggedObject, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return tagging.TaggedObject{}, err
	}
	target := accepted.Before.Target
	// Catalog lifecycle and association writers share this MSP-scoped
	// transaction lock before acquiring any target or tag row. That prevents a
	// merge from introducing a canonical survivor after an association's
	// sorted prelock snapshot and removes cross-writer tag-lock cycles.
	if err := lockTagMutationNamespace(ctx, tx, target.MSPID); err != nil {
		_ = tx.Rollback(ctx)
		return tagging.TaggedObject{}, err
	}
	// A missing operation row cannot be locked. Serialize the stable target/key
	// namespace before the replay lookup so concurrent retries re-read the
	// accepted response instead of racing the optimistic object version.
	if err := tx.QueryRow(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, scopedAssociationKey(target, accepted.IdempotencyKey)).Scan(new(any)); err != nil {
		_ = tx.Rollback(ctx)
		return tagging.TaggedObject{}, err
	}
	if replay, found, err := acceptedAssociationOperation(
		ctx, tx, target, accepted.IdempotencyKey, true,
	); err != nil {
		_ = tx.Rollback(ctx)
		return tagging.TaggedObject{}, err
	} else if found {
		_ = tx.Rollback(ctx)
		return replay, nil
	}
	current, err := getTaggedObject(ctx, tx, target, true)
	if err != nil {
		_ = tx.Rollback(ctx)
		return tagging.TaggedObject{}, err
	}
	version, err := lockClassificationObjectVersion(
		ctx, tx, target, current.ObjectVersion,
	)
	if err != nil {
		_ = tx.Rollback(ctx)
		return tagging.TaggedObject{}, err
	}
	current.ObjectVersion = version
	if current.ObjectVersion != accepted.ExpectedObjectVersion {
		_ = tx.Rollback(ctx)
		return tagging.TaggedObject{}, object.ErrVersionConflict
	}
	if err := lockTaskInheritedSource(ctx, tx, target); err != nil {
		_ = tx.Rollback(ctx)
		return tagging.TaggedObject{}, err
	}
	lockIDs := make([]string, 0, len(accepted.Direct)+len(current.Inherited))
	for _, assignment := range accepted.Direct {
		lockIDs = append(lockIDs, assignment.Tag.ID)
	}
	for _, assignment := range current.Inherited {
		lockIDs = append(lockIDs, assignment.Tag.ID)
	}
	if err := lockCatalogTags(ctx, tx, target.MSPID, lockIDs...); err != nil {
		_ = tx.Rollback(ctx)
		return tagging.TaggedObject{}, err
	}
	if target.ObjectType == tagging.ObjectTask {
		current.Inherited, err = loadInheritedAssignments(ctx, tx, target)
		if err != nil {
			_ = tx.Rollback(ctx)
			return tagging.TaggedObject{}, err
		}
		current = normalizeTaggingObject(current)
	}
	key := scopedAssociationKey(target, accepted.IdempotencyKey)
	accepted.Direct, err = resolveActiveDirectAssignments(ctx, tx, target.MSPID, accepted.Direct)
	if err != nil {
		_ = tx.Rollback(ctx)
		return tagging.TaggedObject{}, err
	}
	currentIDs := assignmentIDs(current.Direct)
	desiredIDs := assignmentIDs(accepted.Direct)
	if accepted.Source == tagging.SourceAIAutomatic {
		for tagID := range currentIDs {
			if _, retained := desiredIDs[tagID]; !retained {
				_ = tx.Rollback(ctx)
				return tagging.TaggedObject{}, tagging.ErrAutomaticReplacement
			}
		}
	}
	if !meaningfulTagAssignments(accepted.Direct, current.Inherited) {
		_ = tx.Rollback(ctx)
		return tagging.TaggedObject{}, tagging.ErrMeaningfulTagRequired
	}
	if sameTagSet(currentIDs, desiredIDs) {
		if err := persistAcceptedAssociationOperation(ctx, tx, target, accepted, current); err != nil {
			_ = tx.Rollback(ctx)
			return tagging.TaggedObject{}, err
		}
		facts := associationFactsAtVersion(accepted, current.ObjectVersion)
		if err := writeMutationFacts(ctx, tx, facts.Audit, facts.Event); err != nil {
			_ = tx.Rollback(ctx)
			return tagging.TaggedObject{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			_ = tx.Rollback(ctx)
			return tagging.TaggedObject{}, err
		}
		return current, nil
	}
	nextVersion := current.ObjectVersion + 1
	if err := updateTaggingTargetVersion(ctx, tx, target, current.ObjectVersion); err != nil {
		_ = tx.Rollback(ctx)
		return tagging.TaggedObject{}, err
	}
	evidence, err := json.Marshal(map[string]string{
		"reason": accepted.Reason, "correlation_id": accepted.CorrelationID,
		"causation_id":    accepted.CausationID,
		"idempotency_key": accepted.IdempotencyKey,
	})
	if err != nil {
		_ = tx.Rollback(ctx)
		return tagging.TaggedObject{}, err
	}
	for _, assignment := range current.Direct {
		if _, keep := desiredIDs[assignment.Tag.ID]; keep {
			continue
		}
		if err := writeTagAssignmentEvent(
			ctx, tx, target, assignment, "removed", nextVersion,
			accepted, key+":removed:"+assignment.Tag.ID, evidence,
		); err != nil {
			_ = tx.Rollback(ctx)
			return tagging.TaggedObject{}, err
		}
		if _, err := tx.Exec(ctx, `
DELETE FROM object_tag_assignments
WHERE id = $1 AND msp_id = $2 AND object_type = $3 AND object_id = $4
`, assignment.ID, target.MSPID, target.ObjectType.String(), target.ObjectID); err != nil {
			_ = tx.Rollback(ctx)
			return tagging.TaggedObject{}, err
		}
	}
	for _, assignment := range accepted.Direct {
		if _, exists := currentIDs[assignment.Tag.ID]; exists {
			continue
		}
		assignment.ID = associationAssignmentID(target, assignment.Tag.ID)
		assignment.AssignedAt = accepted.Audit.OccurredAt
		assignment.AssignedBy = accepted.Audit.ActorID
		if assignment.Source == tagging.SourceSystemFallback {
			assignment.AssignedBy = ""
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO object_tag_assignments (
  id, msp_id, client_id, object_type, object_id, object_version,
  tag_id, assignment_source, assigned_at, assigned_by, evidence, version
) VALUES (
  $1::uuid, $2::uuid, NULLIF($3, '')::uuid, $4, $5::uuid, $6,
  $7::uuid, $8, $9, NULLIF($10, '')::uuid, $11::jsonb, 1
)
`, assignment.ID, target.MSPID, target.ClientID, target.ObjectType.String(),
			target.ObjectID, nextVersion, assignment.Tag.ID, assignment.Source.String(),
			accepted.Audit.OccurredAt, assignment.AssignedBy, evidence); err != nil {
			_ = tx.Rollback(ctx)
			return tagging.TaggedObject{}, classifyTaggingWriteError(err)
		}
		if err := writeTagAssignmentEvent(
			ctx, tx, target, assignment, "added", nextVersion, accepted,
			key+":added:"+assignment.Tag.ID, evidence,
		); err != nil {
			_ = tx.Rollback(ctx)
			return tagging.TaggedObject{}, err
		}
	}
	persistedDirect, err := loadDirectAssignments(ctx, tx, target)
	if err != nil {
		_ = tx.Rollback(ctx)
		return tagging.TaggedObject{}, err
	}
	next := normalizeTaggingObject(tagging.TaggedObject{
		Target: target, ObjectVersion: nextVersion, Direct: persistedDirect,
		Inherited: current.Inherited,
	})
	if err := writeTagMutationOutbox(ctx, tx, current, next, accepted); err != nil {
		_ = tx.Rollback(ctx)
		return tagging.TaggedObject{}, err
	}
	if err := persistAcceptedAssociationOperation(ctx, tx, target, accepted, next); err != nil {
		_ = tx.Rollback(ctx)
		return tagging.TaggedObject{}, err
	}
	if err := writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event); err != nil {
		_ = tx.Rollback(ctx)
		return tagging.TaggedObject{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return tagging.TaggedObject{}, err
	}
	return next, nil
}

func writeTagMutationOutbox(ctx context.Context, tx transaction, before, after tagging.TaggedObject, accepted tagging.AssociationMutation) error {
	directIDs := assignmentTagIDs(after.Direct)
	effectiveIDs := assignmentTagIDs(after.Effective)
	groupIDs := assignmentGroupIDs(after.Effective)
	directJSON, _ := json.Marshal(directIDs)
	effectiveJSON, _ := json.Marshal(effectiveIDs)
	groupJSON, _ := json.Marshal(groupIDs)
	beforeIDs, afterIDs := assignmentIDs(before.Direct), assignmentIDs(after.Direct)
	write := func(eventType, tagID string, source tagging.Source) error {
		_, err := tx.Exec(ctx, `
INSERT INTO event_outbox (event_id,event_type,schema_version,occurred_at,msp_id,client_id,actor_type,actor_id,subject_type,subject_id,subject_version,correlation_id,causation_id,source,data)
VALUES (
  md5($5::text || ':' || COALESCE($6::text, '') || ':' || $9::text || ':' ||
      $10::text || ':' || $2::text || ':' || $1::text || ':' || $3::text)::uuid,
  $1::text,1,$4::timestamptz,$5::uuid,NULLIF($6::text,'')::uuid,
  $7::text,NULLIF($8::text,'')::uuid,$9::text,$10::uuid,$11::bigint,
  $12::uuid,NULLIF($13::text,'')::uuid,$14::text,
  jsonb_build_object(
    'tag_id',$3::text,
    'direct_tag_ids',$15::jsonb,
    'effective_tag_ids',$16::jsonb,
    'group_ids',$17::jsonb,
    'assignment_source',$14::text
  )
)
ON CONFLICT (event_id) DO NOTHING
`, eventType, accepted.IdempotencyKey, tagID, accepted.Audit.OccurredAt, after.Target.MSPID, after.Target.ClientID, accepted.Event.ActorType, accepted.Event.ActorID, after.Target.ObjectType.String(), after.Target.ObjectID, after.ObjectVersion, accepted.CorrelationID, accepted.CausationID, source.String(), directJSON, effectiveJSON, groupJSON)
		return err
	}
	for _, assignment := range before.Direct {
		if _, retained := afterIDs[assignment.Tag.ID]; !retained {
			if err := write("tag.removed", assignment.Tag.ID, assignment.Source); err != nil {
				return err
			}
		}
	}
	for _, assignment := range after.Direct {
		if _, existed := beforeIDs[assignment.Tag.ID]; !existed {
			if err := write("tag.added", assignment.Tag.ID, assignment.Source); err != nil {
				return err
			}
		}
	}
	return nil
}

func assignmentTagIDs(assignments []tagging.Assignment) []string {
	result := make([]string, 0, len(assignments))
	for _, a := range assignments {
		result = append(result, a.Tag.ID)
	}
	return result
}
func assignmentGroupIDs(assignments []tagging.Assignment) []string {
	seen := map[string]struct{}{}
	result := []string{}
	for _, a := range assignments {
		if a.Tag.GroupID != "" {
			if _, ok := seen[a.Tag.GroupID]; !ok {
				seen[a.Tag.GroupID] = struct{}{}
				result = append(result, a.Tag.GroupID)
			}
		}
	}
	return result
}

func associationFactsAtVersion(
	accepted tagging.AssociationMutation,
	version int64,
) tagging.AssociationMutation {
	accepted.Audit.SubjectVersion = version
	accepted.Event.SubjectVersion = version
	return accepted
}

func updateTaggingTargetVersion(
	ctx context.Context,
	tx transaction,
	target tagging.TargetRef,
	expectedVersion int64,
) error {
	result, err := tx.Exec(ctx, `
UPDATE classification_object_versions
SET version = version + 1
WHERE msp_id = $1 AND object_type = $2 AND object_id = $3
  AND (($4 = '' AND client_id IS NULL) OR client_id = NULLIF($4, '')::uuid)
  AND version = $5
`, target.MSPID, target.ObjectType.String(), target.ObjectID, target.ClientID,
		expectedVersion)
	if err != nil {
		return classifyTaggingWriteError(err)
	}
	if result.RowsAffected() != 1 {
		return object.ErrVersionConflict
	}
	return nil
}

func resolveActiveDirectAssignments(
	ctx context.Context,
	tx transaction,
	mspID string,
	direct []tagging.Assignment,
) ([]tagging.Assignment, error) {
	resolved := make([]tagging.Assignment, 0, len(direct))
	seen := map[string]struct{}{}
	for _, assignment := range direct {
		tagID := assignment.Tag.ID
		for depth := 0; depth < 32; depth++ {
			var tag tagging.Tag
			err := tx.QueryRow(ctx, `
SELECT id::text, msp_id::text, internal_key, label, group_id::text,
       description, COALESCE(color, ''), lifecycle_state, system_tag,
       COALESCE(merged_into_id::text, ''), version
FROM tags WHERE id = $1 AND msp_id = $2 FOR UPDATE
`, tagID, mspID).Scan(&tag.ID, &tag.MSPID, &tag.InternalKey, &tag.Label,
				&tag.GroupID, &tag.Description, &tag.Color, &tag.State,
				&tag.SystemManaged, &tag.MergedIntoTagID, &tag.Version)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, scope.ErrNotFound
			}
			if err != nil {
				return nil, err
			}
			if tag.State == tagging.StateMerged && tag.MergedIntoTagID != "" {
				tagID = tag.MergedIntoTagID
				continue
			}
			if tag.State != tagging.StateActive {
				return nil, tagging.ErrInactiveTarget
			}
			if _, exists := seen[tag.ID]; !exists {
				seen[tag.ID] = struct{}{}
				tag.Synonyms = []string{}
				assignment.Tag = tag
				resolved = append(resolved, assignment)
			}
			break
		}
	}
	return resolved, nil
}

func meaningfulTagAssignments(
	direct, inherited []tagging.Assignment,
) bool {
	for _, assignment := range append(append([]tagging.Assignment{}, direct...), inherited...) {
		if assignment.Tag.State == tagging.StateActive &&
			assignment.Tag.InternalKey != "taxonomy.system.unclassified" {
			return true
		}
	}
	return false
}

func persistAcceptedAssociationOperation(
	ctx context.Context,
	tx transaction,
	target tagging.TargetRef,
	accepted tagging.AssociationMutation,
	response tagging.TaggedObject,
) error {
	body, err := json.Marshal(response)
	if err != nil {
		return err
	}
	operationID := uuid.NewMD5(uuid.Nil, []byte(
		scopedAssociationKey(target, accepted.IdempotencyKey),
	)).String()
	_, err = tx.Exec(ctx, `
INSERT INTO classification_tag_operations (
  id, msp_id, client_id, object_type, object_id, idempotency_key,
  accepted_object_version, response, accepted_at
) VALUES (
  $1::uuid, $2::uuid, NULLIF($3, '')::uuid, $4, $5::uuid, $6,
  $7, $8::jsonb, $9
)
`, operationID, target.MSPID, target.ClientID, target.ObjectType.String(),
		target.ObjectID, accepted.IdempotencyKey, response.ObjectVersion, body,
		accepted.Audit.OccurredAt)
	return err
}

func writeTagAssignmentEvent(
	ctx context.Context,
	tx transaction,
	target tagging.TargetRef,
	assignment tagging.Assignment,
	operation string,
	targetVersion int64,
	accepted tagging.AssociationMutation,
	idempotencyKey string,
	evidence []byte,
) error {
	_, err := tx.Exec(ctx, `
INSERT INTO tag_assignment_events (
  id, msp_id, client_id, assignment_id, object_type, object_id,
  target_version, tag_id, operation, assignment_source,
  actor_type, actor_id, occurred_at, evidence,
  idempotency_key, correlation_id, causation_id
) VALUES (
  md5($1 || ':' || $2)::uuid, $3::uuid, NULLIF($4, '')::uuid,
  $5::uuid, $6, $7::uuid, $8, $9::uuid, $10, $11,
  $12, NULLIF($13, '')::uuid, $14, $15::jsonb, $1, $16::uuid,
  NULLIF($17, '')::uuid
)
`, idempotencyKey, operation, target.MSPID, target.ClientID, assignment.ID,
		target.ObjectType.String(), target.ObjectID, targetVersion, assignment.Tag.ID,
		operation, assignment.Source.String(), accepted.Audit.ActorType,
		accepted.Audit.ActorID, accepted.Audit.OccurredAt, evidence,
		accepted.CorrelationID, accepted.CausationID)
	return err
}

func associationAssignmentID(target tagging.TargetRef, tagID string) string {
	seed := fmt.Sprintf("%s:%s:%s:%s:%s", target.MSPID, target.ClientID,
		target.ObjectType, target.ObjectID, tagID)
	return uuid.NewMD5(uuid.Nil, []byte(seed)).String()
}

func assignmentIDs(assignments []tagging.Assignment) map[string]struct{} {
	ids := make(map[string]struct{}, len(assignments))
	for _, assignment := range assignments {
		ids[assignment.Tag.ID] = struct{}{}
	}
	return ids
}

func sameTagSet(left, right map[string]struct{}) bool {
	if len(left) != len(right) {
		return false
	}
	for id := range left {
		if _, exists := right[id]; !exists {
			return false
		}
	}
	return true
}

func (r *TaggingRepository) EnsureSystemCatalog(
	ctx context.Context,
	accepted tagging.SystemCatalogMutation,
) (tagging.Tag, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return tagging.Tag{}, err
	}
	group, tag := accepted.Group, accepted.Tag
	if _, err := tx.Exec(ctx, `
INSERT INTO tag_groups (
  id, msp_id, internal_key, label, description, position,
  lifecycle_state, system_group, version,
  created_at, created_by, updated_at, updated_by
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, true, 1,
  $8, $9, $8, $9
)
ON CONFLICT (msp_id, internal_key) DO NOTHING
`, group.ID, group.MSPID, group.InternalKey, group.Label,
		group.Description, group.Position, group.State,
		accepted.Audit.OccurredAt, accepted.Audit.ActorID); err != nil {
		_ = tx.Rollback(ctx)
		return tagging.Tag{}, fmt.Errorf(
			"ensure classification system group: %w",
			classifyTaggingWriteError(err),
		)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO tags (
  id, msp_id, group_id, internal_key, label, description,
  lifecycle_state, system_tag, version,
  created_at, created_by, updated_at, updated_by
)
SELECT
  $1, $2, tag_group.id, $3, $4, $5,
  $6, true, 1, $7, $8, $7, $8
FROM tag_groups tag_group
WHERE tag_group.msp_id = $2 AND tag_group.internal_key = 'taxonomy.system'
ON CONFLICT (msp_id, internal_key) DO NOTHING
`, tag.ID, tag.MSPID, tag.InternalKey, tag.Label, tag.Description,
		tag.State, accepted.Audit.OccurredAt,
		accepted.Audit.ActorID); err != nil {
		_ = tx.Rollback(ctx)
		return tagging.Tag{}, fmt.Errorf(
			"ensure unclassified tag: %w",
			classifyTaggingWriteError(err),
		)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO tag_ai_policies (
  id, msp_id, automatic_apply_enabled, automatic_apply_threshold,
  version, created_at, created_by, updated_at, updated_by
) VALUES (
  md5($1::uuid::text || ':classification:ai-policy')::uuid,
  $1::uuid, false, 0.950, 1, $2, $3, $2, $3
)
ON CONFLICT (msp_id) DO NOTHING
`, tag.MSPID, accepted.Audit.OccurredAt,
		accepted.Audit.ActorID); err != nil {
		_ = tx.Rollback(ctx)
		return tagging.Tag{}, fmt.Errorf("ensure tag AI policy: %w", err)
	}
	var ensured tagging.Tag
	if err := tx.QueryRow(ctx, `
SELECT id::text, msp_id::text, internal_key, label, group_id::text,
       description, COALESCE(color, ''), lifecycle_state, system_tag,
       COALESCE(merged_into_id::text, ''), version
FROM tags
WHERE msp_id = $1 AND internal_key = 'taxonomy.system.unclassified'
`, tag.MSPID).Scan(
		&ensured.ID, &ensured.MSPID, &ensured.InternalKey,
		&ensured.Label, &ensured.GroupID, &ensured.Description,
		&ensured.Color, &ensured.State, &ensured.SystemManaged,
		&ensured.MergedIntoTagID, &ensured.Version,
	); err != nil {
		_ = tx.Rollback(ctx)
		return tagging.Tag{}, fmt.Errorf("read ensured unclassified tag: %w", err)
	}
	ensured.Synonyms = []string{}
	accepted.Audit.SubjectID = ensured.ID
	accepted.Event.SubjectID = ensured.ID
	if err := writeMutationFacts(
		ctx, tx, accepted.Audit, accepted.Event,
	); err != nil {
		_ = tx.Rollback(ctx)
		return tagging.Tag{}, fmt.Errorf("write system catalog facts: %w", err)
	}
	if err := commitAutomationRuntime(ctx, tx); err != nil {
		return tagging.Tag{}, err
	}
	return ensured, nil
}

func (r *TaggingRepository) List(
	ctx context.Context,
	mspID string,
) (tagging.Catalog, error) {
	groupRows, err := r.db.Query(ctx, `
SELECT id::text, msp_id::text, internal_key, label, description,
       position, lifecycle_state, system_group, version
FROM tag_groups
WHERE msp_id = $1
ORDER BY position, normalized_label, id
`, mspID)
	if err != nil {
		return tagging.Catalog{}, err
	}
	defer groupRows.Close()
	catalog := tagging.Catalog{Groups: []tagging.Group{}, Tags: []tagging.Tag{}}
	for groupRows.Next() {
		var group tagging.Group
		if err := groupRows.Scan(
			&group.ID, &group.MSPID, &group.InternalKey, &group.Label,
			&group.Description, &group.Position, &group.State,
			&group.SystemManaged, &group.Version,
		); err != nil {
			return tagging.Catalog{}, err
		}
		catalog.Groups = append(catalog.Groups, group)
	}
	if err := groupRows.Err(); err != nil {
		return tagging.Catalog{}, err
	}

	tagRows, err := r.db.Query(ctx, `
SELECT tag.id::text, tag.msp_id::text, tag.internal_key, tag.label,
       tag.group_id::text, tag.description, COALESCE(tag.color, ''),
       tag.lifecycle_state, tag.system_tag,
       COALESCE(tag.merged_into_id::text, ''), tag.version,
       COALESCE(array_agg(synonym.label ORDER BY synonym.normalized_label)
         FILTER (WHERE synonym.id IS NOT NULL), '{}'::text[])
FROM tags tag
LEFT JOIN tag_synonyms synonym
  ON synonym.tag_id = tag.id AND synonym.msp_id = tag.msp_id
WHERE tag.msp_id = $1
GROUP BY tag.id
ORDER BY tag.normalized_label, tag.id
`, mspID)
	if err != nil {
		return tagging.Catalog{}, err
	}
	defer tagRows.Close()
	for tagRows.Next() {
		var tag tagging.Tag
		if err := tagRows.Scan(
			&tag.ID, &tag.MSPID, &tag.InternalKey, &tag.Label,
			&tag.GroupID, &tag.Description, &tag.Color, &tag.State,
			&tag.SystemManaged, &tag.MergedIntoTagID, &tag.Version,
			&tag.Synonyms,
		); err != nil {
			return tagging.Catalog{}, err
		}
		catalog.Tags = append(catalog.Tags, tag)
	}
	if err := tagRows.Err(); err != nil {
		return tagging.Catalog{}, err
	}
	return catalog, nil
}

func (r *TaggingRepository) Health(
	ctx context.Context,
	target scope.Target,
) (tagging.Health, error) {
	rows, err := r.db.Query(ctx, `
SELECT assignment.object_type,
       count(DISTINCT assignment.object_id)
         FILTER (
           WHERE tag.lifecycle_state = 'active'
             AND tag.internal_key <> 'taxonomy.system.unclassified'
         ) AS meaningful,
       count(DISTINCT assignment.object_id)
         FILTER (
           WHERE tag.internal_key = 'taxonomy.system.unclassified'
         ) AS unclassified,
       count(DISTINCT assignment.object_id)
         FILTER (
           WHERE assignment.evidence->>'reason' = 'archive_fallback'
         ) AS archive_fallback
FROM object_tag_assignments assignment
JOIN tags tag
  ON tag.id = assignment.tag_id AND tag.msp_id = assignment.msp_id
WHERE assignment.msp_id = $1
  AND ($2::uuid IS NULL OR assignment.client_id = $2)
GROUP BY assignment.object_type
`, target.MSPID, nullableID(target.ClientID))
	if err != nil {
		return tagging.Health{}, err
	}
	defer rows.Close()
	health := tagging.Health{
		ByObjectType: map[tagging.ObjectType]tagging.ObjectHealth{},
	}
	for rows.Next() {
		var objectType tagging.ObjectType
		var objectHealth tagging.ObjectHealth
		if err := rows.Scan(
			&objectType, &objectHealth.Meaningful,
			&objectHealth.Unclassified, &objectHealth.ArchiveFallback,
		); err != nil {
			return tagging.Health{}, err
		}
		health.ByObjectType[objectType] = objectHealth
	}
	return health, rows.Err()
}

func (r *TaggingRepository) MigrationHistory(
	ctx context.Context,
	mspID string,
) ([]tagging.MigrationRun, error) {
	rows, err := r.db.Query(ctx, `
SELECT id::text, msp_id::text, started_at, completed_at, status,
       rows_discovered, rows_migrated, fallback_assignments,
       category_source_present
FROM classification_migration_runs
WHERE msp_id = $1
ORDER BY started_at DESC, id DESC
`, mspID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []tagging.MigrationRun{}
	for rows.Next() {
		var run tagging.MigrationRun
		if err := rows.Scan(
			&run.ID, &run.MSPID, &run.StartedAt, &run.CompletedAt,
			&run.Status, &run.RowsDiscovered, &run.RowsMigrated,
			&run.FallbackAssignments, &run.CategorySourcePresent,
		); err != nil {
			return nil, err
		}
		result = append(result, run)
	}
	return result, rows.Err()
}

func (r *TaggingRepository) FindGroup(
	ctx context.Context,
	mspID string,
	id string,
) (tagging.Group, error) {
	var group tagging.Group
	err := r.db.QueryRow(ctx, `
SELECT id::text, msp_id::text, internal_key, label, description,
       position, lifecycle_state, system_group, version
FROM tag_groups
WHERE id = $1 AND msp_id = $2
`, id, mspID).Scan(
		&group.ID, &group.MSPID, &group.InternalKey, &group.Label,
		&group.Description, &group.Position, &group.State,
		&group.SystemManaged, &group.Version,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return tagging.Group{}, scope.ErrNotFound
	}
	return group, err
}

func (r *TaggingRepository) FindTag(
	ctx context.Context,
	mspID string,
	id string,
) (tagging.Tag, error) {
	return r.findTag(ctx, `
WHERE tag.id = $1 AND tag.msp_id = $2
`, id, mspID)
}

func (r *TaggingRepository) FindUnclassified(
	ctx context.Context,
	mspID string,
) (tagging.Tag, error) {
	return r.findTag(ctx, `
WHERE tag.internal_key = 'taxonomy.system.unclassified'
  AND tag.msp_id = $1
`, mspID)
}

func (r *TaggingRepository) findTag(
	ctx context.Context,
	where string,
	args ...any,
) (tagging.Tag, error) {
	var tag tagging.Tag
	err := r.db.QueryRow(ctx, `
SELECT tag.id::text, tag.msp_id::text, tag.internal_key, tag.label,
       tag.group_id::text, tag.description, COALESCE(tag.color, ''),
       tag.lifecycle_state, tag.system_tag,
       COALESCE(tag.merged_into_id::text, ''), tag.version,
       COALESCE(array_agg(synonym.label ORDER BY synonym.normalized_label)
         FILTER (WHERE synonym.id IS NOT NULL), '{}'::text[])
FROM tags tag
LEFT JOIN tag_synonyms synonym
  ON synonym.tag_id = tag.id AND synonym.msp_id = tag.msp_id
`+where+`
GROUP BY tag.id
`, args...).Scan(
		&tag.ID, &tag.MSPID, &tag.InternalKey, &tag.Label,
		&tag.GroupID, &tag.Description, &tag.Color, &tag.State,
		&tag.SystemManaged, &tag.MergedIntoTagID, &tag.Version,
		&tag.Synonyms,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return tagging.Tag{}, scope.ErrNotFound
	}
	return tag, err
}

func (r *TaggingRepository) TermsAvailable(
	ctx context.Context,
	mspID string,
	excludeTagID string,
	normalizedTerms []string,
) error {
	var conflict bool
	err := r.db.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1
  FROM tags
  WHERE msp_id = $1
    AND normalized_label = ANY($3::text[])
    AND ($2 = '' OR id <> $2::uuid)
  UNION ALL
  SELECT 1
  FROM tag_synonyms
  WHERE msp_id = $1
    AND normalized_label = ANY($3::text[])
    AND ($2 = '' OR tag_id <> $2::uuid)
)
`, mspID, excludeTagID, normalizedTerms).Scan(&conflict)
	if err != nil {
		return err
	}
	if conflict {
		return tagging.ErrDuplicateTerm
	}
	return nil
}

func (r *TaggingRepository) CreateGroup(
	ctx context.Context,
	accepted tagging.CatalogMutation,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	group := accepted.Group
	if _, err := tx.Exec(ctx, `
INSERT INTO tag_groups (
  id, msp_id, internal_key, label, description, position,
  lifecycle_state, system_group, version,
  created_at, created_by, updated_at, updated_by
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8, $9,
  $10, $11, $10, $11
)
`, group.ID, group.MSPID, group.InternalKey, group.Label,
		group.Description, group.Position, group.State,
		group.SystemManaged, group.Version, accepted.Audit.OccurredAt,
		accepted.Audit.ActorID); err != nil {
		_ = tx.Rollback(ctx)
		return classifyTaggingWriteError(err)
	}
	return commitTaggingMutation(ctx, tx, accepted.Audit, accepted.Event)
}

func (r *TaggingRepository) UpdateGroup(
	ctx context.Context,
	accepted tagging.CatalogMutation,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	group := accepted.Group
	result, err := tx.Exec(ctx, `
UPDATE tag_groups
SET label = $4, description = $5, position = $6,
    lifecycle_state = $7, version = version + 1,
    updated_at = $8, updated_by = $9
WHERE id = $1 AND msp_id = $2 AND version = $3
`, group.ID, group.MSPID, accepted.ExpectedVersion,
		group.Label, group.Description, group.Position, group.State,
		accepted.Audit.OccurredAt, accepted.Audit.ActorID)
	if err != nil || result.RowsAffected() != 1 {
		_ = tx.Rollback(ctx)
		if err != nil {
			return classifyTaggingWriteError(err)
		}
		return object.ErrVersionConflict
	}
	return commitTaggingMutation(ctx, tx, accepted.Audit, accepted.Event)
}

func (r *TaggingRepository) CreateTag(
	ctx context.Context,
	accepted tagging.CatalogMutation,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	tag := accepted.Tag
	if _, err := tx.Exec(ctx, `
INSERT INTO tags (
  id, msp_id, group_id, internal_key, label, description, color,
  lifecycle_state, merged_into_id, system_tag, version,
  created_at, created_by, updated_at, updated_by
) VALUES (
  $1, $2, $3, $4, $5, $6, NULLIF($7, ''),
  $8, NULLIF($9, '')::uuid, $10, $11,
  $12, $13, $12, $13
)
`, tag.ID, tag.MSPID, tag.GroupID, tag.InternalKey, tag.Label,
		tag.Description, tag.Color, tag.State, tag.MergedIntoTagID,
		tag.SystemManaged, tag.Version, accepted.Audit.OccurredAt,
		accepted.Audit.ActorID); err != nil {
		_ = tx.Rollback(ctx)
		return classifyTaggingWriteError(err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO tag_synonyms (
  id, msp_id, tag_id, label, created_at, created_by
)
SELECT
  md5($1::uuid::text || ':synonym:' || lower(btrim(value)))::uuid,
  $2::uuid, $1::uuid, value, $4, $5
FROM unnest($3::text[]) AS value
`, tag.ID, tag.MSPID, tag.Synonyms, accepted.Audit.OccurredAt,
		accepted.Audit.ActorID); err != nil {
		_ = tx.Rollback(ctx)
		return classifyTaggingWriteError(err)
	}
	return commitTaggingMutation(ctx, tx, accepted.Audit, accepted.Event)
}

func (r *TaggingRepository) UpdateTag(
	ctx context.Context,
	accepted tagging.CatalogMutation,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	tag := accepted.Tag
	if _, err := tx.Exec(ctx, `
DELETE FROM tag_synonyms WHERE tag_id = $1 AND msp_id = $2
`, tag.ID, tag.MSPID); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	result, err := tx.Exec(ctx, `
UPDATE tags
SET group_id = $4, label = $5, description = $6,
    color = NULLIF($7, ''), version = version + 1,
    updated_at = $8, updated_by = $9
WHERE id = $1 AND msp_id = $2 AND version = $3
`, tag.ID, tag.MSPID, accepted.ExpectedVersion, tag.GroupID,
		tag.Label, tag.Description, tag.Color,
		accepted.Audit.OccurredAt, accepted.Audit.ActorID)
	if err != nil || result.RowsAffected() != 1 {
		_ = tx.Rollback(ctx)
		if err != nil {
			return classifyTaggingWriteError(err)
		}
		return object.ErrVersionConflict
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO tag_synonyms (
  id, msp_id, tag_id, label, created_at, created_by
)
SELECT
  md5($1::uuid::text || ':synonym:' || lower(btrim(value)))::uuid,
  $2::uuid, $1::uuid, value, $4, $5
FROM unnest($3::text[]) AS value
`, tag.ID, tag.MSPID, tag.Synonyms, accepted.Audit.OccurredAt,
		accepted.Audit.ActorID); err != nil {
		_ = tx.Rollback(ctx)
		return classifyTaggingWriteError(err)
	}
	return commitTaggingMutation(ctx, tx, accepted.Audit, accepted.Event)
}

func (r *TaggingRepository) PreviewImpact(
	ctx context.Context,
	mspID string,
	tagID string,
	operation tagging.ImpactOperation,
	replacementTagID string,
) (tagging.Impact, error) {
	rows, err := r.db.Query(ctx, `
SELECT assignment.object_type,
       count(*) AS affected_objects,
       count(*) FILTER (
         WHERE NOT EXISTS (
           SELECT 1
           FROM object_tag_assignments other_assignment
           JOIN tags other_tag
             ON other_tag.id = other_assignment.tag_id
            AND other_tag.msp_id = other_assignment.msp_id
           WHERE other_assignment.msp_id = assignment.msp_id
             AND other_assignment.object_type = assignment.object_type
             AND other_assignment.object_id = assignment.object_id
             AND other_assignment.tag_id <> assignment.tag_id
             AND other_tag.lifecycle_state = 'active'
             AND other_tag.internal_key <> 'taxonomy.system.unclassified'
         )
       ) AS fallback_objects
FROM object_tag_assignments assignment
WHERE assignment.msp_id = $1 AND assignment.tag_id = $2
GROUP BY assignment.object_type
`, mspID, tagID)
	if err != nil {
		return tagging.Impact{}, err
	}
	defer rows.Close()
	impact := tagging.Impact{
		Operation: operation, TagID: tagID,
		ReplacementTagID:     replacementTagID,
		FallbackByObjectType: map[tagging.ObjectType]int64{},
	}
	for rows.Next() {
		var objectType tagging.ObjectType
		var affected, fallback int64
		if err := rows.Scan(&objectType, &affected, &fallback); err != nil {
			return tagging.Impact{}, err
		}
		impact.AffectedObjects += affected
		impact.FallbackByObjectType[objectType] = fallback
	}
	return impact, rows.Err()
}

func (r *TaggingRepository) Merge(
	ctx context.Context,
	accepted tagging.MergeMutation,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	retired, survivor := accepted.Retired, accepted.Survivor
	if err := lockTagMutationNamespace(ctx, tx, retired.MSPID); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := lockCatalogTags(ctx, tx, retired.MSPID, retired.ID, survivor.ID); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO object_tag_assignments (
  id, msp_id, client_id, object_type, object_id, object_version,
  tag_id, assignment_source, assigned_at, assigned_by, evidence, version
)
SELECT
  md5(assignment.id::text || ':merge:' || $3::uuid::text)::uuid,
  assignment.msp_id, assignment.client_id, assignment.object_type,
  assignment.object_id, assignment.object_version, $3::uuid,
  assignment.assignment_source, $4, $5,
  assignment.evidence || jsonb_build_object(
    'merge_from_tag_id', $2::uuid::text,
    'correlation_id', $6::uuid::text
  ),
  1
FROM object_tag_assignments assignment
WHERE assignment.msp_id = $1::uuid AND assignment.tag_id = $2::uuid
ON CONFLICT (msp_id, object_type, object_id, tag_id) DO NOTHING
`, retired.MSPID, retired.ID, survivor.ID,
		accepted.Audit.OccurredAt, accepted.Audit.ActorID,
		accepted.Audit.CorrelationID); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO tag_assignment_events (
  id, msp_id, client_id, assignment_id, object_type, object_id,
  target_version, tag_id, operation, assignment_source,
  actor_type, actor_id, occurred_at, evidence,
  idempotency_key, correlation_id
)
SELECT
  md5(assignment.id::text || ':merge-removed:' || $3::uuid::text)::uuid,
  assignment.msp_id, assignment.client_id, assignment.id,
  assignment.object_type, assignment.object_id, assignment.object_version,
  assignment.tag_id, 'removed', assignment.assignment_source,
  'technician', $4, $5,
  jsonb_build_object('merged_into_tag_id', $3::uuid::text),
  'tag-merge:removed:' || assignment.id::text || ':' || $3::uuid::text,
  $6::uuid
FROM object_tag_assignments assignment
WHERE assignment.msp_id = $1::uuid AND assignment.tag_id = $2::uuid
`, retired.MSPID, retired.ID, survivor.ID, accepted.Audit.ActorID,
		accepted.Audit.OccurredAt, accepted.Audit.CorrelationID); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO tag_assignment_events (
  id, msp_id, client_id, assignment_id, object_type, object_id,
  target_version, tag_id, operation, assignment_source,
  actor_type, actor_id, occurred_at, evidence,
  idempotency_key, correlation_id
)
SELECT
  md5(assignment.id::text || ':merge-added')::uuid,
  assignment.msp_id, assignment.client_id, assignment.id,
  assignment.object_type, assignment.object_id, assignment.object_version,
  assignment.tag_id, 'added', assignment.assignment_source,
  'technician', $3, $4, assignment.evidence,
  'tag-merge:added:' || assignment.id::text,
  $5::uuid
FROM object_tag_assignments assignment
WHERE assignment.msp_id = $1::uuid AND assignment.tag_id = $2::uuid
  AND assignment.evidence->>'correlation_id' = $5::uuid::text
`, retired.MSPID, survivor.ID, accepted.Audit.ActorID,
		accepted.Audit.OccurredAt, accepted.Audit.CorrelationID); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if _, err := tx.Exec(ctx, `
DELETE FROM object_tag_assignments
WHERE msp_id = $1::uuid AND tag_id = $2::uuid
`, retired.MSPID, retired.ID); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	result, err := tx.Exec(ctx, `
UPDATE tags
SET lifecycle_state = 'merged', merged_into_id = $4,
    version = version + 1, updated_at = $5, updated_by = $6
WHERE id = $1::uuid AND msp_id = $2::uuid AND version = $3
  AND lifecycle_state = 'active' AND system_tag = false
`, retired.ID, retired.MSPID, accepted.ExpectedVersion, survivor.ID,
		accepted.Audit.OccurredAt, accepted.Audit.ActorID)
	if err != nil || result.RowsAffected() != 1 {
		_ = tx.Rollback(ctx)
		if err != nil {
			return err
		}
		return object.ErrVersionConflict
	}
	return commitTaggingMutation(ctx, tx, accepted.Audit, accepted.Event)
}

func (r *TaggingRepository) Archive(
	ctx context.Context,
	accepted tagging.ArchiveMutation,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	tag := accepted.Tag
	if err := lockTagMutationNamespace(ctx, tx, tag.MSPID); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := lockCatalogTags(ctx, tx, tag.MSPID, tag.ID, accepted.ReplacementTagID, accepted.UnclassifiedTag.ID); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO object_tag_assignments (
  id, msp_id, client_id, object_type, object_id, object_version,
  tag_id, assignment_source, assigned_at, assigned_by, evidence, version
)
SELECT
  md5(
    assignment.id::text || ':archive-replacement:'
    || COALESCE(NULLIF($3, ''), $4)
  )::uuid,
  assignment.msp_id, assignment.client_id, assignment.object_type,
  assignment.object_id, assignment.object_version,
  COALESCE(NULLIF($3, '')::uuid, $4::uuid),
  CASE WHEN $3 = '' THEN 'system_fallback' ELSE 'human' END,
  $5, CASE WHEN $3 = '' THEN NULL ELSE $6::uuid END,
  jsonb_build_object(
    'reason', 'archive_fallback',
    'archived_tag_id', $2::uuid::text,
    'correlation_id', $7::uuid::text
  ),
  1
FROM object_tag_assignments assignment
WHERE assignment.msp_id = $1::uuid AND assignment.tag_id = $2::uuid
  AND (
    $3 <> ''
    OR NOT EXISTS (
      SELECT 1
      FROM object_tag_assignments other_assignment
      JOIN tags other_tag
        ON other_tag.id = other_assignment.tag_id
       AND other_tag.msp_id = other_assignment.msp_id
      WHERE other_assignment.msp_id = assignment.msp_id
        AND other_assignment.object_type = assignment.object_type
        AND other_assignment.object_id = assignment.object_id
        AND other_assignment.tag_id <> assignment.tag_id
        AND other_tag.lifecycle_state = 'active'
        AND other_tag.internal_key <> 'taxonomy.system.unclassified'
    )
  )
ON CONFLICT (msp_id, object_type, object_id, tag_id) DO NOTHING
`, tag.MSPID, tag.ID, accepted.ReplacementTagID,
		accepted.UnclassifiedTag.ID, accepted.Audit.OccurredAt,
		accepted.Audit.ActorID, accepted.Audit.CorrelationID); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO tag_assignment_events (
  id, msp_id, client_id, assignment_id, object_type, object_id,
  target_version, tag_id, operation, assignment_source,
  actor_type, actor_id, occurred_at, evidence,
  idempotency_key, correlation_id
)
SELECT
  md5(assignment.id::text || ':archive-added')::uuid,
  assignment.msp_id, assignment.client_id, assignment.id,
  assignment.object_type, assignment.object_id, assignment.object_version,
  assignment.tag_id, 'added', assignment.assignment_source,
  CASE
    WHEN assignment.assignment_source = 'system_fallback' THEN 'system'
    ELSE 'technician'
  END,
  CASE
    WHEN assignment.assignment_source = 'system_fallback' THEN NULL
    ELSE $2::uuid
  END,
  $3, assignment.evidence,
  'tag-archive:added:' || assignment.id::text,
  $4::uuid
FROM object_tag_assignments assignment
WHERE assignment.msp_id = $1::uuid
  AND assignment.evidence->>'correlation_id' = $4::uuid::text
`, tag.MSPID, accepted.Audit.ActorID, accepted.Audit.OccurredAt,
		accepted.Audit.CorrelationID); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO tag_assignment_events (
  id, msp_id, client_id, assignment_id, object_type, object_id,
  target_version, tag_id, operation, assignment_source,
  actor_type, actor_id, occurred_at, evidence,
  idempotency_key, correlation_id
)
SELECT
  md5(assignment.id::text || ':archive-removed')::uuid,
  assignment.msp_id, assignment.client_id, assignment.id,
  assignment.object_type, assignment.object_id, assignment.object_version,
  assignment.tag_id, 'removed', assignment.assignment_source,
  'technician', $3::uuid, $4,
  jsonb_build_object(
    'archived_tag_id', $2::uuid::text,
    'replacement_tag_id', NULLIF($5, ''),
    'correlation_id', $6::uuid::text
  ),
  'tag-archive:removed:' || assignment.id::text,
  $6::uuid
FROM object_tag_assignments assignment
WHERE assignment.msp_id = $1::uuid AND assignment.tag_id = $2::uuid
`, tag.MSPID, tag.ID, accepted.Audit.ActorID,
		accepted.Audit.OccurredAt, accepted.ReplacementTagID,
		accepted.Audit.CorrelationID); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if _, err := tx.Exec(ctx, `
DELETE FROM object_tag_assignments
WHERE msp_id = $1::uuid AND tag_id = $2::uuid
`, tag.MSPID, tag.ID); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	result, err := tx.Exec(ctx, `
UPDATE tags
SET lifecycle_state = 'archived', version = version + 1,
    updated_at = $4, updated_by = $5
WHERE id = $1 AND msp_id = $2 AND version = $3
  AND lifecycle_state = 'active' AND system_tag = false
`, tag.ID, tag.MSPID, accepted.ExpectedVersion,
		accepted.Audit.OccurredAt, accepted.Audit.ActorID)
	if err != nil || result.RowsAffected() != 1 {
		_ = tx.Rollback(ctx)
		if err != nil {
			return err
		}
		return object.ErrVersionConflict
	}
	return commitTaggingMutation(ctx, tx, accepted.Audit, accepted.Event)
}

func commitTaggingMutation(
	ctx context.Context,
	tx transaction,
	audit mutation.AuditRecord,
	event mutation.EventRecord,
) error {
	if err := writeMutationFacts(ctx, tx, audit, event); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return commitAutomationRuntime(ctx, tx)
}

func classifyTaggingWriteError(err error) error {
	var postgres *pgconn.PgError
	if errors.As(err, &postgres) && postgres.Code == "23505" {
		switch postgres.ConstraintName {
		case "tags_label_unique",
			"tags_msp_id_normalized_label_key",
			"tag_synonyms_msp_id_normalized_label_key":
			return tagging.ErrDuplicateTerm
		}
	}
	return err
}

func lockCatalogTags(ctx context.Context, tx transaction, mspID string, ids ...string) error {
	unique := map[string]struct{}{}
	ordered := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, found := unique[id]; found {
			continue
		}
		unique[id] = struct{}{}
		ordered = append(ordered, id)
	}
	sort.Strings(ordered)
	for _, id := range ordered {
		rows, err := tx.Query(ctx, `SELECT id::text FROM tags WHERE id = $1::uuid AND msp_id = $2::uuid FOR UPDATE`, id, mspID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var locked string
			if err := rows.Scan(&locked); err != nil {
				rows.Close()
				return err
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
	}
	return nil
}
