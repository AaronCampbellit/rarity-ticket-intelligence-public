package psa

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/id"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/notifications"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/store"
)

func TestMentionNotificationPlannerPreferencesAndAuthorizationAgainstPostgres(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for mention notification PostgreSQL verification")
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
	mspID, clientID, authorID, recipientID := id.New(), id.New(), id.New(), id.New()
	roleID, authorAssignment, recipientAssignment := id.New(), id.New(), id.New()
	workID, sourceID, occurrenceID, resolutionID, itemID := id.New(), id.New(), id.New(), id.New(), id.New()
	policyID, eventID, correlationID, tokenID := id.New(), id.New(), id.New(), id.New()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, execErr := pool.Exec(ctx, query, args...); execErr != nil {
			t.Fatalf("fixture: %v", execErr)
		}
	}
	exec(`INSERT INTO msp_organizations(id,display_id,name,created_by,updated_by) VALUES($1,$2,'notification live',$3,$3)`, mspID, "NOTIFY-"+mspID, authorID)
	exec(`INSERT INTO client_organizations(id,msp_id,display_id,name,created_by,updated_by) VALUES($1,$2,$3,'client',$4,$4)`, clientID, mspID, "CLIENT-"+clientID, authorID)
	exec(`INSERT INTO technicians(id,msp_id,email,display_name) VALUES($1,$3,$4,'Ada'),($2,$3,$5,'Taylor')`, authorID, recipientID, mspID, authorID+"@example.test", recipientID+"@example.test")
	exec(`INSERT INTO roles(id,msp_id,key,name) VALUES($1,$2,$3,'Mention reader')`, roleID, mspID, "notify-"+roleID)
	exec(`INSERT INTO role_capabilities(role_id,msp_id,capability) VALUES($1,$2,'mention.read'),($1,$2,'work_record.read')`, roleID, mspID)
	exec(`INSERT INTO role_assignments(id,msp_id,client_id,technician_id,role_id,granted_by) VALUES($1,$2,$3,$4,$6,$4),($5,$2,$3,$7,$6,$4)`, authorAssignment, mspID, clientID, authorID, recipientAssignment, roleID, recipientID)
	exec(`INSERT INTO work_records(id,msp_id,client_id,display_id,record_type,title,status,priority,created_by,updated_by) VALUES($1,$2,$3,$4,'incident','VPN investigation','new','normal',$5,$5)`, workID, mspID, clientID, "INC-"+workID, authorID)
	token := fmt.Sprintf(`[{"id":%q,"target_type":"technician","target_id":%q,"label":"@Taylor","start":0,"end":7}]`, tokenID, recipientID)
	exec(`INSERT INTO internal_collaboration_sources(id,msp_id,client_id,parent_type,parent_id,source_kind,body,mention_tokens,author_id,version,created_at,updated_at) VALUES($1,$2,$3,'work_record',$4,'comment','@Taylor investigate',$5::jsonb,$6,1,now(),now())`, sourceID, mspID, clientID, workID, token, authorID)
	exec(`INSERT INTO mention_occurrences(id,msp_id,client_id,source_id,source_revision,parent_type,parent_id,token_id,author_id,target_type,target_id,mentioned_at,correlation_id) VALUES($1,$2,$3,$4,1,'work_record',$5,$6,$7,'technician',$8,now(),$9)`, occurrenceID, mspID, clientID, sourceID, workID, tokenID, authorID, recipientID, correlationID)
	exec(`INSERT INTO mention_recipient_resolutions(id,msp_id,client_id,occurrence_id,recipient_id,decision,resolution_path,decided_at,reason_code) VALUES($1,$2,$3,$4,$5,'eligible','direct',now(),'eligible')`, resolutionID, mspID, clientID, occurrenceID, recipientID)
	exec(`INSERT INTO mention_items(id,msp_id,client_id,recipient_id,parent_type,parent_id,latest_occurrence_id,last_mentioned_at) VALUES($1,$2,$3,$4,'work_record',$5,$6,now())`, itemID, mspID, clientID, recipientID, workID, occurrenceID)
	destinations := `[{"channel":"email","recipient_ref":"recipient","content_classification":"internal"}]`
	exec(`INSERT INTO notification_policies(id,msp_id,client_id,key,name,event_type,channels) VALUES($1,$2,$3,$4,'Mentions','mention.occurred',$5::jsonb)`, policyID, mspID, clientID, "mentions-"+policyID, destinations)
	exec(`INSERT INTO notification_policy_versions(policy_id,msp_id,version,event_type,destinations,published_at,published_by) VALUES($1,$2,1,'mention.occurred',$3::jsonb,now(),$4)`, policyID, mspID, destinations, authorID)
	exec(`INSERT INTO event_outbox(event_id,event_type,schema_version,occurred_at,msp_id,client_id,actor_type,actor_id,subject_type,subject_id,subject_version,correlation_id,source) VALUES($1,'mention.occurred',1,'2000-01-01', $2,$3,'technician',$4,'internal_collaboration_source',$5,1,$6,'integration')`, eventID, mspID, clientID, authorID, sourceID, correlationID)

	repository := NewNotificationRepositoryFromPool(pool).WithExternalDelivery("https://rarity.example", true)
	concurrentEventID := id.New()
	exec(`INSERT INTO event_outbox(event_id,event_type,schema_version,occurred_at,msp_id,client_id,actor_type,actor_id,subject_type,subject_id,subject_version,correlation_id,source) VALUES($1,'mention.occurred',1,'2000-01-01', $2,$3,'technician',$4,'internal_collaboration_source',$5,1,$6,'integration')`, concurrentEventID, mspID, clientID, authorID, sourceID, id.New())
	concurrentAt := time.Now().UTC()
	decisions := []notifications.PlannedDecision{
		{Event: notifications.PlanningEvent{ID: concurrentEventID}, PlannedAt: concurrentAt, Deliveries: []notifications.PlannedDelivery{{ID: id.New(), PolicyID: policyID, PolicyVersion: 1, MSPID: mspID, ClientID: clientID, EventID: concurrentEventID, Channel: notifications.Email, RecipientRef: "recipient", ContentClassification: "internal", State: notifications.Pending, PlannedAt: concurrentAt, MentionOccurrenceID: occurrenceID, RecipientTechnicianID: recipientID}}},
		{Event: notifications.PlanningEvent{ID: concurrentEventID}, PlannedAt: concurrentAt, Deliveries: []notifications.PlannedDelivery{{ID: id.New(), PolicyID: policyID, PolicyVersion: 1, MSPID: mspID, ClientID: clientID, EventID: concurrentEventID, Channel: notifications.Email, RecipientRef: "changed-policy-destination", ContentClassification: "internal", State: notifications.Pending, PlannedAt: concurrentAt, MentionOccurrenceID: occurrenceID, RecipientTechnicianID: recipientID}}},
	}
	start := make(chan struct{})
	type planResult struct {
		owned bool
		err   error
	}
	resultsByPlanner := make(chan planResult, len(decisions))
	var planners sync.WaitGroup
	for _, decision := range decisions {
		planners.Add(1)
		go func(candidate notifications.PlannedDecision) {
			defer planners.Done()
			<-start
			owned, planErr := repository.PlanAtomic(ctx, candidate)
			resultsByPlanner <- planResult{owned: owned, err: planErr}
		}(decision)
	}
	close(start)
	planners.Wait()
	close(resultsByPlanner)
	ownedPlans := 0
	for result := range resultsByPlanner {
		if result.err != nil {
			t.Fatalf("concurrent PlanAtomic: %v", result.err)
		}
		if result.owned {
			ownedPlans++
		}
	}
	if ownedPlans != 1 {
		t.Fatalf("concurrent PlanAtomic owners=%d want 1", ownedPlans)
	}
	var planCount, deliveryCount int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM notification_event_plans WHERE event_id=$1`, concurrentEventID).Scan(&planCount); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM notification_deliveries WHERE event_id=$1 AND recipient_technician_id=$2 AND channel='email'`, concurrentEventID, recipientID).Scan(&deliveryCount); err != nil {
		t.Fatal(err)
	}
	if planCount != 1 || deliveryCount != 1 {
		t.Fatalf("concurrent event plan count=%d delivery count=%d", planCount, deliveryCount)
	}
	preference, err := repository.SaveMentionPreference(ctx, mspID, notifications.RecipientPreference{TechnicianID: recipientID, EventType: notifications.MentionOccurred, EmailEnabled: true, TeamsEnabled: false, TimeZone: "America/New_York", Version: 1}, 0)
	if err != nil || preference.Version != 1 {
		t.Fatalf("preference=%+v err=%v", preference, err)
	}
	events, err := repository.ClaimEvents(ctx, 1000, time.Now().Add(time.Second))
	if err != nil {
		t.Fatalf("claim mention event: %v", err)
	}
	found := false
	for _, event := range events {
		if event.ID == eventID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("mention event %s not claimed among %+v", eventID, events)
	}
	planner := notifications.NewPlanner(repository, time.Now, id.New)
	result, err := planner.RunOnce(ctx, 10)
	if err != nil || result.Events != 1 || result.Pending != 1 {
		t.Fatalf("planning=%+v err=%v", result, err)
	}
	result, err = planner.RunOnce(ctx, 10)
	if err != nil || result.Events != 0 {
		t.Fatalf("idempotent planning=%+v err=%v", result, err)
	}
	jobs, err := repository.ClaimPending(ctx, 1000, time.Now().Add(time.Second))
	if err != nil {
		t.Fatalf("jobs=%+v err=%v", jobs, err)
	}
	var job notifications.DeliveryJob
	for _, candidate := range jobs {
		if candidate.EventID == eventID {
			job = candidate
			break
		}
	}
	if job.ID == "" || job.RecipientTechnicianID != recipientID {
		t.Fatalf("own delivery missing from jobs=%+v", jobs)
	}
	authorized, err := repository.ReauthorizeMentionDelivery(ctx, job)
	if err != nil || !authorized.Authorized || authorized.RecipientEmail != recipientID+"@example.test" || !strings.Contains(authorized.Summary.AuthenticatedURL, occurrenceID) {
		t.Fatalf("authorization=%+v err=%v", authorized, err)
	}
	exec(`UPDATE technicians SET email='' WHERE id=$1`, recipientID)
	authorized, err = repository.ReauthorizeMentionDelivery(ctx, job)
	if err != nil || authorized.Authorized || authorized.SuppressionReason != "channel_unavailable" {
		t.Fatalf("invalid current email authorization=%+v err=%v", authorized, err)
	}
	exec(`UPDATE technicians SET email=$2 WHERE id=$1`, recipientID, recipientID+"@example.test")
	exec(`UPDATE notification_policies SET event_type='ticket.updated' WHERE id=$1`, policyID)
	authorized, err = repository.ReauthorizeMentionDelivery(ctx, job)
	if err != nil || authorized.Authorized || authorized.SuppressionReason != "channel_unavailable" {
		t.Fatalf("changed policy event authorization=%+v err=%v", authorized, err)
	}
	exec(`UPDATE notification_policies SET event_type='mention.occurred' WHERE id=$1`, policyID)
	connectionID := id.New()
	exec(`INSERT INTO teams_connections(id,msp_id,client_id,name,webhook_secret_ref,enabled,created_by,updated_by) VALUES($1,$2,$3,'mention delivery','env://RARITY_TEAMS_WEBHOOK_MENTION',true,$4,$4)`, connectionID, mspID, clientID, authorID)
	teamsDestinations := fmt.Sprintf(`[{"channel":"teams","recipient_ref":%q,"content_classification":"internal"}]`, connectionID)
	exec(`UPDATE notification_policies SET channels=$2::jsonb WHERE id=$1`, policyID, teamsDestinations)
	exec(`UPDATE notification_recipient_preferences SET teams_enabled=true WHERE msp_id=$1 AND technician_id=$2 AND event_type='mention.occurred'`, mspID, recipientID)
	exec(`UPDATE notification_deliveries SET channel='teams',recipient_ref=$2 WHERE id=$1`, job.ID, connectionID)
	teamsJob := job
	teamsJob.Channel = notifications.Teams
	authorized, err = repository.ReauthorizeMentionDelivery(ctx, teamsJob)
	if err != nil || !authorized.Authorized || authorized.Connection.ID != connectionID {
		t.Fatalf("current Teams authorization=%+v err=%v", authorized, err)
	}
	exec(`UPDATE teams_connections SET enabled=false WHERE id=$1`, connectionID)
	authorized, err = repository.ReauthorizeMentionDelivery(ctx, teamsJob)
	if err != nil || authorized.Authorized || authorized.SuppressionReason != "channel_unavailable" {
		t.Fatalf("unavailable named Teams authorization=%+v err=%v", authorized, err)
	}
	exec(`DELETE FROM role_assignments WHERE id=$1`, recipientAssignment)
	authorized, err = repository.ReauthorizeMentionDelivery(ctx, teamsJob)
	if err != nil || authorized.Authorized || authorized.SuppressionReason != "access_revoked" {
		t.Fatalf("revoked authorization=%+v err=%v", authorized, err)
	}
}

