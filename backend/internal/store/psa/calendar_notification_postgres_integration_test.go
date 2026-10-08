package psa_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar/adapters"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/httpapi"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/id"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/notifications"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/store"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/store/psa"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tasks"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workforce"
)

func TestCalendarPlanAtomicPersistsTypedPayloadAndZeroDeliveryMarkerAgainstPostgres(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for calendar planning verification")
	}
	ctx := context.Background()
	if err := store.Migrate(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	mspID, technicianID, actorID, policyID := id.New(), id.New(), id.New(), id.New()
	eventID, digestEventID, emptyEventID := id.New(), id.New(), id.New()
	rollbackEventA, rollbackEventB := id.New(), id.New()
	deliveryID, rollbackDeliveryID, missingPolicyID := id.New(), id.New(), id.New()
	sourceA, sourceB, correlationID, rollbackCorrelationID := id.New(), id.New(), id.New(), id.New()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, execErr := pool.Exec(ctx, query, args...); execErr != nil {
			t.Fatal(execErr)
		}
	}
	exec(`INSERT INTO msp_organizations(id,display_id,name,created_by,updated_by) VALUES($1,$2,'Calendar planning',$3,$3)`, mspID, "M-"+mspID, actorID)
	exec(`INSERT INTO technicians(id,msp_id,email,display_name) VALUES($1,$2,$3,'Calendar recipient')`, technicianID, mspID, technicianID+"@example.test")
	exec(`INSERT INTO notification_policies(id,msp_id,key,name,event_type,channels) VALUES($1,$2,$3,'Calendar','calendar.schedule_changed','[]')`, policyID, mspID, "calendar-"+policyID)
	exec(`INSERT INTO notification_policy_versions(policy_id,msp_id,version,event_type,destinations,published_at,published_by) VALUES($1,$2,2,'calendar.schedule_changed','[]',now(),$3)`, policyID, mspID, actorID)
	for _, value := range []struct {
		eventID, class, sourceID, correlationID string
		revision                                int64
	}{
		{eventID, "schedule", sourceA, correlationID, 1},
		{digestEventID, "schedule", sourceB, correlationID, 2},
		{emptyEventID, "pto", sourceA, correlationID, 1},
		{rollbackEventA, "schedule", sourceA, rollbackCorrelationID, 3},
		{rollbackEventB, "schedule", sourceB, rollbackCorrelationID, 4},
	} {
		exec(`INSERT INTO event_outbox(event_id,event_type,schema_version,occurred_at,msp_id,actor_type,actor_id,subject_type,subject_id,subject_version,correlation_id,source,data) VALUES($1,'calendar.schedule_changed',1,now(),$2,'system',$3,'calendar_projection',$4::uuid,$8::bigint,$5,'calendar',jsonb_build_object('recipient_id',$6::text,'change_class',$7::text,'urgency','routine','source_refs',jsonb_build_array(jsonb_build_object('type','task','id',$4::text,'client_id','','event_role','scheduled_work','source_revision',$8::bigint)),'action_path','/calendar'))`, value.eventID, mspID, actorID, value.sourceID, value.correlationID, technicianID, value.class, value.revision)
	}
	repository := psa.NewNotificationRepositoryFromPool(pool)
	at := time.Now().UTC()
	planningEvent := func(eventID, changeClass, sourceID, correlationID string, revision int64) notifications.PlanningEvent {
		return notifications.PlanningEvent{
			ID: eventID, Type: notifications.CalendarNotificationEventType, MSPID: mspID, CorrelationID: correlationID,
			CalendarRecipientID: technicianID, CalendarChangeClass: notifications.CalendarChangeClass(changeClass), CalendarUrgency: notifications.CalendarRoutine,
			CalendarSources: []notifications.CalendarSourceReference{{Type: "task", ID: sourceID, EventRole: "scheduled_work", SourceRevision: revision}}, CalendarActionPath: "/calendar",
		}
	}
	firstEvent := planningEvent(eventID, "schedule", sourceA, correlationID, 1)
	secondEvent := planningEvent(digestEventID, "schedule", sourceB, correlationID, 2)
	mergedEvent := firstEvent
	mergedEvent.CalendarSources = append(append([]notifications.CalendarSourceReference(nil), firstEvent.CalendarSources...), secondEvent.CalendarSources...)
	owned, err := repository.PlanAtomic(ctx, notifications.PlannedDecision{Event: mergedEvent, Events: []notifications.PlanningEvent{firstEvent, secondEvent}, PlannedAt: at, Deliveries: []notifications.PlannedDelivery{{
		ID: deliveryID, PolicyID: policyID, PolicyVersion: 2, EventID: eventID, MSPID: mspID, RecipientTechnicianID: technicianID,
		Channel: notifications.InApp, RecipientRef: notifications.CalendarAssigneeRecipientRef, ContentClassification: "internal", State: notifications.Pending, PlannedAt: at,
		CalendarPayload: &notifications.CalendarDeliveryPayload{CorrelationID: correlationID, ChangeClass: notifications.CalendarSchedule, Urgency: notifications.CalendarRoutine, Sources: mergedEvent.CalendarSources, ActionPath: "/calendar"},
	}}})
	if err != nil || !owned {
		t.Fatalf("PlanAtomic() owned=%t error=%v", owned, err)
	}
	emptyEvent := planningEvent(emptyEventID, "pto", sourceA, correlationID, 1)
	owned, err = repository.PlanAtomic(ctx, notifications.PlannedDecision{Event: emptyEvent, Events: []notifications.PlanningEvent{emptyEvent}, PlannedAt: at})
	if err != nil || !owned {
		t.Fatalf("zero-delivery PlanAtomic() owned=%t error=%v", owned, err)
	}
	rollbackFirst := planningEvent(rollbackEventA, "schedule", sourceA, rollbackCorrelationID, 3)
	rollbackSecond := planningEvent(rollbackEventB, "schedule", sourceB, rollbackCorrelationID, 4)
	rollbackMerged := rollbackFirst
	rollbackMerged.CalendarSources = append(append([]notifications.CalendarSourceReference(nil), rollbackFirst.CalendarSources...), rollbackSecond.CalendarSources...)
	owned, err = repository.PlanAtomic(ctx, notifications.PlannedDecision{Event: rollbackMerged, Events: []notifications.PlanningEvent{rollbackFirst, rollbackSecond}, PlannedAt: at, Deliveries: []notifications.PlannedDelivery{{
		ID: rollbackDeliveryID, PolicyID: missingPolicyID, PolicyVersion: 1, EventID: rollbackEventA, MSPID: mspID, RecipientTechnicianID: technicianID,
		Channel: notifications.InApp, RecipientRef: notifications.CalendarAssigneeRecipientRef, ContentClassification: "internal", State: notifications.Pending, PlannedAt: at,
		CalendarPayload: &notifications.CalendarDeliveryPayload{CorrelationID: rollbackCorrelationID, ChangeClass: notifications.CalendarSchedule, Urgency: notifications.CalendarRoutine, Sources: rollbackMerged.CalendarSources, ActionPath: "/calendar"},
	}}})
	if err == nil || owned {
		t.Fatalf("rollback PlanAtomic() owned=%t error=%v", owned, err)
	}
	var deliveries, payloads, markers, rolledBackMarkers, rolledBackDeliveries int
	var stableOnly, mentionRecipientEmpty, typedRecipient bool
	if err = pool.QueryRow(ctx, `SELECT
	  (SELECT count(*) FROM notification_deliveries WHERE id=$1),
	  (SELECT count(*) FROM notification_calendar_delivery_payloads WHERE delivery_id=$1),
	  (SELECT count(*) FROM notification_event_plans WHERE event_id IN ($2,$3,$4)),
	  (SELECT jsonb_array_length(source_refs)=2 AND source_refs <@ jsonb_build_array(jsonb_build_object('type','task','id',$5::text,'event_role','scheduled_work','source_revision',1),jsonb_build_object('type','task','id',$6::text,'event_role','scheduled_work','source_revision',2)) AND source_refs @> jsonb_build_array(jsonb_build_object('type','task','id',$5::text,'event_role','scheduled_work','source_revision',1),jsonb_build_object('type','task','id',$6::text,'event_role','scheduled_work','source_revision',2)) FROM notification_calendar_delivery_payloads WHERE delivery_id=$1),
	  (SELECT recipient_technician_id IS NULL FROM notification_deliveries WHERE id=$1),
	  (SELECT recipient_technician_id=$7 FROM notification_calendar_delivery_payloads WHERE delivery_id=$1),
	  (SELECT count(*) FROM notification_event_plans WHERE event_id IN ($8,$9)),
	  (SELECT count(*) FROM notification_deliveries WHERE id=$10)`, deliveryID, eventID, digestEventID, emptyEventID, sourceA, sourceB, technicianID, rollbackEventA, rollbackEventB, rollbackDeliveryID).Scan(&deliveries, &payloads, &markers, &stableOnly, &mentionRecipientEmpty, &typedRecipient, &rolledBackMarkers, &rolledBackDeliveries); err != nil {
		t.Fatal(err)
	}
	if deliveries != 1 || payloads != 1 || markers != 3 || !stableOnly || !mentionRecipientEmpty || !typedRecipient || rolledBackMarkers != 0 || rolledBackDeliveries != 0 {
		t.Fatalf("deliveries=%d payloads=%d markers=%d stable_only=%t mention_recipient_empty=%t typed_recipient=%t rollback_markers=%d rollback_deliveries=%d", deliveries, payloads, markers, stableOnly, mentionRecipientEmpty, typedRecipient, rolledBackMarkers, rolledBackDeliveries)
	}
}

