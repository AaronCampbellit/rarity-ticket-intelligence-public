package psa

import (
	"context"
	"encoding/json"
	"errors"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/collaboration"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mentions"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/observability"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type CollaborationRepository struct {
	db        database
	telemetry *observability.MentionTelemetry
}

var _ collaboration.Repository = (*CollaborationRepository)(nil)

func NewCollaborationRepository(db database) *CollaborationRepository {
	return &CollaborationRepository{db: db}
}

func (r *CollaborationRepository) WithTelemetry(telemetry *observability.MentionTelemetry) *CollaborationRepository {
	if r != nil {
		r.telemetry = telemetry
	}
	return r
}

func (r *CollaborationRepository) WithTransaction(ctx context.Context, ref mentions.SourceRef, fn func(collaboration.Transaction) error) error {
	if r == nil || r.db == nil || fn == nil {
		return collaboration.ErrInvalid
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// The MSP revision row is the first lock in every collaboration mutation.
	// Authorization writers bump and lock the same row before changing facts,
	// making revoke-vs-mention ordering deterministic at commit time.
	if _, err = tx.Exec(ctx, `SELECT lock_mention_authorization_revision($1::uuid)`, ref.MSPID); err != nil {
		return err
	}
	unit := &collaborationTransaction{tx: tx, ref: ref, telemetry: r.telemetry}
	if err = fn(unit); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	for _, metric := range unit.telemetryFacts {
		r.telemetry.Count(metric)
	}
	return nil
}

type collaborationTransaction struct {
	tx             transaction
	ref            mentions.SourceRef
	trusted        *mentions.SourceRef
	telemetry      *observability.MentionTelemetry
	telemetryFacts []observability.MentionMetric
}

func (u *collaborationTransaction) LoadSourceForUpdate(ctx context.Context, ref mentions.SourceRef, sourceID string) (collaboration.Source, error) {
	if ref != u.ref {
		return collaboration.Source{}, collaboration.ErrInvalid
	}
	trusted, err := lockMentionParent(ctx, u.tx, ref)
	if err != nil {
		return collaboration.Source{}, err
	}
	u.trusted = &trusted
	if sourceID == "" && ref.SourceKind != mentions.SourceDetails {
		return collaboration.Source{MSPID: trusted.MSPID, ClientID: trusted.ClientID, Parent: collaboration.ParentRef{Type: trusted.ParentType, ID: trusted.ParentID}, Kind: trusted.SourceKind}, nil
	}
	var source collaboration.Source
	var tokens []byte
	query := `SELECT id::text,msp_id::text,client_id::text,parent_type,parent_id::text,source_kind,body,mention_tokens,author_id::text,lifecycle_state,version,created_at,updated_at,redacted_at FROM internal_collaboration_sources WHERE msp_id=$1::uuid AND client_id=$2::uuid AND parent_type=$3 AND parent_id=$4::uuid AND source_kind=$5`
	args := []any{trusted.MSPID, trusted.ClientID, trusted.ParentType, trusted.ParentID, trusted.SourceKind}
	if sourceID != "" {
		query += ` AND id=$6::uuid`
		args = append(args, sourceID)
	} else {
		query += ` AND lifecycle_state='active'`
	}
	query += ` FOR UPDATE`
	err = u.tx.QueryRow(ctx, query, args...).Scan(&source.ID, &source.MSPID, &source.ClientID, &source.Parent.Type, &source.Parent.ID, &source.Kind, &source.Body, &tokens, &source.AuthorID, &source.LifecycleState, &source.Version, &source.CreatedAt, &source.UpdatedAt, &source.RedactedAt)
	if errors.Is(err, pgx.ErrNoRows) && sourceID == "" {
		return collaboration.Source{MSPID: trusted.MSPID, ClientID: trusted.ClientID, Parent: collaboration.ParentRef{Type: trusted.ParentType, ID: trusted.ParentID}, Kind: trusted.SourceKind}, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return collaboration.Source{}, scope.ErrNotFound
	}
	if err != nil {
		return collaboration.Source{}, err
	}
	if err = json.Unmarshal(tokens, &source.Tokens); err != nil {
		return collaboration.Source{}, err
	}
	return source, nil
}

func lockMentionParent(ctx context.Context, tx transaction, ref mentions.SourceRef) (mentions.SourceRef, error) {
	trusted := ref
	var mspID, clientID string
	var err error
	switch ref.ParentType {
	case mentions.ParentWorkRecord:
		err = tx.QueryRow(ctx, `SELECT msp_id::text,client_id::text FROM work_records WHERE id=$1::uuid AND msp_id=$2::uuid AND ($3='' OR client_id=NULLIF($3,'')::uuid) AND deleted_at IS NULL FOR UPDATE`, ref.ParentID, ref.MSPID, ref.ClientID).Scan(&mspID, &clientID)
	case mentions.ParentProject:
		err = tx.QueryRow(ctx, `SELECT msp_id::text,client_id::text FROM projects WHERE id=$1::uuid AND msp_id=$2::uuid AND ($3='' OR client_id=NULLIF($3,'')::uuid) FOR UPDATE`, ref.ParentID, ref.MSPID, ref.ClientID).Scan(&mspID, &clientID)
	case mentions.ParentTask:
		err = tx.QueryRow(ctx, `SELECT msp_id::text,client_id::text FROM tasks WHERE id=$1::uuid AND msp_id=$2::uuid AND ($3='' OR client_id=NULLIF($3,'')::uuid) FOR UPDATE`, ref.ParentID, ref.MSPID, ref.ClientID).Scan(&mspID, &clientID)
	default:
		return trusted, collaboration.ErrInvalid
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return trusted, scope.ErrNotFound
	}
	if err != nil {
		return trusted, err
	}
	trusted.MSPID, trusted.ClientID = mspID, clientID
	return trusted, nil
}

func (u *collaborationTransaction) LoadIdempotentSource(ctx context.Context, key string) (collaboration.Source, bool, error) {
	if u.trusted == nil {
		return collaboration.Source{}, false, collaboration.ErrInvalid
	}
	claim := u.trusted.MSPID + "|" + u.trusted.ClientID + "|" + string(u.trusted.ParentType) + "|" + u.trusted.ParentID + "|" + string(u.trusted.SourceKind) + "|" + key
	if _, err := u.tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, claim); err != nil {
		return collaboration.Source{}, false, err
	}
	var claimedSourceID, claimedLifecycle string
	var claimedVersion int64
	err := u.tx.QueryRow(ctx, `SELECT event.subject_id::text,event.subject_version,event.data->>'lifecycle_state' FROM event_outbox event WHERE event.msp_id=$1::uuid AND event.client_id=$2::uuid AND event.subject_type='internal_collaboration_source' AND event.data->>'idempotency_key'=$3 AND event.data->>'parent_type'=$4 AND event.data->>'parent_id'=$5 AND event.data->>'source_kind'=$6 ORDER BY event.occurred_at DESC LIMIT 1`, u.trusted.MSPID, u.trusted.ClientID, key, u.trusted.ParentType, u.trusted.ParentID, u.trusted.SourceKind).Scan(&claimedSourceID, &claimedVersion, &claimedLifecycle)
	if errors.Is(err, pgx.ErrNoRows) {
		return collaboration.Source{}, false, nil
	}
	if err != nil {
		return collaboration.Source{}, false, err
	}
	var currentVersion int64
	var currentLifecycle string
	err = u.tx.QueryRow(ctx, `SELECT version,lifecycle_state FROM internal_collaboration_sources WHERE id=$1::uuid AND msp_id=$2::uuid AND client_id=$3::uuid AND parent_type=$4 AND parent_id=$5::uuid AND source_kind=$6 FOR UPDATE`, claimedSourceID, u.trusted.MSPID, u.trusted.ClientID, u.trusted.ParentType, u.trusted.ParentID, u.trusted.SourceKind).Scan(&currentVersion, &currentLifecycle)
	if errors.Is(err, pgx.ErrNoRows) {
		return collaboration.Source{}, false, object.ErrVersionConflict
	}
	if err != nil {
		return collaboration.Source{}, false, err
	}
	if currentVersion != claimedVersion || currentLifecycle != claimedLifecycle {
		return collaboration.Source{}, false, object.ErrVersionConflict
	}
	var source collaboration.Source
	var tokens []byte
	err = u.tx.QueryRow(ctx, `SELECT id::text,msp_id::text,client_id::text,parent_type,parent_id::text,source_kind,body,mention_tokens,author_id::text,lifecycle_state,version,created_at,updated_at,redacted_at FROM internal_collaboration_sources WHERE id=$1::uuid AND msp_id=$2::uuid AND client_id=$3::uuid AND version=$4 AND lifecycle_state=$5`, claimedSourceID, u.trusted.MSPID, u.trusted.ClientID, claimedVersion, claimedLifecycle).Scan(&source.ID, &source.MSPID, &source.ClientID, &source.Parent.Type, &source.Parent.ID, &source.Kind, &source.Body, &tokens, &source.AuthorID, &source.LifecycleState, &source.Version, &source.CreatedAt, &source.UpdatedAt, &source.RedactedAt)
	if err != nil {
		return collaboration.Source{}, false, err
	}
	if err = json.Unmarshal(tokens, &source.Tokens); err != nil {
		return collaboration.Source{}, false, err
	}
	return source, true, nil
}