func TestNotificationRepositoryPublishesVersionAndFactsAtomically(t *testing.T) {
	tx := &fakeSalesTx{}
	at := time.Date(2026, time.July, 29, 16, 0, 0, 0, time.UTC)
	err := NewNotificationRepository(&fakeSalesDB{tx: tx}).PublishPolicyAtomic(
		context.Background(),
		notifications.PolicyPublishMutation{
			Created: true,
			Policy: notifications.PublishedPolicy{
				ID: "policy", MSPID: "msp", ClientID: "client",
				Key: "sla-warning", Name: "SLA warning", EventType: "sla.warning",
				Version: 1, Enabled: true, Priority: 100, StableOrder: 10,
				PublishedAt: at, PublishedBy: "actor",
				Destinations: []notifications.Destination{{
					Channel: notifications.Teams, RecipientRef: "teams",
					ContentClassification: "restricted",
				}},
			},
			Audit: mutation.AuditRecord{
				ID: "audit", OccurredAt: at, MSPID: "msp", ClientID: "client",
				ActorType: "technician", ActorID: "actor",
				Action: "notification_policy.published", SubjectType: "notification_policy",
				SubjectID: "policy", SubjectVersion: 1, Source: "api",
				CorrelationID: "correlation",
			},
			Event: mutation.EventRecord{
				EventID: "event", EventType: "notification_policy.published",
				SchemaVersion: 1, OccurredAt: at, MSPID: "msp", ClientID: "client",
				ActorType: "technician", ActorID: "actor",
				SubjectType: "notification_policy", SubjectID: "policy",
				SubjectVersion: 1, Source: "api", CorrelationID: "correlation",
			},
		},
	)
	if err != nil {
		t.Fatalf("PublishPolicyAtomic() error = %v", err)
	}
	assertQueryOrder(
		t, tx.queries,
		"INSERT INTO notification_policies",
		"INSERT INTO notification_policy_versions",
		"INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
}

func TestNotificationRepositoryValidatesNamedTeamsConnectionInPolicyScope(t *testing.T) {
	db := &fakeSalesDB{queryRow: fakeRow{scan: func(destinations ...any) {
		*(destinations[0].(*int)) = 1
	}}}
	err := NewNotificationRepository(db).ValidateDestinations(
		context.Background(),
		scope.Target{MSPID: "msp", ClientID: "client"},
		[]notifications.Destination{{
			Channel: notifications.Teams, RecipientRef: "teams-connection",
			ContentClassification: "restricted",
		}},
	)
	if err != nil {
		t.Fatalf("ValidateDestinations() error = %v", err)
	}
	if !strings.Contains(db.query, "teams_connections") ||
		!strings.Contains(db.query, "connection.client_id IS NULL") ||
		!strings.Contains(db.query, "connection.client_id = NULLIF($2, '')::uuid") {
		t.Fatalf("Teams validation lacks scope boundary: %s", db.query)
	}
}

func TestNotificationPlannerClaimsEventBeforePersistingDeliveries(t *testing.T) {
	tx := &fakeSalesTx{}
	at := time.Date(2026, time.July, 29, 16, 0, 0, 0, time.UTC)
	owned, err := NewNotificationRepository(&fakeSalesDB{tx: tx}).PlanAtomic(
		context.Background(),
		notifications.PlannedDecision{
			Event:     notifications.PlanningEvent{ID: "event"},
			PlannedAt: at,
			Deliveries: []notifications.PlannedDelivery{
				{ID: "one", PolicyID: "policy", PolicyVersion: 2,
					EventID: "event", MSPID: "msp", ClientID: "client",
					WorkRecordID: "work", Channel: notifications.InApp,
					RecipientRef: "queue:noc", ContentClassification: "internal",
					State: notifications.Pending, PlannedAt: at},
				{ID: "two", PolicyID: "policy", PolicyVersion: 2,
					EventID: "event", MSPID: "msp", ClientID: "client",
					WorkRecordID: "work", Channel: notifications.Teams,
					RecipientRef: "teams", ContentClassification: "restricted",
					State: notifications.Suppressed, SuppressionReason: "quiet_period",
					PlannedAt: at},
			},
		},
	)
	if err != nil {
		t.Fatalf("PlanAtomic() error = %v", err)
	}
	if !owned {
		t.Fatal("successful event claim did not report ownership")
	}
	assertQueryOrder(
		t, tx.queries,
		"INSERT INTO notification_event_plans",
		"INSERT INTO notification_deliveries",
		"INSERT INTO notification_deliveries",
	)
}

func TestNotificationPlannerDoesNotPersistDeliveriesWhenEventClaimIsLost(t *testing.T) {
	tx := &fakeSalesTx{zeroRowsAt: 1}
	owned, err := NewNotificationRepository(&fakeSalesDB{tx: tx}).PlanAtomic(context.Background(), notifications.PlannedDecision{
		Event: notifications.PlanningEvent{ID: "event"}, PlannedAt: time.Now(),
		Deliveries: []notifications.PlannedDelivery{{ID: "delivery", EventID: "event"}},
	})
	if err != nil {
		t.Fatalf("PlanAtomic() error = %v", err)
	}
	if owned {
		t.Fatal("lost event claim reported ownership")
	}
	if len(tx.queries) != 1 || !tx.committed {
		t.Fatalf("lost claim wrote deliveries: queries=%v committed=%t", tx.queries, tx.committed)
	}
}

func TestCalendarPlanAtomicPersistsOneTypedPayloadAfterEachDelivery(t *testing.T) {
	tx := &fakeSalesTx{}
	decision := calendarRepositoryDecision("event")
	decision.Deliveries[0].ID = "one"
	second := decision.Deliveries[0]
	second.ID, second.Channel = "two", notifications.Email
	decision.Deliveries = append(decision.Deliveries, second)

	owned, err := NewNotificationRepository(&fakeSalesDB{tx: tx}).PlanAtomic(context.Background(), decision)
	if err != nil || !owned {
		t.Fatalf("PlanAtomic() owned=%t error=%v", owned, err)
	}
	assertQueryOrder(t, tx.queries,
		"INSERT INTO notification_event_plans",
		"INSERT INTO notification_deliveries", "INSERT INTO notification_calendar_delivery_payloads",
		"INSERT INTO notification_deliveries", "INSERT INTO notification_calendar_delivery_payloads",
	)
	if got := tx.args[1][16]; got != "" {
		t.Fatalf("calendar delivery populated mention recipient column with %v", got)
	}
	if got := tx.args[2][1]; got != "tech" {
		t.Fatalf("typed calendar payload recipient=%v, want tech", got)
	}
	for index, query := range tx.queries {
		if !strings.Contains(query, "notification_calendar_delivery_payloads") {
			continue
		}
		sources, ok := tx.args[index][5].([]byte)
		if !ok || strings.Contains(string(sources), "title") || strings.Contains(string(sources), "body") || !strings.Contains(string(sources), `"source_revision":1`) {
			t.Fatalf("payload source refs are not stable canonical references: %q", sources)
		}
	}
}

func TestCalendarPlanAtomicPersistsLogicalKeyAndMergesSourcesOnConflict(t *testing.T) {
	tx := &fakeSalesTx{}
	owned, err := NewNotificationRepository(&fakeSalesDB{tx: tx}).PlanAtomic(context.Background(), calendarRepositoryDecision("event"))
	if err != nil || !owned {
		t.Fatalf("PlanAtomic() owned=%t error=%v", owned, err)
	}
	if len(tx.queries) < 3 {
		t.Fatalf("PlanAtomic() queries=%v", tx.queries)
	}
	logicalKey := "msp:correlation:tech:schedule:routine:in_app:policy:2"
	if !strings.Contains(tx.queries[1], "calendar_delivery_key") ||
		!strings.Contains(tx.queries[1], "ON CONFLICT (msp_id,calendar_delivery_key)") {
		t.Fatalf("calendar delivery is not serialized by its logical key: %s", tx.queries[1])
	}
	foundKey := false
	for _, argument := range tx.args[1] {
		foundKey = foundKey || argument == logicalKey
	}
	if !foundKey {
		t.Fatalf("calendar delivery did not persist logical key %q: args=%v", logicalKey, tx.args[1])
	}
	if !strings.Contains(tx.queries[2], "ON CONFLICT (delivery_id) DO UPDATE") ||
		!strings.Contains(tx.queries[2], "jsonb_array_elements") {
		t.Fatalf("calendar payload does not atomically merge source refs: %s", tx.queries[2])
	}
}

func TestCalendarPlanAtomicRollsBackDeliveryWhenTypedPayloadInsertFails(t *testing.T) {
	tx := &fakeSalesTx{failAt: 3}
	_, err := NewNotificationRepository(&fakeSalesDB{tx: tx}).PlanAtomic(context.Background(), calendarRepositoryDecision("event"))
	if err == nil || !tx.rolledBack || tx.committed {
		t.Fatalf("PlanAtomic() error=%v rolled back=%t committed=%t", err, tx.rolledBack, tx.committed)
	}
}

func TestCalendarPlanAtomicDoesNotConsumeEventWhenDeliveryInsertConflicts(t *testing.T) {
	tx := &fakeSalesTx{zeroRowsAt: 2}
	_, err := NewNotificationRepository(&fakeSalesDB{tx: tx}).PlanAtomic(context.Background(), calendarRepositoryDecision("event"))
	if err == nil || !tx.rolledBack || tx.committed {
		t.Fatalf("PlanAtomic() error=%v rolled back=%t committed=%t", err, tx.rolledBack, tx.committed)
	}
}

func TestCalendarPlanAtomicRejectsMissingTypedPayloadBeforeClaim(t *testing.T) {
	tx := &fakeSalesTx{}
	_, err := NewNotificationRepository(&fakeSalesDB{tx: tx}).PlanAtomic(context.Background(), notifications.PlannedDecision{
		Event: notifications.PlanningEvent{ID: "event", Type: notifications.CalendarNotificationEventType}, PlannedAt: time.Now(),
		Deliveries: []notifications.PlannedDelivery{{ID: "delivery", EventID: "event"}},
	})
	if err == nil {
		t.Fatal("PlanAtomic() error = nil, want missing typed calendar payload error")
	}
	if len(tx.queries) != 0 || tx.committed || tx.rolledBack {
		t.Fatalf("invalid calendar plan wrote transaction: queries=%v committed=%t rolled_back=%t", tx.queries, tx.committed, tx.rolledBack)
	}
}

func TestCalendarPlanAtomicRejectsDeliveryOutsideClaimedEventGroup(t *testing.T) {
	tx := &fakeSalesTx{}
	decision := calendarRepositoryDecision("event-a")
	decision.Deliveries[0].EventID = "event-b"

	owned, err := NewNotificationRepository(&fakeSalesDB{tx: tx}).PlanAtomic(context.Background(), decision)
	if err == nil || owned {
		t.Fatalf("PlanAtomic() owned=%t error=%v, want bound-event rejection", owned, err)
	}
	if len(tx.queries) != 0 {
		t.Fatalf("unbound delivery reached transaction: %v", tx.queries)
	}
}

func TestCalendarPlanAtomicRejectsDeliveryOutsideClaimedOrganization(t *testing.T) {
	tx := &fakeSalesTx{}
	decision := calendarRepositoryDecision("event-a")
	decision.Deliveries[0].MSPID = "other-msp"

	owned, err := NewNotificationRepository(&fakeSalesDB{tx: tx}).PlanAtomic(context.Background(), decision)
	if err == nil || owned {
		t.Fatalf("PlanAtomic() owned=%t error=%v, want organization rejection", owned, err)
	}
	if len(tx.queries) != 0 {
		t.Fatalf("cross-organization delivery reached transaction: %v", tx.queries)
	}
}

func TestCalendarPlanAtomicRejectsDeliveryMetadataOutsideClaimedDigest(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*notifications.PlannedDecision)
	}{
		{"recipient", func(value *notifications.PlannedDecision) { value.Deliveries[0].RecipientTechnicianID = "other-tech" }},
		{"correlation", func(value *notifications.PlannedDecision) {
			value.Deliveries[0].CalendarPayload.CorrelationID = "other-correlation"
		}},
		{"change class", func(value *notifications.PlannedDecision) {
			value.Deliveries[0].CalendarPayload.ChangeClass = notifications.CalendarConflict
		}},
		{"urgency", func(value *notifications.PlannedDecision) {
			value.Deliveries[0].CalendarPayload.Urgency = notifications.CalendarUrgent
		}},
		{"channel", func(value *notifications.PlannedDecision) { value.Deliveries[0].Channel = notifications.Teams }},
		{"recipient ref", func(value *notifications.PlannedDecision) { value.Deliveries[0].RecipientRef = "technician:fixed" }},
		{"source refs", func(value *notifications.PlannedDecision) {
			value.Deliveries[0].CalendarPayload.Sources[0].ID = "other-task"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tx := &fakeSalesTx{}
			decision := calendarRepositoryDecision("event-a")
			test.mutate(&decision)
			owned, err := NewNotificationRepository(&fakeSalesDB{tx: tx}).PlanAtomic(context.Background(), decision)
			if err == nil || owned || len(tx.queries) != 0 {
				t.Fatalf("PlanAtomic() owned=%t error=%v queries=%v", owned, err, tx.queries)
			}
		})
	}
}