func TestCalendarPlannerCompletesBatchBoundaryAndSerializesLogicalDeliveryAgainstPostgres(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for calendar batch verification")
	}
	ctx := context.Background()
	pool := isolatedCalendarNotificationPool(t, databaseURL)
	mspID, technicianID, actorID, policyID := id.New(), id.New(), id.New(), id.New()
	correlationID, firstEventID, secondEventID := id.New(), id.New(), id.New()
	firstSourceID, secondSourceID := id.New(), id.New()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	exec(`INSERT INTO msp_organizations(id,display_id,name,created_by,updated_by) VALUES($1,$2,'Calendar batching',$3,$3)`, mspID, "M-"+mspID, actorID)
	exec(`INSERT INTO technicians(id,msp_id,email,display_name) VALUES($1,$2,$3,'Batch recipient'),($4,$2,$5,'Batch actor')`, technicianID, mspID, technicianID+"@example.test", actorID, actorID+"@example.test")
	destinations := `[{"channel":"in_app","recipient_ref":"calendar.assignee","content_classification":"internal"}]`
	exec(`INSERT INTO notification_policies(id,msp_id,key,name,event_type,channels,version) VALUES($1,$2,$3,'Calendar','calendar.schedule_changed',$4::jsonb,2)`, policyID, mspID, "calendar-"+policyID, destinations)
	exec(`INSERT INTO notification_policy_versions(policy_id,msp_id,version,event_type,destinations,published_at,published_by) VALUES($1,$2,2,'calendar.schedule_changed',$3::jsonb,now(),$4)`, policyID, mspID, destinations, actorID)
	insertEvent := func(eventID, sourceID, correlationID string, revision int64, occurredAt time.Time) {
		exec(`INSERT INTO event_outbox(event_id,event_type,schema_version,occurred_at,msp_id,actor_type,actor_id,subject_type,subject_id,subject_version,correlation_id,source,data) VALUES($1,'calendar.schedule_changed',1,$2,$3,'system',$4,'calendar_projection',$5::uuid,$6::bigint,$7,'calendar',jsonb_build_object('recipient_id',$8::text,'change_class','schedule','urgency','routine','source_refs',jsonb_build_array(jsonb_build_object('type','task','id',$5::text,'client_id','','event_role','scheduled_work','source_revision',$6::bigint)),'action_path','/calendar'))`, eventID, occurredAt, mspID, actorID, sourceID, revision, correlationID, technicianID)
	}
	base := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)
	insertEvent(firstEventID, firstSourceID, correlationID, 1, base)
	insertEvent(secondEventID, secondSourceID, correlationID, 2, base.Add(time.Second))

	repository := psa.NewNotificationRepositoryFromPool(pool)
	claimed, err := repository.ClaimEvents(ctx, 1, base.Add(time.Hour))
	if err != nil || len(claimed) != 2 {
		t.Fatalf("ClaimEvents(limit=1) claimed=%+v error=%v, want the complete two-event correlation", claimed, err)
	}
	result, err := notifications.NewPlanner(repository, func() time.Time { return base.Add(time.Hour) }, id.New).RunOnce(ctx, 1)
	if err != nil || result.Events != 2 || result.Pending != 1 {
		t.Fatalf("planner result=%+v error=%v", result, err)
	}
	logicalKey := mspID + ":" + correlationID + ":" + technicianID + ":schedule:routine:in_app:" + policyID + ":2"
	var deliveryCount, markerCount, sourceCount int
	var persistedKey string
	if err = pool.QueryRow(ctx, `SELECT count(*),COALESCE(min(calendar_delivery_key),'') FROM notification_deliveries WHERE msp_id=$1 AND calendar_delivery_key=$2`, mspID, logicalKey).Scan(&deliveryCount, &persistedKey); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM notification_event_plans WHERE event_id IN ($1,$2)`, firstEventID, secondEventID).Scan(&markerCount); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT jsonb_array_length(payload.source_refs) FROM notification_calendar_delivery_payloads payload JOIN notification_deliveries delivery ON delivery.id=payload.delivery_id WHERE delivery.msp_id=$1 AND delivery.calendar_delivery_key=$2`, mspID, logicalKey).Scan(&sourceCount); err != nil {
		t.Fatal(err)
	}
	if deliveryCount != 1 || persistedKey != logicalKey || markerCount != 2 || sourceCount != 2 {
		t.Fatalf("deliveries=%d key=%q markers=%d sources=%d", deliveryCount, persistedKey, markerCount, sourceCount)
	}

	mergeCorrelationID := id.New()
	mergeEventIDs := [2]string{id.New(), id.New()}
	mergeSourceIDs := [2]string{id.New(), id.New()}
	mergeDeliveryIDs := [2]string{id.New(), id.New()}
	for index := range mergeEventIDs {
		insertEvent(mergeEventIDs[index], mergeSourceIDs[index], mergeCorrelationID, int64(index+1), base.Add(time.Duration(index+2)*time.Second))
	}
	calendarEvent := func(index int) notifications.PlanningEvent {
		return notifications.PlanningEvent{
			ID: mergeEventIDs[index], Type: notifications.CalendarNotificationEventType, MSPID: mspID, CorrelationID: mergeCorrelationID,
			CalendarRecipientID: technicianID, CalendarChangeClass: notifications.CalendarSchedule, CalendarUrgency: notifications.CalendarRoutine,
			CalendarSources: []notifications.CalendarSourceReference{{Type: "task", ID: mergeSourceIDs[index], EventRole: "scheduled_work", SourceRevision: int64(index + 1)}}, CalendarActionPath: "/calendar",
		}
	}
	decisions := [2]notifications.PlannedDecision{}
	for index := range decisions {
		event := calendarEvent(index)
		decisions[index] = notifications.PlannedDecision{Event: event, Events: []notifications.PlanningEvent{event}, PlannedAt: base.Add(time.Hour), Deliveries: []notifications.PlannedDelivery{{
			ID: mergeDeliveryIDs[index], PolicyID: policyID, PolicyVersion: 2, EventID: event.ID, MSPID: mspID,
			RecipientTechnicianID: technicianID, Channel: notifications.InApp, RecipientRef: notifications.CalendarAssigneeRecipientRef,
			ContentClassification: "internal", State: notifications.Pending, PlannedAt: base.Add(time.Hour),
			CalendarPayload: &notifications.CalendarDeliveryPayload{CorrelationID: mergeCorrelationID, ChangeClass: notifications.CalendarSchedule, Urgency: notifications.CalendarRoutine, Sources: event.CalendarSources, ActionPath: "/calendar"},
		}}}
	}
	type planResult struct {
		owned bool
		err   error
	}
	results := [2]planResult{}
	var wait sync.WaitGroup
	wait.Add(2)
	for index := range decisions {
		go func(index int) {
			defer wait.Done()
			results[index].owned, results[index].err = repository.PlanAtomic(ctx, decisions[index])
		}(index)
	}
	wait.Wait()
	for index, planned := range results {
		if planned.err != nil || !planned.owned {
			t.Fatalf("concurrent PlanAtomic(%d) owned=%t error=%v", index, planned.owned, planned.err)
		}
	}
	mergeKey := mspID + ":" + mergeCorrelationID + ":" + technicianID + ":schedule:routine:in_app:" + policyID + ":2"
	if err = pool.QueryRow(ctx, `SELECT count(*),COALESCE(max(jsonb_array_length(payload.source_refs)),0) FROM notification_deliveries delivery JOIN notification_calendar_delivery_payloads payload ON payload.delivery_id=delivery.id WHERE delivery.msp_id=$1 AND delivery.calendar_delivery_key=$2`, mspID, mergeKey).Scan(&deliveryCount, &sourceCount); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM notification_event_plans WHERE event_id IN ($1,$2)`, mergeEventIDs[0], mergeEventIDs[1]).Scan(&markerCount); err != nil {
		t.Fatal(err)
	}
	if deliveryCount != 1 || sourceCount != 2 || markerCount != 2 {
		t.Fatalf("serialized deliveries=%d merged sources=%d atomic markers=%d", deliveryCount, sourceCount, markerCount)
	}
}

func TestCalendarPlanAtomicSeparatesLateSourcesAfterClaimAndTerminalDeliveryAgainstPostgres(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for late calendar source verification")
	}
	ctx := context.Background()
	pool := isolatedCalendarNotificationPool(t, databaseURL)
	mspID, technicianID, actorID, policyID := id.New(), id.New(), id.New(), id.New()
	correlationID, plannerFirstCorrelationID := id.New(), id.New()
	eventIDs := [5]string{id.New(), id.New(), id.New(), id.New(), id.New()}
	sourceIDs := [5]string{id.New(), id.New(), id.New(), id.New(), id.New()}
	deliveryIDs := [5]string{id.New(), id.New(), id.New(), id.New(), id.New()}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	exec(`INSERT INTO msp_organizations(id,display_id,name,created_by,updated_by) VALUES($1,$2,'Late calendar sources',$3,$3)`, mspID, "M-"+mspID, actorID)
	exec(`INSERT INTO technicians(id,msp_id,email,display_name) VALUES($1,$2,$3,'Late source recipient'),($4,$2,$5,'Late source actor')`, technicianID, mspID, technicianID+"@example.test", actorID, actorID+"@example.test")
	destinations := `[{"channel":"in_app","recipient_ref":"calendar.assignee","content_classification":"internal"}]`
	exec(`INSERT INTO notification_policies(id,msp_id,key,name,event_type,channels,version) VALUES($1,$2,$3,'Calendar','calendar.schedule_changed',$4::jsonb,1)`, policyID, mspID, "late-calendar-"+policyID, destinations)
	exec(`INSERT INTO notification_policy_versions(policy_id,msp_id,version,event_type,destinations,published_at,published_by) VALUES($1,$2,1,'calendar.schedule_changed',$3::jsonb,now(),$4)`, policyID, mspID, destinations, actorID)
	base := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)
	for index := range eventIDs {
		exec(`INSERT INTO event_outbox(event_id,event_type,schema_version,occurred_at,msp_id,actor_type,actor_id,subject_type,subject_id,subject_version,correlation_id,source,data) VALUES($1,'calendar.schedule_changed',1,$2,$3,'system',$4,'calendar_projection',$5::uuid,$6,$7,'calendar',jsonb_build_object('recipient_id',$8::text,'change_class','schedule','urgency','routine','source_refs',jsonb_build_array(jsonb_build_object('type','task','id',($5::uuid)::text,'client_id','','event_role','scheduled_work','source_revision',$6::bigint)),'action_path','/calendar'))`, eventIDs[index], base.Add(time.Duration(index)*time.Second), mspID, actorID, sourceIDs[index], index+1, correlationID, technicianID)
	}
	decision := func(index int, correlation string, plannedAt time.Time) notifications.PlannedDecision {
		event := notifications.PlanningEvent{
			ID: eventIDs[index], Type: notifications.CalendarNotificationEventType, MSPID: mspID, CorrelationID: correlation,
			CalendarRecipientID: technicianID, CalendarChangeClass: notifications.CalendarSchedule, CalendarUrgency: notifications.CalendarRoutine,
			CalendarSources: []notifications.CalendarSourceReference{{Type: "task", ID: sourceIDs[index], EventRole: "scheduled_work", SourceRevision: int64(index + 1)}}, CalendarActionPath: "/calendar",
		}
		return notifications.PlannedDecision{Event: event, Events: []notifications.PlanningEvent{event}, PlannedAt: plannedAt, Deliveries: []notifications.PlannedDelivery{{
			ID: deliveryIDs[index], PolicyID: policyID, PolicyVersion: 1, EventID: event.ID, MSPID: mspID,
			RecipientTechnicianID: technicianID, Channel: notifications.InApp, RecipientRef: notifications.CalendarAssigneeRecipientRef,
			ContentClassification: "internal", State: notifications.Pending, PlannedAt: plannedAt,
			CalendarPayload: &notifications.CalendarDeliveryPayload{CorrelationID: correlation, ChangeClass: notifications.CalendarSchedule, Urgency: notifications.CalendarRoutine, Sources: event.CalendarSources, ActionPath: "/calendar"},
		}}}
	}
	repository := psa.NewNotificationRepositoryFromPool(pool)
	var err error
	if owned, err := repository.PlanAtomic(ctx, decision(0, correlationID, base.Add(time.Hour))); err != nil || !owned {
		t.Fatalf("initial plan owned=%t error=%v", owned, err)
	}
	exec(`CREATE FUNCTION calendar_notification_test_barrier() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  PERFORM pg_advisory_xact_lock(TG_ARGV[0]::bigint);
  RETURN NEW;
