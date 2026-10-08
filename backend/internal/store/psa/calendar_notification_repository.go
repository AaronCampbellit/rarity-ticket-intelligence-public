package psa

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/notifications"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type CalendarNotificationRepository struct {
	db    database
	newID func() string
}

func NewCalendarNotificationRepository(db database, newID func() string) *CalendarNotificationRepository {
	return &CalendarNotificationRepository{db: db, newID: newID}
}

func (r *CalendarNotificationRepository) GetCalendarPreference(ctx context.Context, mspID, technicianID string) (notifications.CalendarPreference, error) {
	preference := notifications.CalendarPreference{TechnicianID: technicianID, Rules: []notifications.CalendarPreferenceRule{}}
	err := r.db.QueryRow(ctx, `SELECT version FROM calendar_notification_preference_sets WHERE msp_id=$1::uuid AND technician_id=$2::uuid`, mspID, technicianID).Scan(&preference.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return preference, nil
	}
	if err != nil {
		return notifications.CalendarPreference{}, err
	}
	rows, err := r.db.Query(ctx, `SELECT event_class,change_class,urgency,channel,enabled FROM calendar_notification_preference_rules WHERE msp_id=$1::uuid AND technician_id=$2::uuid ORDER BY event_class,change_class,urgency,channel`, mspID, technicianID)
	if err != nil {
		return notifications.CalendarPreference{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var rule notifications.CalendarPreferenceRule
		if err = rows.Scan(&rule.EventClass, &rule.ChangeClass, &rule.Urgency, &rule.Channel, &rule.Enabled); err != nil {
			return notifications.CalendarPreference{}, err
		}
		preference.Rules = append(preference.Rules, rule)
	}
	return preference, rows.Err()
}

func (r *CalendarNotificationRepository) CalendarPreference(ctx context.Context, mspID, technicianID string) (notifications.CalendarPreference, error) {
	return r.GetCalendarPreference(ctx, mspID, technicianID)
}

func (r *CalendarNotificationRepository) ReplaceCalendarPreference(ctx context.Context, accepted notifications.CalendarPreferenceMutation) (result notifications.CalendarPreference, err error) {
	if r == nil || r.db == nil {
		return result, notifications.ErrInvalidCalendarPreference
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	result = accepted.Preference
	err = tx.QueryRow(ctx, `INSERT INTO calendar_notification_preference_sets(msp_id,technician_id,version,created_at,updated_at) SELECT $1::uuid,$2::uuid,1,now(),now() WHERE $3=0 ON CONFLICT(msp_id,technician_id) DO UPDATE SET version=calendar_notification_preference_sets.version+1,updated_at=now() WHERE calendar_notification_preference_sets.version=$3 RETURNING version`, accepted.MSPID, result.TechnicianID, accepted.ExpectedVersion).Scan(&result.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		_ = tx.Rollback(ctx)
		return notifications.CalendarPreference{}, object.ErrVersionConflict
	}
	if err != nil {
		return notifications.CalendarPreference{}, err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM calendar_notification_preference_rules WHERE msp_id=$1::uuid AND technician_id=$2::uuid`, accepted.MSPID, result.TechnicianID); err != nil {
		return result, err
	}
	for _, rule := range result.Rules {
		if _, err = tx.Exec(ctx, `INSERT INTO calendar_notification_preference_rules(msp_id,technician_id,event_class,change_class,urgency,channel,enabled,created_at) VALUES($1::uuid,$2::uuid,$3,$4,$5,$6,$7,now())`, accepted.MSPID, result.TechnicianID, rule.EventClass, rule.ChangeClass, rule.Urgency, rule.Channel, rule.Enabled); err != nil {
			return result, err
		}
	}
	accepted.Audit.SubjectVersion, accepted.Event.SubjectVersion = result.Version, result.Version
	accepted.Audit.SafeDiff = map[string]any{"rule_count": len(result.Rules)}
	accepted.Event.Data = map[string]any{"rule_count": len(result.Rules)}
	if err = writeAuditOnly(ctx, tx, accepted.Audit); err != nil {
		return result, err
	}
	if err = writeEventOnly(ctx, tx, accepted.Event); err != nil {
		return result, err
	}
	if err = tx.Commit(ctx); err != nil {
		return notifications.CalendarPreference{}, err
	}
	return result, nil
}

func (r *CalendarNotificationRepository) DueReminderCandidates(ctx context.Context, mspID string, now time.Time, limit int) ([]calendar.ReminderCandidate, error) {
	sourceReadSQL := strings.ReplaceAll(calendarProjectionSourceReadSQL, "$24::uuid", "projection.assignee_id")
	query := `SELECT projection.msp_id::text,projection.id::text,COALESCE(NULLIF(projection.source_role_key,''),'single'),projection.source_revision,'24h',
  (CASE WHEN projection.all_day THEN projection.starts_on::timestamp AT TIME ZONE 'UTC' ELSE projection.starts_at END)-interval '24 hours',
  COALESCE(NULLIF(projection.timezone,''),'UTC'),projection.assignee_id::text,true,` + sourceReadSQL + `
FROM calendar_event_projections projection
JOIN technicians technician ON technician.id=projection.assignee_id AND technician.msp_id=projection.msp_id AND technician.lifecycle_state='active'
WHERE projection.msp_id=$1::uuid AND projection.terminal_state='active'
  AND NOT projection.all_day AND projection.recurrence_rule IS NULL
  AND projection.event_role IN ('due','follow_up','milestone','maintenance','notice','renewal','expiration')
  AND projection.starts_at>$2 AND projection.starts_at-interval '24 hours'<=$2
  AND NOT EXISTS(SELECT 1 FROM calendar_reminder_facts fact WHERE fact.msp_id=projection.msp_id AND fact.projection_id=projection.id AND fact.occurrence_key=COALESCE(NULLIF(projection.source_role_key,''),'single') AND fact.threshold='24h' AND fact.source_revision=projection.source_revision)
ORDER BY (CASE WHEN projection.all_day THEN projection.starts_on::timestamp AT TIME ZONE 'UTC' ELSE projection.starts_at END),projection.id LIMIT $3`
	rows, err := r.db.Query(ctx, query, mspID, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]calendar.ReminderCandidate, 0)
	for rows.Next() {
		var candidate calendar.ReminderCandidate
		if err = rows.Scan(&candidate.MSPID, &candidate.ProjectionID, &candidate.OccurrenceID, &candidate.SourceRevision, &candidate.Threshold, &candidate.DueAt, &candidate.Timezone, &candidate.RecipientID, &candidate.Active, &candidate.Accessible); err != nil {
			return nil, err
		}
		result = append(result, candidate)
	}
	return result, rows.Err()
}

func (r *CalendarNotificationRepository) ClaimReminder(ctx context.Context, key string, fact calendar.ReminderFact) (owned bool, err error) {
	want := fmt.Sprintf("%s:%s:%s:%d", fact.ProjectionID, fact.OccurrenceID, fact.Threshold, fact.SourceRevision)
	if r == nil || r.db == nil || r.newID == nil || key != want {
		return false, calendar.ErrInvalidReminderEvaluation
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	sourceReadSQL := strings.ReplaceAll(calendarProjectionSourceReadSQL, "$24::uuid", "projection.assignee_id")
	claimQuery := `INSERT INTO calendar_reminder_facts(id,msp_id,client_id,client_scope_key,projection_id,occurrence_key,reminder_kind,remind_at,state,claimed_at,source_revision,threshold,created_at,updated_at)
SELECT $1::uuid,projection.msp_id,projection.client_id,projection.client_scope_key,projection.id,$4::text,'due',$7::timestamptz,'claimed',$8::timestamptz,$5::bigint,$6::text,$8::timestamptz,$8::timestamptz
FROM calendar_event_projections projection
JOIN technicians technician ON technician.id=projection.assignee_id AND technician.msp_id=projection.msp_id AND technician.lifecycle_state='active'
WHERE projection.msp_id=$2::uuid AND projection.id=$3::uuid AND projection.source_revision=$5 AND projection.terminal_state='active'
  AND projection.assignee_id=$9::uuid AND (` + sourceReadSQL + `)
ON CONFLICT (msp_id,projection_id,occurrence_key,threshold,source_revision) DO NOTHING`
	tag, err := tx.Exec(ctx, claimQuery, r.newID(), fact.MSPID, fact.ProjectionID, fact.OccurrenceID, fact.SourceRevision, fact.Threshold, fact.DueAt, fact.EvaluatedAt, fact.RecipientID)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() != 1 {
		_ = tx.Rollback(ctx)
		return false, nil
	}
	var sourceType, sourceID, clientID, eventRole string
	err = tx.QueryRow(ctx, `SELECT source_type,source_id::text,COALESCE(client_id::text,''),event_role FROM calendar_event_projections WHERE msp_id=$1::uuid AND id=$2::uuid AND source_revision=$3 AND assignee_id=$4::uuid AND terminal_state='active'`, fact.MSPID, fact.ProjectionID, fact.SourceRevision, fact.RecipientID).Scan(&sourceType, &sourceID, &clientID, &eventRole)
	if err != nil {
		return false, err
	}
	eventID := schedulingFactID(key, "calendar.schedule_changed")
	payload := canonicalCalendarEventData{RecipientID: fact.RecipientID, ChangeClass: "reminder", Urgency: "routine", SourceRefs: []canonicalCalendarSourceRef{{Type: sourceType, ID: sourceID, ClientID: clientID, EventRole: eventRole, SourceRevision: fact.SourceRevision}}, ActionPath: "/calendar"}
	if err = writeCanonicalCalendarEvent(ctx, tx, eventID, fact.EvaluatedAt, fact.MSPID, clientID, "system", "00000000-0000-0000-0000-000000000000", "calendar_projection", fact.ProjectionID, fact.SourceRevision, eventID, payload); err != nil {
		return false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func (r *CalendarNotificationRepository) CanViewCalendarSource(ctx context.Context, recipientID string, source calendar.SourceRef) (bool, error) {
	if r == nil || r.db == nil {
		return false, nil
	}
	principal := authorization.Principal{ID: recipientID, Scope: scope.Principal{MSPID: source.MSPID}}
	return NewCalendarRepository(r.db).CanReadCalendarSource(ctx, principal, source)
}

var _ notifications.CalendarPreferenceRepository = (*CalendarNotificationRepository)(nil)
var _ notifications.CalendarPreferenceReader = (*CalendarNotificationRepository)(nil)
var _ notifications.CalendarPermissionChecker = (*CalendarNotificationRepository)(nil)
var _ calendar.ReminderRepository = (*CalendarNotificationRepository)(nil)