func TestCalendarPlanAtomicRejectsCalendarShapedZeroDeliveryWithoutCanonicalType(t *testing.T) {
	tx := &fakeSalesTx{}
	event := calendarRepositoryEvent("event-a")
	event.Type = ""
	owned, err := NewNotificationRepository(&fakeSalesDB{tx: tx}).PlanAtomic(context.Background(), notifications.PlannedDecision{Event: event, PlannedAt: time.Now()})
	if err == nil || owned {
		t.Fatalf("PlanAtomic() owned=%t error=%v, want canonical type rejection", owned, err)
	}
	if len(tx.queries) != 0 {
		t.Fatalf("malformed zero-delivery event reached transaction: %v", tx.queries)
	}
}

func TestCalendarPlanAtomicRollsBackWhenTypedPayloadInsertAffectsNoRow(t *testing.T) {
	tx := &fakeSalesTx{zeroRowsAt: 3}
	owned, err := NewNotificationRepository(&fakeSalesDB{tx: tx}).PlanAtomic(context.Background(), calendarRepositoryDecision("event-a"))
	if err == nil || owned || !tx.rolledBack || tx.committed {
		t.Fatalf("PlanAtomic() owned=%t error=%v rolled_back=%t committed=%t", owned, err, tx.rolledBack, tx.committed)
	}
}