END
$$`)
	type asyncPlanResult struct {
		owned bool
		err   error
	}
	claimFirstLock := int64(time.Now().UnixNano()%1_000_000_000 + 1)
	exec(fmt.Sprintf(`CREATE TRIGGER calendar_claim_first_barrier
BEFORE INSERT ON notification_event_plans
FOR EACH ROW WHEN (NEW.event_id='%s'::uuid)
EXECUTE FUNCTION calendar_notification_test_barrier('%d')`, eventIDs[1], claimFirstLock))
	releaseClaimFirst := holdCalendarNotificationAdvisoryLock(t, pool, claimFirstLock)
	claimFirstPlan := make(chan asyncPlanResult, 1)
	go func() {
		owned, planErr := repository.PlanAtomic(ctx, decision(1, correlationID, base.Add(2*time.Hour)))
		claimFirstPlan <- asyncPlanResult{owned: owned, err: planErr}
	}()
	waitForCalendarNotificationAdvisoryWaiter(t, pool, claimFirstLock)

	// ClaimPending wins while the late planner is blocked before its conditional
	// upsert. Once released, the planner must create a follow-up generation
	// instead of mutating the payload already loaded by the worker.
	claimAt := base.Add(2 * time.Hour)
	claimed, claimErr := repository.ClaimPending(ctx, 1, claimAt)
	releaseClaimFirst()
	planned := <-claimFirstPlan
	if claimErr != nil || len(claimed) != 1 || len(claimed[0].CalendarPayload.Sources) != 1 {
		t.Fatalf("initial concurrent claim=%+v error=%v", claimed, claimErr)
	}
	if planned.err != nil || !planned.owned {
		t.Fatalf("post-claim concurrent plan owned=%t error=%v", planned.owned, planned.err)
	}
	logicalKey := mspID + ":" + correlationID + ":" + technicianID + ":schedule:routine:in_app:" + policyID + ":1"
	var deliveryCount, markerCount int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM notification_deliveries WHERE msp_id=$1 AND (calendar_delivery_key=$2 OR starts_with(calendar_delivery_key,$2 || ':followup:'))`, mspID, logicalKey).Scan(&deliveryCount); err != nil || deliveryCount != 2 {
		t.Fatalf("post-claim deliveries=%d error=%v, want original plus follow-up", deliveryCount, err)
	}
	if err = repository.MarkDelivered(ctx, claimed[0].ID, claimed[0].Attempts, claimAt.Add(time.Second)); err != nil {
		t.Fatalf("complete initially claimed delivery: %v", err)
	}
	followups, err := repository.ClaimPending(ctx, 1, claimAt.Add(time.Minute))
	if err != nil || len(followups) != 1 || followups[0].DeduplicationKey == logicalKey || len(followups[0].CalendarPayload.Sources) != 1 || followups[0].CalendarPayload.Sources[0].ID != sourceIDs[1] {
		t.Fatalf("follow-up claim=%+v error=%v", followups, err)
	}
	if err = repository.MarkDelivered(ctx, followups[0].ID, followups[0].Attempts, claimAt.Add(time.Minute+time.Second)); err != nil {
		t.Fatalf("complete follow-up delivery: %v", err)
	}

	// Once every prior generation is terminal, another late source gets a new
	// generation instead of mutating an already delivered payload.
	if owned, planErr := repository.PlanAtomic(ctx, decision(2, correlationID, claimAt.Add(2*time.Minute))); planErr != nil || !owned {
		t.Fatalf("post-terminal plan owned=%t error=%v", owned, planErr)
	}
	var singleSourcePayloads int
	if err = pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER (WHERE jsonb_array_length(payload.source_refs)=1) FROM notification_deliveries delivery JOIN notification_calendar_delivery_payloads payload ON payload.delivery_id=delivery.id AND payload.msp_id=delivery.msp_id WHERE delivery.msp_id=$1 AND (delivery.calendar_delivery_key=$2 OR starts_with(delivery.calendar_delivery_key,$2 || ':followup:'))`, mspID, logicalKey).Scan(&deliveryCount, &singleSourcePayloads); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM notification_event_plans WHERE event_id=ANY($1::uuid[])`, eventIDs[:3]).Scan(&markerCount); err != nil {
		t.Fatal(err)
	}
	if deliveryCount != 3 || singleSourcePayloads != 3 || markerCount != 3 {
		t.Fatalf("late generations=%d single-source payloads=%d atomic markers=%d", deliveryCount, singleSourcePayloads, markerCount)
	}
	terminalFollowups, err := repository.ClaimPending(ctx, 1, claimAt.Add(3*time.Minute))
	if err != nil || len(terminalFollowups) != 1 || len(terminalFollowups[0].CalendarPayload.Sources) != 1 || terminalFollowups[0].CalendarPayload.Sources[0].ID != sourceIDs[2] {
		t.Fatalf("terminal follow-up claim=%+v error=%v", terminalFollowups, err)
	}
	if err = repository.MarkDelivered(ctx, terminalFollowups[0].ID, terminalFollowups[0].Attempts, claimAt.Add(3*time.Minute+time.Second)); err != nil {
		t.Fatalf("complete terminal follow-up delivery: %v", err)
	}

	// The inverse ordering is also explicit: PlanAtomic owns the base delivery
	// row while merging a late source, so ClaimPending must skip the locked row
	// and only load the complete payload after the planner commits.
	plannerFirstAt := claimAt.Add(4 * time.Minute)
	if owned, planErr := repository.PlanAtomic(ctx, decision(3, plannerFirstCorrelationID, plannerFirstAt)); planErr != nil || !owned {
		t.Fatalf("planner-first initial plan owned=%t error=%v", owned, planErr)
	}
	plannerFirstKey := mspID + ":" + plannerFirstCorrelationID + ":" + technicianID + ":schedule:routine:in_app:" + policyID + ":1"
	plannerFirstLock := claimFirstLock + 1
	exec(fmt.Sprintf(`CREATE TRIGGER calendar_planner_first_barrier
AFTER UPDATE ON notification_deliveries
FOR EACH ROW WHEN (NEW.calendar_delivery_key='%s' AND OLD.attempts=0 AND NEW.attempts=0)
EXECUTE FUNCTION calendar_notification_test_barrier('%d')`, plannerFirstKey, plannerFirstLock))
	releasePlannerFirst := holdCalendarNotificationAdvisoryLock(t, pool, plannerFirstLock)
	plannerFirstPlan := make(chan asyncPlanResult, 1)
	go func() {
		owned, planErr := repository.PlanAtomic(ctx, decision(4, plannerFirstCorrelationID, plannerFirstAt.Add(time.Minute)))
		plannerFirstPlan <- asyncPlanResult{owned: owned, err: planErr}
	}()
	waitForCalendarNotificationAdvisoryWaiter(t, pool, plannerFirstLock)
	skipped, skipErr := repository.ClaimPending(ctx, 1, plannerFirstAt.Add(2*time.Minute))
	releasePlannerFirst()
	planned = <-plannerFirstPlan
	if skipErr != nil || len(skipped) != 0 {
		t.Fatalf("claim while planner owns row=%+v error=%v, want locked row skipped", skipped, skipErr)
	}
	if planned.err != nil || !planned.owned {
		t.Fatalf("planner-first concurrent plan owned=%t error=%v", planned.owned, planned.err)
	}
	merged, err := repository.ClaimPending(ctx, 1, plannerFirstAt.Add(3*time.Minute))
	if err != nil || len(merged) != 1 || merged[0].DeduplicationKey != plannerFirstKey || len(merged[0].CalendarPayload.Sources) != 2 {
		t.Fatalf("planner-first merged claim=%+v error=%v", merged, err)
	}
}

func holdCalendarNotificationAdvisoryLock(t *testing.T, pool *pgxpool.Pool, key int64) func() {
	t.Helper()
	ctx := context.Background()
	connection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = connection.Exec(ctx, `SELECT pg_advisory_lock($1)`, key); err != nil {
		connection.Release()
		t.Fatal(err)
	}
	var once sync.Once
	release := func() {
		once.Do(func() {
			_, unlockErr := connection.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, key)
			connection.Release()
			if unlockErr != nil {
				t.Errorf("release advisory test barrier %d: %v", key, unlockErr)
			}
		})
	}
	t.Cleanup(release)
	return release
}

