package psa

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/collaboration"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mentions"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type MentionRepository struct{ db database }

var _ mentions.QueryRepository = (*MentionRepository)(nil)
var _ mentions.InvalidationRepository = (*MentionRepository)(nil)
var _ collaboration.ListRepository = (*MentionRepository)(nil)

func NewMentionRepository(db database) *MentionRepository { return &MentionRepository{db: db} }

type mentionQueryer interface {
	Query(context.Context, string, ...any) (rows, error)
	QueryRow(context.Context, string, ...any) row
}

const effectiveParentAccessSQL = `
EXISTS (
 SELECT 1
 FROM technicians access_technician
 WHERE access_technician.id = %s::uuid
   AND access_technician.msp_id = %s::uuid
   AND access_technician.lifecycle_state = 'active'
   %s
   AND (
     (%s='work_record' AND EXISTS(SELECT 1 FROM work_records access_work WHERE access_work.id=%s::uuid AND access_work.msp_id=%s::uuid AND access_work.client_id=%s::uuid AND access_work.deleted_at IS NULL)
       AND EXISTS(SELECT 1 FROM role_assignments parent_assignment JOIN role_capabilities parent_capability ON parent_capability.role_id=parent_assignment.role_id AND parent_capability.msp_id=parent_assignment.msp_id WHERE parent_assignment.msp_id=%s::uuid AND parent_assignment.technician_id=access_technician.id AND (parent_assignment.client_id IS NULL OR parent_assignment.client_id=%s::uuid) AND (parent_assignment.expires_at IS NULL OR parent_assignment.expires_at>now()) AND parent_capability.capability='work_record.read'))
     OR (%s='project' AND EXISTS(SELECT 1 FROM projects access_project WHERE access_project.id=%s::uuid AND access_project.msp_id=%s::uuid AND access_project.client_id=%s::uuid)
       AND EXISTS(SELECT 1 FROM role_assignments project_assignment JOIN role_capabilities project_capability ON project_capability.role_id=project_assignment.role_id AND project_capability.msp_id=project_assignment.msp_id WHERE project_assignment.msp_id=%s::uuid AND project_assignment.technician_id=access_technician.id AND (project_assignment.client_id IS NULL OR project_assignment.client_id=%s::uuid) AND (project_assignment.expires_at IS NULL OR project_assignment.expires_at>now()) AND project_capability.capability='project.read'))
     OR (%s='task' AND EXISTS(
       SELECT 1 FROM tasks access_task
       WHERE access_task.id=%s::uuid AND access_task.msp_id=%s::uuid AND access_task.client_id=%s::uuid
       AND ((access_task.work_record_id IS NOT NULL AND EXISTS(SELECT 1 FROM work_records task_work WHERE task_work.id=access_task.work_record_id AND task_work.msp_id=access_task.msp_id AND task_work.client_id=access_task.client_id AND task_work.deleted_at IS NULL)
          AND EXISTS(SELECT 1 FROM role_assignments task_assignment JOIN role_capabilities task_capability ON task_capability.role_id=task_assignment.role_id AND task_capability.msp_id=task_assignment.msp_id WHERE task_assignment.msp_id=access_task.msp_id AND task_assignment.technician_id=access_technician.id AND (task_assignment.client_id IS NULL OR task_assignment.client_id=access_task.client_id) AND (task_assignment.expires_at IS NULL OR task_assignment.expires_at>now()) AND task_capability.capability='work_record.read'))
         OR (access_task.parent_type='project' AND EXISTS(SELECT 1 FROM projects task_project WHERE task_project.id=access_task.parent_id AND task_project.msp_id=access_task.msp_id AND task_project.client_id=access_task.client_id)
          AND EXISTS(SELECT 1 FROM role_assignments task_assignment JOIN role_capabilities task_capability ON task_capability.role_id=task_assignment.role_id AND task_capability.msp_id=task_assignment.msp_id WHERE task_assignment.msp_id=access_task.msp_id AND task_assignment.technician_id=access_technician.id AND (task_assignment.client_id IS NULL OR task_assignment.client_id=access_task.client_id) AND (task_assignment.expires_at IS NULL OR task_assignment.expires_at>now()) AND task_capability.capability='project.read'))
         OR (access_task.parent_type='phase' AND EXISTS(SELECT 1 FROM phases task_phase JOIN projects task_project ON task_project.id=task_phase.project_id AND task_project.msp_id=task_phase.msp_id AND task_project.client_id=task_phase.client_id WHERE task_phase.id=access_task.parent_id AND task_phase.msp_id=access_task.msp_id AND task_phase.client_id=access_task.client_id)
          AND EXISTS(SELECT 1 FROM role_assignments task_assignment JOIN role_capabilities task_capability ON task_capability.role_id=task_assignment.role_id AND task_capability.msp_id=task_assignment.msp_id WHERE task_assignment.msp_id=access_task.msp_id AND task_assignment.technician_id=access_technician.id AND (task_assignment.client_id IS NULL OR task_assignment.client_id=access_task.client_id) AND (task_assignment.expires_at IS NULL OR task_assignment.expires_at>now()) AND task_capability.capability='project.read')))
     ))
   )
)`

func parentAccessPredicate(staff, msp, client, parentType, parentID, additionalAccess string) string {
	return fmt.Sprintf(effectiveParentAccessSQL, staff, msp, additionalAccess, parentType, parentID, msp, client, msp, client, parentType, parentID, msp, client, msp, client, parentType, parentID, msp, client)
}

func accessPredicate(staff, msp, client, parentType, parentID string) string {
	mentionRead := fmt.Sprintf(`AND EXISTS (
     SELECT 1 FROM role_assignments mention_assignment
     JOIN role_capabilities mention_capability
       ON mention_capability.role_id=mention_assignment.role_id
      AND mention_capability.msp_id=mention_assignment.msp_id
     WHERE mention_assignment.msp_id=%s::uuid
       AND mention_assignment.technician_id=access_technician.id
       AND (mention_assignment.client_id IS NULL OR mention_assignment.client_id=%s::uuid)
       AND (mention_assignment.expires_at IS NULL OR mention_assignment.expires_at>now())
       AND mention_capability.capability='mention.read'
   )`, msp, client)
	return parentAccessPredicate(staff, msp, client, parentType, parentID, mentionRead)
}

func internalCollaborationAccessPredicate(staff, msp, client, parentType, parentID string) string {
	readAccess := parentAccessPredicate(staff, msp, client, parentType, parentID, "")
	editAccess := strings.NewReplacer(
		"'work_record.read'", "'work_record.edit'",
		"'project.read'", "'project.edit'",
	).Replace(readAccess)
	return `((` + readAccess + `) OR (` + editAccess + `))`
}