func TestCalendarPlanAtomicClaimsEveryDigestEventBeforePersistingDelivery(t *testing.T) {
	tx := &fakeSalesTx{}
	decision := calendarRepositoryDecision("event-a")
	second := calendarRepositoryEvent("event-b")
	second.CalendarSources = []notifications.CalendarSourceReference{{Type: "task", ID: "task-b", ClientID: "client", EventRole: "scheduled_work", SourceRevision: 2}}
	decision.Events = []notifications.PlanningEvent{decision.Event, second}
	decision.Event.CalendarSources = append(decision.Event.CalendarSources, second.CalendarSources...)
	decision.Deliveries[0].CalendarPayload.Sources = append(decision.Deliveries[0].CalendarPayload.Sources, second.CalendarSources...)

	owned, err := NewNotificationRepository(&fakeSalesDB{tx: tx}).PlanAtomic(context.Background(), decision)
	if err != nil || !owned {
		t.Fatalf("PlanAtomic() owned=%t error=%v", owned, err)
	}
	assertQueryOrder(t, tx.queries,
		"INSERT INTO notification_event_plans", "INSERT INTO notification_event_plans",
		"INSERT INTO notification_deliveries", "INSERT INTO notification_calendar_delivery_payloads",
	)
}

func TestCalendarPlanAtomicRollsBackAllMarkersWhenSecondClaimIsLost(t *testing.T) {
	tx := &fakeSalesTx{zeroRowsAt: 2}
	decision := calendarRepositoryDecision("event-a")
	second := calendarRepositoryEvent("event-b")
	second.CalendarSources = []notifications.CalendarSourceReference{{Type: "task", ID: "task-b", ClientID: "client", EventRole: "scheduled_work", SourceRevision: 2}}
	decision.Events = []notifications.PlanningEvent{decision.Event, second}
	decision.Event.CalendarSources = append(decision.Event.CalendarSources, second.CalendarSources...)
	decision.Deliveries[0].CalendarPayload.Sources = append(decision.Deliveries[0].CalendarPayload.Sources, second.CalendarSources...)

	owned, err := NewNotificationRepository(&fakeSalesDB{tx: tx}).PlanAtomic(context.Background(), decision)
	if err != nil || owned || !tx.rolledBack || tx.committed || len(tx.queries) != 2 {
		t.Fatalf("PlanAtomic() owned=%t error=%v rolled_back=%t committed=%t queries=%v", owned, err, tx.rolledBack, tx.committed, tx.queries)
	}
}