func waitForCalendarNotificationAdvisoryWaiter(t *testing.T, pool *pgxpool.Pool, key int64) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var waiting bool
		if err := pool.QueryRow(context.Background(), `SELECT EXISTS (
  SELECT 1
  FROM pg_locks
  WHERE locktype='advisory' AND NOT granted
    AND classid=0 AND objid=$1::oid
)`, key).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for advisory test barrier %d", key)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func isolatedCalendarNotificationPool(t *testing.T, databaseURL string) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	schema := fmt.Sprintf("calendar_batch_%d", time.Now().UnixNano())
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE") })
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	if err = store.Migrate(ctx, parsed.String()); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestCalendarReminderProductionIsCurrentAuthorizedAndInsertOnce(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for calendar reminder verification")
	}
	ctx := context.Background()
	if err := store.Migrate(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	mspID, technicianID, roleID, assignmentID, projectionID := id.New(), id.New(), id.New(), id.New(), id.New()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, execErr := pool.Exec(ctx, query, args...); execErr != nil {
			t.Fatal(execErr)
		}
	}
	exec(`INSERT INTO msp_organizations(id,display_id,name,created_by,updated_by) VALUES($1,$2,'Reminder test',$3,$3)`, mspID, "M-"+mspID, technicianID)
	exec(`INSERT INTO technicians(id,msp_id,email,display_name) VALUES($1,$2,$3,'Recipient')`, technicianID, mspID, technicianID+"@example.test")
	exec(`INSERT INTO roles(id,msp_id,key,name) VALUES($1,$2,$3,'Workforce reader')`, roleID, mspID, "reminder_"+roleID)
	exec(`INSERT INTO role_capabilities(role_id,msp_id,capability) VALUES($1,$2,'calendar.commitment.manage')`, roleID, mspID)
	exec(`INSERT INTO role_assignments(id,msp_id,technician_id,role_id,granted_by) VALUES($1,$2,$3,$4,$3)`, assignmentID, mspID, technicianID, roleID)
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	exec(`INSERT INTO calendar_event_projections(id,msp_id,source_type,source_id,event_role,source_revision,title,starts_at,ends_at,timezone,all_day,scheduling_mode,assignee_id) VALUES($1,$2,'commercial_commitment',$1,'renewal',7,'Upcoming renewal',$3,$4,'UTC',false,'informational',$5)`, projectionID, mspID, now.Add(24*time.Hour), now.Add(25*time.Hour), technicianID)

	repository := psa.NewCalendarNotificationRepositoryFromPool(pool, id.New)
	candidates, err := repository.DueReminderCandidates(ctx, mspID, now, 10)
	if err != nil || len(candidates) != 1 {
		t.Fatalf("candidates=%+v err=%v", candidates, err)
	}
	exec(`INSERT INTO calendar_notification_preference_sets(msp_id,technician_id) VALUES($1,$2)`, mspID, technicianID)
	exec(`INSERT INTO calendar_notification_preference_rules(msp_id,technician_id,event_class,change_class,urgency,channel,enabled) VALUES($1,$2,'calendar.schedule_changed','reminder','routine','in_app',false)`, mspID, technicianID)
	service := calendar.NewReminderService(repository, func() time.Time { return now })
	first, err := service.EvaluateDue(ctx, mspID, 10)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.EvaluateDue(ctx, mspID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || first[0].SourceRevision != 7 || first[0].Threshold != "24h" || len(second) != 0 {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
	var facts, events int
	var canonical bool
	if err = pool.QueryRow(ctx, `SELECT
  (SELECT count(*) FROM calendar_reminder_facts WHERE msp_id=$1 AND projection_id=$2 AND source_revision=7 AND threshold='24h'),
  (SELECT count(*) FROM event_outbox WHERE msp_id=$1 AND event_type='calendar.schedule_changed' AND subject_id=$2),
  (SELECT data=jsonb_build_object('recipient_id',$3::text,'change_class','reminder','urgency','routine','source_refs',jsonb_build_array(jsonb_build_object('type','commercial_commitment','id',$2::text,'client_id','','event_role','renewal','source_revision',7)),'action_path','/calendar') FROM event_outbox WHERE msp_id=$1 AND event_type='calendar.schedule_changed' AND subject_id=$2)
`, mspID, projectionID, technicianID).Scan(&facts, &events, &canonical); err != nil {
		t.Fatal(err)
	}
	if facts != 1 || events != 1 || !canonical {
		t.Fatalf("facts=%d events=%d canonical=%v", facts, events, canonical)
	}
}

func TestCanonicalCalendarPTOProjectionDoesNotEmitDecisionFacts(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for canonical PTO notification verification")
	}
	ctx := context.Background()
	if err := store.Migrate(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	mspID, technicianID, sourceID, projectionID := id.New(), id.New(), id.New(), id.New()
	if _, err = pool.Exec(ctx, `INSERT INTO msp_organizations(id,display_id,name,created_by,updated_by) VALUES($1,$2,'PTO notification',$3,$3)`, mspID, "M-"+mspID, technicianID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO technicians(id,msp_id,email,display_name) VALUES($1,$2,$3,'PTO owner')`, technicianID, mspID, technicianID+"@example.test"); err != nil {
		t.Fatal(err)
	}
	date := time.Date(2026, 8, 22, 0, 0, 0, 0, time.UTC)
	source := calendar.SourceRef{MSPID: mspID, Type: "pto", ID: sourceID}
	projection := calendar.Projection{ID: projectionID, Source: source, EventRole: "unavailability", SourceRevision: 1, Title: "Unavailable", AllDay: true, StartsOn: &date, SchedulingMode: calendar.Informational, OwnerID: technicianID, TerminalState: calendar.Active}
	repository := psa.NewCalendarRepositoryFromPool(pool)
	firstCursor := calendar.ProjectionCursor{ConsumerKey: "canonical-pto", OccurredAt: date, EventID: id.New()}
	if applied, applyErr := repository.ApplyProjectionBatchAtomic(ctx, calendar.ProjectionBatch{Source: source, SourceRevision: 1, Projections: []calendar.Projection{projection}, Cursor: firstCursor}); applyErr != nil || !applied {
		t.Fatalf("initial applied=%v err=%v", applied, applyErr)
	}
	secondCursor := calendar.ProjectionCursor{ConsumerKey: "canonical-pto", OccurredAt: date.Add(time.Minute), EventID: id.New()}
	if applied, applyErr := repository.ApplyProjectionBatchAtomic(ctx, calendar.ProjectionBatch{Source: source, SourceRevision: 2, Cursor: secondCursor}); applyErr != nil || !applied {
		t.Fatalf("removal applied=%v err=%v", applied, applyErr)
	}
	var facts int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM event_outbox WHERE msp_id=$1 AND event_type='calendar.schedule_changed' AND data->>'change_class'='pto'`, mspID).Scan(&facts); err != nil {
		t.Fatal(err)
	}
	if facts != 0 {
		t.Fatalf("projection refresh emitted %d PTO decision facts", facts)
	}
}

func TestRejectedPTORequestPlansAndDeliversDecisionNotificationAgainstPostgres(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for rejected PTO delivery verification")
	}
	ctx := context.Background()
	pool := isolatedCalendarNotificationPool(t, databaseURL)
	mspID, ownerID, managerID := id.New(), id.New(), id.New()
	teamID, roleID, assignmentID, policyID := id.New(), id.New(), id.New(), id.New()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	exec(`INSERT INTO msp_organizations(id,display_id,name,created_by,updated_by) VALUES($1,$2,'Rejected PTO delivery',$3,$3)`, mspID, "M-"+mspID, managerID)
	exec(`INSERT INTO technicians(id,msp_id,email,display_name) VALUES($1,$3,$4,'PTO owner'),($2,$3,$5,'PTO manager')`, ownerID, managerID, mspID, ownerID+"@example.test", managerID+"@example.test")
	exec(`INSERT INTO teams(id,msp_id,key,name,workforce_manager_id) VALUES($1,$2,$3,'PTO team',$4)`, teamID, mspID, "pto-"+teamID, managerID)
	exec(`INSERT INTO team_memberships(team_id,technician_id,msp_id,lifecycle_state,created_at,created_by,updated_at,updated_by) VALUES($1,$2,$3,'active',CURRENT_TIMESTAMP,$4,CURRENT_TIMESTAMP,$4),($1,$4,$3,'active',CURRENT_TIMESTAMP,$4,CURRENT_TIMESTAMP,$4)`, teamID, ownerID, mspID, managerID)
	exec(`INSERT INTO roles(id,msp_id,key,name) VALUES($1,$2,$3,'PTO source reader')`, roleID, mspID, "pto-reader-"+roleID)
	exec(`INSERT INTO role_capabilities(role_id,msp_id,capability) VALUES($1,$2,'calendar.workforce.manage')`, roleID, mspID)
	exec(`INSERT INTO role_assignments(id,msp_id,technician_id,role_id,granted_by) VALUES($1,$2,$3,$4,$5)`, assignmentID, mspID, ownerID, roleID, managerID)
	destinations := `[{"channel":"in_app","recipient_ref":"calendar.assignee","content_classification":"internal"}]`
	exec(`INSERT INTO notification_policies(id,msp_id,key,name,event_type,channels,version) VALUES($1,$2,$3,'PTO decisions','calendar.schedule_changed',$4::jsonb,1)`, policyID, mspID, "pto-policy-"+policyID, destinations)
	exec(`INSERT INTO notification_policy_versions(policy_id,msp_id,version,event_type,destinations,published_at,published_by) VALUES($1,$2,1,'calendar.schedule_changed',$3::jsonb,now(),$4)`, policyID, mspID, destinations, managerID)

	now := time.Date(2026, time.August, 21, 9, 0, 0, 0, time.UTC)
	start, end := now.Add(24*time.Hour), now.Add(32*time.Hour)
	service := workforce.NewPTOService(psa.NewWorkforceRepositoryFromPool(pool), func() time.Time { return now }, id.New)
	owner := authorization.Principal{ID: ownerID, Scope: scope.Principal{MSPID: mspID}}
	manager := authorization.Principal{ID: managerID, Scope: scope.Principal{MSPID: mspID}, Capabilities: authorization.NewCapabilitySet("calendar.workforce.manage")}
	requested, err := service.Request(ctx, workforce.RequestPTOCommand{Principal: owner, TechnicianID: ownerID, PTOType: "sick", StartsAt: &start, EndsAt: &end, Timezone: "UTC", ActorID: ownerID, Source: "integration", IdempotencyKey: "rejected-pto-request"})
	if err != nil {
		t.Fatalf("request PTO: %v", err)
	}
	rejected, err := service.Decide(ctx, workforce.DecidePTOCommand{Principal: manager, RequestID: requested.ID, Decision: workforce.Rejected, Reason: "private medical detail", ActorID: managerID, ExpectedVersion: 1, Source: "integration", IdempotencyKey: "rejected-pto-decision"})
	if err != nil {
		t.Fatalf("reject PTO: %v", err)
	}

	var sourceEventID string
	if err = pool.QueryRow(ctx, `SELECT event_id::text FROM event_outbox WHERE msp_id=$1 AND event_type='pto.rejected' AND subject_id=$2`, mspID, rejected.ID).Scan(&sourceEventID); err != nil {
		t.Fatal(err)
	}
	source := calendar.SourceRef{MSPID: mspID, Type: "pto", ID: rejected.ID}
	calendarRepository := psa.NewCalendarRepositoryFromPool(pool)
	projections, err := adapters.NewPTOAdapter(calendarRepository).Project(ctx, source)
	if err != nil || len(projections) != 1 || projections[0].TerminalState != calendar.Cancelled {
		t.Fatalf("rejected PTO projections=%+v error=%v", projections, err)
	}
	applied, err := calendarRepository.ApplyProjectionBatchAtomic(ctx, calendar.ProjectionBatch{Source: source, SourceRevision: rejected.Version, Projections: projections, Cursor: calendar.ProjectionCursor{ConsumerKey: "rejected-pto-delivery", OccurredAt: now, EventID: sourceEventID}})
	if err != nil || !applied {
		t.Fatalf("project rejected PTO applied=%t error=%v", applied, err)
	}

	notificationRepository := psa.NewNotificationRepositoryFromPool(pool)
	planned, err := notifications.NewPlanner(notificationRepository, func() time.Time { return now.Add(time.Second) }, id.New).RunOnce(ctx, 50)
	if err != nil || planned.Pending != 1 {
		t.Fatalf("plan rejected PTO result=%+v error=%v", planned, err)
	}
	result, err := notifications.NewDeliveryWorker(notificationRepository, nil, func() time.Time { return now.Add(2 * time.Second) }).RunOnce(ctx, 50)
	if err != nil || result.Delivered != 1 || result.Suppressed != 0 {
		t.Fatalf("deliver rejected PTO result=%+v error=%v", result, err)
	}
	var state, title, body string
	if err = pool.QueryRow(ctx, `SELECT delivery.state,recipient.title,recipient.body FROM notification_deliveries delivery JOIN recipient_notifications recipient ON recipient.delivery_id=delivery.id AND recipient.msp_id=delivery.msp_id JOIN event_outbox event ON event.event_id=delivery.event_id AND event.msp_id=delivery.msp_id WHERE delivery.msp_id=$1 AND event.event_type='calendar.schedule_changed' AND event.subject_id=$2`, mspID, rejected.ID).Scan(&state, &title, &body); err != nil {
		t.Fatal(err)
	}
	if state != "delivered" || strings.Contains(strings.ToLower(title+" "+body), "sick") || strings.Contains(strings.ToLower(title+" "+body), "medical") {
		t.Fatalf("rejected PTO state=%q title=%q body=%q", state, title, body)
	}
}

