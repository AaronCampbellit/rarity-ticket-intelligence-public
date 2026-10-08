package psa_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/id"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/store"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/store/psa"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
)

func TestTaggingProjectionReplayCanonicalContinuityInheritanceAndClientIsolation(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for projection verification")
	}
	ctx := context.Background()
	if err := store.Migrate(ctx, databaseURL); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	msp, clientA, clientB, actor := id.New(), id.New(), id.New(), id.New()
	group, survivor, merged, other, unclassified := id.New(), id.New(), id.New(), id.New(), id.New()
	chainA, chainB, chainC, orderOld, orderNew := id.New(), id.New(), id.New(), id.New(), id.New()
	workA, workB, workC, workD, workE, workF := id.New(), id.New(), id.New(), id.New(), id.New(), id.New()
	technician, team, queue := id.New(), id.New(), id.New()
	pipeline, stage, opportunity, proposal, proposalVersion, project, phase, task, phaseTask := id.New(), id.New(), id.New(), id.New(), id.New(), id.New(), id.New(), id.New(), id.New()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatalf("fixture: %v\n%s", err, query)
		}
	}
	exec(`INSERT INTO msp_organizations (id,display_id,name,created_by,updated_by) VALUES ($1,$2,'projection',$3,$3)`, msp, "PROJECTION-"+msp, actor)
	for _, client := range []string{clientA, clientB} {
		exec(`INSERT INTO client_organizations (id,msp_id,display_id,name,created_by,updated_by) VALUES ($1,$2,$3,'client',$4,$4)`, client, msp, "CLIENT-"+client, actor)
	}
	exec(`INSERT INTO technicians (id,msp_id,email,display_name) VALUES ($1,$2,$3,'Projection technician')`, technician, msp, technician+"@example.test")
	exec(`INSERT INTO teams (id,msp_id,key,name) VALUES ($1,$2,$3,'Projection team')`, team, msp, "projection-"+team)
	exec(`INSERT INTO queues (id,msp_id,client_id,team_id,key,name) VALUES ($1,$2,$3,$4,$5,'Projection queue')`, queue, msp, clientA, team, "projection-"+queue)
	exec(`INSERT INTO tag_groups (id,msp_id,internal_key,label,position,created_by,updated_by) VALUES ($1,$2,$3,'Projection',1,$4,$4)`, group, msp, "taxonomy.projection."+group, actor)
	exec(`INSERT INTO tags (id,msp_id,group_id,internal_key,label,created_by,updated_by) VALUES ($1,$2,$3,$4,'Survivor',$5,$5)`, survivor, msp, group, "taxonomy.projection."+survivor, actor)
	exec(`INSERT INTO tags (id,msp_id,group_id,internal_key,label,lifecycle_state,merged_into_id,created_by,updated_by) VALUES ($1,$2,$3,$4,'Former','merged',$5,$6,$6)`, merged, msp, group, "taxonomy.projection."+merged, survivor, actor)
	exec(`INSERT INTO tags (id,msp_id,group_id,internal_key,label,created_by,updated_by) VALUES ($1,$2,$3,$4,'Other',$5,$5)`, other, msp, group, "taxonomy.projection."+other, actor)
	exec(`INSERT INTO tags (id,msp_id,group_id,internal_key,label,system_tag,created_by,updated_by) VALUES ($1,$2,$3,'taxonomy.system.unclassified','Unclassified',true,$4,$4)`, unclassified, msp, group, actor)
	for index, tagID := range []string{chainA, chainB, chainC} {
		exec(`INSERT INTO tags (id,msp_id,group_id,internal_key,label,created_by,updated_by) VALUES ($1,$2,$3,$4,$5,$6,$6)`, tagID, msp, group, "taxonomy.projection."+tagID, fmt.Sprintf("Chain %d", index), actor)
	}
	exec(`INSERT INTO tags (id,msp_id,group_id,internal_key,label,created_by,updated_by) VALUES ($1,$2,$3,$4,'Order old',$5,$5),($6,$2,$3,$7,'Order new',$5,$5)`, orderOld, msp, group, "taxonomy.projection."+orderOld, actor, orderNew, "taxonomy.projection."+orderNew)
	for _, fixture := range []struct{ id, client string }{{workA, clientA}, {workB, clientB}, {workC, clientA}, {workD, clientA}, {workE, clientA}, {workF, clientA}} {
		exec(`INSERT INTO work_records (id,msp_id,client_id,display_id,record_type,title,status,priority,created_by,updated_by) VALUES ($1,$2,$3,$4,'incident','projection','new','normal',$5,$5)`, fixture.id, msp, fixture.client, "W-"+fixture.id, actor)
	}
	exec(`UPDATE work_records SET status='waiting_customer' WHERE id=$1`, workE)
	exec(`UPDATE work_records SET primary_owner_id=$1,queue_id=$2,priority='high' WHERE id=$3`, technician, queue, workD)
	exec(`INSERT INTO pipelines (id,msp_id,key,name,created_by,updated_by) VALUES ($1,$2,$3,'projection',$4,$4)`, pipeline, msp, "projection-"+pipeline, actor)
	exec(`INSERT INTO pipeline_stages (id,pipeline_id,msp_id,key,name,position,probability,forecast_category) VALUES ($1,$2,$3,'won','won',1,100,'weighted')`, stage, pipeline, msp)
	exec(`INSERT INTO opportunities (id,msp_id,client_id,pipeline_id,stage_id,display_id,name,currency,created_by,updated_by) VALUES ($1,$2,$3,$4,$5,$6,'projection','USD',$7,$7)`, opportunity, msp, clientA, pipeline, stage, "O-"+opportunity, actor)
	exec(`INSERT INTO proposals (id,msp_id,client_id,opportunity_id,display_id,current_version,state,created_by,updated_by) VALUES ($1,$2,$3,$4,$5,1,'draft',$6,$6)`, proposal, msp, clientA, opportunity, "P-"+proposal, actor)
	exec(`INSERT INTO proposal_versions (id,proposal_id,msp_id,version,currency,subtotal_minor,tax_minor,total_minor,cost_minor,margin_minor,issued_by,pdf_snapshot_id) VALUES ($1,$2,$3,1,'USD',0,0,0,0,0,$4,$5)`, proposalVersion, proposal, msp, actor, id.New())
	exec(`INSERT INTO projects (id,msp_id,client_id,display_id,name,original_proposal_version_id,created_by,updated_by) VALUES ($1,$2,$3,$4,'projection',$5,$6,$6)`, project, msp, clientA, "PRJ-"+project, proposalVersion, actor)
	exec(`INSERT INTO phases (id,project_id,msp_id,client_id,name,position) VALUES ($1,$2,$3,$4,'phase',1)`, phase, project, msp, clientA)
	exec(`INSERT INTO tasks (id,msp_id,client_id,parent_type,parent_id,title,status,position,created_by,updated_by) VALUES ($1,$2,$3,'project',$4,'projection task','new',1,$5,$5)`, task, msp, clientA, project, actor)
	exec(`INSERT INTO tasks (id,msp_id,client_id,parent_type,parent_id,title,status,position,created_by,updated_by) VALUES ($1,$2,$3,'phase',$4,'phase task','new',1,$5,$5)`, phaseTask, msp, clientA, phase, actor)
	// Other integration tests may share this database. Advance their independent
	// MSP cursors without touching their facts so this test's bounded batch is deterministic.
	exec(`INSERT INTO tag_projection_cursors (msp_id,projection_name,last_occurred_at,last_event_id)
SELECT DISTINCT ON (msp_id) msp_id,'tagging-daily-v1',occurred_at,id
FROM tag_assignment_events ORDER BY msp_id,occurred_at DESC,id DESC
ON CONFLICT (msp_id,projection_name) DO UPDATE SET last_occurred_at=EXCLUDED.last_occurred_at,last_event_id=EXCLUDED.last_event_id,updated_at=now()`)

	base := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	insertEvent := func(client, objectType, objectID, tagID, operation string, at time.Time) {
		eventID := id.New()
		exec(`INSERT INTO tag_assignment_events (id,msp_id,client_id,assignment_id,object_type,object_id,target_version,tag_id,operation,assignment_source,actor_type,actor_id,occurred_at,idempotency_key,correlation_id) VALUES ($1,$2,$3,$4,$5,$6,1,$7,$8,'human','technician',$9,$10,$11,$12)`, eventID, msp, client, id.New(), objectType, objectID, tagID, operation, actor, at, "projection-"+eventID, id.New())
	}
	insertEvent(clientA, "work_record", workA, merged, "added", base)
	insertEvent(clientA, "work_record", workA, other, "added", base.Add(time.Second))
	insertEvent(clientB, "work_record", workB, merged, "added", base.Add(2*time.Second))
	insertEvent(clientA, "task", task, merged, "added", base.Add(2500*time.Millisecond))
	insertEvent(clientA, "project", project, merged, "added", base.Add(3*time.Second))
	insertEvent(clientA, "work_record", workC, chainA, "added", base.Add(4*time.Second))

	repository := psa.NewTaggingProjectionRepositoryFromPool(pool)
	first, err := repository.ProjectTagEvents(ctx, 500)
	if err != nil || first.Processed != 6 || first.InheritedEffects != 1 {
		t.Fatalf("first projection=%+v error=%v", first, err)
	}
	var usage, inherited, pairs int64
	if err := pool.QueryRow(ctx, `SELECT sum(added_count) FROM tag_usage_daily WHERE msp_id=$1 AND client_id=$2 AND tag_id=$3 AND object_type='work_record'`, msp, clientA, survivor).Scan(&usage); err != nil || usage != 1 {
		t.Fatalf("canonical usage=%d error=%v", usage, err)
	}
	if err := pool.QueryRow(ctx, `SELECT COALESCE(sum(added_count-removed_count),0) FROM tag_usage_daily WHERE msp_id=$1 AND client_id=$2 AND tag_id=$3 AND object_type='task' AND assignment_source LIKE 'inherited:%'`, msp, clientA, survivor).Scan(&inherited); err != nil || inherited != 1 {
		t.Fatalf("phase inheritance usage=%d error=%v", inherited, err)
	}
	var directTaskInherited int64
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM tag_projection_effects WHERE msp_id=$1 AND object_id=$2 AND inherited=true`, msp, task).Scan(&directTaskInherited)
	if directTaskInherited != 0 {
		t.Fatalf("direct task duplicated inherited effects=%d", directTaskInherited)
	}
	if err := pool.QueryRow(ctx, `SELECT sum(object_count) FROM tag_cooccurrence_daily WHERE msp_id=$1 AND client_id=$2 AND left_tag_id=LEAST($3::uuid,$4::uuid) AND right_tag_id=GREATEST($3::uuid,$4::uuid)`, msp, clientA, survivor, other).Scan(&pairs); err != nil || pairs != 1 {
		t.Fatalf("canonical pairs=%d error=%v", pairs, err)
	}
	// Removing both tags on the following UTC day closes both usage and the
	// pair interval instead of clamping a removal-only daily bucket to zero.
	removeAt := base.Add(24 * time.Hour)
	insertEvent(clientA, "work_record", workA, merged, "removed", removeAt)
	insertEvent(clientA, "work_record", workA, other, "removed", removeAt.Add(time.Second))
	second, err := repository.ProjectTagEvents(ctx, 500)
	if err != nil || second.Processed != 2 {
		t.Fatalf("cross-day removal=%+v error=%v", second, err)
	}
	var closedUsage, closedPairs int64
	_ = pool.QueryRow(ctx, `SELECT COALESCE(sum(active_count),0) FROM tag_usage_daily WHERE msp_id=$1 AND client_id=$2 AND tag_id=$3 AND object_type='work_record'`, msp, clientA, survivor).Scan(&closedUsage)
	_ = pool.QueryRow(ctx, `SELECT COALESCE(sum(object_count),0) FROM tag_cooccurrence_daily WHERE msp_id=$1 AND client_id=$2 AND left_tag_id=LEAST($3::uuid,$4::uuid) AND right_tag_id=GREATEST($3::uuid,$4::uuid)`, msp, clientA, survivor, other).Scan(&closedPairs)
	if closedUsage != 0 || closedPairs != 0 {
		t.Fatalf("cross-day state usage=%d pairs=%d, want closed", closedUsage, closedPairs)
	}
	replay, err := repository.ProjectTagEvents(ctx, 500)
	if err != nil || replay.Processed != 0 {
		t.Fatalf("replay=%+v error=%v", replay, err)
	}
	// A was already projected before two later lifecycle merges. Rebuilding
	// through the complete survivor chain must leave only C in aggregates.
	mergeAt := removeAt.Add(time.Minute)
	exec(`UPDATE tags SET lifecycle_state='merged',merged_into_id=$1,version=version+1 WHERE id=$2 AND msp_id=$3`, chainB, chainA, msp)
	insertEvent(clientA, "work_record", workC, chainA, "removed", mergeAt)
	insertEvent(clientA, "work_record", workC, chainB, "added", mergeAt)
	if result, err := repository.ProjectTagEvents(ctx, 500); err != nil || result.Processed != 2 {
		t.Fatalf("first merge projection=%+v error=%v", result, err)
	}
	exec(`UPDATE tags SET lifecycle_state='merged',merged_into_id=$1,version=version+1 WHERE id=$2 AND msp_id=$3`, chainC, chainB, msp)
	insertEvent(clientA, "work_record", workC, chainB, "removed", mergeAt.Add(time.Minute))
	insertEvent(clientA, "work_record", workC, chainC, "added", mergeAt.Add(time.Minute))
	if result, err := repository.ProjectTagEvents(ctx, 500); err != nil || result.Processed != 2 {
		t.Fatalf("second merge projection=%+v error=%v", result, err)
	}
	var chainActive, retiredRows int64
	_ = pool.QueryRow(ctx, `SELECT COALESCE(sum(active_count),0) FROM tag_usage_daily WHERE msp_id=$1 AND client_id=$2 AND tag_id=$3`, msp, clientA, chainC).Scan(&chainActive)
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM tag_usage_daily WHERE msp_id=$1 AND client_id=$2 AND tag_id IN($3,$4)`, msp, clientA, chainA, chainB).Scan(&retiredRows)
	if chainActive != 1 || retiredRows != 0 {
		t.Fatalf("multi-hop continuity active=%d retired_rows=%d", chainActive, retiredRows)
	}
	filterAt := mergeAt.Add(2 * time.Minute)
	insertEvent(clientA, "work_record", workD, survivor, "added", filterAt)
	insertEvent(clientA, "work_record", workD, other, "added", filterAt.Add(time.Second))
	insertEvent(clientA, "work_record", workC, other, "added", filterAt.Add(1500*time.Millisecond))
	insertEvent(clientA, "work_record", workE, survivor, "added", filterAt.Add(2*time.Second))
	insertEvent(clientA, "work_record", workF, unclassified, "added", mergeAt.Add(90*time.Second))
	// Event-time evidence must survive a lagging worker: mutate the object after
	// the ledger append but before projection.
	exec(`UPDATE work_records SET status='new',primary_owner_id=NULL,queue_id=NULL,priority='low' WHERE id=$1`, workE)
	if result, err := repository.ProjectTagEvents(ctx, 500); err != nil || result.Processed != 5 {
		t.Fatalf("filter fixtures=%+v error=%v", result, err)
	}
	filterFrom, filterTo := base.Add(-time.Hour), filterAt.Add(time.Hour)
	insertEvent(clientA, "work_record", workF, survivor, "added", filterTo.AddDate(0, 0, 1))
	if result, err := repository.ProjectTagEvents(ctx, 500); err != nil || result.Processed != 1 {
		t.Fatalf("exclusive-boundary fixture=%+v error=%v", result, err)
	}
	orderBase := filterTo.AddDate(0, 0, 1).Add(time.Minute)
	insertEvent(clientA, "task", task, orderOld, "added", orderBase)
	insertEvent(clientA, "project", project, orderOld, "added", orderBase.Add(time.Second))
	if result, err := repository.ProjectTagEvents(ctx, 500); err != nil || result.Processed != 2 {
		t.Fatalf("ordering setup=%+v error=%v", result, err)
	}
	exec(`UPDATE tags SET lifecycle_state='merged',merged_into_id=$1,version=version+1 WHERE id=$2 AND msp_id=$3`, orderNew, orderOld, msp)
	insertOrderedMerge := func(objectType, objectID string, at time.Time) {
		low, high := id.New(), id.New()
		if low > high {
			low, high = high, low
		}
		correlation := id.New()
		for _, event := range []struct{ id, tagID, operation string }{{high, orderOld, "removed"}, {low, orderNew, "added"}} {
			exec(`INSERT INTO tag_assignment_events (id,msp_id,client_id,assignment_id,object_type,object_id,target_version,tag_id,operation,assignment_source,actor_type,actor_id,occurred_at,idempotency_key,correlation_id) VALUES ($1,$2,$3,$4,$5,$6,2,$7,$8,'human','technician',$9,$10,$11,$12)`, event.id, msp, clientA, id.New(), objectType, objectID, event.tagID, event.operation, actor, at, "ordered-"+event.id, correlation)
		}
	}
	insertOrderedMerge("task", task, orderBase.Add(time.Minute))
	insertOrderedMerge("project", project, orderBase.Add(2*time.Minute))
	if result, err := repository.ProjectTagEvents(ctx, 500); err != nil || result.Processed != 4 {
		t.Fatalf("same-time add-first merge=%+v error=%v", result, err)
	}
	var orderedActive int64
	_ = pool.QueryRow(ctx, `WITH RECURSIVE chain(root,current,depth) AS (SELECT id,id,0 FROM tags WHERE msp_id=$1 UNION ALL SELECT chain.root,tags.merged_into_id,depth+1 FROM chain JOIN tags ON tags.id=chain.current WHERE tags.merged_into_id IS NOT NULL), survivor AS (SELECT DISTINCT ON(root) root,current FROM chain ORDER BY root,depth DESC), latest AS (SELECT DISTINCT ON(effect.object_id,survivor.current) effect.object_id,survivor.current,effect.operation FROM tag_projection_effects effect JOIN survivor ON survivor.root=effect.tag_id WHERE effect.msp_id=$1 AND effect.object_id IN($2,$3) ORDER BY effect.object_id,survivor.current,effect.occurred_at DESC,effect.effect_order DESC,effect.id DESC) SELECT count(*) FROM latest WHERE current=$4 AND operation='added'`, msp, task, phaseTask, orderNew).Scan(&orderedActive)
	if orderedActive != 2 {
		t.Fatalf("same-time canonical task states active=%d, want direct and Phase task", orderedActive)
	}
	openingUsage, err := repository.ClassificationReport(ctx, msp, tagging.ReportUsage, tagging.ReportFilter{ClientID: clientA, From: filterAt, To: filterTo, ObjectType: tagging.ObjectWorkRecord, TagIDs: []string{chainC}, Match: "any", Limit: 50})
	if err != nil || len(openingUsage.Rows) == 0 {
		t.Fatalf("opening usage=%+v error=%v", openingUsage, err)
	}
	openingPairs, err := repository.ClassificationReport(ctx, msp, tagging.ReportCombinations, tagging.ReportFilter{ClientID: clientA, From: filterAt, To: filterTo, ObjectType: tagging.ObjectWorkRecord, TagIDs: []string{chainC}, Match: "any", Limit: 50})
	if err != nil || len(openingPairs.Rows) != 1 || openingPairs.Rows[0].Count != 1 {
		t.Fatalf("opening combination=%+v error=%v", openingPairs, err)
	}
	health, err := repository.ClassificationReport(ctx, msp, tagging.ReportHealth, tagging.ReportFilter{ClientID: clientA, From: filterAt, To: filterTo, ObjectType: tagging.ObjectWorkRecord, Limit: 50})
	if err != nil {
		t.Fatalf("health age report error=%v", err)
	}
	var unclassifiedAge int64
	for _, row := range health.Rows {
		if row.Source == "unclassified" {
			unclassifiedAge = row.AgeSeconds
		}
	}
	if unclassifiedAge <= 0 || len(health.Rows) != 2 {
		t.Fatalf("health report must contain two mutually exclusive states with opening age: %+v", health)
	}
	all, err := repository.ClassificationReport(ctx, msp, tagging.ReportUsage, tagging.ReportFilter{ClientID: clientA, From: filterFrom, To: filterTo, ObjectType: tagging.ObjectWorkRecord, TagIDs: []string{survivor, other}, Match: "all", Limit: 50})
	if err != nil || len(all.Rows) != 2 || all.Rows[0].Count != 1 || all.Rows[1].Count != 1 {
		t.Fatalf("exact all report=%+v error=%v", all, err)
	}
	waiting, err := repository.ClassificationReport(ctx, msp, tagging.ReportUsage, tagging.ReportFilter{ClientID: clientA, From: filterFrom, To: filterTo, ObjectType: tagging.ObjectWorkRecord, TagIDs: []string{survivor}, Match: "any", Status: "waiting_customer", Limit: 50})
	if err != nil || len(waiting.Rows) != 1 || waiting.Rows[0].TagID != survivor || waiting.Rows[0].Count != 1 {
		t.Fatalf("exact status report=%+v error=%v", waiting, err)
	}
	historical, err := repository.ClassificationReport(ctx, msp, tagging.ReportUsage, tagging.ReportFilter{ClientID: clientA, From: filterFrom, To: filterTo, ObjectType: tagging.ObjectWorkRecord, TagIDs: []string{survivor}, Match: "any", Status: "waiting_customer", Limit: 50})
	if err != nil || len(historical.Rows) != 1 || historical.Rows[0].Count != 1 {
		t.Fatalf("event-time dimensions report=%+v error=%v", historical, err)
	}
	for label, extra := range map[string]tagging.ReportFilter{
		"technician": {TechnicianID: technician}, "team": {TeamID: team}, "priority": {Priority: "high"},
		"group-source-direct": {GroupID: group, Source: tagging.SourceHuman, Inheritance: "direct"},
	} {
		extra.ClientID, extra.From, extra.To, extra.ObjectType, extra.TagIDs, extra.Match, extra.Limit = clientA, filterFrom, filterTo, tagging.ObjectWorkRecord, []string{survivor, other}, "all", 50
		filtered, filterErr := repository.ClassificationReport(ctx, msp, tagging.ReportUsage, extra)
		if filterErr != nil || len(filtered.Rows) != 2 || filtered.Rows[0].Count != 1 || filtered.Rows[1].Count != 1 {
			t.Fatalf("exact %s report=%+v error=%v", label, filtered, filterErr)
		}
	}
	none, err := repository.ClassificationReport(ctx, msp, tagging.ReportUsage, tagging.ReportFilter{ClientID: clientA, From: filterFrom, To: filterTo, ObjectType: tagging.ObjectWorkRecord, TagIDs: []string{other}, Match: "none", Limit: 50})
	if err != nil {
		t.Fatalf("none report error=%v", err)
	}
	var noneSurvivor int64
	for _, row := range none.Rows {
		if row.TagID == survivor {
			noneSurvivor = row.Count
		}
	}
	if noneSurvivor != 1 {
		t.Fatalf("exact none report=%+v", none)
	}
	wrongSource, err := repository.ClassificationReport(ctx, msp, tagging.ReportUsage, tagging.ReportFilter{ClientID: clientA, From: filterFrom, To: filterTo, ObjectType: tagging.ObjectWorkRecord, TagIDs: []string{survivor}, Match: "any", Source: tagging.SourceAutomation, Limit: 50})
	if err != nil || len(wrongSource.Rows) != 0 {
		t.Fatalf("source exclusion report=%+v error=%v", wrongSource, err)
	}
	evidence, err := repository.ClassificationEvidence(ctx, msp, survivor, tagging.ReportFilter{ClientID: clientA, From: filterFrom, To: filterTo, ObjectType: tagging.ObjectWorkRecord, Limit: 1})
	if err != nil || len(evidence.Items) != 1 || evidence.NextCursor == "" {
		t.Fatalf("evidence first page=%+v error=%v", evidence, err)
	}
	nextEvidence, err := repository.ClassificationEvidence(ctx, msp, survivor, tagging.ReportFilter{ClientID: clientA, From: filterFrom, To: filterTo, ObjectType: tagging.ObjectWorkRecord, Limit: 1, Cursor: evidence.NextCursor})
	if err != nil || len(nextEvidence.Items) != 1 || nextEvidence.Items[0].ObjectID == evidence.Items[0].ObjectID || !nextEvidence.ProjectionAsOf.Equal(evidence.ProjectionAsOf) {
		t.Fatalf("evidence stable page=%+v first=%+v error=%v", nextEvidence, evidence, err)
	}
	_, err = repository.ClassificationEvidence(ctx, msp, survivor, tagging.ReportFilter{ClientID: clientA, From: filterFrom, To: filterTo, ObjectType: tagging.ObjectWorkRecord, Status: "new", Limit: 1, Cursor: evidence.NextCursor})
	if !errors.Is(err, tagging.ErrInvalidReportFilter) {
		t.Fatalf("edited-filter cursor error=%v", err)
	}
	from, to := base.Add(-time.Hour), base.Add(time.Hour)
	reportA, err := repository.ClassificationReport(ctx, msp, tagging.ReportUsage, tagging.ReportFilter{ClientID: clientA, From: from, To: to, Limit: 50})
	if err != nil || len(reportA.Rows) == 0 {
		t.Fatalf("client A report=%+v error=%v", reportA, err)
	}
	reportB, err := repository.ClassificationReport(ctx, msp, tagging.ReportUsage, tagging.ReportFilter{ClientID: clientB, From: from, To: to, Limit: 50})
	if err != nil || len(reportB.Rows) != 1 || reportB.Rows[0].Count != 1 {
		t.Fatalf("client B isolated report=%+v error=%v", reportB, err)
	}
}