func TestNotificationPlanAtomicPreservesGenericEventWithCorrelationID(t *testing.T) {
	tx := &fakeSalesTx{}
	at := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)
	owned, err := NewNotificationRepository(&fakeSalesDB{tx: tx}).PlanAtomic(context.Background(), notifications.PlannedDecision{
		Event: notifications.PlanningEvent{ID: "event", Type: "sla.warning", MSPID: "msp", CorrelationID: "correlation"}, PlannedAt: at,
		Deliveries: []notifications.PlannedDelivery{{ID: "delivery", PolicyID: "policy", PolicyVersion: 1, EventID: "event", MSPID: "msp", Channel: notifications.InApp, RecipientRef: "queue:noc", ContentClassification: "internal", State: notifications.Pending, PlannedAt: at}},
	})
	if err != nil || !owned {
		t.Fatalf("PlanAtomic() owned=%t error=%v", owned, err)
	}
}

func calendarRepositoryDecision(eventID string) notifications.PlannedDecision {
	event := calendarRepositoryEvent(eventID)
	payload := &notifications.CalendarDeliveryPayload{
		CorrelationID: event.CorrelationID, ChangeClass: event.CalendarChangeClass, Urgency: event.CalendarUrgency,
		Sources: append([]notifications.CalendarSourceReference(nil), event.CalendarSources...), ActionPath: event.CalendarActionPath,
	}
	return notifications.PlannedDecision{
		Event: event, Events: []notifications.PlanningEvent{event}, PlannedAt: time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC),
		Deliveries: []notifications.PlannedDelivery{{
			ID: "delivery", PolicyID: "policy", PolicyVersion: 2, EventID: event.ID, MSPID: event.MSPID, ClientID: event.ClientID,
			RecipientTechnicianID: event.CalendarRecipientID, Channel: notifications.InApp, RecipientRef: notifications.CalendarAssigneeRecipientRef,
			ContentClassification: "internal", State: notifications.Pending, PlannedAt: time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC), CalendarPayload: payload,
		}},
	}
}