func TestCalendarNotificationEndToEndAcceptedScheduleUsesInboxRoutesAgainstPostgres(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for calendar notification end-to-end verification")
	}
	ctx := context.Background()
	if err := store.Migrate(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	mspID, clientID, technicianID := id.New(), id.New(), id.New()
	roleID, assignmentID, policyID := id.New(), id.New(), id.New()
	taskID, parentID, proposalID, changeID, projectionID := id.New(), id.New(), id.New(), id.New(), id.New()
	otherTechnicianID := id.New()
	now := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)
	originalStart, originalEnd := now.Add(24*time.Hour), now.Add(25*time.Hour)
	movedStart, movedEnd := originalStart.Add(2*time.Hour), originalEnd.Add(2*time.Hour)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, execErr := pool.Exec(ctx, query, args...); execErr != nil {
			t.Fatal(execErr)
		}
	}
	exec(`INSERT INTO msp_organizations(id,display_id,name,created_by,updated_by) VALUES($1,$2,'Calendar inbox end to end',$3,$3)`, mspID, "M-"+mspID, technicianID)
	exec(`INSERT INTO client_organizations(id,msp_id,display_id,name,created_by,updated_by) VALUES($1,$2,$3,'Calendar inbox client',$4,$4)`, clientID, mspID, "C-"+clientID, technicianID)
	exec(`INSERT INTO technicians(id,msp_id,email,display_name) VALUES($1,$3,$4,'Recipient'),($2,$3,$5,'Other recipient')`, technicianID, otherTechnicianID, mspID, technicianID+"@example.test", otherTechnicianID+"@example.test")
	exec(`INSERT INTO roles(id,msp_id,key,name) VALUES($1,$2,$3,'Calendar inbox role')`, roleID, mspID, "calendar-inbox-"+roleID)
	exec(`INSERT INTO role_capabilities(role_id,msp_id,capability) VALUES($1,$2,'calendar.schedule'),($1,$2,'task.edit'),($1,$2,'project.read')`, roleID, mspID)
	exec(`INSERT INTO role_assignments(id,msp_id,client_id,technician_id,role_id,granted_by) VALUES($1,$2,$3,$4,$5,$4)`, assignmentID, mspID, clientID, technicianID, roleID)
	exec(`INSERT INTO tasks(id,msp_id,client_id,parent_type,parent_id,title,status,position,owner_id,estimate_minutes,version,created_by,updated_by,scheduled_starts_at,scheduled_ends_at,schedule_timezone,scheduling_mode) VALUES($1,$2,$3,'project',$4,'Accepted schedule item','open',1,$5,60,1,$5,$5,$6,$7,'UTC','fixed_block')`, taskID, mspID, clientID, parentID, technicianID, originalStart, originalEnd)

	destinations := `[{"channel":"in_app","recipient_ref":"calendar.assignee","content_classification":"internal"}]`
	exec(`INSERT INTO notification_policies(id,msp_id,key,name,event_type,conditions,channels,enabled,version) VALUES($1,$2,$3,'Calendar inbox policy','calendar.schedule_changed','{}',$4::jsonb,true,1)`, policyID, mspID, "calendar-inbox-"+policyID, destinations)
	exec(`INSERT INTO notification_policy_versions(policy_id,msp_id,version,event_type,conditions,destinations,enabled,published_at,published_by) VALUES($1,$2,1,'calendar.schedule_changed','{}',$3::jsonb,true,$4,$5)`, policyID, mspID, destinations, now, technicianID)

	principal := authorization.Principal{
		ID: technicianID, Scope: scope.Principal{MSPID: mspID},
		Capabilities: authorization.NewCapabilitySet("calendar.schedule", "task.edit", "project.read"),
	}
	source := calendar.SourceRef{MSPID: mspID, ClientID: clientID, Type: "task", ID: taskID}
	requested := calendar.RequestedChange{
		ID: changeID, ProjectionID: projectionID, Source: source, EventRole: "scheduled_work",
		SourceRevision: 1, StartsAt: &movedStart, EndsAt: &movedEnd, Timezone: "UTC", Required: true,
	}
	accepted := tasks.ScheduleMutation{
		TaskID: taskID, MSPID: mspID, ClientID: clientID, ExpectedVersion: 1,
		Interval: calendar.TypedInterval{StartsAt: &movedStart, EndsAt: &movedEnd, Timezone: "UTC"},
		Mode:     calendar.FixedBlock, EstimateMinutes: 60, ProjectionID: projectionID,
	}
	change := calendar.ProposedChange{
		ID: changeID, Required: true, Requested: requested,
		Prepared: calendar.PreparedChange{ID: changeID, Request: requested, Source: source, EventRole: "scheduled_work", ExpectedSourceRevision: 1, Mutation: accepted},
		Schedule: calendar.ProposedSchedule{ProjectionID: projectionID, ClientID: clientID, TechnicianID: technicianID, Interval: calendar.TimeInterval{Start: movedStart, End: movedEnd}},
	}
	proposal := calendar.SchedulingProposal{
		ID: proposalID, ActorID: technicianID, MSPID: mspID, ClientID: clientID,
		AuthorizationHash: calendar.AuthorizationFingerprint(principal), CreatedAt: now,
		ExpiresAt: now.Add(5 * time.Minute), State: "previewed", Changes: []calendar.ProposedChange{change},
		Bindings: []calendar.RevisionBinding{{Kind: calendar.RevisionSource, ID: mspID + "/" + clientID + "/task/" + taskID, Version: 1}},
	}
	calendarRepository := psa.NewCalendarRepositoryFromPool(pool)
	if err = calendarRepository.SaveSchedulingProposal(ctx, proposal); err != nil {
		t.Fatal(err)
	}
	registry := calendar.NewWriteAdapterRegistry()
	if err = registry.Register(adapters.NewTaskAdapter(calendarRepository)); err != nil {
		t.Fatal(err)
	}
	unitOfWork := calendar.NewScheduleUnitOfWork(calendarRepository, registry, func() time.Time { return now }, id.New)
	applied, err := unitOfWork.ApplyAtomic(ctx, calendar.ScheduleApplyRequest{Principal: principal, Proposal: proposal, Changes: []calendar.ProposedChange{change}})
	if err != nil || applied.CorrelationID == "" {
		t.Fatalf("accepted schedule result=%+v err=%v", applied, err)
	}
	exec(`INSERT INTO calendar_event_projections(id,msp_id,client_id,source_type,source_id,event_role,source_revision,title,starts_at,ends_at,timezone,all_day,scheduling_mode,assignee_id) VALUES($1,$2,$3,'task',$4,'scheduled_work',2,'Accepted schedule item',$5,$6,'UTC',false,'fixed_block',$7)`, projectionID, mspID, clientID, taskID, movedStart, movedEnd, technicianID)

	notificationRepository := psa.NewNotificationRepositoryFromPool(pool)
	planned, err := notifications.NewPlanner(notificationRepository, func() time.Time { return now.Add(time.Second) }, id.New).RunOnce(ctx, 50)
	if err != nil || planned.Pending < 1 {
		t.Fatalf("existing planner result=%+v err=%v", planned, err)
	}
	var deliveryID string
	if err = pool.QueryRow(ctx, `SELECT delivery.id::text FROM notification_deliveries delivery JOIN event_outbox event ON event.event_id=delivery.event_id WHERE delivery.msp_id=$1 AND event.correlation_id=$2 AND delivery.state='pending'`, mspID, applied.CorrelationID).Scan(&deliveryID); err != nil {
		t.Fatal(err)
	}
	delivered, err := notifications.NewDeliveryWorkerWithEmail(notificationRepository, nil, nil, func() time.Time { return now.Add(2 * time.Second) }).RunOnce(ctx, 50)
	if err != nil || delivered.Delivered < 1 {
		t.Fatalf("existing delivery worker result=%+v err=%v", delivered, err)
	}
	var deliveredState string
	if err = pool.QueryRow(ctx, `SELECT state FROM notification_deliveries WHERE id=$1`, deliveryID).Scan(&deliveredState); err != nil || deliveredState != "delivered" {
		t.Fatalf("delivery state=%q err=%v", deliveredState, err)
	}

	inbox := notifications.NewInboxService(notificationRepository, func() time.Time { return now.Add(3 * time.Second) })
	newRouter := func(current authorization.Principal) http.Handler {
		return httpapi.NewRouter(httpapi.Dependencies{
			Principal:         func(*http.Request) (authorization.Principal, error) { return current, nil },
			NotificationInbox: inbox,
		})
	}
	router := newRouter(principal)
	listResponse := httptest.NewRecorder()
	router.ServeHTTP(listResponse, httptest.NewRequest(http.MethodGet, "/api/v1/notifications", nil))
	var page struct {
		Notifications []struct {
			ID      string `json:"id"`
			Body    string `json:"body"`
			Version int64  `json:"version"`
		} `json:"notifications"`
	}
	if decodeErr := json.NewDecoder(listResponse.Body).Decode(&page); decodeErr != nil || listResponse.Code != http.StatusOK || len(page.Notifications) != 1 {
		t.Fatalf("list status=%d page=%+v decode_err=%v body=%s", listResponse.Code, page, decodeErr, listResponse.Body.String())
	}
	item := page.Notifications[0]
	if item.ID == "" || item.ID == deliveryID || item.Body != "schedule: Calendar inbox client: Accepted schedule item" || item.Version != 1 {
		t.Fatalf("inbox item=%+v delivery_id=%q", item, deliveryID)
	}

	unreadResponse := httptest.NewRecorder()
	router.ServeHTTP(unreadResponse, httptest.NewRequest(http.MethodGet, "/api/v1/notifications/unread-count", nil))
	if unreadResponse.Code != http.StatusOK || strings.TrimSpace(unreadResponse.Body.String()) != `{"count":1}` {
		t.Fatalf("unread status=%d body=%s", unreadResponse.Code, unreadResponse.Body.String())
	}
	mark := func(target http.Handler, version int64) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPatch, "/api/v1/notifications/"+item.ID+"/read", strings.NewReader(fmt.Sprintf(`{"expected_version":%d}`, version)))
		request.Header.Set("Content-Type", "application/json")
		target.ServeHTTP(response, request)
		return response
	}
	for attempt := 0; attempt < 2; attempt++ {
		response := mark(router, item.Version)
		if response.Code != http.StatusOK || response.Header().Get("ETag") != `"2"` {
			t.Fatalf("mark attempt=%d status=%d etag=%q body=%s", attempt, response.Code, response.Header().Get("ETag"), response.Body.String())
		}
	}
	if response := mark(router, 99); response.Code != http.StatusConflict {
		t.Fatalf("stale mark status=%d body=%s", response.Code, response.Body.String())
	}
	otherPrincipal := authorization.Principal{ID: otherTechnicianID, Scope: scope.Principal{MSPID: mspID}}
	if response := mark(newRouter(otherPrincipal), item.Version); response.Code != http.StatusNotFound {
		t.Fatalf("cross-recipient mark status=%d body=%s", response.Code, response.Body.String())
	}
	malformedCursor := httptest.NewRecorder()
	router.ServeHTTP(malformedCursor, httptest.NewRequest(http.MethodGet, "/api/v1/notifications?cursor=%25%25%25", nil))
	if malformedCursor.Code != http.StatusBadRequest {
		t.Fatalf("malformed cursor status=%d body=%s", malformedCursor.Code, malformedCursor.Body.String())
	}
	unreadResponse = httptest.NewRecorder()
	router.ServeHTTP(unreadResponse, httptest.NewRequest(http.MethodGet, "/api/v1/notifications/unread-count", nil))
	if unreadResponse.Code != http.StatusOK || strings.TrimSpace(unreadResponse.Body.String()) != `{"count":0}` {
		t.Fatalf("read count status=%d body=%s", unreadResponse.Code, unreadResponse.Body.String())
	}
}