func TestClassificationSnapshotsTraverseMoreThanTenThousandRowsAcrossMerge(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for projection verification")
	}
	ctx := context.Background()
	if err := store.Migrate(ctx, databaseURL); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	const objectCount = 10002
	msp, client, actor, group, survivor := id.New(), id.New(), id.New(), id.New(), id.New()
	at := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatalf("fixture: %v\n%s", err, query)
		}
	}
	exec(`INSERT INTO msp_organizations(id,display_id,name,created_by,updated_by) VALUES($1,$2,'large snapshot',$3,$3)`, msp, "SNAPSHOT-"+msp, actor)
	exec(`INSERT INTO client_organizations(id,msp_id,display_id,name,created_by,updated_by) VALUES($1,$2,$3,'large snapshot',$4,$4)`, client, msp, "CLIENT-"+client, actor)
	exec(`INSERT INTO tag_groups(id,msp_id,internal_key,label,position,created_by,updated_by) VALUES($1,$2,$3,'Large snapshot',1,$4,$4)`, group, msp, "taxonomy.snapshot."+group, actor)
	exec(`INSERT INTO tags(id,msp_id,group_id,internal_key,label,created_by,updated_by) VALUES($1,$2,$3,$4,'Shared evidence',$5,$5)`, survivor, msp, group, "taxonomy.snapshot."+survivor, actor)
	exec(`INSERT INTO tags(id,msp_id,group_id,internal_key,label,created_by,updated_by)
SELECT md5($1::text||':tag:'||value)::uuid,$1::uuid,$2::uuid,'taxonomy.snapshot.'||$1::text||'.'||value,'Aggregate '||value,$3::uuid,$3::uuid
FROM generate_series(1,$4) value`, msp, group, actor, objectCount)
	exec(`INSERT INTO work_records(id,msp_id,client_id,display_id,record_type,title,status,priority,created_by,updated_by)
SELECT md5($1::text||':work:'||value)::uuid,$1::uuid,$2::uuid,'L-'||value,'incident','Large evidence '||value,'new','normal',$3::uuid,$3::uuid
FROM generate_series(1,$4) value`, msp, client, actor, objectCount)
	exec(`INSERT INTO tag_assignment_events(id,msp_id,client_id,assignment_id,object_type,object_id,target_version,tag_id,operation,assignment_source,actor_type,actor_id,occurred_at,idempotency_key,correlation_id)
VALUES(md5($1::text||':event')::uuid,$1::uuid,$2::uuid,md5($1::text||':assignment')::uuid,'work_record',md5($1::text||':work:1')::uuid,1,$3::uuid,'added','human','technician',$4::uuid,$5::timestamptz,'large-snapshot-'||$1::text,md5($1::text||':correlation')::uuid)`, msp, client, survivor, actor, at)
	exec(`INSERT INTO tag_projection_effects(id,msp_id,client_id,source_event_id,occurred_at,object_type,object_id,tag_id,operation,effect_order,assignment_source,inherited,origin_object_type,origin_object_id,dimensions)
SELECT md5($1::text||':shared-effect:'||value)::uuid,$1::uuid,$2::uuid,md5($1::text||':event')::uuid,$3::timestamptz,'work_record',md5($1::text||':work:'||value)::uuid,$4::uuid,'added',1,'human',false,'work_record',md5($1::text||':work:'||value)::uuid,'{}'::jsonb
FROM generate_series(1,$5) value
UNION ALL
SELECT md5($1::text||':unique-effect:'||value)::uuid,$1::uuid,$2::uuid,md5($1::text||':event')::uuid,$3::timestamptz,'work_record',md5($1::text||':work:'||value)::uuid,md5($1::text||':tag:'||value)::uuid,'added',1,'human',false,'work_record',md5($1::text||':work:'||value)::uuid,'{}'::jsonb
FROM generate_series(1,$5) value`, msp, client, at, survivor, objectCount)
	exec(`INSERT INTO tag_projection_cursors(msp_id,projection_name,last_occurred_at,last_event_id) VALUES($1::uuid,'tagging-daily-v1',$2,md5($1::text||':event')::uuid)`, msp, at)

	filter := tagging.ReportFilter{ClientID: client, From: at.Add(-time.Hour), To: at.Add(time.Hour), ObjectType: tagging.ObjectWorkRecord, Limit: 257}
	report, err := psa.NewTaggingProjectionRepositoryFromPool(pool).ClassificationReport(ctx, msp, tagging.ReportUsage, filter)
	if err != nil || len(report.Rows) != filter.Limit || report.NextCursor == "" {
		t.Fatalf("first report page rows=%d cursor=%t error=%v", len(report.Rows), report.NextCursor != "", err)
	}
	evidence, err := psa.NewTaggingProjectionRepositoryFromPool(pool).ClassificationEvidence(ctx, msp, survivor, filter)
	if err != nil || len(evidence.Items) != filter.Limit || evidence.NextCursor == "" {
		t.Fatalf("first evidence page items=%d cursor=%t error=%v", len(evidence.Items), evidence.NextCursor != "", err)
	}
	var snapshotID, parentPayload string
	var storedRows, storedBytes, normalizedRows, normalizedBytes int64
	if err := pool.QueryRow(ctx, `
SELECT snapshot.id::text,COALESCE(snapshot.payload::text,''),snapshot.total_rows,snapshot.payload_bytes,count(row.ordinal),COALESCE(sum(row.payload_bytes),0)
FROM tag_report_snapshots snapshot
LEFT JOIN tag_report_snapshot_rows row ON row.snapshot_id=snapshot.id
WHERE snapshot.msp_id=$1::uuid AND snapshot.client_id=$2::uuid AND snapshot.report_kind='usage'
GROUP BY snapshot.id
`, msp, client).Scan(&snapshotID, &parentPayload, &storedRows, &storedBytes, &normalizedRows, &normalizedBytes); err != nil {
		t.Fatalf("inspect normalized report snapshot: %v", err)
	}
	if parentPayload != "" || storedRows != objectCount+1 || normalizedRows != storedRows || normalizedBytes != storedBytes || storedRows > 50_000 || storedBytes > 64<<20 {
		t.Fatalf("snapshot storage payload=%q rows=%d normalized=%d bytes=%d normalized_bytes=%d", parentPayload, storedRows, normalizedRows, storedBytes, normalizedBytes)
	}
	repeated, err := psa.NewTaggingProjectionRepositoryFromPool(pool).ClassificationReport(ctx, msp, tagging.ReportUsage, filter)
	if err != nil {
		t.Fatalf("repeat report request: %v", err)
	}
	if repeated.NextCursor != report.NextCursor {
		t.Fatalf("repeat cursor changed; snapshot was not reused")
	}
	renewalFilter := filter
	renewalFilter.Source = tagging.SourceHuman
	renewal, err := psa.NewTaggingProjectionRepositoryFromPool(pool).ClassificationReport(ctx, msp, tagging.ReportUsage, renewalFilter)
	if err != nil || renewal.NextCursor == "" {
		t.Fatalf("initial renewal snapshot: report=%+v error=%v", renewal, err)
	}
	var expiredTarget, expiredTargetHash string
	if err := pool.QueryRow(ctx, `SELECT id::text,query_hash FROM tag_report_snapshots WHERE msp_id=$1::uuid AND client_id=$2::uuid AND report_kind='usage' AND id<>$3::uuid ORDER BY expires_at DESC LIMIT 1`, msp, client, snapshotID).Scan(&expiredTarget, &expiredTargetHash); err != nil {
		t.Fatalf("load renewal snapshot: %v", err)
	}
	exec(`UPDATE tag_report_snapshots SET expires_at=now()-interval '1 hour' WHERE id=$1::uuid`, expiredTarget)
	exec(`INSERT INTO tag_report_snapshots(id,msp_id,client_id,report_kind,query_hash,payload,projection_as_of,expires_at,total_rows,payload_bytes,materialization_state)
SELECT md5($1::text||':expired:'||value)::uuid,$1::uuid,$2::uuid,'expired:'||value,'expired:'||value,NULL,$3,now()-interval '2 hours',1,2,'ready'
FROM generate_series(1,101) value`, msp, client, at)
	exec(`INSERT INTO tag_report_snapshot_rows(snapshot_id,ordinal,payload,payload_bytes)
SELECT md5($1::text||':expired:'||value)::uuid,0,'{}'::jsonb,2 FROM generate_series(1,101) value`, msp)
	renewed, err := psa.NewTaggingProjectionRepositoryFromPool(pool).ClassificationReport(ctx, msp, tagging.ReportUsage, renewalFilter)
	if err != nil || renewed.NextCursor == "" || renewed.NextCursor == renewal.NextCursor {
		t.Fatalf("renew expired target beyond cleanup batch: report=%+v error=%v", renewed, err)
	}
	var expiredCount, expiredTargetChildren, readyForKey int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM tag_report_snapshots WHERE msp_id=$1::uuid AND expires_at<=now()`, msp).Scan(&expiredCount); err != nil || expiredCount != 0 {
		t.Fatalf("expired snapshot backlog count=%d error=%v", expiredCount, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM tag_report_snapshot_rows WHERE snapshot_id=$1::uuid`, expiredTarget).Scan(&expiredTargetChildren); err != nil || expiredTargetChildren != 0 {
		t.Fatalf("expired target child rows=%d error=%v", expiredTargetChildren, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM tag_report_snapshots WHERE msp_id=$1::uuid AND client_id=$2::uuid AND report_kind='usage' AND query_hash=$3 AND materialization_state='ready' AND expires_at>now()`, msp, client, expiredTargetHash).Scan(&readyForKey); err != nil || readyForKey != 1 {
		t.Fatalf("renewed ready snapshots=%d error=%v", readyForKey, err)
	}
	var concurrentTarget string
	if err := pool.QueryRow(ctx, `UPDATE tag_report_snapshots SET expires_at=now()-interval '1 minute' WHERE msp_id=$1::uuid AND client_id=$2::uuid AND report_kind='usage' AND query_hash=$3 RETURNING id::text`, msp, client, expiredTargetHash).Scan(&concurrentTarget); err != nil {
		t.Fatalf("expire concurrent target: %v", err)
	}
	type concurrentResult struct {
		cursor string
		err    error
	}
	start := make(chan struct{})
	results := make(chan concurrentResult, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			value, queryErr := psa.NewTaggingProjectionRepositoryFromPool(pool).ClassificationReport(ctx, msp, tagging.ReportUsage, renewalFilter)
			results <- concurrentResult{cursor: value.NextCursor, err: queryErr}
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	concurrentCursors := []string{}
	for value := range results {
		if value.err != nil || value.cursor == "" {
			t.Fatalf("concurrent renewal cursor=%q error=%v", value.cursor, value.err)
		}
		concurrentCursors = append(concurrentCursors, value.cursor)
	}
	if len(concurrentCursors) != 2 || concurrentCursors[0] != concurrentCursors[1] {
		t.Fatalf("concurrent requests did not reuse one snapshot: %v", concurrentCursors)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM tag_report_snapshots WHERE msp_id=$1::uuid AND client_id=$2::uuid AND report_kind='usage' AND query_hash=$3 AND materialization_state='ready' AND expires_at>now()`, msp, client, expiredTargetHash).Scan(&readyForKey); err != nil || readyForKey != 1 {
		t.Fatalf("concurrent ready snapshots=%d error=%v", readyForKey, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM tag_report_snapshot_rows WHERE snapshot_id=$1::uuid`, concurrentTarget).Scan(&expiredTargetChildren); err != nil || expiredTargetChildren != 0 {
		t.Fatalf("concurrent expired target child rows=%d error=%v", expiredTargetChildren, err)
	}
	var activeParentRows, activeParentBytes, activeChildRows, activeChildBytes int64
	if err := pool.QueryRow(ctx, `
SELECT COALESCE(sum(snapshot.total_rows),0),COALESCE(sum(snapshot.payload_bytes),0),
       (SELECT count(*) FROM tag_report_snapshot_rows row JOIN tag_report_snapshots parent ON parent.id=row.snapshot_id WHERE parent.msp_id=$1::uuid AND parent.expires_at>now()),
       (SELECT COALESCE(sum(row.payload_bytes),0) FROM tag_report_snapshot_rows row JOIN tag_report_snapshots parent ON parent.id=row.snapshot_id WHERE parent.msp_id=$1::uuid AND parent.expires_at>now())
FROM tag_report_snapshots snapshot WHERE snapshot.msp_id=$1::uuid AND snapshot.expires_at>now()
`, msp).Scan(&activeParentRows, &activeParentBytes, &activeChildRows, &activeChildBytes); err != nil || activeParentRows != activeChildRows || activeParentBytes != activeChildBytes {
		t.Fatalf("active snapshot quota parent=%d/%d child=%d/%d error=%v", activeParentRows, activeParentBytes, activeChildRows, activeChildBytes, err)
	}
	quotaSnapshot := id.New()
	exec(`INSERT INTO tag_report_snapshots(id,msp_id,client_id,report_kind,query_hash,payload,projection_as_of,total_rows,payload_bytes,materialization_state) VALUES($1::uuid,$2::uuid,$3::uuid,'quota','quota',NULL,$4,$5,$6,'ready')`, quotaSnapshot, msp, client, at, 250_000, 256<<20)
	quotaFilter := filter
	quotaFilter.Inheritance = "all"
	if _, err := psa.NewTaggingProjectionRepositoryFromPool(pool).ClassificationReport(ctx, msp, tagging.ReportUsage, quotaFilter); !errors.Is(err, tagging.ErrReportCapacityExceeded) {
		t.Fatalf("quota report error=%v, want ErrReportCapacityExceeded", err)
	}
	exec(`DELETE FROM tag_report_snapshots WHERE id=$1::uuid`, quotaSnapshot)
	if report.Rows[0].TagID != survivor || report.Rows[0].Count != objectCount {
		t.Fatalf("first report row=%+v, want shared tag count %d", report.Rows[0], objectCount)
	}
	exec(`UPDATE tags SET lifecycle_state='merged',merged_into_id=md5($1::text||':tag:2')::uuid,version=version+1 WHERE id=md5($1::text||':tag:1')::uuid`, msp)

	reportKeys := map[string]bool{}
	lastReportKey := ""
	reportOrdinal := 0
	visitReportRow := func(row tagging.ReportRow) {
		t.Helper()
		key := row.Date + "/" + row.TagID + "/" + string(row.ObjectType)
		if reportOrdinal > 0 && key <= lastReportKey {
			t.Fatalf("report order %s after %s", key, lastReportKey)
		}
		if reportKeys[key] {
			t.Fatalf("duplicate report row %s", key)
		}
		if reportOrdinal > 0 {
			lastReportKey = key
		}
		reportKeys[key] = true
		reportOrdinal++
	}
	for _, row := range report.Rows {
		visitReportRow(row)
	}
	for report.NextCursor != "" {
		filter.Cursor = report.NextCursor
		report, err = psa.NewTaggingProjectionRepositoryFromPool(pool).ClassificationReport(ctx, msp, tagging.ReportUsage, filter)
		if err != nil {
			t.Fatalf("report continuation: %v", err)
		}
		for _, row := range report.Rows {
			visitReportRow(row)
		}
	}
	if len(reportKeys) != objectCount+1 || !reportKeys[at.Format("2006-01-02")+"/"+survivor+"/work_record"] {
		t.Fatalf("report traversal rows=%d want=%d", len(reportKeys), objectCount+1)
	}

	evidenceIDs := map[string]bool{}
	lastEvidenceID := ""
	for _, item := range evidence.Items {
		if item.ObjectID <= lastEvidenceID {
			t.Fatalf("evidence order %s after %s", item.ObjectID, lastEvidenceID)
		}
		lastEvidenceID, evidenceIDs[item.ObjectID] = item.ObjectID, true
	}
	for evidence.NextCursor != "" {
		filter.Cursor = evidence.NextCursor
		evidence, err = psa.NewTaggingProjectionRepositoryFromPool(pool).ClassificationEvidence(ctx, msp, survivor, filter)
		if err != nil {
			t.Fatalf("evidence continuation: %v", err)
		}
		for _, item := range evidence.Items {
			if evidenceIDs[item.ObjectID] {
				t.Fatalf("duplicate evidence object %s", item.ObjectID)
			}
			if item.ObjectID <= lastEvidenceID {
				t.Fatalf("evidence order %s after %s", item.ObjectID, lastEvidenceID)
			}
			lastEvidenceID, evidenceIDs[item.ObjectID] = item.ObjectID, true
		}
	}
	if len(evidenceIDs) != objectCount {
		t.Fatalf("evidence traversal items=%d want=%d", len(evidenceIDs), objectCount)
	}
}