func noConfirmedMentionLossPredicate(itemAlias string) string {
	return `NOT EXISTS (
  SELECT 1 FROM mention_access_invalidations access_loss
  WHERE access_loss.mention_item_id=` + itemAlias + `.id
    AND access_loss.msp_id=` + itemAlias + `.msp_id
    AND access_loss.client_id=` + itemAlias + `.client_id
    AND access_loss.access_loss_confirmed
    AND access_loss.snapshot_occurrence_id=` + itemAlias + `.latest_occurrence_id
)
AND NOT EXISTS (
  SELECT 1 FROM mention_access_loss_markers pending_loss
  JOIN mention_invalidation_event_claims pending_claim
    ON pending_claim.event_id=pending_loss.event_id
  WHERE pending_loss.msp_id=` + itemAlias + `.msp_id
    AND pending_loss.completed_at IS NULL
    AND pending_loss.boundary_revision>=` + itemAlias + `.authorization_revision
    AND mention_access_loss_marker_applies(pending_loss.event_id,` + itemAlias + `.id)
)`
}

func (r *MentionRepository) ListCandidates(ctx context.Context, query mentions.CandidateQuery) ([]mentions.Candidate, error) {
	if r == nil || r.db == nil {
		return nil, collaboration.ErrInvalid
	}
	return listMentionCandidates(ctx, r.db, query)
}

func listMentionCandidates(ctx context.Context, q mentionQueryer, query mentions.CandidateQuery) ([]mentions.Candidate, error) {
	// AuthorizationOnly is deliberately evaluated before any target directory
	// rows are loaded, preventing picker enumeration by an unauthorized author.
	authorAccess := accessPredicate("$1", "$2", "$3", "$4", "$5")
	authorAccess = strings.NewReplacer(
		"'mention.read'", "'mention.create'",
		"'work_record.read'", "'work_record.edit'",
		"'project.read'", "'project.edit'",
	).Replace(authorAccess)
	var authorized bool
	err := q.QueryRow(ctx, `SELECT (`+authorAccess+`) AND EXISTS(SELECT 1 FROM role_assignments create_assignment JOIN role_capabilities create_capability ON create_capability.role_id=create_assignment.role_id AND create_capability.msp_id=create_assignment.msp_id WHERE create_assignment.msp_id=$2::uuid AND create_assignment.technician_id=$1::uuid AND (create_assignment.client_id IS NULL OR create_assignment.client_id=$3::uuid) AND (create_assignment.expires_at IS NULL OR create_assignment.expires_at>now()) AND create_capability.capability='mention.create')`, query.AuthorID, query.Source.MSPID, query.Source.ClientID, query.Source.ParentType, query.Source.ParentID).Scan(&authorized)
	if err != nil {
		return nil, err
	}
	if !authorized {
		return nil, scope.ErrNotFound
	}
	if query.AuthorizationOnly {
		return []mentions.Candidate{}, nil
	}
	access := accessPredicate("technician.id", "$1", "$2", "$3", "$4")
	teamMemberAccess := accessPredicate("membership.technician_id", "$1", "$2", "$3", "$4")
	teamEligible := `(membership.technician_id<>$5::uuid AND (` + teamMemberAccess + `))`
	rowsResult, err := q.Query(ctx, `SELECT 'staff',technician.id::text,technician.display_name,0,0,ARRAY[]::text[] AS eligible_member_ids,technician.version,technician.msp_id::text,technician.lifecycle_state='active',true,`+access+`,`+access+` FROM technicians technician WHERE technician.msp_id=$1::uuid AND technician.id<>$5::uuid AND $8='' AND ($6='' OR technician.display_name ILIKE '%'||$6||'%')
UNION ALL
SELECT 'team',team.id::text,team.name,
	count(DISTINCT membership.technician_id) FILTER (WHERE `+teamEligible+`),
	count(DISTINCT membership.technician_id) FILTER (WHERE NOT (`+teamEligible+`)),
	COALESCE(array_agg(DISTINCT membership.technician_id::text ORDER BY membership.technician_id::text) FILTER (WHERE `+teamEligible+`),'{}'::text[]) AS eligible_member_ids,
	team.version,team.msp_id::text,true,true,true,true
FROM teams team JOIN team_memberships membership ON membership.team_id=team.id AND membership.msp_id=team.msp_id AND membership.lifecycle_state='active'
WHERE team.msp_id=$1::uuid AND ($8='' OR ($8='team' AND team.id=NULLIF($9,'')::uuid)) AND ($6='' OR team.name ILIKE '%'||$6||'%') GROUP BY team.id,team.name,team.version,team.msp_id ORDER BY 3,1,2 LIMIT $7`, query.Source.MSPID, query.Source.ClientID, query.Source.ParentType, query.Source.ParentID, query.AuthorID, strings.TrimSpace(query.Search), 50, string(query.ExactTargetType), strings.TrimSpace(query.ExactTargetID))
	if err != nil {
		return nil, err
	}
	defer rowsResult.Close()
	result := []mentions.Candidate{}
	for rowsResult.Next() {
		var candidate mentions.Candidate
		if err = rowsResult.Scan(&candidate.TargetType, &candidate.ID, &candidate.Label, &candidate.EligibleCount, &candidate.ExcludedCount, &candidate.EligibleMemberIDs, &candidate.Version, &candidate.MSPID, &candidate.Active, &candidate.Internal, &candidate.HasMentionRead, &candidate.CanRead); err != nil {
			return nil, err
		}
		result = append(result, candidate)
	}
	return result, rowsResult.Err()
}