func TestCalendarDeliveryReauthorizesAndCompletesInboxAtomicallyAgainstPostgres(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for calendar delivery verification")
	}
	ctx := context.Background()
	if err := store.Migrate(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	mspID, technicianID, actorID := id.New(), id.New(), id.New()
	transferClientID := id.New()
	roleID, assignmentID, policyID := id.New(), id.New(), id.New()
	eventID, deliveryID, sourceID, projectionID, correlationID := id.New(), id.New(), id.New(), id.New(), id.New()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, execErr := pool.Exec(ctx, query, args...); execErr != nil {
			t.Fatal(execErr)
		}
	}
	exec(`INSERT INTO msp_organizations(id,display_id,name,created_by,updated_by) VALUES($1,$2,'Calendar delivery',$3,$3)`, mspID, "M-"+mspID, actorID)
	exec(`INSERT INTO technicians(id,msp_id,email,display_name) VALUES($1,$3,$4,'Recipient'),($2,$3,$5,'Actor')`, technicianID, actorID, mspID, "current@example.test", actorID+"@example.test")
	exec(`INSERT INTO client_organizations(id,msp_id,display_id,name,created_by,updated_by) VALUES($1,$2,$3,'Transfer target',$4,$4)`, transferClientID, mspID, "C-"+transferClientID, actorID)
	exec(`INSERT INTO roles(id,msp_id,key,name) VALUES($1,$2,$3,'Calendar reader')`, roleID, mspID, "calendar-reader-"+roleID)
	exec(`INSERT INTO role_capabilities(role_id,msp_id,capability) VALUES($1,$2,'calendar.workforce.manage')`, roleID, mspID)
	exec(`INSERT INTO role_assignments(id,msp_id,technician_id,role_id,granted_by) VALUES($1,$2,$3,$4,$5)`, assignmentID, mspID, technicianID, roleID, actorID)
	destinations := `[{"channel":"in_app","recipient_ref":"calendar.assignee","content_classification":"internal"},{"channel":"email","recipient_ref":"calendar.assignee","content_classification":"internal"}]`
	exec(`INSERT INTO notification_policies(id,msp_id,key,name,event_type,channels) VALUES($1,$2,$3,'Calendar','calendar.schedule_changed',$4::jsonb)`, policyID, mspID, "calendar-"+policyID, destinations)
	exec(`INSERT INTO notification_policy_versions(policy_id,msp_id,version,event_type,destinations,published_at,published_by) VALUES($1,$2,1,'calendar.schedule_changed',$3::jsonb,now(),$4)`, policyID, mspID, destinations, actorID)
	exec(`INSERT INTO event_outbox(event_id,event_type,schema_version,occurred_at,msp_id,actor_type,actor_id,subject_type,subject_id,subject_version,correlation_id,source,data) VALUES($1,'calendar.schedule_changed',1,now(),$2,'system',$3,'calendar_projection',$4,7,$5,'calendar',jsonb_build_object('recipient_id',$6::text,'change_class','schedule','urgency','routine','source_refs',jsonb_build_array(jsonb_build_object('type','technician_schedule','id',$7::text,'event_role','scheduled_work','source_revision',7)),'action_path','/calendar'))`, eventID, mspID, actorID, projectionID, correlationID, technicianID, sourceID)
	now := time.Now().UTC()
	exec(`INSERT INTO calendar_event_projections(id,msp_id,source_type,source_id,event_role,source_revision,title,starts_at,ends_at,timezone,all_day,scheduling_mode,assignee_id) VALUES($1,$2,'technician_schedule',$3,'scheduled_work',8,'Current authorized label',$4,$5,'UTC',false,'fixed_block',$6)`, projectionID, mspID, sourceID, now.Add(time.Hour), now.Add(2*time.Hour), technicianID)
	exec(`INSERT INTO notification_deliveries(id,policy_id,policy_version,msp_id,event_id,channel,recipient_ref,content_classification,state,next_attempt_at,planned_at,calendar_delivery_key) VALUES($1,$2,1,$3,$4,'in_app','calendar.assignee','internal','pending','1900-01-01',$5,$6)`, deliveryID, policyID, mspID, eventID, now.Add(-time.Minute), mspID+":"+correlationID+":"+technicianID+":schedule:routine:in_app:"+policyID+":1")
	exec(`INSERT INTO notification_calendar_delivery_payloads(delivery_id,msp_id,recipient_technician_id,correlation_id,change_class,urgency,source_refs,action_path,calendar_delivery_key) VALUES($1,$2,$3,$4,'schedule','routine',jsonb_build_array(jsonb_build_object('type','technician_schedule','id',$5::text,'event_role','scheduled_work','source_revision',7)),'/calendar',(SELECT calendar_delivery_key FROM notification_deliveries WHERE id=$1))`, deliveryID, mspID, technicianID, correlationID, sourceID)

	repository := psa.NewNotificationRepositoryFromPool(pool).WithExternalDelivery("https://rarity.example", true)
	jobs, err := repository.ClaimPending(ctx, 1, now)
	if err != nil || len(jobs) != 1 || jobs[0].CalendarPayload == nil || jobs[0].CalendarRecipientID != technicianID {
		t.Fatalf("jobs=%+v err=%v", jobs, err)
	}
	job := jobs[0]
	wantDeduplicationKey := mspID + ":" + correlationID + ":" + technicianID + ":schedule:routine:in_app:" + policyID + ":1"
	if job.DeduplicationKey != wantDeduplicationKey {
		t.Fatalf("deduplication key=%q want %q", job.DeduplicationKey, wantDeduplicationKey)
	}
	rendered, err := repository.ReauthorizeCalendarDelivery(ctx, job)
	if err != nil || !rendered.Authorized || rendered.Body != "schedule: Current authorized label" || rendered.ActionPath != "/calendar" {
		t.Fatalf("authorized rendering=%+v err=%v", rendered, err)
	}
	exec(`UPDATE calendar_event_projections SET source_revision=6 WHERE id=$1`, projectionID)
	stale, err := repository.ReauthorizeCalendarDelivery(ctx, job)
	if err != nil || stale.Authorized || stale.Outcome != notifications.CalendarAuthorizationRetryable || stale.RetryReason != "projection_lag" {
		t.Fatalf("stale projection rendering=%+v err=%v", stale, err)
	}
	// A processed physical removal has no current projection row. Its durable
	// tombstone proves the projection worker is caught up, so this is terminal
	// irrelevance rather than projection lag.
	exec(`DELETE FROM calendar_event_projections WHERE id=$1`, projectionID)
	exec(`INSERT INTO calendar_live_changes(msp_id,client_id,client_scope_key,projection_id,source_type,source_id,event_role,change_type,source_revision) VALUES($1,NULL,'global',NULL,'technician_schedule',$2,'scheduled_work','removed',8)`, mspID, sourceID)
	removed, err := repository.ReauthorizeCalendarDelivery(ctx, job)
	if err != nil || removed.Authorized || removed.Outcome != notifications.CalendarAuthorizationSuppressed || removed.SuppressionReason != "recipient_no_longer_relevant" {
		t.Fatalf("removed projection rendering=%+v err=%v", removed, err)
	}
	exec(`DELETE FROM calendar_live_changes WHERE msp_id=$1 AND source_type='technician_schedule' AND source_id=$2`, mspID, sourceID)
	exec(`INSERT INTO calendar_live_changes(msp_id,client_id,client_scope_key,projection_id,source_type,source_id,event_role,change_type,source_revision) VALUES($1,NULL,'global',NULL,'technician_schedule',$2,'source','removed',8)`, mspID, sourceID)
	genericRemoval, err := repository.ReauthorizeCalendarDelivery(ctx, job)
	if err != nil || genericRemoval.Authorized || genericRemoval.Outcome != notifications.CalendarAuthorizationSuppressed || genericRemoval.SuppressionReason != "recipient_no_longer_relevant" {
		t.Fatalf("generic removal rendering=%+v err=%v", genericRemoval, err)
	}

	// A trusted client transfer likewise removes the old scoped row and writes
	// a tombstone while creating a current projection in the new scope. The old
	// scoped notification must suppress immediately rather than retry.
	exec(`DELETE FROM calendar_live_changes WHERE msp_id=$1 AND source_type='technician_schedule' AND source_id=$2`, mspID, sourceID)
	transferredProjectionID := id.New()
	exec(`INSERT INTO calendar_live_changes(msp_id,client_id,client_scope_key,projection_id,source_type,source_id,event_role,change_type,source_revision) VALUES($1,NULL,'global',NULL,'technician_schedule',$2,'scheduled_work','removed',9)`, mspID, sourceID)
	exec(`INSERT INTO calendar_event_projections(id,msp_id,client_id,source_type,source_id,event_role,source_revision,title,starts_at,ends_at,timezone,all_day,scheduling_mode,assignee_id) VALUES($1,$2,$3,'technician_schedule',$4,'scheduled_work',9,'Transferred label',$5,$6,'UTC',false,'fixed_block',$7)`, transferredProjectionID, mspID, transferClientID, sourceID, now.Add(time.Hour), now.Add(2*time.Hour), technicianID)
	transferred, err := repository.ReauthorizeCalendarDelivery(ctx, job)
	if err != nil || transferred.Authorized || transferred.Outcome != notifications.CalendarAuthorizationSuppressed || transferred.SuppressionReason != "recipient_no_longer_relevant" {
		t.Fatalf("transferred projection rendering=%+v err=%v", transferred, err)
	}
	exec(`DELETE FROM calendar_event_projections WHERE id=$1`, transferredProjectionID)
	exec(`DELETE FROM calendar_live_changes WHERE msp_id=$1 AND source_type='technician_schedule' AND source_id=$2`, mspID, sourceID)
	exec(`INSERT INTO calendar_event_projections(id,msp_id,source_type,source_id,event_role,source_revision,title,starts_at,ends_at,timezone,all_day,scheduling_mode,assignee_id) VALUES($1,$2,'technician_schedule',$3,'scheduled_work',8,'Current authorized label',$4,$5,'UTC',false,'fixed_block',$6)`, projectionID, mspID, sourceID, now.Add(time.Hour), now.Add(2*time.Hour), technicianID)
	exec(`UPDATE calendar_event_projections SET source_revision=8,terminal_state='completed' WHERE id=$1`, projectionID)
	terminal, err := repository.ReauthorizeCalendarDelivery(ctx, job)
	if err != nil || terminal.Authorized || terminal.SuppressionReason != "recipient_no_longer_relevant" {
		t.Fatalf("terminal projection rendering=%+v err=%v", terminal, err)
	}
	exec(`UPDATE calendar_event_projections SET terminal_state='active' WHERE id=$1`, projectionID)
	for _, invalidAction := range []string{"/calendar?private=1", "/calendar#private"} {
		exec(`UPDATE notification_calendar_delivery_payloads SET action_path=$2 WHERE delivery_id=$1`, deliveryID, invalidAction)
		job.CalendarPayload.ActionPath = invalidAction
		invalid, invalidErr := repository.ReauthorizeCalendarDelivery(ctx, job)
		if invalidErr != nil || invalid.Authorized || invalid.SuppressionReason != "channel_unavailable" {
			t.Fatalf("action=%q rendering=%+v err=%v", invalidAction, invalid, invalidErr)
		}
	}
	exec(`UPDATE notification_calendar_delivery_payloads SET action_path='/calendar' WHERE delivery_id=$1`, deliveryID)
	job.CalendarPayload.ActionPath = "/calendar"
	exec(`INSERT INTO calendar_notification_preference_sets(msp_id,technician_id) VALUES($1,$2)`, mspID, technicianID)
	exec(`INSERT INTO calendar_notification_preference_rules(msp_id,technician_id,event_class,change_class,urgency,channel,enabled) VALUES($1,$2,'calendar.schedule_changed','schedule','routine','email',true)`, mspID, technicianID)
	exec(`UPDATE notification_deliveries SET channel='email' WHERE id=$1`, deliveryID)
	job.Channel = notifications.Email
	invalidPublicURL, err := psa.NewNotificationRepositoryFromPool(pool).WithExternalDelivery("https://rarity.example/root?private=1", true).ReauthorizeCalendarDelivery(ctx, job)
	if err != nil || invalidPublicURL.Authorized || invalidPublicURL.SuppressionReason != "channel_unavailable" {
		t.Fatalf("invalid public URL rendering=%+v err=%v", invalidPublicURL, err)
	}
	exec(`UPDATE notification_deliveries SET channel='in_app' WHERE id=$1`, deliveryID)
	job.Channel = notifications.InApp
	wrongMSPJob := job
	wrongMSPJob.MSPID = actorID
	wrongMSP, err := repository.ReauthorizeCalendarDelivery(ctx, wrongMSPJob)
	if err != nil || wrongMSP.Authorized || wrongMSP.RecipientEmail != "" {
		t.Fatalf("cross-organization rendering=%+v err=%v", wrongMSP, err)
	}
	exec(`DELETE FROM role_assignments WHERE id=$1`, assignmentID)
	generic, err := repository.ReauthorizeCalendarDelivery(ctx, job)
	if err != nil || !generic.Authorized || generic.Body != "schedule: calendar item" {
		t.Fatalf("generic rendering=%+v err=%v", generic, err)
	}
	if err = repository.CompleteCalendarInApp(ctx, job, generic, now); err != nil {
		t.Fatal(err)
	}
	// Simulate an ambiguous completion acknowledgement: the durable inbox row
	// exists while the delivery becomes reclaimable again. Completion converges.
	exec(`UPDATE notification_deliveries SET state='pending',attempts=2 WHERE id=$1`, deliveryID)
	job.Attempts = 1
	if err = repository.CompleteCalendarInApp(ctx, job, generic, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	var inboxRows, deliveredRows int
	var inboxBody string
	if err = pool.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM recipient_notifications WHERE msp_id=$1 AND recipient_technician_id=$2),
 (SELECT count(*) FROM notification_deliveries WHERE id=$3 AND state='delivered'),
 (SELECT body FROM recipient_notifications WHERE msp_id=$1 AND recipient_technician_id=$2)`, mspID, technicianID, deliveryID).Scan(&inboxRows, &deliveredRows, &inboxBody); err != nil {
		t.Fatal(err)
	}
	if inboxRows != 1 || deliveredRows != 1 || inboxBody != "schedule: calendar item" {
		t.Fatalf("inbox_rows=%d delivered_rows=%d body=%q", inboxRows, deliveredRows, inboxBody)
	}
	page, err := repository.ListRecipientNotifications(ctx, mspID, technicianID, "", 50)
	if err != nil || len(page.Notifications) != 1 || page.Notifications[0].DeliveryID != deliveryID || page.Notifications[0].ID == deliveryID {
		t.Fatalf("inbox page=%+v err=%v", page, err)
	}
	unread, err := repository.CountUnreadRecipientNotifications(ctx, mspID, technicianID)
	if err != nil || unread != 1 {
		t.Fatalf("unread count=%d err=%v", unread, err)
	}
	marked, err := repository.MarkRecipientNotificationRead(ctx, mspID, technicianID, page.Notifications[0].ID, page.Notifications[0].Version, now)
	if err != nil || marked.DeliveryID != deliveryID || marked.ReadAt == nil {
		t.Fatalf("marked notification=%+v err=%v", marked, err)
	}
	unread, err = repository.CountUnreadRecipientNotifications(ctx, mspID, technicianID)
	if err != nil || unread != 0 {
		t.Fatalf("unread after mark=%d err=%v", unread, err)
	}

	// A deduplication collision owned by a different delivery must not allow
	// this delivery to become delivered.
	wrongNotificationID, wrongDeliveryID, wrongInboxEventID := id.New(), id.New(), id.New()
	exec(`DELETE FROM recipient_notifications WHERE msp_id=$1 AND recipient_technician_id=$2`, mspID, technicianID)
	exec(`UPDATE notification_deliveries SET state='pending',attempts=3 WHERE id=$1`, deliveryID)
	exec(`INSERT INTO event_outbox(event_id,event_type,schema_version,occurred_at,msp_id,actor_type,actor_id,subject_type,subject_id,subject_version,correlation_id,source,data) VALUES($1,'calendar.schedule_changed',1,now(),$2,'system',$3,'calendar_projection',$4,7,$5,'calendar','{}')`, wrongInboxEventID, mspID, actorID, projectionID, correlationID)
	exec(`INSERT INTO notification_deliveries(id,policy_id,policy_version,msp_id,event_id,channel,recipient_ref,content_classification,state,next_attempt_at,planned_at,calendar_delivery_key) VALUES($1,$2,1,$3,$4,'email','calendar.assignee','internal','delivered',$5,$5,$6)`, wrongDeliveryID, policyID, mspID, wrongInboxEventID, now, "wrong-inbox-owner:"+wrongDeliveryID)
	exec(`INSERT INTO recipient_notifications(id,delivery_id,msp_id,recipient_technician_id,event_id,deduplication_key,title,body,action_path,content_classification,created_at) VALUES($1,$2,$3,$4,$5,$6,'Wrong delivery','calendar item','/calendar','internal',$7)`, wrongNotificationID, wrongDeliveryID, mspID, technicianID, wrongInboxEventID, job.DeduplicationKey, now)
	job.Attempts = 2
	if err = repository.CompleteCalendarInApp(ctx, job, generic, now.Add(2*time.Second)); err == nil {
		t.Fatal("wrong existing inbox delivery completed current delivery")
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM notification_deliveries WHERE id=$1 AND state='pending'`, deliveryID).Scan(&deliveredRows); err != nil || deliveredRows != 1 {
		t.Fatalf("current delivery did not remain pending: count=%d err=%v", deliveredRows, err)
	}

	exec(`UPDATE notification_deliveries SET state='pending',attempts=3 WHERE id=$1`, deliveryID)
	exec(`UPDATE calendar_event_projections SET assignee_id=NULL WHERE id=$1`, projectionID)
	job.Attempts = 2
	suppressed, err := repository.ReauthorizeCalendarDelivery(ctx, job)
	if err != nil || suppressed.Authorized || suppressed.SuppressionReason != "recipient_no_longer_relevant" {
		t.Fatalf("irrelevant rendering=%+v err=%v", suppressed, err)
	}
	exec(`UPDATE notification_calendar_delivery_payloads SET change_class='cancellation' WHERE delivery_id=$1`, deliveryID)
	job.CalendarPayload.ChangeClass = notifications.CalendarCancellation
	cancellation, err := repository.ReauthorizeCalendarDelivery(ctx, job)
	if err != nil || !cancellation.Authorized || cancellation.Body != "cancellation: calendar item" {
		t.Fatalf("cancellation rendering=%+v err=%v", cancellation, err)
	}

	importantEventID, importantDeliveryID := id.New(), id.New()
	exec(`INSERT INTO event_outbox(event_id,event_type,schema_version,occurred_at,msp_id,actor_type,actor_id,subject_type,subject_id,subject_version,correlation_id,source,data) VALUES($1,'calendar.schedule_changed',1,now(),$2,'system',$3,'calendar_projection',$4,8,$5,'calendar',jsonb_build_object('recipient_id',$6::text,'change_class','schedule','urgency','important','source_refs',jsonb_build_array(jsonb_build_object('type','technician_schedule','id',$7::text,'event_role','scheduled_work','source_revision',7)),'action_path','/calendar'))`, importantEventID, mspID, actorID, projectionID, correlationID, technicianID, sourceID)
	exec(`INSERT INTO notification_deliveries(id,policy_id,policy_version,msp_id,event_id,channel,recipient_ref,content_classification,state,next_attempt_at,planned_at,calendar_delivery_key) VALUES($1,$2,1,$3,$4,'in_app','calendar.assignee','internal','pending','1800-01-01',$5,$6)`, importantDeliveryID, policyID, mspID, importantEventID, now, mspID+":"+correlationID+":"+technicianID+":schedule:important:in_app:"+policyID+":1")
	exec(`INSERT INTO notification_calendar_delivery_payloads(delivery_id,msp_id,recipient_technician_id,correlation_id,change_class,urgency,source_refs,action_path,calendar_delivery_key) VALUES($1,$2,$3,$4,'schedule','important',jsonb_build_array(jsonb_build_object('type','technician_schedule','id',$5::text,'event_role','scheduled_work','source_revision',7)),'/calendar',(SELECT calendar_delivery_key FROM notification_deliveries WHERE id=$1))`, importantDeliveryID, mspID, technicianID, correlationID, sourceID)
	importantJobs, err := repository.ClaimPending(ctx, 1, now)
	if err != nil || len(importantJobs) != 1 || importantJobs[0].ID != importantDeliveryID || importantJobs[0].DeduplicationKey == wantDeduplicationKey {
		t.Fatalf("important jobs=%+v err=%v", importantJobs, err)
	}
}

