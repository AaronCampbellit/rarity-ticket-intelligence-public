package psa

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/id"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/notifications"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type NotificationRepository struct {
	db              database
	publicURL       string
	emailConfigured bool
}

var _ notifications.PolicyManagementRepository = (*NotificationRepository)(nil)
var _ notifications.PlannerRepository = (*NotificationRepository)(nil)
var _ notifications.DeliveryRepository = (*NotificationRepository)(nil)
var _ notifications.InboxRepository = (*NotificationRepository)(nil)
var _ notifications.TeamsDeliveryHistory = (*NotificationRepository)(nil)
var _ notifications.PreferenceRepository = (*NotificationRepository)(nil)

func NewNotificationRepository(db database) *NotificationRepository {
	return &NotificationRepository{db: db}
}

func (r *NotificationRepository) WithExternalDelivery(publicURL string, emailConfigured bool) *NotificationRepository {
	r.publicURL = strings.TrimRight(strings.TrimSpace(publicURL), "/")
	r.emailConfigured = emailConfigured
	return r
}

func (r *NotificationRepository) ListPolicies(
	ctx context.Context,
	target scope.Target,
) ([]notifications.PublishedPolicy, error) {
	rows, err := r.db.Query(ctx, `
SELECT p.id::text, p.msp_id::text, COALESCE(p.client_id::text, ''),
       p.key, p.name, p.version, v.event_type, v.conditions, v.destinations,
       v.quiet_period_seconds, v.critical_bypass, v.enabled,
       v.priority, v.stable_order, v.published_at, v.published_by::text
FROM notification_policies p
JOIN notification_policy_versions v
  ON v.policy_id = p.id AND v.version = p.version
WHERE p.msp_id = $1
  AND (
    ($2 = '' AND p.client_id IS NULL)
    OR ($2 <> '' AND (p.client_id IS NULL OR p.client_id = $2::uuid))
  )
ORDER BY v.priority DESC, v.stable_order, p.id
`, target.MSPID, target.ClientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]notifications.PublishedPolicy, 0)
	for rows.Next() {
		var policy notifications.PublishedPolicy
		var conditions, destinations []byte
		if err := rows.Scan(
			&policy.ID, &policy.MSPID, &policy.ClientID,
			&policy.Key, &policy.Name, &policy.Version, &policy.EventType,
			&conditions, &destinations, &policy.QuietPeriodSeconds,
			&policy.CriticalBypass, &policy.Enabled, &policy.Priority,
			&policy.StableOrder, &policy.PublishedAt, &policy.PublishedBy,
		); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(conditions, &policy.Conditions); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(destinations, &policy.Destinations); err != nil {
			return nil, err
		}
		result = append(result, policy)
	}
	return result, rows.Err()
}

func (r *NotificationRepository) ValidateDestinations(
	ctx context.Context,
	target scope.Target,
	destinations []notifications.Destination,
) error {
	teams := make([]string, 0)
	seen := map[string]struct{}{}
	for _, destination := range destinations {
		if destination.Channel != notifications.Teams {
			continue
		}
		if _, exists := seen[destination.RecipientRef]; !exists {
			teams = append(teams, destination.RecipientRef)
			seen[destination.RecipientRef] = struct{}{}
		}
	}
	if len(teams) == 0 {
		return nil
	}
	var count int
	err := r.db.QueryRow(ctx, `
SELECT count(*)::integer
FROM teams_connections connection
WHERE connection.msp_id = $1
  AND connection.id = ANY($3::uuid[])
  AND connection.enabled
  AND (
    connection.client_id IS NULL
    OR connection.client_id = NULLIF($2, '')::uuid
  )
`, target.MSPID, target.ClientID, teams).Scan(&count)
	if err != nil {
		return err
	}
	if count != len(teams) {
		return scope.ErrNotFound
	}
	return nil
}