func (r *MentionRepository) LoadDirectAccess(ctx context.Context, ref mentions.SourceRef, ids []string) ([]mentions.MemberAccess, error) {
	return loadMentionDirectAccess(ctx, r.db, ref, ids)
}
func loadMentionDirectAccess(ctx context.Context, q mentionQueryer, ref mentions.SourceRef, ids []string) ([]mentions.MemberAccess, error) {
	if len(ids) == 0 {
		return []mentions.MemberAccess{}, nil
	}
	access := accessPredicate("technician.id", "$1", "$2", "$3", "$4")
	rowsResult, err := q.Query(ctx, `SELECT technician.id::text,technician.msp_id::text,technician.lifecycle_state='active',true,EXISTS(SELECT 1 FROM role_assignments assignment JOIN role_capabilities capability ON capability.role_id=assignment.role_id AND capability.msp_id=assignment.msp_id WHERE assignment.msp_id=technician.msp_id AND assignment.technician_id=technician.id AND (assignment.client_id IS NULL OR assignment.client_id=$2::uuid) AND (assignment.expires_at IS NULL OR assignment.expires_at>now()) AND capability.capability='mention.read'),`+access+` FROM technicians technician WHERE technician.msp_id=$1::uuid AND technician.id=ANY($5::uuid[]) ORDER BY technician.id FOR SHARE`, ref.MSPID, ref.ClientID, ref.ParentType, ref.ParentID, ids)
	if err != nil {
		return nil, err
	}
	defer rowsResult.Close()
	result := []mentions.MemberAccess{}
	for rowsResult.Next() {
		var value mentions.MemberAccess
		if err = rowsResult.Scan(&value.StaffID, &value.MSPID, &value.Active, &value.Internal, &value.HasMentionRead, &value.CanRead); err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rowsResult.Err()
}
func (r *MentionRepository) LoadTeamAccess(ctx context.Context, ref mentions.SourceRef, ids []string) ([]mentions.TeamAccess, error) {
	return loadMentionTeamAccess(ctx, r.db, ref, ids, false)
}

const mentionAccessInvalidationEventTypesSQL = `(
 'role.unassigned',
 'role.capabilities_replaced',
 'technician.status.changed',
 'technician.disabled',
 'client.access.changed',
 'client.access.removed',
 'project.visibility.changed',
 'work_record.client.transferred',
 'task.client.transferred',
 'project.client.transferred',
 'internal_collaboration_source.saved',
 'internal_collaboration_source.redacted',
 'work_record.merged',
 'work_record.deleted',
 'task.deleted',
 'project.deleted'
)`

const mentionAccessInvalidationEventFilterSQL = `(
 event.event_type IN ` + mentionAccessInvalidationEventTypesSQL + `
 AND (event.event_type<>'internal_collaboration_source.saved'
      OR event.data->>'lifecycle_state'='redacted')
)`

func (r *MentionRepository) ClaimAccessInvalidations(
	ctx context.Context,
	limit int,
	now time.Time,
	lease time.Duration,
) ([]mentions.AffectedItem, error) {
	if r == nil || r.db == nil || limit < 1 || lease <= 0 || now.IsZero() {
		return nil, mentions.ErrInvalidInvalidationRun
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Expiration is promoted from an implicit wall-clock denial to an explicit,
	// revisioned authorization mutation. The bounded sweep closes temporal
	// assignment history and emits the same compact loss marker as an explicit
	// role unassignment, preventing a later grant from reviving an old mention.
	_, err = tx.Exec(ctx, `
SELECT begin_mention_authorization_revision(candidate.msp_id)
FROM (
  SELECT assignment.msp_id
  FROM role_assignments assignment
	  WHERE assignment.expires_at IS NOT NULL AND assignment.expires_at<=$1
  ORDER BY assignment.expires_at,assignment.id
  LIMIT 1
	) candidate`, now)
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(ctx, `
WITH expired_assignments AS MATERIALIZED (
  SELECT assignment.id
  FROM role_assignments assignment
  WHERE assignment.expires_at IS NOT NULL AND assignment.expires_at<=$2
    AND COALESCE(NULLIF(current_setting('rarity.mention_authorization_revisions',true),''),'{}')::jsonb ? assignment.msp_id::text
  ORDER BY assignment.expires_at,assignment.id
  FOR UPDATE SKIP LOCKED
  LIMIT $1
), deleted_assignments AS MATERIALIZED (
  DELETE FROM role_assignments assignment
  USING expired_assignments expired
  WHERE assignment.id=expired.id
  RETURNING assignment.id,assignment.msp_id,assignment.client_id,
            assignment.technician_id,assignment.role_id,assignment.granted_by,
            assignment.expires_at
), audited AS (
  INSERT INTO audit_ledger(
    id,occurred_at,msp_id,client_id,actor_type,actor_id,action,
    subject_type,subject_id,subject_version,source,reason,correlation_id
  )
  SELECT md5('mention-expiry-audit:'||assignment.id::text||':'||assignment.expires_at::text)::uuid,
         $2,assignment.msp_id,assignment.client_id,'system',assignment.granted_by,
         'role.unassigned','role_assignment',assignment.id,1,
         'mention-invalidation','assignment_expired',
         md5('mention-expiry:'||assignment.id::text||':'||assignment.expires_at::text)::uuid
  FROM deleted_assignments assignment
  ON CONFLICT (id) DO NOTHING
), emitted AS (
  INSERT INTO event_outbox(
    event_id,event_type,schema_version,occurred_at,msp_id,client_id,
    actor_type,actor_id,subject_type,subject_id,subject_version,
    correlation_id,source,data
  )
  SELECT md5('mention-expiry-event:'||assignment.id::text||':'||assignment.expires_at::text)::uuid,
         'role.unassigned',1,$2,assignment.msp_id,assignment.client_id,
         'system',assignment.granted_by,'role_assignment',assignment.id,1,
         md5('mention-expiry:'||assignment.id::text||':'||assignment.expires_at::text)::uuid,
         'mention-invalidation',jsonb_build_object(
           'reason','assignment_expired','technician_id',assignment.technician_id,
           'role_id',assignment.role_id
         )
  FROM deleted_assignments assignment
  ON CONFLICT (event_id) DO NOTHING
  RETURNING event_id
)
SELECT count(*) FROM emitted`, limit, now)
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(ctx, `
WITH locked_cursor AS (
  SELECT last_sequence
  FROM mention_invalidation_event_cursor
  WHERE singleton
  FOR UPDATE
), event_page AS MATERIALIZED (
  SELECT event.event_id,event.mention_invalidation_sequence,event.event_type,event.data
  FROM event_outbox event,locked_cursor cursor
  WHERE event.mention_invalidation_sequence>cursor.last_sequence
  ORDER BY event.mention_invalidation_sequence
  LIMIT $1
), queued AS (
  INSERT INTO mention_invalidation_event_claims(event_id,event_sequence)
  SELECT event.event_id,event.mention_invalidation_sequence
  FROM event_page event
  WHERE `+mentionAccessInvalidationEventFilterSQL+`
    AND EXISTS (
      SELECT 1 FROM mention_access_loss_markers marker
      WHERE marker.event_id=event.event_id
    )
  ON CONFLICT (event_id) DO NOTHING
)
UPDATE mention_invalidation_event_cursor cursor
SET last_sequence=COALESCE((SELECT max(mention_invalidation_sequence) FROM event_page),cursor.last_sequence),
    updated_at=$2
WHERE cursor.singleton`, limit, now)
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(ctx, `
WITH claimed_marker AS MATERIALIZED (
  SELECT marker.event_id,marker.msp_id,marker.boundary_revision,
         marker.scope_kind,marker.parent_type,marker.parent_id,
         marker.definitive_loss,marker.last_item_id,
         event.event_type,event.occurred_at,event.correlation_id
  FROM mention_access_loss_markers marker
  JOIN event_outbox event ON event.event_id=marker.event_id
  WHERE marker.completed_at IS NULL
    AND (marker.lease_until IS NULL OR marker.lease_until<=$2)
  ORDER BY event.mention_invalidation_sequence,marker.event_id
  FOR UPDATE OF marker SKIP LOCKED
  LIMIT 1
), raw_item_page AS MATERIALIZED (
  SELECT item.id,item.msp_id,item.client_id,item.recipient_id,item.parent_type,
         item.parent_id,item.latest_occurrence_id,item.authorization_revision,
         marker.event_id,marker.boundary_revision,
         (marker.definitive_loss AND (
           marker.scope_kind IN ('msp','client','recipient','source')
           OR (marker.scope_kind='object'
               AND item.parent_type=marker.parent_type
               AND item.parent_id=marker.parent_id)
         )) AS definitive_loss,
         marker.event_type,marker.occurred_at,marker.correlation_id
  FROM claimed_marker marker
  JOIN mention_items item ON item.msp_id=marker.msp_id
  WHERE (marker.last_item_id IS NULL OR item.id>marker.last_item_id)
  ORDER BY item.id
  LIMIT $1
), candidate_items AS MATERIALIZED (
  SELECT page.*
  FROM raw_item_page page
  WHERE mention_access_loss_marker_applies(page.event_id,page.id)
), confirmed_losses AS MATERIALIZED (
  SELECT candidate.*
  FROM candidate_items candidate
  WHERE candidate.definitive_loss
     OR NOT mention_has_effective_access_at_revision(
       candidate.recipient_id,candidate.msp_id,candidate.client_id,
       candidate.parent_type,candidate.parent_id,candidate.boundary_revision
     )
), inserted AS (
  INSERT INTO mention_access_invalidations(
    id,msp_id,client_id,mention_item_id,recipient_id,parent_type,parent_id,
    snapshot_occurrence_id,access_loss_confirmed,
    invalidated_at,reason_code,correlation_id,causation_id
  )
  SELECT md5(loss.event_id::text||':'||loss.id::text)::uuid,
         loss.msp_id,loss.client_id,loss.id,loss.recipient_id,
         loss.parent_type,loss.parent_id,loss.latest_occurrence_id,true,
         loss.occurred_at,loss.event_type,loss.correlation_id,loss.event_id
  FROM confirmed_losses loss
  ON CONFLICT (causation_id,mention_item_id) DO UPDATE
  SET snapshot_occurrence_id=EXCLUDED.snapshot_occurrence_id,
      access_loss_confirmed=true
), advanced AS (
  UPDATE mention_access_loss_markers marker
  SET last_item_id=COALESCE(
        (SELECT id FROM raw_item_page ORDER BY id DESC LIMIT 1),
        marker.last_item_id
      ),
      completed_at=CASE
        WHEN (SELECT count(*) FROM raw_item_page)<$1 THEN $2::timestamptz
        ELSE NULL
      END,
      lease_token=NULL,lease_until=NULL,
      attempt_count=marker.attempt_count+1,updated_at=$2
  WHERE marker.event_id=(SELECT event_id FROM claimed_marker)
  RETURNING marker.event_id,marker.last_item_id,marker.completed_at
)
UPDATE mention_invalidation_event_claims claim
SET last_item_id=advanced.last_item_id,
    completed_at=advanced.completed_at,
    attempt_count=claim.attempt_count+1,
    updated_at=$2
FROM advanced
WHERE claim.event_id=advanced.event_id`, limit, now)
	if err != nil {
		return nil, err
	}
	rowsResult, err := tx.Query(ctx, `
WITH candidates AS (
  SELECT invalidation.id
  FROM mention_access_invalidations invalidation
  WHERE invalidation.completed_at IS NULL
    AND (invalidation.lease_until IS NULL OR invalidation.lease_until<=$1)
  ORDER BY invalidation.invalidated_at,invalidation.id
  FOR UPDATE SKIP LOCKED
  LIMIT $2
), leased AS (
  UPDATE mention_access_invalidations invalidation
  SET lease_token=md5(invalidation.id::text||':'||$1::text)::uuid,
      lease_until=$1+($3 * interval '1 microsecond'),
      attempt_count=invalidation.attempt_count+1,
      safe_error_code=NULL
  FROM candidates
  WHERE invalidation.id=candidates.id
  RETURNING invalidation.*
)
SELECT leased.id::text,leased.lease_token::text,leased.mention_item_id::text,
       leased.msp_id::text,leased.client_id::text,leased.recipient_id::text,
       leased.parent_type,leased.parent_id::text,leased.reason_code,
       leased.snapshot_occurrence_id::text,leased.access_loss_confirmed
FROM leased ORDER BY leased.invalidated_at,leased.id`, now, limit, leaseMicroseconds(lease))
	if err != nil {
		return nil, err
	}
	defer rowsResult.Close()
	result := make([]mentions.AffectedItem, 0)
	for rowsResult.Next() {
		var item mentions.AffectedItem
		if err = rowsResult.Scan(
			&item.InvalidationID, &item.LeaseToken, &item.ID,
			&item.MSPID, &item.ClientID, &item.RecipientID,
			&item.ParentType, &item.ParentID, &item.ReasonCode,
			&item.SnapshotOccurrenceID, &item.AccessLossConfirmed,
		); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	if err = rowsResult.Err(); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *MentionRepository) ProcessAccessInvalidation(
	ctx context.Context,
	claimed mentions.AffectedItem,
	completedAt time.Time,
) (mentions.InvalidationDecision, error) {
	if r == nil || r.db == nil || claimed.InvalidationID == "" || claimed.LeaseToken == "" || claimed.ID == "" || completedAt.IsZero() {
		return mentions.InvalidationDecision{}, mentions.ErrInvalidInvalidationRun
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return mentions.InvalidationDecision{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var accessAllowed, accessLossConfirmed bool
	err = tx.QueryRow(ctx, `
SELECT `+accessPredicate("item.recipient_id", "item.msp_id", "item.client_id", "item.parent_type", "item.parent_id")+`,
       invalidation.access_loss_confirmed
FROM mention_access_invalidations invalidation
JOIN mention_items item
  ON item.id=invalidation.mention_item_id
 AND item.msp_id=invalidation.msp_id AND item.client_id=invalidation.client_id
WHERE invalidation.id=$1::uuid AND invalidation.mention_item_id=$2::uuid
  AND invalidation.lease_token=$3::uuid AND invalidation.lease_until>$4
  AND invalidation.completed_at IS NULL
FOR UPDATE OF invalidation,item`, claimed.InvalidationID, claimed.ID, claimed.LeaseToken, completedAt).Scan(&accessAllowed, &accessLossConfirmed)
	if errors.Is(err, pgx.ErrNoRows) {
		return mentions.InvalidationDecision{}, scope.ErrNotFound
	}
	if err != nil {
		return mentions.InvalidationDecision{}, err
	}
	// A loss confirmed in the access-changing transaction remains authoritative
	// if access is re-granted before this asynchronous worker runs.
	accessLoss := accessLossConfirmed || !accessAllowed
	query := `
WITH completed AS (
  UPDATE mention_access_invalidations
  SET completed_at=$4,lease_token=NULL,lease_until=NULL,safe_error_code=NULL
  WHERE id=$1::uuid AND mention_item_id=$2::uuid AND lease_token=$3::uuid
    AND completed_at IS NULL
  RETURNING mention_item_id
)
SELECT 0::bigint,0::bigint FROM completed`
	if accessLoss {
		query = `
WITH target_item AS MATERIALIZED (
  SELECT item.id,item.msp_id,item.client_id,item.recipient_id,
         invalidation.snapshot_occurrence_id
  FROM mention_access_invalidations invalidation
  JOIN mention_items item
    ON item.id=invalidation.mention_item_id
   AND item.msp_id=invalidation.msp_id AND item.client_id=invalidation.client_id
  WHERE invalidation.id=$1::uuid AND invalidation.mention_item_id=$2::uuid
    AND invalidation.lease_token=$3::uuid AND invalidation.completed_at IS NULL
), suppressed_item AS (
  UPDATE mention_items item
  SET suppressed_at=$4,suppression_reason='access_revoked',version=item.version+1
  FROM target_item target
  WHERE item.id=target.id
    AND item.latest_occurrence_id=target.snapshot_occurrence_id
    AND item.suppressed_at IS NULL
  RETURNING item.id,item.msp_id,item.client_id,item.recipient_id,item.parent_type,item.parent_id
), suppressed_delivery AS (
  UPDATE notification_deliveries delivery
  SET state='suppressed',next_attempt_at=$4,suppressed_at=$4,
      suppression_reason='access_revoked',failure_code=NULL
  FROM target_item item
  WHERE delivery.state='pending'
    AND delivery.mention_occurrence_id=item.snapshot_occurrence_id
    AND delivery.msp_id=item.msp_id AND delivery.client_id=item.client_id
    AND delivery.recipient_technician_id=item.recipient_id
  RETURNING delivery.id
), completed AS (
  UPDATE mention_access_invalidations
  SET completed_at=$4,lease_token=NULL,lease_until=NULL,safe_error_code=NULL
  WHERE id=$1::uuid AND mention_item_id=$2::uuid AND lease_token=$3::uuid
    AND completed_at IS NULL
  RETURNING mention_item_id
)
SELECT (SELECT count(*) FROM suppressed_item),
       (SELECT count(*) FROM suppressed_delivery)
FROM completed`
	}
	var suppressed, deliveries int64
	if err = tx.QueryRow(ctx, query, claimed.InvalidationID, claimed.ID, claimed.LeaseToken, completedAt).Scan(&suppressed, &deliveries); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return mentions.InvalidationDecision{}, scope.ErrNotFound
		}
		return mentions.InvalidationDecision{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return mentions.InvalidationDecision{}, err
	}
	return mentions.InvalidationDecision{
		AccessAllowed: !accessLoss, ItemSuppressed: suppressed > 0,
		DeliveriesCanceled: int(deliveries),
	}, nil
}
func loadMentionTeamAccess(ctx context.Context, q mentionQueryer, ref mentions.SourceRef, ids []string, lock bool) ([]mentions.TeamAccess, error) {
	if len(ids) == 0 {
		return []mentions.TeamAccess{}, nil
	}
	ids = append([]string(nil), ids...)
	sort.Strings(ids)
	if lock {
		memberIDs := []string{}
		locked, lockErr := q.Query(ctx, `SELECT team.id::text FROM teams team WHERE team.msp_id=$1::uuid AND team.id=ANY($2::uuid[]) ORDER BY team.id FOR SHARE`, ref.MSPID, ids)
		if lockErr != nil {
			return nil, lockErr
		}
		for locked.Next() {
			var teamID string
			if lockErr = locked.Scan(&teamID); lockErr != nil {
				locked.Close()
				return nil, lockErr
			}
		}
		lockErr = locked.Err()
		locked.Close()
		if lockErr != nil {
			return nil, lockErr
		}
		memberships, membershipErr := q.Query(ctx, `SELECT team_id::text,technician_id::text FROM team_memberships WHERE msp_id=$1::uuid AND team_id=ANY($2::uuid[]) AND lifecycle_state='active' ORDER BY team_id,technician_id FOR SHARE`, ref.MSPID, ids)
		if membershipErr != nil {
			return nil, membershipErr
		}
		for memberships.Next() {
			var teamID, technicianID string
			if membershipErr = memberships.Scan(&teamID, &technicianID); membershipErr != nil {
				memberships.Close()
				return nil, membershipErr
			}
			memberIDs = append(memberIDs, technicianID)
		}
		membershipErr = memberships.Err()
		memberships.Close()
		if membershipErr != nil {
			return nil, membershipErr
		}
		if membershipErr = lockMentionAuthorizationEvidence(ctx, q, ref.MSPID, ref.ClientID, memberIDs); membershipErr != nil {
			return nil, membershipErr
		}
	}
	access := accessPredicate("technician.id", "$1", "$2", "$3", "$4")
	rowsResult, err := q.Query(ctx, `SELECT team.id::text,team.msp_id::text,team.version,COALESCE(technician.id::text,''),COALESCE(technician.msp_id::text,''),COALESCE(technician.lifecycle_state='active',false),true,EXISTS(SELECT 1 FROM role_assignments assignment JOIN role_capabilities capability ON capability.role_id=assignment.role_id AND capability.msp_id=assignment.msp_id WHERE assignment.msp_id=team.msp_id AND assignment.technician_id=technician.id AND (assignment.client_id IS NULL OR assignment.client_id=$2::uuid) AND (assignment.expires_at IS NULL OR assignment.expires_at>now()) AND capability.capability='mention.read'),`+access+` FROM teams team LEFT JOIN team_memberships membership ON membership.team_id=team.id AND membership.msp_id=team.msp_id AND membership.lifecycle_state='active' LEFT JOIN technicians technician ON technician.id=membership.technician_id AND technician.msp_id=membership.msp_id WHERE team.msp_id=$1::uuid AND team.id=ANY($5::uuid[]) ORDER BY team.id,technician.id`, ref.MSPID, ref.ClientID, ref.ParentType, ref.ParentID, ids)
	if err != nil {
		return nil, err
	}
	defer rowsResult.Close()
	byID := map[string]*mentions.TeamAccess{}
	order := []string{}
	for rowsResult.Next() {
		var teamID, mspID, staffID, staffMSP string
		var version int64
		var active, internal, hasRead, canRead bool
		if err = rowsResult.Scan(&teamID, &mspID, &version, &staffID, &staffMSP, &active, &internal, &hasRead, &canRead); err != nil {
			return nil, err
		}
		team := byID[teamID]
		if team == nil {
			team = &mentions.TeamAccess{TeamID: teamID, MSPID: mspID, Version: version, Members: []mentions.MemberAccess{}}
			byID[teamID] = team
			order = append(order, teamID)
		}
		if staffID != "" {
			team.Members = append(team.Members, mentions.MemberAccess{StaffID: staffID, MSPID: staffMSP, Active: active, Internal: internal, HasMentionRead: hasRead, CanRead: canRead})
		}
	}
	if err = rowsResult.Err(); err != nil {
		return nil, err
	}
	result := make([]mentions.TeamAccess, 0, len(order))
	for _, id := range order {
		result = append(result, *byID[id])
	}
	return result, nil
}

func (r *MentionRepository) ListWidget(ctx context.Context, query mentions.WidgetStoreQuery) (mentions.WidgetSnapshot, error) {
	empty := mentions.WidgetSnapshot{Rows: []mentions.WidgetRecord{}}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY`); err != nil {
		return empty, err
	}
	access := accessPredicate("item.recipient_id", "item.msp_id", "item.client_id", "item.parent_type", "item.parent_id")
	err = tx.QueryRow(ctx, `SELECT count(*) FILTER(WHERE item.state='unread'),count(*) FILTER(WHERE item.state='read'),count(*) FILTER(WHERE item.state='archived') FROM mention_items item WHERE item.msp_id=$1::uuid AND item.recipient_id=$2::uuid AND item.suppressed_at IS NULL AND `+noConfirmedMentionLossPredicate("item")+` AND `+access, query.MSPID, query.RecipientID).Scan(&empty.Counts.Unread, &empty.Counts.Read, &empty.Counts.Archived)
	if err != nil {
		return empty, err
	}
	rowsResult, err := tx.Query(ctx, `SELECT item.id::text,item.parent_type,item.parent_id::text,
CASE item.parent_type WHEN 'work_record' THEN COALESCE(work.display_id,'') WHEN 'project' THEN COALESCE(project.display_id,'') ELSE COALESCE(task.id::text,'') END,
CASE item.parent_type WHEN 'work_record' THEN COALESCE(work.title,'') WHEN 'project' THEN COALESCE(project.name,'') ELSE COALESCE(task.title,'') END,
item.latest_occurrence_id::text,author.display_name,resolution.resolution_path,item.state,item.last_mentioned_at,item.version,
(source.lifecycle_state='active' AND source.version>=occurrence.source_revision AND current_token.token IS NOT NULL),
CASE WHEN source.lifecycle_state='active' THEN source.body ELSE '' END,
COALESCE((current_token.token->>'start')::int,0),COALESCE((current_token.token->>'end')::int,0)
FROM mention_items item JOIN mention_occurrences occurrence ON occurrence.id=item.latest_occurrence_id AND occurrence.msp_id=item.msp_id AND occurrence.client_id=item.client_id
JOIN mention_recipient_resolutions resolution ON resolution.occurrence_id=occurrence.id AND resolution.msp_id=occurrence.msp_id AND resolution.client_id=occurrence.client_id AND resolution.recipient_id=item.recipient_id
JOIN technicians author ON author.id=occurrence.author_id AND author.msp_id=occurrence.msp_id
LEFT JOIN internal_collaboration_sources source ON source.id=occurrence.source_id AND source.msp_id=occurrence.msp_id AND source.client_id=occurrence.client_id
LEFT JOIN LATERAL (
  SELECT token
  FROM jsonb_array_elements(source.mention_tokens) token
  WHERE token->>'id'=occurrence.token_id::text
    AND token->>'target_id'=occurrence.target_id::text
  LIMIT 1
) current_token ON true
LEFT JOIN work_records work ON item.parent_type='work_record' AND work.id=item.parent_id AND work.msp_id=item.msp_id AND work.client_id=item.client_id AND work.deleted_at IS NULL
LEFT JOIN projects project ON item.parent_type='project' AND project.id=item.parent_id AND project.msp_id=item.msp_id AND project.client_id=item.client_id
LEFT JOIN tasks task ON item.parent_type='task' AND task.id=item.parent_id AND task.msp_id=item.msp_id AND task.client_id=item.client_id
WHERE item.msp_id=$1::uuid AND item.recipient_id=$2::uuid AND item.state=$3 AND item.suppressed_at IS NULL AND `+noConfirmedMentionLossPredicate("item")+` AND `+access+` AND ($4::timestamptz IS NULL OR (item.last_mentioned_at, item.id)<($4,$5::uuid)) ORDER BY item.last_mentioned_at DESC,item.id DESC LIMIT $6`, query.MSPID, query.RecipientID, query.State, nullableMentionTime(query.BeforeAt), nullableID(query.BeforeID), query.Limit)
	if err != nil {
		return empty, err
	}
	defer rowsResult.Close()
	for rowsResult.Next() {
		var record mentions.WidgetRecord
		if err = rowsResult.Scan(&record.Item.ID, &record.Item.ParentType, &record.Item.ParentID, &record.Item.ParentDisplayID, &record.Item.ParentSubject, &record.Item.LatestOccurrenceID, &record.Item.AuthorLabel, &record.Item.Origin, &record.Item.State, &record.Item.LastMentionedAt, &record.Item.Version, &record.SourceAvailable, &record.Body, &record.TokenStart, &record.TokenEnd); err != nil {
			return empty, err
		}
		empty.Rows = append(empty.Rows, record)
	}
	if err = rowsResult.Err(); err != nil {
		return empty, err
	}
	if err = tx.Commit(ctx); err != nil {
		return empty, err
	}
	return empty, nil
}

func (r *MentionRepository) ChangeState(ctx context.Context, command mentions.StateStoreChange) (mentions.Item, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return mentions.Item{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var item mentions.Item
	err = tx.QueryRow(ctx, `UPDATE mention_items AS item SET state=$4,read_at=CASE WHEN $4='read' THEN $6::timestamptz ELSE NULL END,archived_at=CASE WHEN $4='archived' THEN $6::timestamptz ELSE NULL END,version=item.version+1 WHERE item.id=$3::uuid AND item.msp_id=$1::uuid AND item.recipient_id=$2::uuid AND item.version=$5 AND item.suppressed_at IS NULL AND `+noConfirmedMentionLossPredicate("item")+` AND `+accessPredicate("item.recipient_id", "item.msp_id", "item.client_id", "item.parent_type", "item.parent_id")+` RETURNING item.id::text,item.msp_id::text,item.client_id::text,item.recipient_id::text,item.parent_type,item.parent_id::text,item.latest_occurrence_id::text,item.state,item.read_at,item.archived_at,item.last_mentioned_at,item.suppressed_at,COALESCE(item.suppression_reason,''),item.version`, command.MSPID, command.RecipientID, command.ItemID, command.State, command.ExpectedVersion, command.ChangedAt).Scan(&item.ID, &item.MSPID, &item.ClientID, &item.RecipientID, &item.ParentType, &item.ParentID, &item.LatestOccurrenceID, &item.State, &item.ReadAt, &item.ArchivedAt, &item.LastMentionedAt, &item.SuppressedAt, &item.SuppressionReason, &item.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return mentions.Item{}, classifyMentionItemMiss(ctx, tx, command.MSPID, command.RecipientID, command.ItemID, command.ExpectedVersion)
	}
	if err != nil {
		return mentions.Item{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return mentions.Item{}, err
	}
	return item, nil
}

func (r *MentionRepository) ResolveDeepLink(ctx context.Context, query mentions.DeepLinkStoreQuery) (mentions.DeepLink, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return mentions.DeepLink{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var clientID, parentType, parentID, sourceID, sourceKind, lifecycle, tokenID string
	var currentVersion int64
	var tokenCurrent bool
	err = tx.QueryRow(ctx, `SELECT item.client_id::text,item.parent_type,item.parent_id::text,source.id::text,source.source_kind,source.lifecycle_state,occurrence.token_id::text,item.version,EXISTS(SELECT 1 FROM jsonb_array_elements(source.mention_tokens) token WHERE token->>'id'=occurrence.token_id::text AND token->>'target_id'=occurrence.target_id::text) FROM mention_items item JOIN mention_occurrences occurrence ON occurrence.id=$4::uuid AND occurrence.id=item.latest_occurrence_id AND occurrence.msp_id=item.msp_id AND occurrence.client_id=item.client_id LEFT JOIN internal_collaboration_sources source ON source.id=occurrence.source_id AND source.msp_id=occurrence.msp_id AND source.client_id=occurrence.client_id WHERE item.id=$3::uuid AND item.msp_id=$1::uuid AND item.recipient_id=$2::uuid AND item.suppressed_at IS NULL AND `+noConfirmedMentionLossPredicate("item")+` AND `+accessPredicate("item.recipient_id", "item.msp_id", "item.client_id", "item.parent_type", "item.parent_id")+` FOR UPDATE OF item`, query.MSPID, query.RecipientID, query.ItemID, query.OccurrenceID).Scan(&clientID, &parentType, &parentID, &sourceID, &sourceKind, &lifecycle, &tokenID, &currentVersion, &tokenCurrent)
	if errors.Is(err, pgx.ErrNoRows) {
		return mentions.DeepLink{}, scope.ErrNotFound
	}
	if err != nil {
		return mentions.DeepLink{}, err
	}
	if currentVersion != query.ExpectedVersion {
		return mentions.DeepLink{}, object.ErrVersionConflict
	}
	var nextVersion int64
	err = tx.QueryRow(ctx, `UPDATE mention_items SET state='read',read_at=$4,archived_at=NULL,version=version+1 WHERE id=$1::uuid AND recipient_id=$2::uuid AND version=$3 RETURNING version`, query.ItemID, query.RecipientID, query.ExpectedVersion, query.ResolvedAt).Scan(&nextVersion)
	if err != nil {
		return mentions.DeepLink{}, err
	}
	available := lifecycle == "active" && tokenCurrent
	href := mentionParentHref(mentions.ParentType(parentType), parentID)
	href += "&mentionOccurrenceID=" + url.QueryEscape(query.OccurrenceID)
	if available {
		href += "&sourceID=" + url.QueryEscape(sourceID)
	}
	if err = tx.Commit(ctx); err != nil {
		return mentions.DeepLink{}, err
	}
	if !available {
		sourceID = ""
		tokenID = ""
	}
	return mentions.DeepLink{Href: href, ClientID: clientID, ParentType: parentType, ParentID: parentID, SourceID: sourceID, TokenID: tokenID, SourceAvailable: available, ItemVersion: nextVersion}, nil
}

func (r *MentionRepository) LoadPreview(ctx context.Context, query mentions.PreviewStoreQuery) (mentions.PreviewRecord, error) {
	var record mentions.PreviewRecord
	err := r.db.QueryRow(ctx, `SELECT
	  item.parent_type,item.msp_id::text,item.client_id::text,item.parent_id::text,
	  source.lifecycle_state='active'
    AND source.version>=occurrence.source_revision
    AND current_token.token IS NOT NULL,
  CASE WHEN source.lifecycle_state='active' THEN source.body ELSE '' END,
  COALESCE((current_token.token->>'start')::int,0),
  COALESCE((current_token.token->>'end')::int,0)
FROM mention_items item
JOIN mention_occurrences occurrence
  ON occurrence.id=item.latest_occurrence_id
 AND occurrence.msp_id=item.msp_id AND occurrence.client_id=item.client_id
LEFT JOIN internal_collaboration_sources source
  ON source.id=occurrence.source_id
 AND source.msp_id=occurrence.msp_id AND source.client_id=occurrence.client_id
LEFT JOIN LATERAL (
  SELECT token
  FROM jsonb_array_elements(source.mention_tokens) token
  WHERE token->>'id'=occurrence.token_id::text
    AND token->>'target_id'=occurrence.target_id::text
  LIMIT 1
) current_token ON true
WHERE item.id=$3::uuid AND item.msp_id=$1::uuid AND item.recipient_id=$2::uuid
	  AND item.suppressed_at IS NULL AND `+noConfirmedMentionLossPredicate("item")+` AND `+accessPredicate("item.recipient_id", "item.msp_id", "item.client_id", "item.parent_type", "item.parent_id"), query.MSPID, query.RecipientID, query.ItemID).Scan(&record.ParentType, &record.MSPID, &record.ClientID, &record.ParentID, &record.SourceAvailable, &record.Body, &record.TokenStart, &record.TokenEnd)
	if errors.Is(err, pgx.ErrNoRows) {
		return mentions.PreviewRecord{}, scope.ErrNotFound
	}
	return record, err
}

func classifyMentionItemMiss(ctx context.Context, q mentionQueryer, mspID, recipientID, itemID string, expected int64) error {
	var version int64
	err := q.QueryRow(ctx, `SELECT item.version FROM mention_items item WHERE item.id=$1::uuid AND item.msp_id=$2::uuid AND item.recipient_id=$3::uuid AND item.suppressed_at IS NULL AND `+noConfirmedMentionLossPredicate("item")+` AND `+accessPredicate("item.recipient_id", "item.msp_id", "item.client_id", "item.parent_type", "item.parent_id"), itemID, mspID, recipientID).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return scope.ErrNotFound
	}
	if err != nil {
		return err
	}
	if version != expected {
		return object.ErrVersionConflict
	}
	return scope.ErrNotFound
}
func mentionParentHref(parentType mentions.ParentType, id string) string {
	switch parentType {
	case mentions.ParentWorkRecord:
		return "#/work?parentID=" + url.QueryEscape(id)
	case mentions.ParentTask:
		return "#/work?parentID=" + url.QueryEscape(id)
	case mentions.ParentProject:
		return "#/project?parentID=" + url.QueryEscape(id)
	default:
		return "#/home?parentID=" + url.QueryEscape(id)
	}
}
func nullableMentionTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

// ListInternalContent resolves the parent inside the authenticated MSP/client
// boundary, verifies current parent read-or-edit access, and returns
// native structured sources together with immutable legacy internal comments.
func (r *MentionRepository) ListInternalContent(ctx context.Context, query collaboration.ListQuery) ([]collaboration.ListedSource, error) {
	if r == nil || r.db == nil || query.Principal.ID == "" || query.Principal.Scope.MSPID == "" || query.Parent.ID == "" {
		return []collaboration.ListedSource{}, collaboration.ErrInvalid
	}
	ref := mentions.SourceRef{MSPID: query.Principal.Scope.MSPID, ClientID: query.Principal.Scope.ClientID, ParentType: query.Parent.Type, ParentID: query.Parent.ID, SourceKind: mentions.SourceDetails}
	trusted, err := resolveMentionParent(ctx, r.db, ref)
	if err != nil {
		return []collaboration.ListedSource{}, err
	}
	var allowed bool
	err = r.db.QueryRow(ctx, `SELECT `+internalCollaborationAccessPredicate("$1", "$2", "$3", "$4", "$5"), query.Principal.ID, trusted.MSPID, trusted.ClientID, trusted.ParentType, trusted.ParentID).Scan(&allowed)
	if err != nil {
		return []collaboration.ListedSource{}, err
	}
	if !allowed {
		return []collaboration.ListedSource{}, scope.ErrNotFound
	}
	rowsResult, err := r.db.Query(ctx, `SELECT id::text,msp_id::text,client_id::text,parent_type,parent_id::text,source_kind,body,mention_tokens,author_id::text,lifecycle_state,version,created_at,updated_at,redacted_at FROM internal_collaboration_sources WHERE msp_id=$1::uuid AND client_id=$2::uuid AND parent_type=$3 AND parent_id=$4::uuid ORDER BY created_at,id`, trusted.MSPID, trusted.ClientID, trusted.ParentType, trusted.ParentID)
	if err != nil {
		return []collaboration.ListedSource{}, err
	}
	result := []collaboration.ListedSource{}
	for rowsResult.Next() {
		var source collaboration.Source
		var tokenJSON []byte
		if err = rowsResult.Scan(&source.ID, &source.MSPID, &source.ClientID, &source.Parent.Type, &source.Parent.ID, &source.Kind, &source.Body, &tokenJSON, &source.AuthorID, &source.LifecycleState, &source.Version, &source.CreatedAt, &source.UpdatedAt, &source.RedactedAt); err != nil {
			rowsResult.Close()
			return []collaboration.ListedSource{}, err
		}
		if err = json.Unmarshal(tokenJSON, &source.Tokens); err != nil {
			rowsResult.Close()
			return []collaboration.ListedSource{}, err
		}
		if source.Tokens == nil {
			source.Tokens = []mentions.Token{}
		}
		result = append(result, collaboration.ListedSource{Source: source})
	}
	if err = rowsResult.Err(); err != nil {
		rowsResult.Close()
		return []collaboration.ListedSource{}, err
	}
	rowsResult.Close()
	if trusted.ParentType == mentions.ParentWorkRecord {
		legacy, legacyErr := r.LegacyInternalComments(ctx, trusted.MSPID, trusted.ClientID, trusted.ParentID)
		if legacyErr != nil {
			return []collaboration.ListedSource{}, legacyErr
		}
		for _, source := range legacy {
			result = append(result, collaboration.ListedSource{Source: source, ReadOnly: true, Legacy: true})
		}
	}
	sort.SliceStable(result, func(left, right int) bool {
		if result[left].Source.CreatedAt.Equal(result[right].Source.CreatedAt) {
			return result[left].Source.ID < result[right].Source.ID
		}
		return result[left].Source.CreatedAt.Before(result[right].Source.CreatedAt)
	})
	return result, nil
}

func resolveMentionParent(ctx context.Context, q mentionQueryer, ref mentions.SourceRef) (mentions.SourceRef, error) {
	trusted := ref
	var err error
	switch ref.ParentType {
	case mentions.ParentWorkRecord:
		err = q.QueryRow(ctx, `SELECT msp_id::text,client_id::text FROM work_records WHERE id=$1::uuid AND msp_id=$2::uuid AND ($3='' OR client_id=NULLIF($3,'')::uuid) AND deleted_at IS NULL`, ref.ParentID, ref.MSPID, ref.ClientID).Scan(&trusted.MSPID, &trusted.ClientID)
	case mentions.ParentProject:
		err = q.QueryRow(ctx, `SELECT msp_id::text,client_id::text FROM projects WHERE id=$1::uuid AND msp_id=$2::uuid AND ($3='' OR client_id=NULLIF($3,'')::uuid)`, ref.ParentID, ref.MSPID, ref.ClientID).Scan(&trusted.MSPID, &trusted.ClientID)
	case mentions.ParentTask:
		err = q.QueryRow(ctx, `SELECT msp_id::text,client_id::text FROM tasks WHERE id=$1::uuid AND msp_id=$2::uuid AND ($3='' OR client_id=NULLIF($3,'')::uuid)`, ref.ParentID, ref.MSPID, ref.ClientID).Scan(&trusted.MSPID, &trusted.ClientID)
	default:
		return trusted, collaboration.ErrInvalid
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return trusted, scope.ErrNotFound
	}
	return trusted, err
}

// LegacyInternalComments intentionally reads plain internal comments without
// token parsing or copying them into the structured collaboration source table.
func (r *MentionRepository) LegacyInternalComments(ctx context.Context, mspID, clientID, workRecordID string) ([]collaboration.Source, error) {
	rowsResult, err := r.db.Query(ctx, `SELECT id::text,msp_id::text,client_id::text,work_record_id::text,body,author_id::text,version,created_at,COALESCE(edited_at,created_at) FROM comments WHERE msp_id=$1::uuid AND client_id=$2::uuid AND work_record_id=$3::uuid AND visibility='internal' ORDER BY created_at,id`, mspID, clientID, workRecordID)
	if err != nil {
		return nil, err
	}
	defer rowsResult.Close()
	result := []collaboration.Source{}
	for rowsResult.Next() {
		var value collaboration.Source
		if err = rowsResult.Scan(&value.ID, &value.MSPID, &value.ClientID, &value.Parent.ID, &value.Body, &value.AuthorID, &value.Version, &value.CreatedAt, &value.UpdatedAt); err != nil {
			return nil, err
		}
		value.Parent.Type = mentions.ParentWorkRecord
		value.Kind = mentions.SourceComment
		value.LifecycleState = collaboration.SourceActive
		value.Tokens = []mentions.Token{}
		result = append(result, value)
	}
	return result, rowsResult.Err()
}