func (u *collaborationTransaction) Save(ctx context.Context, accepted collaboration.Mutation) error {
	if u.trusted == nil {
		return collaboration.ErrInvalid
	}
	source := accepted.Source
	if source.MSPID != u.trusted.MSPID || source.ClientID != u.trusted.ClientID ||
		source.Parent.Type != u.trusted.ParentType || source.Parent.ID != u.trusted.ParentID ||
		source.Kind != u.trusted.SourceKind {
		return collaboration.ErrInvalid
	}
	tokenValues := source.Tokens
	if tokenValues == nil {
		tokenValues = []mentions.Token{}
	}
	tokens, err := json.Marshal(tokenValues)
	if err != nil {
		return err
	}
	if source.Version == 1 {
		_, err = u.tx.Exec(ctx, `INSERT INTO internal_collaboration_sources (id,msp_id,client_id,parent_type,parent_id,source_kind,body,mention_tokens,author_id,lifecycle_state,version,created_at,updated_at,redacted_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9,$10,$11,$12,$13,$14)`, source.ID, source.MSPID, source.ClientID, source.Parent.Type, source.Parent.ID, source.Kind, source.Body, tokens, source.AuthorID, source.LifecycleState, source.Version, source.CreatedAt, source.UpdatedAt, source.RedactedAt)
	} else {
		var tagRows int64
		tag, updateErr := u.tx.Exec(ctx, `UPDATE internal_collaboration_sources SET body=$7,mention_tokens=$8::jsonb,lifecycle_state=$9,version=$10,updated_at=$11,redacted_at=$12 WHERE id=$1::uuid AND msp_id=$2::uuid AND client_id=$3::uuid AND parent_type=$4 AND parent_id=$5::uuid AND source_kind=$6 AND version=$10-1`, source.ID, source.MSPID, source.ClientID, source.Parent.Type, source.Parent.ID, source.Kind, source.Body, tokens, source.LifecycleState, source.Version, source.UpdatedAt, source.RedactedAt)
		err = updateErr
		if err == nil {
			tagRows = tag.RowsAffected()
			if tagRows != 1 {
				return object.ErrVersionConflict
			}
		}
	}
	if err != nil {
		return err
	}
	for _, occurrence := range accepted.Mentions.Occurrences {
		targetType := string(occurrence.TargetType)
		if occurrence.TargetType == mentions.TargetStaff {
			targetType = "technician"
		}
		if _, err = u.tx.Exec(ctx, `INSERT INTO mention_occurrences (id,msp_id,client_id,source_id,source_revision,parent_type,parent_id,token_id,author_id,target_type,target_id,mentioned_at,correlation_id,authorization_revision) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,(SELECT revision FROM mention_access_revisions WHERE msp_id=$2::uuid))`, occurrence.ID, occurrence.MSPID, occurrence.ClientID, occurrence.SourceID, occurrence.SourceRevision, occurrence.ParentType, occurrence.ParentID, occurrence.TokenID, occurrence.AuthorID, targetType, occurrence.TargetID, occurrence.MentionedAt, occurrence.CorrelationID); err != nil {
			return err
		}
	}
	for _, resolution := range accepted.Mentions.Resolutions {
		teams := append([]string{}, resolution.ContributingTeamIDs...)
		sort.Strings(teams)
		if _, err = u.tx.Exec(ctx, `INSERT INTO mention_recipient_resolutions (id,msp_id,client_id,occurrence_id,recipient_id,decision,resolution_path,contributing_team_ids,decided_at,reason_code) VALUES ($1,$2,$3,$4,$5,$6,$7,$8::uuid[],$9,$10)`, resolution.ID, resolution.MSPID, resolution.ClientID, resolution.OccurrenceID, resolution.RecipientID, resolution.Decision, resolution.Path, teams, resolution.DecidedAt, resolution.ReasonCode); err != nil {
			return err
		}
	}
	for _, item := range accepted.Mentions.Items {
		const upsert = `INSERT INTO mention_items (id,msp_id,client_id,recipient_id,parent_type,parent_id,latest_occurrence_id,authorization_revision,state,read_at,archived_at,last_mentioned_at,suppressed_at,suppression_reason,version) VALUES ($1,$2,$3,$4,$5,$6,$7,(SELECT revision FROM mention_access_revisions WHERE msp_id=$2::uuid),'unread',NULL,NULL,$8,NULL,NULL,1) ON CONFLICT (msp_id, recipient_id, parent_type, parent_id) DO UPDATE SET client_id = EXCLUDED.client_id, latest_occurrence_id = EXCLUDED.latest_occurrence_id, authorization_revision = EXCLUDED.authorization_revision, last_mentioned_at = EXCLUDED.last_mentioned_at, state = 'unread', read_at = NULL, archived_at = NULL, suppressed_at = NULL, suppression_reason = NULL, version = mention_items.version + 1`
		if u.telemetry == nil {
			if _, err = u.tx.Exec(ctx, upsert, item.ID, item.MSPID, item.ClientID, item.RecipientID, item.ParentType, item.ParentID, item.LatestOccurrenceID, item.LastMentionedAt); err != nil {
				return err
			}
			continue
		}
		var version int64
		if err = u.tx.QueryRow(ctx, upsert+` RETURNING version`, item.ID, item.MSPID, item.ClientID, item.RecipientID, item.ParentType, item.ParentID, item.LatestOccurrenceID, item.LastMentionedAt).Scan(&version); err != nil {
			return err
		}
		if version > 1 {
			u.telemetryFacts = append(u.telemetryFacts, observability.MentionMetric{
				Name: "remention", ParentType: string(item.ParentType),
				State: "unread", Outcome: "unread_again", MSPID: item.MSPID,
				ClientID: item.ClientID, ObjectID: item.ParentID,
				OccurrenceID: item.LatestOccurrenceID,
			})
		}
	}
	for _, audit := range accepted.Mentions.Audits {
		if err = writeAuditOnly(ctx, u.tx, audit); err != nil {
			return err
		}
	}
	if accepted.Mentions.Event != nil {
		event := *accepted.Mentions.Event
		event.Data = cloneEventData(event.Data)
		event.Data["idempotency_key"] = accepted.IdempotencyKey
		event.Data["parent_type"] = string(source.Parent.Type)
		event.Data["parent_id"] = source.Parent.ID
		event.Data["source_kind"] = string(source.Kind)
		event.Data["lifecycle_state"] = string(source.LifecycleState)
		if err = writeEventOnly(ctx, u.tx, event); err != nil {
			return err
		}
	} else {
		data, _ := json.Marshal(map[string]any{"idempotency_key": accepted.IdempotencyKey, "parent_type": source.Parent.Type, "parent_id": source.Parent.ID, "source_kind": source.Kind, "lifecycle_state": source.LifecycleState})
		if _, err = u.tx.Exec(ctx, `INSERT INTO audit_ledger(id,occurred_at,msp_id,client_id,actor_type,actor_id,action,subject_type,subject_id,subject_version,source,correlation_id) VALUES(md5($1||':audit')::uuid,$2,$3,$4,'technician',$5,'internal_collaboration_source.saved','internal_collaboration_source',$6,$7,'internal-collaboration',md5($1||':correlation')::uuid)`, accepted.IdempotencyKey+source.ID, source.UpdatedAt, source.MSPID, source.ClientID, source.AuthorID, source.ID, source.Version); err != nil {
			return err
		}
		_, err = u.tx.Exec(ctx, `INSERT INTO event_outbox(event_id,event_type,schema_version,occurred_at,msp_id,client_id,actor_type,actor_id,subject_type,subject_id,subject_version,correlation_id,source,data) VALUES(md5($1)::uuid,'internal_collaboration_source.saved',1,$2,$3,$4,'technician',$5,'internal_collaboration_source',$6,$7,md5($1||':correlation')::uuid,'internal-collaboration',$8::jsonb)`, accepted.IdempotencyKey+source.ID, source.UpdatedAt, source.MSPID, source.ClientID, source.AuthorID, source.ID, source.Version, data)
	}
	if err == nil && u.telemetry != nil {
		u.telemetryFacts = append(u.telemetryFacts, accepted.Mentions.Telemetry...)
	}
	return err
}