func (r *NotificationRepository) PublishPolicyAtomic(
	ctx context.Context,
	accepted notifications.PolicyPublishMutation,
) error {
	conditions, err := json.Marshal(accepted.Policy.Conditions)
	if err != nil {
		return err
	}
	destinations, err := json.Marshal(accepted.Policy.Destinations)
	if err != nil {
		return err
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	policy := accepted.Policy
	if accepted.Created {
		if _, err := tx.Exec(ctx, `
INSERT INTO notification_policies (
  id, msp_id, client_id, key, name, event_type, conditions, channels,
  quiet_period_seconds, critical_bypass, enabled, priority, stable_order, version
) VALUES (
  $1, $2, NULLIF($3, '')::uuid, $4, $5, $6, $7, $8,
  $9, $10, $11, $12, $13, $14
)
`, policy.ID, policy.MSPID, policy.ClientID, policy.Key, policy.Name,
			policy.EventType, conditions, destinations, policy.QuietPeriodSeconds,
			policy.CriticalBypass, policy.Enabled, policy.Priority,
			policy.StableOrder, policy.Version); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
	} else {
		tag, err := tx.Exec(ctx, `
UPDATE notification_policies
SET key = $4, name = $5, event_type = $6, conditions = $7, channels = $8,
    quiet_period_seconds = $9, critical_bypass = $10, enabled = $11,
    priority = $12, stable_order = $13, version = version + 1
WHERE id = $1 AND msp_id = $2
  AND (
    ($3 = '' AND client_id IS NULL)
    OR ($3 <> '' AND client_id = $3::uuid)
  )
  AND version = $14
`, policy.ID, policy.MSPID, policy.ClientID, policy.Key, policy.Name,
			policy.EventType, conditions, destinations, policy.QuietPeriodSeconds,
			policy.CriticalBypass, policy.Enabled, policy.Priority,
			policy.StableOrder, accepted.ExpectedVersion)
		if err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if tag.RowsAffected() != 1 {
			_ = tx.Rollback(ctx)
			return object.ErrVersionConflict
		}
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO notification_policy_versions (
  policy_id, msp_id, version, event_type, conditions, destinations,
  quiet_period_seconds, critical_bypass, enabled, priority, stable_order,
  published_at, published_by
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
`, policy.ID, policy.MSPID, policy.Version, policy.EventType, conditions,
		destinations, policy.QuietPeriodSeconds, policy.CriticalBypass,
		policy.Enabled, policy.Priority, policy.StableOrder,
		policy.PublishedAt, policy.PublishedBy); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}

func (r *NotificationRepository) ClaimEvents(
	ctx context.Context,
	limit int,
	now time.Time,
) ([]notifications.PlanningEvent, error) {
	mentionAccess := accessPredicate("resolution.recipient_id", "event.msp_id", "event.client_id", "occurrence.parent_type", "occurrence.parent_id")
	rows, err := r.db.Query(ctx, `
WITH seed AS (
 SELECT event.event_id,event.event_type,event.msp_id,event.correlation_id
 FROM event_outbox event
 LEFT JOIN notification_event_plans planned ON planned.event_id=event.event_id
 WHERE planned.event_id IS NULL AND event.occurred_at <= $1
   AND event.event_type <> 'notification_policy.published'
 ORDER BY event.occurred_at,event.event_id
 LIMIT $2
),
candidates AS (
 SELECT event.*
 FROM event_outbox event
 LEFT JOIN notification_event_plans planned ON planned.event_id=event.event_id
 WHERE planned.event_id IS NULL AND event.occurred_at <= $1
   AND event.event_type <> 'notification_policy.published'
   AND (
     event.event_id IN (SELECT seed.event_id FROM seed)
     OR (
       event.event_type='calendar.schedule_changed'
       AND EXISTS (
         SELECT 1 FROM seed
         WHERE seed.event_type='calendar.schedule_changed'
           AND seed.msp_id=event.msp_id
           AND seed.correlation_id=event.correlation_id
       )
     )
   )
)
SELECT event.event_id::text,event.event_type,event.msp_id::text,
       COALESCE(event.client_id::text,''),COALESCE(work.id::text,''),event.occurred_at,
       COALESCE(work.record_type,''),COALESCE(work.priority,''),COALESCE(work.queue_id::text, ''),
       COALESCE(queue.team_id::text, ''), COALESCE(queue.department_id::text, ''),
       COALESCE(binding.workflow_id::text, ''), COALESCE(work.contract_id::text, ''),
       COALESCE(work.priority='critical',false),
       COALESCE(mention.parent_type,event.subject_type),
       COALESCE(mention.parent_id::text,event.subject_id::text),
       COALESCE(mention.occurrence_id::text,''),COALESCE(mention.recipient_id::text,''),
       event.correlation_id::text,event.data
FROM candidates event
LEFT JOIN LATERAL (
 SELECT occurrence.id occurrence_id,occurrence.parent_type,occurrence.parent_id,
        resolution.recipient_id,
        CASE WHEN occurrence.parent_type='work_record' THEN occurrence.parent_id
             WHEN occurrence.parent_type='task' THEN task.work_record_id END work_record_id
 FROM mention_occurrences occurrence
 JOIN mention_recipient_resolutions resolution
   ON resolution.occurrence_id=occurrence.id AND resolution.msp_id=occurrence.msp_id
  AND resolution.client_id=occurrence.client_id AND resolution.decision='eligible'
 JOIN mention_items item
   ON item.msp_id=occurrence.msp_id AND item.client_id=occurrence.client_id
  AND item.recipient_id=resolution.recipient_id
  AND item.parent_type=occurrence.parent_type AND item.parent_id=occurrence.parent_id
  AND item.latest_occurrence_id=occurrence.id AND item.suppressed_at IS NULL
 LEFT JOIN tasks task ON occurrence.parent_type='task' AND task.id=occurrence.parent_id
 WHERE event.event_type='mention.occurred'
   AND occurrence.source_id=event.subject_id AND occurrence.source_revision=event.subject_version
   AND (`+mentionAccess+`)
 ORDER BY resolution.recipient_id,occurrence.mentioned_at DESC,occurrence.id DESC
) mention ON true
LEFT JOIN work_records work
  ON work.msp_id = event.msp_id
 AND work.client_id = event.client_id
 AND work.id = COALESCE(mention.work_record_id,CASE
   WHEN event.subject_type = 'work_record' THEN event.subject_id
   WHEN event.subject_type = 'comment' THEN (
     SELECT comment.work_record_id FROM comments comment
     WHERE comment.id = event.subject_id
   )
   WHEN event.subject_type = 'task' THEN (
     SELECT task.work_record_id FROM tasks task
     WHERE task.id = event.subject_id
   )
   WHEN event.subject_type = 'work_record_sla' THEN (
     SELECT timer.work_record_id FROM work_record_slas timer
     WHERE timer.id = event.subject_id
   )
 END)
LEFT JOIN queues queue ON queue.id = work.queue_id AND queue.msp_id = work.msp_id
LEFT JOIN work_record_workflows binding ON binding.work_record_id = work.id
WHERE event.event_type IN ('mention.occurred','calendar.schedule_changed')
   OR (event.event_type NOT IN ('mention.occurred','calendar.schedule_changed') AND work.id IS NOT NULL)
ORDER BY event.occurred_at,event.event_id,mention.recipient_id
`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]notifications.PlanningEvent, 0)
	for rows.Next() {
		var event notifications.PlanningEvent
		var data []byte
		if err := rows.Scan(
			&event.ID, &event.Type, &event.MSPID, &event.ClientID,
			&event.WorkRecordID, &event.OccurredAt, &event.RecordType,
			&event.Priority, &event.QueueID, &event.TeamID, &event.DepartmentID,
			&event.WorkflowID, &event.ContractID, &event.Critical,
			&event.SubjectType, &event.SubjectID, &event.MentionOccurrenceID,
			&event.RecipientTechnicianID, &event.CorrelationID, &data,
		); err != nil {
			return nil, err
		}
		if event.Type == notifications.CalendarNotificationEventType {
			if err := event.DecodeCanonicalCalendarData(data); err != nil {
				return nil, err
			}
		}
		result = append(result, event)
	}
	return result, rows.Err()
}

func (r *NotificationRepository) ListPoliciesForEvent(
	ctx context.Context,
	event notifications.PlanningEvent,
) ([]notifications.PublishedPolicy, error) {
	policies, err := r.ListPolicies(
		ctx, scope.Target{MSPID: event.MSPID, ClientID: event.ClientID},
	)
	if err != nil {
		return nil, err
	}
	result := policies[:0]
	for _, policy := range policies {
		if policy.EventType == event.Type {
			result = append(result, policy)
		}
	}
	return result, nil
}

func (r *NotificationRepository) RecentDeliveries(
	ctx context.Context,
	event notifications.PlanningEvent,
) ([]notifications.RecentDelivery, error) {
	rows, err := r.db.Query(ctx, `
SELECT policy_id::text, work_record_id::text, delivered_at
FROM notification_deliveries
WHERE msp_id = $1 AND client_id = $2
  AND work_record_id = $3 AND state = 'delivered'
  AND delivered_at IS NOT NULL
ORDER BY delivered_at DESC
`, event.MSPID, event.ClientID, event.WorkRecordID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]notifications.RecentDelivery, 0)
	for rows.Next() {
		var delivery notifications.RecentDelivery
		if err := rows.Scan(
			&delivery.PolicyID, &delivery.WorkRecordID, &delivery.DeliveredAt,
		); err != nil {
			return nil, err
		}
		result = append(result, delivery)
	}
	return result, rows.Err()
}

func (r *NotificationRepository) PlanAtomic(
	ctx context.Context,
	decision notifications.PlannedDecision,
) (bool, error) {
	calendarEvents, err := notifications.ValidateCalendarPlanningDecision(decision)
	if err != nil {
		return false, err
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	claimEvents := []notifications.PlanningEvent{decision.Event}
	if len(calendarEvents) > 0 {
		claimEvents = calendarEvents
	}
	for _, event := range claimEvents {
		claim, err := tx.Exec(ctx, `
INSERT INTO notification_event_plans (event_id, planned_at)
VALUES ($1, $2)
ON CONFLICT (event_id) DO NOTHING
`, event.ID, decision.PlannedAt)
		if err != nil {
			_ = tx.Rollback(ctx)
			return false, err
		}
		if claim.RowsAffected() == 0 {
			if len(calendarEvents) > 0 {
				_ = tx.Rollback(ctx)
				return false, nil
			}
			if err := tx.Commit(ctx); err != nil {
				_ = tx.Rollback(ctx)
				return false, err
			}
			return false, nil
		}
	}
	for _, delivery := range decision.Deliveries {
		var suppressedAt any
		persistedRecipientID := delivery.RecipientTechnicianID
		calendarDeliveryKey := ""
		persistedCalendarDeliveryKey := ""
		if delivery.CalendarPayload != nil {
			persistedRecipientID = ""
			calendarDeliveryKey = strings.Join([]string{
				delivery.MSPID,
				delivery.CalendarPayload.CorrelationID,
				delivery.RecipientTechnicianID,
				string(delivery.CalendarPayload.ChangeClass),
				string(delivery.CalendarPayload.Urgency),
				string(delivery.Channel),
				delivery.PolicyID,
				fmt.Sprint(delivery.PolicyVersion),
			}, ":")
		}
		if delivery.State == notifications.Suppressed {
			suppressedAt = delivery.PlannedAt
		}
		var inserted int64
		if delivery.CalendarPayload != nil {
			persistedCalendarDeliveryKey, err = persistCalendarNotificationDelivery(ctx, tx, delivery, suppressedAt, calendarDeliveryKey)
			if err == nil {
				inserted = 1
			}
		} else {
			tag, insertErr := tx.Exec(ctx, `
INSERT INTO notification_deliveries (
  id, policy_id, policy_version, msp_id, client_id, work_record_id,
  event_id, channel, recipient_ref, content_classification,
  state, next_attempt_at, suppressed_at, suppression_reason, planned_at,
  mention_occurrence_id,recipient_technician_id
) VALUES (
  $1, $2, $3, $4, NULLIF($5, '')::uuid, NULLIF($6, '')::uuid,
  $7, $8, $9, $10, $11, $12, $13, NULLIF($14, ''), $15,
  NULLIF($16,'')::uuid,NULLIF($17,'')::uuid
)
ON CONFLICT DO NOTHING
`, delivery.ID, delivery.PolicyID, delivery.PolicyVersion,
				delivery.MSPID, delivery.ClientID, delivery.WorkRecordID,
				delivery.EventID, delivery.Channel, delivery.RecipientRef,
				delivery.ContentClassification, delivery.State, delivery.PlannedAt,
				suppressedAt, delivery.SuppressionReason, delivery.PlannedAt,
				delivery.MentionOccurrenceID, persistedRecipientID)
			err = insertErr
			inserted = tag.RowsAffected()
		}
		if err != nil {
			_ = tx.Rollback(ctx)
			return false, err
		}
		if delivery.CalendarPayload != nil && inserted == 0 {
			_ = tx.Rollback(ctx)
			return false, errors.New("calendar notification delivery was not persisted")
		}
		if delivery.CalendarPayload == nil {
			continue
		}
		sources, err := json.Marshal(delivery.CalendarPayload.Sources)
		if err != nil {
			_ = tx.Rollback(ctx)
			return false, err
		}
		payloadTag, err := tx.Exec(ctx, `
INSERT INTO notification_calendar_delivery_payloads (
  delivery_id,msp_id,recipient_technician_id,correlation_id,
  change_class,urgency,source_refs,action_path,calendar_delivery_key
) SELECT delivery.id,$1,$2,$3,$4,$5,$6::jsonb,$7,delivery.calendar_delivery_key
FROM notification_deliveries delivery
WHERE delivery.msp_id=$1::uuid AND delivery.calendar_delivery_key=$8
ON CONFLICT (delivery_id) DO UPDATE
SET source_refs=(
  SELECT COALESCE(jsonb_agg(merged.source_ref ORDER BY merged.source_ref::text),'[]'::jsonb)
  FROM (
    SELECT DISTINCT source_ref
    FROM jsonb_array_elements(
      notification_calendar_delivery_payloads.source_refs || EXCLUDED.source_refs
    ) AS refs(source_ref)
  ) merged
)
WHERE notification_calendar_delivery_payloads.msp_id=EXCLUDED.msp_id
  AND notification_calendar_delivery_payloads.recipient_technician_id=EXCLUDED.recipient_technician_id
  AND notification_calendar_delivery_payloads.correlation_id=EXCLUDED.correlation_id
  AND notification_calendar_delivery_payloads.change_class=EXCLUDED.change_class
  AND notification_calendar_delivery_payloads.urgency=EXCLUDED.urgency
  AND notification_calendar_delivery_payloads.action_path=EXCLUDED.action_path
	AND notification_calendar_delivery_payloads.calendar_delivery_key=EXCLUDED.calendar_delivery_key
	`, delivery.MSPID, delivery.RecipientTechnicianID,
			delivery.CalendarPayload.CorrelationID, delivery.CalendarPayload.ChangeClass,
			delivery.CalendarPayload.Urgency, sources, delivery.CalendarPayload.ActionPath,
			persistedCalendarDeliveryKey)
		if err != nil {
			_ = tx.Rollback(ctx)
			return false, err
		}
		if payloadTag.RowsAffected() != 1 {
			_ = tx.Rollback(ctx)
			return false, errors.New("calendar notification payload was not inserted")
		}
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return false, err
	}
	return true, nil
}

func persistCalendarNotificationDelivery(ctx context.Context, tx transaction, delivery notifications.PlannedDelivery, suppressedAt any, logicalKey string) (string, error) {
	insert := func(key string) (int64, error) {
		tag, err := tx.Exec(ctx, `
INSERT INTO notification_deliveries (
  id, policy_id, policy_version, msp_id, client_id, work_record_id,
  event_id, channel, recipient_ref, content_classification,
  state, next_attempt_at, suppressed_at, suppression_reason, planned_at,
  mention_occurrence_id,recipient_technician_id,calendar_delivery_key
) VALUES (
  $1, $2, $3, $4, NULLIF($5, '')::uuid, NULLIF($6, '')::uuid,
  $7, $8, $9, $10, $11, $12, $13, NULLIF($14, ''), $15,
  NULLIF($16,'')::uuid,NULLIF($17,'')::uuid,$18
)
ON CONFLICT (msp_id,calendar_delivery_key)
WHERE calendar_delivery_key IS NOT NULL
DO UPDATE SET calendar_delivery_key=EXCLUDED.calendar_delivery_key
WHERE notification_deliveries.state='pending'
  AND notification_deliveries.attempts=0
`, delivery.ID, delivery.PolicyID, delivery.PolicyVersion,
			delivery.MSPID, delivery.ClientID, delivery.WorkRecordID,
			delivery.EventID, delivery.Channel, delivery.RecipientRef,
			delivery.ContentClassification, delivery.State, delivery.PlannedAt,
			suppressedAt, delivery.SuppressionReason, delivery.PlannedAt,
			delivery.MentionOccurrenceID, "", key)
		return tag.RowsAffected(), err
	}

	rows, err := insert(logicalKey)
	if err != nil {
		return "", err
	}
	if rows == 1 {
		return logicalKey, nil
	}

	// The failed conditional upsert locks the base logical delivery until this
	// transaction ends. That lock serializes follow-up selection against both a
	// concurrent planner and ClaimPending. Merge only into an unclaimed pending
	// generation; a worker may already hold every other generation's old payload.
	var followupKey string
	err = tx.QueryRow(ctx, `SELECT calendar_delivery_key
FROM notification_deliveries
WHERE msp_id=$1::uuid
  AND starts_with(calendar_delivery_key,$2)
  AND state='pending' AND attempts=0
ORDER BY planned_at,id
FOR UPDATE
LIMIT 1`, delivery.MSPID, logicalKey+":followup:").Scan(&followupKey)
	if err == nil {
		return followupKey, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}

	followupKey = logicalKey + ":followup:" + delivery.EventID
	rows, err = insert(followupKey)
	if err != nil {
		return "", err
	}
	if rows != 1 {
		return "", errors.New("calendar notification follow-up delivery was not persisted")
	}
	return followupKey, nil
}

func (r *NotificationRepository) ClaimPending(
	ctx context.Context,
	limit int,
	now time.Time,
) ([]notifications.DeliveryJob, error) {
	rows, err := r.db.Query(ctx, `
WITH candidates AS (
  SELECT delivery.id
  FROM notification_deliveries delivery
  WHERE delivery.state = 'pending' AND delivery.next_attempt_at <= $1
  ORDER BY delivery.next_attempt_at, delivery.id
  FOR UPDATE SKIP LOCKED
  LIMIT $2
),
leased AS (
  UPDATE notification_deliveries delivery
  SET attempts = delivery.attempts + 1,
      next_attempt_at = $1 + interval '5 minutes'
  FROM candidates
  WHERE delivery.id = candidates.id
  RETURNING delivery.*, delivery.attempts - 1 AS previous_attempts
)
SELECT leased.id::text, leased.event_id::text,
       leased.msp_id::text, COALESCE(leased.work_record_id::text, ''), leased.channel,
       leased.previous_attempts, leased.planned_at,
	   leased.content_classification,
       COALESCE(occurrence.parent_type,''),COALESCE(occurrence.parent_id::text,''),
       COALESCE(leased.mention_occurrence_id::text,''),COALESCE(leased.recipient_technician_id::text,''),
       COALESCE(connection.id::text, ''), COALESCE(connection.msp_id::text, ''),
       COALESCE(connection.client_id::text, ''), COALESCE(connection.name, ''),
       COALESCE(connection.webhook_secret_ref, ''), COALESCE(connection.version, 0),
       COALESCE(NOT connection.enabled, false),
       COALESCE(work.display_id, ''), COALESCE(work.priority, ''),
       COALESCE(work.status, ''), COALESCE(work.title, ''),
       COALESCE(timer.resolution_state, timer.response_state, ''),
       CASE WHEN work.id IS NULL THEN '' ELSE '/work-records/' || work.id::text END,
	   COALESCE(payload.recipient_technician_id::text,''),
	   COALESCE(payload.correlation_id::text,''),COALESCE(payload.change_class,''),
	   COALESCE(payload.urgency,''),COALESCE(payload.source_refs,'[]'::jsonb),
	   COALESCE(payload.action_path,''),
	   COALESCE(payload.calendar_delivery_key,'')
FROM leased
LEFT JOIN notification_calendar_delivery_payloads payload
  ON payload.delivery_id=leased.id AND payload.msp_id=leased.msp_id
LEFT JOIN mention_occurrences occurrence
  ON occurrence.id=leased.mention_occurrence_id AND occurrence.msp_id=leased.msp_id
 AND occurrence.client_id=leased.client_id
LEFT JOIN work_records work
  ON work.id = leased.work_record_id
 AND work.msp_id = leased.msp_id AND work.client_id = leased.client_id
LEFT JOIN work_record_slas timer
  ON timer.work_record_id = work.id
LEFT JOIN teams_connections connection
  ON leased.channel = 'teams'
 AND connection.id = (
   CASE WHEN leased.channel = 'teams' THEN leased.recipient_ref ELSE NULL END
 )::uuid
 AND connection.msp_id = leased.msp_id
 AND (connection.client_id IS NULL OR connection.client_id = leased.client_id)
`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]notifications.DeliveryJob, 0)
	for rows.Next() {
		var job notifications.DeliveryJob
		var correlationID, changeClass, urgency, actionPath string
		var sourceRefs []byte
		if err := rows.Scan(
			&job.ID, &job.EventID, &job.MSPID, &job.WorkRecordID, &job.Channel,
			&job.Attempts, &job.PlannedAt,
			&job.ContentClassification,
			&job.SubjectType, &job.SubjectID, &job.MentionOccurrenceID,
			&job.RecipientTechnicianID,
			&job.Connection.ID, &job.Connection.MSPID, &job.Connection.ClientID,
			&job.Connection.Name, &job.Connection.WebhookSecretRef, &job.Connection.Version,
			&job.Connection.Disabled,
			&job.Summary.DisplayID, &job.Summary.Priority, &job.Summary.Status,
			&job.Summary.Subject, &job.Summary.SLAState,
			&job.Summary.AuthenticatedURL,
			&job.CalendarRecipientID, &correlationID, &changeClass,
			&urgency, &sourceRefs, &actionPath, &job.DeduplicationKey,
		); err != nil {
			return nil, err
		}
		if job.CalendarRecipientID != "" {
			var sources []notifications.CalendarSourceReference
			if err := json.Unmarshal(sourceRefs, &sources); err != nil {
				return nil, err
			}
			job.CalendarPayload = &notifications.CalendarDeliveryPayload{
				CorrelationID: correlationID, ChangeClass: notifications.CalendarChangeClass(changeClass),
				Urgency: notifications.CalendarUrgency(urgency), Sources: sources, ActionPath: actionPath,
			}
			if err := notifications.ValidateCalendarDeliveryPayload(job.MSPID, job.CalendarRecipientID, job.CalendarPayload); err != nil {
				return nil, err
			}
		}
		result = append(result, job)
	}
	return result, rows.Err()
}

func (r *NotificationRepository) ReauthorizeCalendarDelivery(ctx context.Context, job notifications.DeliveryJob) (notifications.CalendarRenderedDelivery, error) {
	rendered := notifications.CalendarRenderedDelivery{Outcome: notifications.CalendarAuthorizationSuppressed}
	if job.CalendarPayload == nil || notifications.ValidateCalendarDeliveryPayload(job.MSPID, job.CalendarRecipientID, job.CalendarPayload) != nil {
		rendered.SuppressionReason = "recipient_no_longer_relevant"
		return rendered, nil
	}
	var active, policyOK, preferenceOK bool
	err := r.db.QueryRow(ctx, `
SELECT recipient.lifecycle_state='active',
	 policy.enabled AND policy.event_type='calendar.schedule_changed' AND EXISTS(
	   SELECT 1 FROM jsonb_array_elements(policy.destinations) destination
   WHERE destination->>'channel'=delivery.channel
     AND destination->>'recipient_ref'='calendar.assignee'
 ),
 COALESCE((SELECT rule.enabled
   FROM calendar_notification_preference_rules rule
   WHERE rule.msp_id=payload.msp_id
     AND rule.technician_id=payload.recipient_technician_id
     AND rule.event_class='calendar.schedule_changed'
     AND rule.change_class=payload.change_class
     AND rule.urgency=payload.urgency
	     AND rule.channel=delivery.channel),true),
 recipient.email
FROM notification_deliveries delivery
JOIN notification_calendar_delivery_payloads payload
  ON payload.delivery_id=delivery.id AND payload.msp_id=delivery.msp_id
JOIN technicians recipient
  ON recipient.id=payload.recipient_technician_id AND recipient.msp_id=payload.msp_id
JOIN notification_policy_versions policy
  ON policy.policy_id=delivery.policy_id AND policy.msp_id=delivery.msp_id
 AND policy.version=delivery.policy_version
WHERE delivery.id=$1::uuid AND delivery.event_id=$2::uuid
  AND payload.recipient_technician_id=$3::uuid
  AND delivery.msp_id=$4::uuid
`, job.ID, job.EventID, job.CalendarRecipientID, job.MSPID).Scan(&active, &policyOK, &preferenceOK, &rendered.RecipientEmail)
	if errors.Is(err, pgx.ErrNoRows) {
		rendered.SuppressionReason = "recipient_no_longer_relevant"
		return rendered, nil
	}
	if err != nil {
		return notifications.CalendarRenderedDelivery{}, err
	}
	if !active {
		rendered.SuppressionReason = "recipient_inactive"
		return rendered, nil
	}
	if !preferenceOK {
		rendered.SuppressionReason = "preference_disabled"
		return rendered, nil
	}
	if !policyOK {
		rendered.SuppressionReason = "channel_unavailable"
		return rendered, nil
	}

	action, err := url.Parse(job.CalendarPayload.ActionPath)
	if err != nil || action.IsAbs() || action.Host != "" || action.User != nil || action.RawQuery != "" || action.Fragment != "" || !strings.HasPrefix(action.Path, "/") || strings.HasPrefix(action.Path, "//") {
		rendered.SuppressionReason = "channel_unavailable"
		return rendered, nil
	}
	details := make([]string, 0, len(job.CalendarPayload.Sources))
	projectionLag := false
	for _, source := range job.CalendarPayload.Sources {
		ptoDecision := source.Type == "pto" && job.CalendarPayload.ChangeClass == notifications.CalendarPTO
		if job.CalendarPayload.ChangeClass != notifications.CalendarCancellation {
			var latestRevision int64
			var relevant bool
			err = r.db.QueryRow(ctx, `SELECT GREATEST(
  COALESCE(MAX(projection.source_revision),0),
  COALESCE((
    SELECT MAX(change.source_revision)
    FROM calendar_live_changes change
    WHERE change.msp_id=$1::uuid
      AND change.client_id IS NOT DISTINCT FROM NULLIF($2,'')::uuid
      AND change.source_type=$3 AND change.source_id=$4::uuid
      AND (change.event_role=$5 OR change.event_role='source')
      AND change.change_type='removed'
  ),0)
),COALESCE(bool_or(
  (($8::boolean AND projection.source_revision=$6) OR
   (NOT $8::boolean AND projection.terminal_state='active' AND projection.source_revision >= $6)) AND
  CASE WHEN projection.source_type='pto' AND projection.owner_id IS NOT NULL
       THEN projection.owner_id=$7::uuid ELSE projection.assignee_id=$7::uuid END
),false)
FROM calendar_event_projections projection
WHERE projection.msp_id=$1::uuid
  AND projection.client_id IS NOT DISTINCT FROM NULLIF($2,'')::uuid
  AND projection.source_type=$3 AND projection.source_id=$4::uuid
  AND projection.event_role=$5`,
				job.MSPID, source.ClientID, source.Type, source.ID, source.EventRole, source.SourceRevision, job.CalendarRecipientID, ptoDecision).Scan(&latestRevision, &relevant)
			if err != nil {
				return notifications.CalendarRenderedDelivery{}, err
			}
			if latestRevision < source.SourceRevision {
				projectionLag = true
				continue
			}
			if !relevant {
				continue
			}
		}
		allowed, err := NewCalendarNotificationRepository(r.db, nil).CanViewCalendarSource(ctx, job.CalendarRecipientID, calendar.SourceRef{
			MSPID: job.MSPID, ClientID: source.ClientID, Type: source.Type, ID: source.ID,
		})
		if err != nil {
			return notifications.CalendarRenderedDelivery{}, err
		}
		if job.CalendarPayload.ChangeClass == notifications.CalendarCancellation {
			// A removed or reassigned projection has no current prior-recipient
			// label to load. The canonical cancellation establishes relevance;
			// render it generically after exercising the source boundary.
			details = append(details, "calendar item")
			continue
		}
		if !allowed {
			details = append(details, "calendar item")
			continue
		}
		var title, clientName string
		err = r.db.QueryRow(ctx, `SELECT projection.title,COALESCE(client.name,'')
FROM calendar_event_projections projection
LEFT JOIN client_organizations client
  ON client.id=projection.client_id AND client.msp_id=projection.msp_id
WHERE projection.msp_id=$1::uuid
  AND projection.client_id IS NOT DISTINCT FROM NULLIF($2,'')::uuid
  AND projection.source_type=$3 AND projection.source_id=$4::uuid
  AND projection.event_role=$5
  AND (($8::boolean AND projection.source_revision=$6) OR
       (NOT $8::boolean AND projection.terminal_state='active' AND projection.source_revision >= $6))
  AND CASE WHEN projection.source_type='pto' AND projection.owner_id IS NOT NULL
      THEN projection.owner_id=$7::uuid ELSE projection.assignee_id=$7::uuid END
ORDER BY projection.updated_at DESC,projection.id LIMIT 1`,
			job.MSPID, source.ClientID, source.Type, source.ID, source.EventRole, source.SourceRevision, job.CalendarRecipientID, ptoDecision).Scan(&title, &clientName)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return notifications.CalendarRenderedDelivery{}, err
		}
		detail := strings.TrimSpace(title)
		if detail == "" {
			detail = "calendar item"
		} else if clientName = strings.TrimSpace(clientName); clientName != "" {
			detail = clientName + ": " + detail
		}
		details = append(details, detail)
	}
	if projectionLag {
		rendered.Outcome = notifications.CalendarAuthorizationRetryable
		rendered.RetryReason = "projection_lag"
		return rendered, nil
	}
	if len(details) == 0 {
		rendered.SuppressionReason = "recipient_no_longer_relevant"
		return rendered, nil
	}
	change := strings.ReplaceAll(string(job.CalendarPayload.ChangeClass), "_", " ")
	rendered.Title = "Calendar " + change + " changed"
	rendered.Body = change + ": " + details[0]
	if len(details) > 1 {
		rendered.Body = fmt.Sprintf("%d calendar changes: %s", len(details), strings.Join(details, "; "))
	}
	rendered.ActionPath = job.CalendarPayload.ActionPath
	if job.Channel == notifications.Email {
		if !r.emailConfigured || !notifications.ValidEmailRecipient(rendered.RecipientEmail) {
			rendered.SuppressionReason = "channel_unavailable"
			return rendered, nil
		}
		base, parseErr := url.Parse(r.publicURL)
		if parseErr != nil || base.Scheme != "https" || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
			rendered.SuppressionReason = "channel_unavailable"
			return rendered, nil
		}
		rendered.ActionPath = base.ResolveReference(action).String()
	}
	rendered.Outcome = notifications.CalendarAuthorizationAuthorized
	rendered.Authorized = true
	return rendered, nil
}

func (r *NotificationRepository) CompleteCalendarInApp(ctx context.Context, job notifications.DeliveryJob, rendered notifications.CalendarRenderedDelivery, deliveredAt time.Time) error {
	if !rendered.Authorized || strings.TrimSpace(job.DeduplicationKey) == "" || strings.TrimSpace(rendered.Title) == "" || strings.TrimSpace(rendered.Body) == "" {
		return notifications.ErrInvalidInboxRequest
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `
INSERT INTO recipient_notifications(
 id,delivery_id,msp_id,recipient_technician_id,event_id,deduplication_key,
 title,body,action_path,content_classification,created_at
) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
ON CONFLICT (msp_id,recipient_technician_id,deduplication_key) DO NOTHING
`, id.New(), job.ID, job.MSPID, job.CalendarRecipientID, job.EventID, job.DeduplicationKey,
		rendered.Title, rendered.Body, rendered.ActionPath, job.ContentClassification, deliveredAt); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	var ownsInbox bool
	if err = tx.QueryRow(ctx, `SELECT delivery_id=$1::uuid AND event_id=$4::uuid
FROM recipient_notifications
WHERE msp_id=$2::uuid AND recipient_technician_id=$3::uuid
  AND deduplication_key=$5`, job.ID, job.MSPID, job.CalendarRecipientID, job.EventID, job.DeduplicationKey).Scan(&ownsInbox); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if !ownsInbox {
		_ = tx.Rollback(ctx)
		return object.ErrVersionConflict
	}
	tag, err := tx.Exec(ctx, `UPDATE notification_deliveries
SET state='delivered',delivered_at=$3,failure_code=NULL
WHERE id=$1 AND state='pending' AND attempts=$2+1`, job.ID, job.Attempts, deliveredAt)
	if err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if tag.RowsAffected() != 1 {
		_ = tx.Rollback(ctx)
		return object.ErrVersionConflict
	}
	if err = tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}

type recipientInboxCursor struct {
	CreatedAt time.Time `json:"created_at"`
	ID        string    `json:"id"`
}

func decodeRecipientInboxCursor(value string) (recipientInboxCursor, error) {
	if value == "" {
		return recipientInboxCursor{}, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return recipientInboxCursor{}, notifications.ErrInvalidInboxRequest
	}
	var cursor recipientInboxCursor
	if json.Unmarshal(data, &cursor) != nil || cursor.CreatedAt.IsZero() || !id.ValidCanonical(cursor.ID) {
		return recipientInboxCursor{}, notifications.ErrInvalidInboxRequest
	}
	return cursor, nil
}

func encodeRecipientInboxCursor(value notifications.RecipientNotification) string {
	data, _ := json.Marshal(recipientInboxCursor{CreatedAt: value.CreatedAt.UTC(), ID: value.ID})
	return base64.RawURLEncoding.EncodeToString(data)
}

func (r *NotificationRepository) ListRecipientNotifications(ctx context.Context, mspID, recipientID, cursorValue string, limit int) (notifications.InboxPage, error) {
	if r == nil || r.db == nil || limit < 1 || limit > 100 {
		return notifications.InboxPage{}, notifications.ErrInvalidInboxRequest
	}
	cursor, err := decodeRecipientInboxCursor(cursorValue)
	if err != nil {
		return notifications.InboxPage{}, err
	}
	var cursorID any
	if cursor.ID != "" {
		cursorID = cursor.ID
	}
	rows, err := r.db.Query(ctx, `SELECT
 recipient.id::text,recipient.recipient_technician_id::text,recipient.delivery_id::text,
 recipient.deduplication_key,recipient.title,recipient.body,recipient.action_path,
 recipient.content_classification,recipient.created_at,recipient.read_at,recipient.version
FROM recipient_notifications recipient
JOIN notification_deliveries delivery
  ON delivery.id=recipient.delivery_id AND delivery.msp_id=recipient.msp_id
 AND delivery.event_id=recipient.event_id
WHERE recipient.msp_id=$1::uuid AND recipient.recipient_technician_id=$2::uuid
  AND ($3::text='' OR (recipient.created_at,recipient.id)<($4::timestamptz,$5::uuid))
ORDER BY recipient.created_at DESC,recipient.id DESC
LIMIT $6`, mspID, recipientID, cursorValue, cursor.CreatedAt, cursorID, limit+1)
	if err != nil {
		return notifications.InboxPage{}, err
	}
	defer rows.Close()
	items := make([]notifications.RecipientNotification, 0, limit+1)
	for rows.Next() {
		var item notifications.RecipientNotification
		if err = rows.Scan(&item.ID, &item.RecipientID, &item.DeliveryID,
			&item.DeduplicationKey, &item.Title, &item.Body, &item.ActionPath,
			&item.ContentClassification, &item.CreatedAt, &item.ReadAt, &item.Version); err != nil {
			return notifications.InboxPage{}, err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return notifications.InboxPage{}, err
	}
	page := notifications.InboxPage{Notifications: items}
	if len(items) > limit {
		page.Notifications = items[:limit]
		page.NextCursor = encodeRecipientInboxCursor(page.Notifications[limit-1])
	}
	return page, nil
}

func (r *NotificationRepository) CountUnreadRecipientNotifications(ctx context.Context, mspID, recipientID string) (int, error) {
	var count int
	err := r.db.QueryRow(ctx, `SELECT count(*)::integer
FROM recipient_notifications recipient
WHERE recipient.msp_id=$1::uuid AND recipient.recipient_technician_id=$2::uuid
  AND recipient.read_at IS NULL`, mspID, recipientID).Scan(&count)
	return count, err
}

func (r *NotificationRepository) MarkRecipientNotificationRead(ctx context.Context, mspID, recipientID, notificationID string, expectedVersion int64, readAt time.Time) (notifications.RecipientNotification, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return notifications.RecipientNotification{}, err
	}
	var item notifications.RecipientNotification
	err = tx.QueryRow(ctx, `WITH updated AS (UPDATE recipient_notifications
SET read_at=COALESCE(read_at,$5),
    version=CASE WHEN read_at IS NULL THEN version+1 ELSE version END
WHERE id=$1::uuid AND msp_id=$2::uuid AND recipient_technician_id=$3::uuid
  AND (version=$4 OR (read_at IS NOT NULL AND version=$4+1))
RETURNING *)
SELECT updated.id::text,updated.recipient_technician_id::text,delivery.id::text,
 updated.deduplication_key,updated.title,updated.body,updated.action_path,
 updated.content_classification,updated.created_at,updated.read_at,updated.version
FROM updated
JOIN notification_deliveries delivery
  ON delivery.id=updated.delivery_id AND delivery.msp_id=updated.msp_id
 AND delivery.event_id=updated.event_id`,
		notificationID, mspID, recipientID, expectedVersion, readAt).Scan(
		&item.ID, &item.RecipientID, &item.DeliveryID, &item.DeduplicationKey,
		&item.Title, &item.Body, &item.ActionPath, &item.ContentClassification,
		&item.CreatedAt, &item.ReadAt, &item.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		var owned bool
		lookupErr := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM recipient_notifications
WHERE id=$1::uuid AND msp_id=$2::uuid AND recipient_technician_id=$3::uuid)`, notificationID, mspID, recipientID).Scan(&owned)
		_ = tx.Rollback(ctx)
		if lookupErr != nil {
			return notifications.RecipientNotification{}, lookupErr
		}
		if owned {
			return notifications.RecipientNotification{}, object.ErrVersionConflict
		}
		return notifications.RecipientNotification{}, scope.ErrNotFound
	}
	if err != nil {
		_ = tx.Rollback(ctx)
		return notifications.RecipientNotification{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return notifications.RecipientNotification{}, err
	}
	return item, nil
}

func (r *NotificationRepository) MarkDelivered(
	ctx context.Context,
	deliveryID string,
	expectedAttempts int,
	deliveredAt time.Time,
) error {
	return r.updateDelivery(ctx, `
UPDATE notification_deliveries
SET state = 'delivered', delivered_at = $3, failure_code = NULL
WHERE id = $1 AND state = 'pending' AND attempts = $2 + 1
`, deliveryID, expectedAttempts, deliveredAt)
}

func (r *NotificationRepository) MarkRetry(
	ctx context.Context,
	failure notifications.DeliveryFailure,
) error {
	return r.updateDelivery(ctx, `
UPDATE notification_deliveries
SET state = 'pending', next_attempt_at = $3, failure_code = $4
WHERE id = $1 AND state = 'pending' AND attempts = $2 + 1
`, failure.ID, failure.ExpectedAttempts, failure.NextAttemptAt, failure.ErrorCode)
}

func (r *NotificationRepository) MarkFailed(
	ctx context.Context,
	failure notifications.DeliveryFailure,
) error {
	return r.updateDelivery(ctx, `
UPDATE notification_deliveries
SET state = 'failed', next_attempt_at = $3, failure_code = $4
WHERE id = $1 AND state = 'pending' AND attempts = $2 + 1
`, failure.ID, failure.ExpectedAttempts, failure.FailedAt, failure.ErrorCode)
}

func (r *NotificationRepository) MarkSuppressed(ctx context.Context, failure notifications.DeliveryFailure) error {
	return r.updateDelivery(ctx, `
UPDATE notification_deliveries
SET state='suppressed',next_attempt_at=$3,suppressed_at=$3,
    suppression_reason=$4,failure_code=NULL
WHERE id=$1 AND state='pending' AND attempts=$2+1
`, failure.ID, failure.ExpectedAttempts, failure.FailedAt, failure.ErrorCode)
}

func (r *NotificationRepository) GetMentionPreference(ctx context.Context, mspID, technicianID string) (notifications.RecipientPreference, error) {
	preference := notifications.DefaultMentionPreference(technicianID)
	err := r.db.QueryRow(ctx, `
SELECT technician_id::text,event_type,email_enabled,teams_enabled,time_zone,
       COALESCE(to_char(quiet_hours_start,'HH24:MI'),''),
       COALESCE(to_char(quiet_hours_end,'HH24:MI'),''),version
FROM notification_recipient_preferences
WHERE msp_id=$1::uuid AND technician_id=$2::uuid AND event_type='mention.occurred'
`, mspID, technicianID).Scan(&preference.TechnicianID, &preference.EventType,
		&preference.EmailEnabled, &preference.TeamsEnabled, &preference.TimeZone,
		&preference.QuietStart, &preference.QuietEnd, &preference.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return preference, nil
	}
	return preference, err
}

func (r *NotificationRepository) PreferenceForMention(ctx context.Context, event notifications.PlanningEvent) (notifications.RecipientPreference, error) {
	preference, err := r.GetMentionPreference(ctx, event.MSPID, event.RecipientTechnicianID)
	if err != nil {
		return notifications.RecipientPreference{}, err
	}
	preference.EmailEnabled = preference.EmailEnabled && r.emailConfigured
	return preference, nil
}

func (r *NotificationRepository) CalendarPreference(ctx context.Context, mspID, technicianID string) (notifications.CalendarPreference, error) {
	return NewCalendarNotificationRepository(r.db, nil).GetCalendarPreference(ctx, mspID, technicianID)
}

func (r *NotificationRepository) MentionChannelAvailability(ctx context.Context, mspID string) (notifications.ChannelAvailability, error) {
	var policyEmail, teams bool
	err := r.db.QueryRow(ctx, `
SELECT
 EXISTS(SELECT 1 FROM notification_policies policy,
        LATERAL jsonb_array_elements(policy.channels) destination
        WHERE policy.msp_id=$1::uuid AND policy.event_type='mention.occurred'
          AND policy.enabled AND destination->>'channel'='email'),
 EXISTS(SELECT 1 FROM notification_policies policy,
        LATERAL jsonb_array_elements(policy.channels) destination
        JOIN teams_connections connection
          ON connection.id=(destination->>'recipient_ref')::uuid
         AND connection.msp_id=policy.msp_id AND connection.enabled
        WHERE policy.msp_id=$1::uuid AND policy.event_type='mention.occurred'
          AND policy.enabled AND destination->>'channel'='teams')
`, mspID).Scan(&policyEmail, &teams)
	return notifications.ChannelAvailability{Email: policyEmail && r.emailConfigured, Teams: teams}, err
}

func (r *NotificationRepository) SaveMentionPreference(ctx context.Context, mspID string, preference notifications.RecipientPreference, expected int64) (notifications.RecipientPreference, error) {
	quietStart, quietEnd := any(nil), any(nil)
	if preference.QuietStart != "" {
		quietStart, quietEnd = preference.QuietStart, preference.QuietEnd
	}
	err := r.db.QueryRow(ctx, `
INSERT INTO notification_recipient_preferences (
 msp_id,technician_id,event_type,email_enabled,teams_enabled,time_zone,
 quiet_hours_start,quiet_hours_end,version,created_at,updated_at
) SELECT $1::uuid,$2::uuid,'mention.occurred',$3,$4,$5,$6::time,$7::time,1,now(),now()
  WHERE $8=0
ON CONFLICT (msp_id,technician_id,event_type) DO UPDATE
SET email_enabled=EXCLUDED.email_enabled,teams_enabled=EXCLUDED.teams_enabled,
    time_zone=EXCLUDED.time_zone,quiet_hours_start=EXCLUDED.quiet_hours_start,
    quiet_hours_end=EXCLUDED.quiet_hours_end,version=notification_recipient_preferences.version+1,
    updated_at=now()
WHERE notification_recipient_preferences.version=$8
RETURNING version
`, mspID, preference.TechnicianID,
		preference.EmailEnabled, preference.TeamsEnabled, preference.TimeZone,
		quietStart, quietEnd, expected).Scan(&preference.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return notifications.RecipientPreference{}, object.ErrVersionConflict
	}
	return preference, err
}

func (r *NotificationRepository) ReauthorizeMentionDelivery(ctx context.Context, job notifications.DeliveryJob) (notifications.DeliveryAuthorization, error) {
	access := accessPredicate("delivery.recipient_technician_id", "delivery.msp_id", "delivery.client_id", "occurrence.parent_type", "occurrence.parent_id")
	var authorized notifications.DeliveryAuthorization
	var accessOK, policyOK, preferenceOK, tokenCurrent bool
	var itemID string
	err := r.db.QueryRow(ctx, `
SELECT
 recipient.lifecycle_state='active' AND source.lifecycle_state='active'
   AND item.suppressed_at IS NULL AND item.latest_occurrence_id=occurrence.id
   AND `+noConfirmedMentionLossPredicate("item")+`
   AND resolution.decision='eligible' AND (`+access+`),
 EXISTS(SELECT 1 FROM jsonb_array_elements(source.mention_tokens) token
        WHERE token->>'id'=occurrence.token_id::text
          AND token->>'target_id'=occurrence.target_id::text),
	 policy.enabled AND policy.event_type='mention.occurred' AND EXISTS(
   SELECT 1 FROM jsonb_array_elements(policy.channels) destination
   WHERE destination->>'channel'=delivery.channel
     AND destination->>'recipient_ref'=delivery.recipient_ref
 ) AND CASE WHEN delivery.channel='email' THEN $3::boolean
            WHEN delivery.channel='teams' THEN connection.id IS NOT NULL AND connection.enabled
            ELSE false END,
 CASE WHEN delivery.channel='email' THEN COALESCE(preference.email_enabled,true)
      WHEN delivery.channel='teams' THEN COALESCE(preference.teams_enabled,true)
      ELSE false END,
 recipient.email,author.display_name,item.id::text,
 CASE occurrence.parent_type
   WHEN 'work_record' THEN COALESCE((SELECT display_id FROM work_records WHERE id=occurrence.parent_id AND msp_id=occurrence.msp_id AND client_id=occurrence.client_id),'')
   WHEN 'project' THEN COALESCE((SELECT display_id FROM projects WHERE id=occurrence.parent_id AND msp_id=occurrence.msp_id AND client_id=occurrence.client_id),'')
   WHEN 'task' THEN 'TASK-'||upper(left(occurrence.parent_id::text,8)) END,
 CASE occurrence.parent_type
   WHEN 'work_record' THEN COALESCE((SELECT title FROM work_records WHERE id=occurrence.parent_id AND msp_id=occurrence.msp_id AND client_id=occurrence.client_id),'')
   WHEN 'project' THEN COALESCE((SELECT name FROM projects WHERE id=occurrence.parent_id AND msp_id=occurrence.msp_id AND client_id=occurrence.client_id),'')
   WHEN 'task' THEN COALESCE((SELECT title FROM tasks WHERE id=occurrence.parent_id AND msp_id=occurrence.msp_id AND client_id=occurrence.client_id),'') END,
 COALESCE(connection.id::text,''),COALESCE(connection.msp_id::text,''),
 COALESCE(connection.client_id::text,''),COALESCE(connection.name,''),
 COALESCE(connection.webhook_secret_ref,''),COALESCE(connection.version,0),
 COALESCE(NOT connection.enabled,false)
FROM notification_deliveries delivery
JOIN mention_occurrences occurrence
  ON occurrence.id=delivery.mention_occurrence_id AND occurrence.msp_id=delivery.msp_id
 AND occurrence.client_id=delivery.client_id
JOIN internal_collaboration_sources source
  ON source.id=occurrence.source_id AND source.msp_id=occurrence.msp_id AND source.client_id=occurrence.client_id
JOIN mention_items item
  ON item.msp_id=delivery.msp_id AND item.client_id=delivery.client_id
 AND item.recipient_id=delivery.recipient_technician_id
 AND item.parent_type=occurrence.parent_type AND item.parent_id=occurrence.parent_id
JOIN mention_recipient_resolutions resolution
  ON resolution.occurrence_id=occurrence.id AND resolution.recipient_id=delivery.recipient_technician_id
JOIN technicians recipient ON recipient.id=delivery.recipient_technician_id AND recipient.msp_id=delivery.msp_id
JOIN technicians author ON author.id=occurrence.author_id AND author.msp_id=delivery.msp_id
JOIN notification_policies policy ON policy.id=delivery.policy_id AND policy.msp_id=delivery.msp_id
LEFT JOIN notification_recipient_preferences preference
  ON preference.msp_id=delivery.msp_id AND preference.technician_id=delivery.recipient_technician_id
 AND preference.event_type='mention.occurred'
LEFT JOIN teams_connections connection
  ON connection.id=NULLIF(CASE WHEN delivery.channel='teams' THEN delivery.recipient_ref ELSE '' END,'')::uuid
 AND connection.msp_id=delivery.msp_id
 AND (connection.client_id IS NULL OR connection.client_id=delivery.client_id)
WHERE delivery.id=$1::uuid AND delivery.event_id=$2::uuid
`, job.ID, job.EventID, r.emailConfigured).Scan(
		&accessOK, &tokenCurrent, &policyOK, &preferenceOK,
		&authorized.RecipientEmail, &authorized.AuthorLabel, &itemID,
		&authorized.Summary.DisplayID, &authorized.Summary.Subject,
		&authorized.Connection.ID, &authorized.Connection.MSPID,
		&authorized.Connection.ClientID, &authorized.Connection.Name,
		&authorized.Connection.WebhookSecretRef, &authorized.Connection.Version,
		&authorized.Connection.Disabled,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		authorized.SuppressionReason = "access_revoked"
		return authorized, nil
	}
	if err != nil {
		return notifications.DeliveryAuthorization{}, err
	}
	if !accessOK || !tokenCurrent {
		authorized.SuppressionReason = "access_revoked"
		return authorized, nil
	}
	if !preferenceOK {
		authorized.SuppressionReason = "preference_disabled"
		return authorized, nil
	}
	if !policyOK || r.publicURL == "" {
		authorized.SuppressionReason = "channel_unavailable"
		return authorized, nil
	}
	if job.Channel == notifications.Email && !notifications.ValidEmailRecipient(authorized.RecipientEmail) {
		authorized.SuppressionReason = "channel_unavailable"
		return authorized, nil
	}
	parsed, err := url.Parse(r.publicURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		authorized.SuppressionReason = "channel_unavailable"
		return authorized, nil
	}
	values := url.Values{"item": {itemID}, "occurrence": {job.MentionOccurrenceID}}
	authorized.Summary.AuthenticatedURL = r.publicURL + "/mentions?" + values.Encode()
	authorized.Summary.Priority, authorized.Summary.Status = "normal", "mentioned"
	authorized.Authorized = true
	return authorized, nil
}

func (r *NotificationRepository) updateDelivery(
	ctx context.Context,
	query string,
	args ...any,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, query, args...)
	if err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if tag.RowsAffected() != 1 {
		_ = tx.Rollback(ctx)
		return object.ErrVersionConflict
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}

func (r *NotificationRepository) Record(
	ctx context.Context,
	attempt notifications.TeamsAttempt,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO teams_delivery_attempts (
  id, connection_id, connection_version, event_id, work_record_id, attempt,
  first_attempt_at, attempted_at, state, error_code
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NULLIF($10, ''))
`, id.New(), attempt.ConnectionID, attempt.ConnectionVersion, attempt.EventID,
		attempt.WorkRecordID, attempt.Attempt, attempt.FirstAttemptAt, attempt.AttemptedAt,
		attempt.State, attempt.ErrorCode); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	healthState := "degraded"
	if attempt.State == notifications.TeamsDelivered {
		healthState = "healthy"
	} else if attempt.State == notifications.TeamsFailed {
		healthState = "failed"
	}
	if _, err := tx.Exec(ctx, `
UPDATE teams_connections
SET health_state = $2,
    last_success_at = CASE WHEN $2 = 'healthy' THEN $3 ELSE last_success_at END,
    last_error_code = NULLIF($4, ''),
    updated_at = $3
WHERE id = $1 AND version = $5 AND enabled
`, attempt.ConnectionID, healthState, attempt.AttemptedAt,
		attempt.ErrorCode, attempt.ConnectionVersion); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}