func calendarRepositoryEvent(eventID string) notifications.PlanningEvent {
	return notifications.PlanningEvent{
		ID: eventID, Type: notifications.CalendarNotificationEventType, MSPID: "msp", ClientID: "client", CorrelationID: "correlation",
		CalendarRecipientID: "tech", CalendarChangeClass: notifications.CalendarSchedule, CalendarUrgency: notifications.CalendarRoutine,
		CalendarSources: []notifications.CalendarSourceReference{{Type: "task", ID: "task-a", ClientID: "client", EventRole: "scheduled_work", SourceRevision: 1}}, CalendarActionPath: "/calendar",
	}
}

func TestCanonicalCalendarClaimDecodesOnlyStructuralData(t *testing.T) {
	data := `{"recipient_id":"tech","change_class":"schedule","urgency":"routine","source_refs":[{"type":"task","id":"task","client_id":"client","event_role":"scheduled_work","source_revision":7}],"action_path":"/calendar","body":"unsafe body","title":"unsafe title","client_name":"unsafe client"}`
	db := &fakeSalesDB{queryRows: &fakeRows{scans: []func(...any){calendarPlanningRow(data)}}}

	events, err := NewNotificationRepository(db).ClaimEvents(context.Background(), 10, time.Now())
	if err != nil {
		t.Fatalf("ClaimEvents() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events=%+v", events)
	}
	event := events[0]
	if event.CalendarRecipientID != "tech" || event.CalendarChangeClass != notifications.CalendarSchedule || event.CalendarUrgency != notifications.CalendarRoutine || event.CalendarActionPath != "/calendar" {
		t.Fatalf("calendar fields=%+v", event)
	}
	if len(event.CalendarSources) != 1 || event.CalendarSources[0] != (notifications.CalendarSourceReference{Type: "task", ID: "task", ClientID: "client", EventRole: "scheduled_work", SourceRevision: 7}) {
		t.Fatalf("calendar sources=%+v", event.CalendarSources)
	}
	if !strings.Contains(db.query, "event.event_type IN ('mention.occurred','calendar.schedule_changed')") {
		t.Fatalf("calendar event still requires a work-record join: %s", db.query)
	}
}

func TestCanonicalCalendarClaimExpandsLimitedSeedToCompleteCorrelationGroup(t *testing.T) {
	db := &fakeSalesDB{queryRows: &fakeRows{}}
	_, err := NewNotificationRepository(db).ClaimEvents(context.Background(), 1, time.Now())
	if err != nil {
		t.Fatalf("ClaimEvents() error = %v", err)
	}
	for _, required := range []string{
		"WITH seed AS",
		"LIMIT $2",
		"seed.event_type='calendar.schedule_changed'",
		"seed.msp_id=event.msp_id",
		"seed.correlation_id=event.correlation_id",
	} {
		if !strings.Contains(db.query, required) {
			t.Fatalf("ClaimEvents() does not expand the limited seed by calendar correlation (%q missing): %s", required, db.query)
		}
	}
}

func TestCanonicalCalendarClaimRejectsLegacyTaxonomyAsRetryable(t *testing.T) {
	data := `{"recipient_id":"tech","change_class":"assigned","urgency":"routine","source_refs":[{"type":"task","id":"task","event_role":"scheduled_work","source_revision":7}],"action_path":"/calendar"}`
	db := &fakeSalesDB{queryRows: &fakeRows{scans: []func(...any){calendarPlanningRow(data)}}}

	events, err := NewNotificationRepository(db).ClaimEvents(context.Background(), 10, time.Now())
	if err == nil || !errors.Is(err, notifications.ErrInvalidCalendarPlanningEvent) {
		t.Fatalf("ClaimEvents() events=%+v error=%v, want invalid canonical calendar event", events, err)
	}
}

func calendarPlanningRow(data string) func(...any) {
	return func(values ...any) {
		*values[0].(*string) = "event"
		*values[1].(*string) = notifications.CalendarNotificationEventType
		*values[2].(*string) = "msp"
		*values[3].(*string) = "client"
		*values[5].(*time.Time) = time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)
		*values[14].(*string) = "calendar_projection"
		*values[15].(*string) = "projection"
		*values[18].(*string) = "correlation"
		*values[19].(*[]byte) = []byte(data)
	}
}

func TestMentionReauthorizationRequiresCurrentMentionPolicyAndOperationalChannel(t *testing.T) {
	db := &fakeSalesDB{}
	repository := NewNotificationRepository(db).WithExternalDelivery("https://rarity.example", true)
	_, _ = repository.ReauthorizeMentionDelivery(context.Background(), notifications.DeliveryJob{ID: "delivery", EventID: "event"})
	for _, required := range []string{"policy.event_type='mention.occurred'", "recipient.email", "mention_access_invalidations", "access_loss_confirmed", "snapshot_occurrence_id=item.latest_occurrence_id"} {
		if !strings.Contains(db.query, required) {
			t.Fatalf("reauthorization query missing %q: %s", required, db.query)
		}
	}
}

func TestCalendarReauthorizationUsesCapturedPolicyVersionAndDisablementOnlyPreference(t *testing.T) {
	db := &fakeSalesDB{}
	_, _ = NewNotificationRepository(db).ReauthorizeCalendarDelivery(context.Background(), notifications.DeliveryJob{
		ID: "delivery", EventID: "event", MSPID: "msp", CalendarRecipientID: "recipient", Channel: notifications.Email,
		CalendarPayload: &notifications.CalendarDeliveryPayload{
			CorrelationID: "correlation", ChangeClass: notifications.CalendarSchedule, Urgency: notifications.CalendarRoutine,
			Sources: []notifications.CalendarSourceReference{{Type: "task", ID: "task", EventRole: "scheduled", SourceRevision: 1}}, ActionPath: "/calendar",
		},
	})
	if len(db.queries) == 0 {
		t.Fatal("calendar authorization did not query its persisted delivery")
	}
	query := db.queries[0]
	for _, required := range []string{"notification_policy_versions", "policy.version=delivery.policy_version", "COALESCE((SELECT rule.enabled", ",true)"} {
		if !strings.Contains(strings.ReplaceAll(query, " ", ""), strings.ReplaceAll(required, " ", "")) {
			t.Fatalf("calendar authorization query missing %q: %s", required, query)
		}
	}
	if strings.Contains(query, "JOIN notification_policies policy") {
		t.Fatalf("calendar authorization used mutable policy state: %s", query)
	}
}