func TestCalendarDeliveryProjectionLagRetriesThenUsesCapturedPolicyVersionAgainstPostgres(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for calendar projection-lag verification")
	}
	ctx := context.Background()
	if err := store.Migrate(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	mspID, recipientID, otherTechnicianID, actorID := id.New(), id.New(), id.New(), id.New()
	roleID, assignmentID, policyID := id.New(), id.New(), id.New()
	eventID, deliveryID, emailDeliveryID := id.New(), id.New(), id.New()
	sourceID, projectionID, correlationID := id.New(), id.New(), id.New()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, execErr := pool.Exec(ctx, query, args...); execErr != nil {
			t.Fatal(execErr)
		}
	}
	exec(`INSERT INTO msp_organizations(id,display_id,name,created_by,updated_by) VALUES($1,$2,'Calendar lag',$3,$3)`, mspID, "M-"+mspID, actorID)
	exec(`INSERT INTO technicians(id,msp_id,email,display_name) VALUES($1,$4,'recipient@example.test','Recipient'),($2,$4,'other@example.test','Other'),($3,$4,'actor@example.test','Actor')`, recipientID, otherTechnicianID, actorID, mspID)
	exec(`INSERT INTO roles(id,msp_id,key,name) VALUES($1,$2,$3,'Calendar reader')`, roleID, mspID, "calendar-reader-"+roleID)
	exec(`INSERT INTO role_capabilities(role_id,msp_id,capability) VALUES($1,$2,'calendar.workforce.manage')`, roleID, mspID)
	exec(`INSERT INTO role_assignments(id,msp_id,technician_id,role_id,granted_by) VALUES($1,$2,$3,$4,$5)`, assignmentID, mspID, recipientID, roleID, actorID)
	destinations := `[{"channel":"in_app","recipient_ref":"calendar.assignee","content_classification":"internal"},{"channel":"email","recipient_ref":"calendar.assignee","content_classification":"internal"}]`
	exec(`INSERT INTO notification_policies(id,msp_id,key,name,event_type,channels,version) VALUES($1,$2,$3,'Calendar','calendar.schedule_changed',$4::jsonb,1)`, policyID, mspID, "calendar-"+policyID, destinations)
	exec(`INSERT INTO notification_policy_versions(policy_id,msp_id,version,event_type,destinations,enabled,published_at,published_by) VALUES($1,$2,1,'calendar.schedule_changed',$3::jsonb,true,now(),$4)`, policyID, mspID, destinations, actorID)
	exec(`INSERT INTO event_outbox(event_id,event_type,schema_version,occurred_at,msp_id,actor_type,actor_id,subject_type,subject_id,subject_version,correlation_id,source,data) VALUES($1,'calendar.schedule_changed',1,now(),$2,'system',$3,'calendar_projection',$4,7,$5,'calendar','{}')`, eventID, mspID, actorID, projectionID, correlationID)
	// ClaimPending is intentionally global across MSPs. Use a deterministic
	// historical clock so pending deliveries left by earlier integration tests
	// cannot become eligible while this test repeatedly migrates a shared
	// database and steal either worker pass from the delivery under test.
	now := time.Date(1700, time.January, 1, 0, 0, 0, 0, time.UTC)
	exec(`INSERT INTO calendar_event_projections(id,msp_id,source_type,source_id,event_role,source_revision,title,starts_at,ends_at,timezone,all_day,scheduling_mode,assignee_id) VALUES($1,$2,'technician_schedule',$3,'scheduled_work',6,'Current authorized label',$4,$5,'UTC',false,'fixed_block',$6)`, projectionID, mspID, sourceID, now.Add(time.Hour), now.Add(2*time.Hour), recipientID)
	exec(`INSERT INTO notification_deliveries(id,policy_id,policy_version,msp_id,event_id,channel,recipient_ref,content_classification,state,next_attempt_at,planned_at,calendar_delivery_key) VALUES($1,$2,1,$3,$4,'in_app','calendar.assignee','internal','pending',$5,$5,$6)`, deliveryID, policyID, mspID, eventID, now, mspID+":"+correlationID+":"+recipientID+":schedule:routine:in_app:"+policyID+":1")
	exec(`INSERT INTO notification_calendar_delivery_payloads(delivery_id,msp_id,recipient_technician_id,correlation_id,change_class,urgency,source_refs,action_path,calendar_delivery_key) VALUES($1,$2,$3,$4,'schedule','routine',jsonb_build_array(jsonb_build_object('type','technician_schedule','id',$5::text,'event_role','scheduled_work','source_revision',7)),'/calendar',(SELECT calendar_delivery_key FROM notification_deliveries WHERE id=$1))`, deliveryID, mspID, recipientID, correlationID, sourceID)

	repository := psa.NewNotificationRepositoryFromPool(pool).WithExternalDelivery("https://rarity.example", true)
	first, err := notifications.NewDeliveryWorker(repository, nil, func() time.Time { return now }).RunOnce(ctx, 1)
	if err != nil || first.Retrying != 1 || first.Suppressed != 0 {
		t.Fatalf("lag result=%+v error=%v", first, err)
	}
	var state, failureCode string
	var nextAttempt time.Time
	if err = pool.QueryRow(ctx, `SELECT state,COALESCE(failure_code,''),next_attempt_at FROM notification_deliveries WHERE id=$1`, deliveryID).Scan(&state, &failureCode, &nextAttempt); err != nil || state != "pending" || failureCode != "projection_lag" || !nextAttempt.After(now) {
		t.Fatalf("state=%q failure=%q next=%s error=%v", state, failureCode, nextAttempt, err)
	}

	// The projection catches up after planning, while the mutable policy row is
	// changed. The captured published version still governs this delivery.
	exec(`UPDATE calendar_event_projections SET source_revision=7 WHERE id=$1`, projectionID)
	exec(`UPDATE notification_policies SET enabled=false,channels='[]'::jsonb,version=2 WHERE id=$1`, policyID)
	secondNow := nextAttempt.Add(time.Second)
	second, err := notifications.NewDeliveryWorker(repository, nil, func() time.Time { return secondNow }).RunOnce(ctx, 1)
	if err != nil || second.Delivered != 1 || second.Suppressed != 0 {
		t.Fatalf("caught-up result=%+v error=%v", second, err)
	}
	var inboxCount int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM recipient_notifications WHERE delivery_id=$1`, deliveryID).Scan(&inboxCount); err != nil || inboxCount != 1 {
		t.Fatalf("inbox count=%d error=%v", inboxCount, err)
	}

	// Missing preferences do not disable a captured email destination.
	exec(`INSERT INTO notification_deliveries(id,policy_id,policy_version,msp_id,event_id,channel,recipient_ref,content_classification,state,next_attempt_at,planned_at,calendar_delivery_key) VALUES($1,$2,1,$3,$4,'email','calendar.assignee','internal','pending',$5,$5,$6)`, emailDeliveryID, policyID, mspID, eventID, secondNow, mspID+":"+correlationID+":"+recipientID+":schedule:routine:email:"+policyID+":1")
	exec(`INSERT INTO notification_calendar_delivery_payloads(delivery_id,msp_id,recipient_technician_id,correlation_id,change_class,urgency,source_refs,action_path,calendar_delivery_key) VALUES($1,$2,$3,$4,'schedule','routine',jsonb_build_array(jsonb_build_object('type','technician_schedule','id',$5::text,'event_role','scheduled_work','source_revision',7)),'/calendar',(SELECT calendar_delivery_key FROM notification_deliveries WHERE id=$1))`, emailDeliveryID, mspID, recipientID, correlationID, sourceID)
	emailJob := notifications.DeliveryJob{ID: emailDeliveryID, EventID: eventID, MSPID: mspID, CalendarRecipientID: recipientID, Channel: notifications.Email, CalendarPayload: &notifications.CalendarDeliveryPayload{CorrelationID: correlationID, ChangeClass: notifications.CalendarSchedule, Urgency: notifications.CalendarRoutine, Sources: []notifications.CalendarSourceReference{{Type: "technician_schedule", ID: sourceID, EventRole: "scheduled_work", SourceRevision: 7}}, ActionPath: "/calendar"}}
	emailRendered, err := repository.ReauthorizeCalendarDelivery(ctx, emailJob)
	if err != nil || !emailRendered.Authorized {
		t.Fatalf("email rendering=%+v error=%v", emailRendered, err)
	}

	// Non-PTO routing follows the assignee even if the old recipient remains
	// owner; PTO routing follows its owner when present.
	exec(`UPDATE calendar_event_projections SET assignee_id=$2,owner_id=$3 WHERE id=$1`, projectionID, otherTechnicianID, recipientID)
	nonPTO, err := repository.ReauthorizeCalendarDelivery(ctx, emailJob)
	if err != nil || nonPTO.Authorized || nonPTO.SuppressionReason != "recipient_no_longer_relevant" {
		t.Fatalf("non-PTO rendering=%+v error=%v", nonPTO, err)
	}
	exec(`UPDATE calendar_event_projections SET source_type='pto',event_role='unavailability' WHERE id=$1`, projectionID)
	exec(`UPDATE notification_calendar_delivery_payloads SET source_refs=jsonb_build_array(jsonb_build_object('type','pto','id',$2::text,'event_role','unavailability','source_revision',7)) WHERE delivery_id=$1`, emailDeliveryID, sourceID)
	emailJob.CalendarPayload.Sources = []notifications.CalendarSourceReference{{Type: "pto", ID: sourceID, EventRole: "unavailability", SourceRevision: 7}}
	ptoRendered, err := repository.ReauthorizeCalendarDelivery(ctx, emailJob)
	if err != nil || !ptoRendered.Authorized {
		t.Fatalf("PTO rendering=%+v error=%v", ptoRendered, err)
	}
}