func writeAuditOnly(ctx context.Context, tx transaction, audit mutation.AuditRecord) error {
	if audit.SafeDiff == nil {
		audit.SafeDiff = map[string]any{}
	}
	if audit.AuthorizationContext == nil {
		audit.AuthorizationContext = map[string]any{}
	}
	safeDiff, err := json.Marshal(audit.SafeDiff)
	if err != nil {
		return err
	}
	authorizationContext, err := json.Marshal(audit.AuthorizationContext)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_ledger (id,occurred_at,msp_id,client_id,actor_type,actor_id,action,subject_type,subject_id,subject_version,source,reason,correlation_id,safe_diff,authorization_context) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14::jsonb,$15::jsonb)`, audit.ID, audit.OccurredAt, audit.MSPID, nullableID(audit.ClientID), audit.ActorType, audit.ActorID, audit.Action, audit.SubjectType, audit.SubjectID, audit.SubjectVersion, audit.Source, nullableText(audit.Reason), audit.CorrelationID, safeDiff, authorizationContext)
	return err
}
func writeEventOnly(ctx context.Context, tx transaction, event mutation.EventRecord) error {
	data, err := json.Marshal(event.Data)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO event_outbox (event_id,event_type,schema_version,occurred_at,msp_id,client_id,actor_type,actor_id,subject_type,subject_id,subject_version,correlation_id,causation_id,source,data) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,NULLIF($13,'')::uuid,$14,$15::jsonb)`, event.EventID, event.EventType, event.SchemaVersion, event.OccurredAt, event.MSPID, nullableID(event.ClientID), event.ActorType, event.ActorID, event.SubjectType, event.SubjectID, event.SubjectVersion, event.CorrelationID, event.CausationID, event.Source, data)
	return err
}
func cloneEventData(input map[string]any) map[string]any {
	result := make(map[string]any, len(input)+4)
	for key, value := range input {
		result[key] = value
	}
	return result
}