func TestCalendarReauthorizationMirrorsCanonicalRecipientSelection(t *testing.T) {
	db := &fakeSalesDB{queryQueue: []row{
		fakeRow{scan: func(values ...any) {
			*(values[0].(*bool)) = true
			*(values[1].(*bool)) = true
			*(values[2].(*bool)) = true
			*(values[3].(*string)) = "recipient@example.test"
		}},
		fakeRow{err: errors.New("stop after recipient-selection query")},
	}}
	_, _ = NewNotificationRepository(db).WithExternalDelivery("https://rarity.example", true).ReauthorizeCalendarDelivery(context.Background(), notifications.DeliveryJob{
		ID: "delivery", EventID: "event", MSPID: "msp", CalendarRecipientID: "recipient", Channel: notifications.InApp,
		CalendarPayload: &notifications.CalendarDeliveryPayload{
			CorrelationID: "correlation", ChangeClass: notifications.CalendarSchedule, Urgency: notifications.CalendarRoutine,
			Sources: []notifications.CalendarSourceReference{{Type: "task", ID: "task", EventRole: "scheduled", SourceRevision: 1}}, ActionPath: "/calendar",
		},
	})
	if len(db.queries) < 2 {
		t.Fatalf("queries=%v", db.queries)
	}
	query := strings.Join(strings.Fields(db.queries[1]), "")
	if !strings.Contains(query, "CASEWHENprojection.source_type='pto'ANDprojection.owner_idISNOTNULLTHENprojection.owner_id=$7::uuidELSEprojection.assignee_id=$7::uuidEND") || strings.Contains(query, "projection.assignee_id=$7::uuidORprojection.owner_id=$7::uuid") {
		t.Fatalf("calendar recipient selection diverges from canonical routing: %s", db.queries[1])
	}
}

func TestNotificationRepositoryClaimsPendingWithAtomicVisibilityLease(t *testing.T) {
	db := &fakeSalesDB{queryRows: &fakeRows{}}
	jobs, err := NewNotificationRepository(db).ClaimPending(
		context.Background(), 50, time.Date(2026, time.July, 29, 17, 0, 0, 0, time.UTC),
	)
	if err != nil || len(jobs) != 0 {
		t.Fatalf("ClaimPending() jobs=%+v error=%v", jobs, err)
	}
	if !strings.Contains(db.query, "FOR UPDATE SKIP LOCKED") ||
		!strings.Contains(db.query, "UPDATE notification_deliveries") ||
		!strings.Contains(db.query, "attempts = delivery.attempts + 1") ||
		!strings.Contains(db.query, "next_attempt_at = $1 + interval '5 minutes'") {
		t.Fatalf("claim is not atomically leased: %s", db.query)
	}
}

func TestNotificationPlannerResolvesChildEventsToTheirWorkRecord(t *testing.T) {
	db := &fakeSalesDB{queryRows: &fakeRows{}}
	_, err := NewNotificationRepository(db).ClaimEvents(
		context.Background(), 50, time.Date(2026, time.July, 29, 17, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("ClaimEvents() error = %v", err)
	}
	for _, required := range []string{
		"event.subject_type = 'comment'",
		"FROM comments comment",
		"event.subject_type = 'task'",
		"FROM tasks task",
		"event.subject_type = 'work_record_sla'",
	} {
		if !strings.Contains(db.query, required) {
			t.Fatalf("event-to-work resolution missing %q: %s", required, db.query)
		}
	}
}

func TestNotificationRepositoryPersistsDeliveryStateWithClaimVersion(t *testing.T) {
	at := time.Date(2026, time.July, 29, 17, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name string
		run  func(*NotificationRepository) error
		want string
	}{
		{
			name: "delivered",
			run: func(repository *NotificationRepository) error {
				return repository.MarkDelivered(context.Background(), "delivery", 2, at)
			},
			want: "state = 'delivered'",
		},
		{
			name: "retry",
			run: func(repository *NotificationRepository) error {
				return repository.MarkRetry(context.Background(), notifications.DeliveryFailure{
					ID: "delivery", ExpectedAttempts: 2, ErrorCode: "delivery_failed",
					NextAttemptAt: at.Add(time.Minute),
				})
			},
			want: "state = 'pending'",
		},
		{
			name: "failed",
			run: func(repository *NotificationRepository) error {
				return repository.MarkFailed(context.Background(), notifications.DeliveryFailure{
					ID: "delivery", ExpectedAttempts: 2,
					ErrorCode: "retry_window_expired", FailedAt: at,
				})
			},
			want: "state = 'failed'",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := &fakeSalesTx{}
			if err := test.run(NewNotificationRepository(&fakeSalesDB{tx: tx})); err != nil {
				t.Fatalf("state update error = %v", err)
			}
			if len(tx.queries) != 1 || !strings.Contains(tx.queries[0], test.want) ||
				!strings.Contains(tx.queries[0], "attempts = $2 + 1") {
				t.Fatalf("state update lacks claim guard: %+v", tx.queries)
			}
		})
	}
}

func TestNotificationRepositoryRecordsTeamsAttemptAndConnectionHealth(t *testing.T) {
	tx := &fakeSalesTx{}
	at := time.Date(2026, time.July, 29, 17, 0, 0, 0, time.UTC)
	err := NewNotificationRepository(&fakeSalesDB{tx: tx}).Record(
		context.Background(),
		notifications.TeamsAttempt{
			ConnectionID: "connection", ConnectionVersion: 4,
			EventID: "event", WorkRecordID: "work",
			Attempt: 3, FirstAttemptAt: at.Add(-time.Hour), AttemptedAt: at,
			State: notifications.TeamsRetrying, ErrorCode: "delivery_failed",
		},
	)
	if err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	assertQueryOrder(
		t, tx.queries,
		"INSERT INTO teams_delivery_attempts",
		"UPDATE teams_connections",
	)
	if !strings.Contains(tx.queries[0], "connection_version") ||
		tx.args[0][2] != int64(4) {
		t.Fatalf("Teams attempt does not preserve claimed connection version: query=%s args=%v", tx.queries[0], tx.args[0])
	}
	if !strings.Contains(tx.queries[1], "WHERE id = $1 AND version = $5 AND enabled") ||
		tx.args[1][4] != int64(4) {
		t.Fatalf("Teams health update is not fenced by claimed enabled connection version: query=%s args=%v", tx.queries[1], tx.args[1])
	}
}

func TestNotificationRepositoryClaimsDisabledTeamsSnapshotForTerminalHandling(t *testing.T) {
	db := &fakeSalesDB{}
	_, _ = NewNotificationRepository(db).ClaimPending(context.Background(), 10, time.Now())
	if !strings.Contains(db.query, "COALESCE(NOT connection.enabled, false)") ||
		strings.Contains(db.query, "AND connection.enabled") {
		t.Fatalf("claim query drops disabled Teams connection snapshot: %s", db.query)
	}
}

func TestCalendarDeliveryClaimLoadsOnlyTypedPayloadAndPersistedDeduplicationKey(t *testing.T) {
	db := &fakeSalesDB{}
	_, _ = NewNotificationRepository(db).ClaimPending(context.Background(), 10, time.Now())
	for _, required := range []string{
		"LEFT JOIN notification_calendar_delivery_payloads",
		"payload.recipient_technician_id",
		"payload.source_refs",
		"payload.calendar_delivery_key",
		"leased.content_classification",
	} {
		if !strings.Contains(db.query, required) {
			t.Fatalf("calendar claim does not load stable typed field %q: %s", required, db.query)
		}
	}
	for _, forbidden := range []string{"projection.title", "client.name", "recipient.email"} {
		if strings.Contains(db.query, forbidden) {
			t.Fatalf("calendar claim prematurely loads mutable rendered data %q: %s", forbidden, db.query)
		}
	}
}

func TestCalendarDeliveryCompletesInboxAndAttemptGuardAtomically(t *testing.T) {
	tx := &fakeSalesTx{queryRow: fakeRow{scan: func(destinations ...any) { *(destinations[0].(*bool)) = true }}}
	job := notifications.DeliveryJob{
		ID: "delivery", EventID: "event", MSPID: "msp", CalendarRecipientID: "recipient",
		Attempts: 2, DeduplicationKey: "dedupe", ContentClassification: "internal",
	}
	rendered := notifications.CalendarRenderedDelivery{Authorized: true, Title: "Calendar changed", Body: "calendar item", ActionPath: "/calendar"}
	err := NewNotificationRepository(&fakeSalesDB{tx: tx}).CompleteCalendarInApp(context.Background(), job, rendered, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	assertQueryOrder(t, tx.queries, "INSERT INTO recipient_notifications", "UPDATE notification_deliveries")
	if !strings.Contains(tx.queries[0], "ON CONFLICT (msp_id,recipient_technician_id,deduplication_key) DO NOTHING") ||
		!strings.Contains(tx.queries[1], "state='pending' AND attempts=$2+1") || !tx.committed {
		t.Fatalf("inbox completion is not convergent and attempt guarded: queries=%v committed=%t", tx.queries, tx.committed)
	}
}

func TestCalendarDeliveryInboxCompletionRollsBackWhenAttemptGuardIsLost(t *testing.T) {
	tx := &fakeSalesTx{zeroRowsAt: 2, queryRow: fakeRow{scan: func(destinations ...any) { *(destinations[0].(*bool)) = true }}}
	err := NewNotificationRepository(&fakeSalesDB{tx: tx}).CompleteCalendarInApp(context.Background(), notifications.DeliveryJob{
		ID: "delivery", EventID: "event", MSPID: "msp", CalendarRecipientID: "recipient", Attempts: 2,
		DeduplicationKey: "dedupe", ContentClassification: "internal",
	}, notifications.CalendarRenderedDelivery{Authorized: true, Title: "Calendar changed", Body: "calendar item", ActionPath: "/calendar"}, time.Now())
	if !errors.Is(err, object.ErrVersionConflict) || !tx.rolledBack || tx.committed {
		t.Fatalf("err=%v rolled_back=%t committed=%t", err, tx.rolledBack, tx.committed)
	}
}

func TestRecipientInboxRepositoryRejectsMalformedCursorBeforeQuery(t *testing.T) {
	db := &fakeSalesDB{}
	_, err := NewNotificationRepository(db).ListRecipientNotifications(context.Background(), "msp", "recipient", "%%%", 50)
	if !errors.Is(err, notifications.ErrInvalidInboxRequest) || db.query != "" {
		t.Fatalf("err=%v query=%q", err, db.query)
	}
}

func TestRecipientInboxRepositoryScopesListAndUnreadToOwner(t *testing.T) {
	db := &fakeSalesDB{queryRows: &fakeRows{}, queryRow: fakeRow{scan: func(destinations ...any) { *(destinations[0].(*int)) = 2 }}}
	repository := NewNotificationRepository(db)
	page, err := repository.ListRecipientNotifications(context.Background(), "msp", "recipient", "", 50)
	if err != nil || len(page.Notifications) != 0 || !strings.Contains(db.query, "recipient.msp_id=$1::uuid") || !strings.Contains(db.query, "recipient.recipient_technician_id=$2::uuid") {
		t.Fatalf("page=%+v query=%s err=%v", page, db.query, err)
	}
	count, err := repository.CountUnreadRecipientNotifications(context.Background(), "msp", "recipient")
	if err != nil || count != 2 || !strings.Contains(db.query, "read_at IS NULL") {
		t.Fatalf("count=%d query=%s err=%v", count, db.query, err)
	}
}

func TestRecipientInboxRepositoryMarkReadUsesOwnerAndVersion(t *testing.T) {
	readAt := time.Date(2026, time.August, 15, 13, 0, 0, 0, time.UTC)
	tx := &fakeSalesTx{queryRow: fakeRow{scan: func(destinations ...any) {
		*(destinations[0].(*string)) = "notification"
		*(destinations[1].(*string)) = "recipient"
		*(destinations[2].(*string)) = "event"
		*(destinations[3].(*string)) = "dedupe"
		*(destinations[4].(*string)) = "Calendar changed"
		*(destinations[5].(*string)) = "calendar item"
		*(destinations[6].(*string)) = "/calendar"
		*(destinations[7].(*string)) = "internal"
		*(destinations[8].(*time.Time)) = readAt.Add(-time.Hour)
		*(destinations[9].(**time.Time)) = &readAt
		*(destinations[10].(*int64)) = 2
	}}}
	value, err := NewNotificationRepository(&fakeSalesDB{tx: tx}).MarkRecipientNotificationRead(context.Background(), "msp", "recipient", "notification", 1, readAt)
	if err != nil || value.Version != 2 || !tx.committed || !strings.Contains(tx.query, "recipient_technician_id=$3::uuid") || !strings.Contains(tx.query, "version=$4") {
		t.Fatalf("value=%+v query=%s committed=%t err=%v", value, tx.query, tx.committed, err)
	}
}