func (u *collaborationTransaction) ListCandidates(ctx context.Context, query mentions.CandidateQuery) ([]mentions.Candidate, error) {
	if err := lockMentionAuthorizationEvidence(ctx, u.tx, query.Source.MSPID, query.Source.ClientID, []string{query.AuthorID}); err != nil {
		return nil, err
	}
	return listMentionCandidates(ctx, u.tx, query)
}
func (u *collaborationTransaction) LoadDirectAccess(ctx context.Context, ref mentions.SourceRef, ids []string) ([]mentions.MemberAccess, error) {
	if err := lockMentionAuthorizationEvidence(ctx, u.tx, ref.MSPID, ref.ClientID, ids); err != nil {
		return nil, err
	}
	return loadMentionDirectAccess(ctx, u.tx, ref, ids)
}
func (u *collaborationTransaction) LoadTeamAccess(ctx context.Context, ref mentions.SourceRef, ids []string) ([]mentions.TeamAccess, error) {
	return loadMentionTeamAccess(ctx, u.tx, ref, ids, true)
}

func lockMentionAuthorizationEvidence(ctx context.Context, q mentionQueryer, mspID, clientID string, technicianIDs []string) error {
	if len(technicianIDs) == 0 {
		return nil
	}
	technicianIDs = append([]string(nil), technicianIDs...)
	sort.Strings(technicianIDs)
	queries := []string{
		`SELECT 1 FROM technicians technician WHERE technician.msp_id=$1::uuid AND $2::text IS NOT NULL AND technician.id=ANY($3::uuid[]) ORDER BY technician.id FOR SHARE`,
		`SELECT 1 FROM role_assignments assignment WHERE assignment.msp_id=$1::uuid AND assignment.technician_id=ANY($3::uuid[]) AND (assignment.client_id IS NULL OR assignment.client_id=$2::uuid) ORDER BY assignment.technician_id,assignment.id FOR SHARE`,
		`SELECT 1 FROM roles role WHERE role.msp_id=$1::uuid AND role.id IN (SELECT assignment.role_id FROM role_assignments assignment WHERE assignment.msp_id=$1::uuid AND assignment.technician_id=ANY($3::uuid[]) AND (assignment.client_id IS NULL OR assignment.client_id=$2::uuid)) ORDER BY role.id FOR SHARE`,
		`SELECT 1 FROM role_capabilities capability WHERE capability.msp_id=$1::uuid AND capability.role_id IN (SELECT assignment.role_id FROM role_assignments assignment WHERE assignment.msp_id=$1::uuid AND assignment.technician_id=ANY($3::uuid[]) AND (assignment.client_id IS NULL OR assignment.client_id=$2::uuid)) ORDER BY capability.role_id,capability.capability FOR SHARE`,
	}
	for _, query := range queries {
		locked, err := q.Query(ctx, query, mspID, clientID, technicianIDs)
		if err != nil {
			return err
		}
		for locked.Next() {
			var marker int
			if err = locked.Scan(&marker); err != nil {
				locked.Close()
				return err
			}
		}
		err = locked.Err()
		locked.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
