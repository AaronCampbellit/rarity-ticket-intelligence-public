package psa_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/id"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/store"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/store/psa"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
)

func TestTagAssociationServiceAgainstPostgres(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for association verification")
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
	msp, client, other, actor := id.New(), id.New(), id.New(), id.New()
	group, tag, insertedTag, first, second, third, fourth := id.New(), id.New(), id.New(), id.New(), id.New(), id.New(), id.New()
	pipeline, stage, opportunity, proposal, proposalVersion := id.New(), id.New(), id.New(), id.New(), id.New()
	project, phase, projectTask, phaseTask := id.New(), id.New(), id.New(), id.New()
	workTask, opportunityTask := id.New(), id.New()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, q, args...); err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	exec(`INSERT INTO msp_organizations (id,display_id,name,created_by,updated_by) VALUES ($1,$2,'association',$3,$3)`, msp, "ASSOC-"+msp, actor)
	for _, c := range []string{client, other} {
		exec(`INSERT INTO client_organizations (id,msp_id,display_id,name,created_by,updated_by) VALUES ($1,$2,$3,'client',$4,$4)`, c, msp, "CLIENT-"+c, actor)
	}
	exec(`INSERT INTO tag_groups (id,msp_id,internal_key,label,position,created_by,updated_by) VALUES ($1,$2,'taxonomy.association','Association',1,$3,$3)`, group, msp, actor)
	exec(`INSERT INTO tags (id,msp_id,group_id,internal_key,label,created_by,updated_by) VALUES ($1,$2,$3,'taxonomy.custom.association','Network',$4,$4)`, tag, msp, group, actor)
	exec(`INSERT INTO tags (id,msp_id,group_id,internal_key,label,created_by,updated_by) VALUES ($1,$2,$3,'taxonomy.custom.association-2','Identity',$4,$4)`, insertedTag, msp, group, actor)
	for _, work := range []string{first, second, third, fourth} {
		exec(`INSERT INTO work_records (id,msp_id,client_id,display_id,record_type,title,status,priority,created_by,updated_by) VALUES ($1,$2,$3,$4,'incident','association','new','normal',$5,$5)`, work, msp, client, "W-"+work, actor)
	}
	exec(`INSERT INTO pipelines (id,msp_id,key,name,created_by,updated_by) VALUES ($1,$2,'association','association',$3,$3)`, pipeline, msp, actor)
	exec(`INSERT INTO pipeline_stages (id,pipeline_id,msp_id,key,name,position,probability,forecast_category) VALUES ($1,$2,$3,'won','won',1,100,'weighted')`, stage, pipeline, msp)
	exec(`INSERT INTO opportunities (id,msp_id,client_id,pipeline_id,stage_id,display_id,name,currency,created_by,updated_by) VALUES ($1,$2,$3,$4,$5,'A-1','association','USD',$6,$6)`, opportunity, msp, client, pipeline, stage, actor)
	exec(`INSERT INTO proposals (id,msp_id,client_id,opportunity_id,display_id,current_version,state,created_by,updated_by) VALUES ($1,$2,$3,$4,'P-1',1,'draft',$5,$5)`, proposal, msp, client, opportunity, actor)
	exec(`INSERT INTO proposal_versions (id,proposal_id,msp_id,version,currency,subtotal_minor,tax_minor,total_minor,cost_minor,margin_minor,issued_by,pdf_snapshot_id) VALUES ($1,$2,$3,1,'USD',0,0,0,0,0,$4,$5)`, proposalVersion, proposal, msp, actor, id.New())
	exec(`INSERT INTO projects (id,msp_id,client_id,display_id,name,original_proposal_version_id,created_by,updated_by) VALUES ($1,$2,$3,'PRJ-1','association',$4,$5,$5)`, project, msp, client, proposalVersion, actor)
	exec(`INSERT INTO phases (id,project_id,msp_id,client_id,name,position) VALUES ($1,$2,$3,$4,'phase',1)`, phase, project, msp, client)
	exec(`INSERT INTO tasks (id,msp_id,client_id,parent_type,parent_id,title,status,position,created_by,updated_by) VALUES ($1,$2,$3,'project',$4,'project task','new',1,$5,$5)`, projectTask, msp, client, project, actor)
	exec(`INSERT INTO tasks (id,msp_id,client_id,parent_type,parent_id,title,status,position,created_by,updated_by) VALUES ($1,$2,$3,'phase',$4,'phase task','new',1,$5,$5)`, phaseTask, msp, client, phase, actor)
	exec(`INSERT INTO tasks (id,msp_id,client_id,parent_type,parent_id,title,status,position,created_by,updated_by) VALUES ($1,$2,$3,'work_record',$4,'work task','new',1,$5,$5)`, workTask, msp, client, first, actor)
	exec(`INSERT INTO tasks (id,msp_id,client_id,parent_type,parent_id,title,status,position,created_by,updated_by) VALUES ($1,$2,$3,'opportunity',$4,'opportunity task','new',1,$5,$5)`, opportunityTask, msp, client, opportunity, actor)
	repo := psa.NewTaggingRepositoryFromPool(pool)
	service := tagging.NewAssociationService(repo)
	principal := authorization.Principal{ID: actor, Scope: scope.Principal{MSPID: msp, ClientID: client}, Capabilities: authorization.NewCapabilitySet("classification.apply")}
	command := func(objectID string, version int64, key string) tagging.ReplaceCommand {
		return tagging.ReplaceCommand{Principal: principal, Target: tagging.TargetRef{MSPID: msp, ClientID: client, ObjectType: tagging.ObjectWorkRecord, ObjectID: objectID}, ExpectedObjectVersion: version, TagIDs: []string{tag}, Source: tagging.SourceHuman, Reason: "classify", IdempotencyKey: key, CorrelationID: id.New()}
	}
	accepted, err := service.ReplaceDirect(ctx, command(first, 1, "first"))
	if err != nil || len(accepted.Effective) != 1 {
		t.Fatalf("ReplaceDirect=%+v err=%v", accepted, err)
	}
	crossObject := command(fourth, 1, "first")
	crossObject.TagIDs = []string{tag, insertedTag}
	crossObjectResult, err := service.ReplaceDirect(ctx, crossObject)
	if err != nil {
		t.Fatalf("cross-object same-key replacement: %v", err)
	}
	removeInserted := command(fourth, crossObjectResult.ObjectVersion, "remove-inserted")
	removeInserted.TagIDs = []string{tag}
	if _, err := service.ReplaceDirect(ctx, removeInserted); err != nil {
		t.Fatalf("remove inserted tag: %v", err)
	}
	var crossObjectAdds, removals int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM event_outbox WHERE msp_id=$1 AND event_type='tag.added' AND subject_id IN($2,$3) AND data->>'tag_id'=$4`, msp, first, fourth, tag).Scan(&crossObjectAdds); err != nil {
		t.Fatalf("load cross-object tag events: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM event_outbox WHERE msp_id=$1 AND event_type='tag.removed' AND subject_id=$2 AND data->>'tag_id'=$3`, msp, fourth, insertedTag).Scan(&removals); err != nil {
		t.Fatalf("load tag removal event: %v", err)
	}
	if crossObjectAdds != 2 || removals != 1 {
		t.Fatalf("tag outbox cross_object_adds=%d removals=%d, want 2/1", crossObjectAdds, removals)
	}
	read, err := service.Get(ctx, tagging.GetCommand{Principal: principal, Target: accepted.Target})
	if err != nil || len(read.Direct) != 1 {
		t.Fatalf("Get=%+v err=%v", read, err)
	}
	addAutomatic := command(first, accepted.ObjectVersion, "fidelity")
	addAutomatic.TagIDs = []string{tag, insertedTag}
	addAutomatic.Source = tagging.SourceAIAutomatic
	fidelity, err := service.ReplaceDirect(ctx, addAutomatic)
	if err != nil {
		t.Fatalf("automatic additive replacement: %v", err)
	}
	if len(fidelity.Direct) != 2 {
		t.Fatalf("automatic additive direct=%+v", fidelity.Direct)
	}
	byTag := map[string]tagging.Assignment{}
	for _, assignment := range fidelity.Direct {
		byTag[assignment.Tag.ID] = assignment
	}
	if retained := byTag[tag]; retained.ID == "" || retained.Source != tagging.SourceHuman || retained.AssignedAt.IsZero() || retained.AssignedBy != actor {
		t.Fatalf("retained assignment=%+v, want persisted human assignment", retained)
	}
	if inserted := byTag[insertedTag]; inserted.ID == "" || inserted.Source != tagging.SourceAIAutomatic || inserted.AssignedAt.IsZero() || inserted.AssignedBy != actor {
		t.Fatalf("inserted assignment=%+v, want persisted automatic assignment", inserted)
	}
	replayed, err := service.ReplaceDirect(ctx, addAutomatic)
	if err != nil || replayed.ObjectVersion != fidelity.ObjectVersion || !sameAssignmentRecords(replayed.Direct, fidelity.Direct) {
		t.Fatalf("automatic replay=%+v err=%v, want %+v", replayed, err, fidelity)
	}
	loadedFidelity, err := service.Get(ctx, tagging.GetCommand{Principal: principal, Target: fidelity.Target})
	if err != nil || loadedFidelity.ObjectVersion != fidelity.ObjectVersion || !sameAssignmentRecords(loadedFidelity.Direct, fidelity.Direct) {
		t.Fatalf("immediate Get=%+v err=%v, want %+v", loadedFidelity, err, fidelity)
	}

	noOp := command(first, fidelity.ObjectVersion, "no-op-evidence")
	noOp.TagIDs = []string{tag, insertedTag}
	noOp.CorrelationID = id.New()
	noOpResult, err := service.ReplaceDirect(ctx, noOp)
	if err != nil || noOpResult.ObjectVersion != fidelity.ObjectVersion {
		t.Fatalf("no-op ReplaceDirect=%+v err=%v", noOpResult, err)
	}
	var operationVersion, operationResponseVersion, auditVersion, eventVersion int64
	if err := pool.QueryRow(ctx, `SELECT accepted_object_version, (response->>'object_version')::bigint FROM classification_tag_operations WHERE msp_id=$1 AND client_id=$2 AND object_type='work_record' AND object_id=$3 AND idempotency_key=$4`, msp, client, first, noOp.IdempotencyKey).Scan(&operationVersion, &operationResponseVersion); err != nil {
		t.Fatalf("no-op operation version: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT subject_version FROM audit_ledger WHERE correlation_id=$1`, noOp.CorrelationID).Scan(&auditVersion); err != nil {
		t.Fatalf("no-op audit version: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT subject_version FROM event_outbox WHERE correlation_id=$1`, noOp.CorrelationID).Scan(&eventVersion); err != nil {
		t.Fatalf("no-op outbox version: %v", err)
	}
	var historyEvents int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM tag_assignment_events WHERE msp_id=$1 AND client_id=$2 AND object_type='work_record' AND object_id=$3 AND idempotency_key LIKE '%' || $4 || '%'`, msp, client, first, noOp.IdempotencyKey).Scan(&historyEvents); err != nil {
		t.Fatalf("no-op assignment history: %v", err)
	}
	if operationVersion != noOpResult.ObjectVersion || operationResponseVersion != noOpResult.ObjectVersion || auditVersion != noOpResult.ObjectVersion || eventVersion != noOpResult.ObjectVersion || historyEvents != 0 {
		t.Fatalf("no-op evidence operation=%d response=%d audit=%d outbox=%d assignment_events=%d accepted=%d", operationVersion, operationResponseVersion, auditVersion, eventVersion, historyEvents, noOpResult.ObjectVersion)
	}

	namespaceHolder, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tag-mutation namespace holder: %v", err)
	}
	namespaceKey := "classification-tag-mutation:" + msp
	if err := namespaceHolder.QueryRow(
		ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
		namespaceKey,
	).Scan(new(any)); err != nil {
		_ = namespaceHolder.Rollback(ctx)
		t.Fatalf("acquire tag-mutation namespace: %v", err)
	}
	namespaceRequest := command(first, noOpResult.ObjectVersion, "namespace-wait")
	namespaceRequest.TagIDs = []string{tag, insertedTag}
	namespaceResult := make(chan error, 1)
	go func() {
		_, callErr := service.ReplaceDirect(ctx, namespaceRequest)
		namespaceResult <- callErr
	}()
	namespaceDeadline := time.Now().Add(time.Second)
	for {
		var waiting int
		if err := pool.QueryRow(
			ctx,
			`SELECT count(*) FROM pg_locks WHERE locktype = 'advisory' AND NOT granted`,
		).Scan(&waiting); err != nil {
			_ = namespaceHolder.Rollback(ctx)
			t.Fatalf("inspect tag-mutation namespace waiter: %v", err)
		}
		if waiting >= 1 {
			break
		}
		if time.Now().After(namespaceDeadline) {
			_ = namespaceHolder.Rollback(ctx)
			t.Fatal("association request did not wait on the shared tag-mutation namespace")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := namespaceHolder.Commit(ctx); err != nil {
		t.Fatalf("release tag-mutation namespace: %v", err)
	}
	if err := <-namespaceResult; err != nil {
		t.Fatalf("association after tag-mutation namespace release: %v", err)
	}

	runConcurrent := func(name string, request tagging.ReplaceCommand, wantVersion int64) {
		t.Helper()
		lock, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("%s begin lock holder: %v", name, err)
		}
		defer lock.Rollback(ctx)
		key := "classification-association:" + request.Target.MSPID + ":" + request.Target.ClientID + ":" + request.Target.ObjectType.String() + ":" + request.Target.ObjectID + ":" + request.IdempotencyKey
		if err := lock.QueryRow(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, key).Scan(new(any)); err != nil {
			t.Fatalf("%s acquire operation lock: %v", name, err)
		}
		start := make(chan struct{})
		started := make(chan struct{}, 2)
		results := make(chan struct {
			object tagging.TaggedObject
			err    error
		}, 2)
		for range 2 {
			go func() {
				<-start
				started <- struct{}{}
				object, err := service.ReplaceDirect(ctx, request)
				results <- struct {
					object tagging.TaggedObject
					err    error
				}{object, err}
			}()
		}
		close(start)
		<-started
		<-started
		deadline := time.Now().Add(time.Second)
		for {
			var waiting int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_locks WHERE locktype = 'advisory' AND NOT granted`).Scan(&waiting); err != nil {
				t.Fatalf("%s inspect advisory waiters: %v", name, err)
			}
			if waiting >= 2 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s requests did not both wait on the operation lock (waiters=%d)", name, waiting)
			}
			time.Sleep(5 * time.Millisecond)
		}
		if err := lock.Commit(ctx); err != nil {
			t.Fatalf("%s release operation lock: %v", name, err)
		}
		firstResult := <-results
		secondResult := <-results
		if firstResult.err != nil || secondResult.err != nil || firstResult.object.ObjectVersion != wantVersion || secondResult.object.ObjectVersion != wantVersion || !sameAssignmentRecords(firstResult.object.Direct, secondResult.object.Direct) {
			t.Fatalf("%s concurrent results first=%+v/%v second=%+v/%v", name, firstResult.object, firstResult.err, secondResult.object, secondResult.err)
		}
	}
	concurrentChange := command(third, 1, "concurrent-change")
	runConcurrent("changing", concurrentChange, 2)
	concurrentNoOp := command(first, fidelity.ObjectVersion, "concurrent-same-set")
	concurrentNoOp.TagIDs = []string{tag, insertedTag}
	runConcurrent("same set", concurrentNoOp, fidelity.ObjectVersion)
	foreign := principal
	foreign.Scope.ClientID = other
	if _, err := service.Get(ctx, tagging.GetCommand{Principal: foreign, Target: accepted.Target}); err == nil {
		t.Fatal("cross-client target disclosed")
	}
	bulk, err := service.Bulk(ctx, tagging.BulkCommand{Principal: principal, Items: []tagging.ReplaceCommand{command(second, 1, "second"), command(first, 1, "stale")}})
	if err != nil || bulk[0].Object == nil || bulk[1].Error == "" {
		t.Fatalf("Bulk=%+v err=%v", bulk, err)
	}
	projectCommand := command(project, 1, "project")
	projectCommand.Target.ObjectType = tagging.ObjectProject
	if _, err := service.ReplaceDirect(ctx, projectCommand); err != nil {
		t.Fatalf("classify project: %v", err)
	}
	for _, taskID := range []string{workTask, opportunityTask} {
		taskCommand := command(taskID, 1, "direct-"+taskID)
		taskCommand.Target.ObjectType = tagging.ObjectTask
		taskResult, err := service.ReplaceDirect(ctx, taskCommand)
		if err != nil || len(taskResult.Direct) != 1 ||
			len(taskResult.Inherited) != 0 ||
			taskResult.Direct[0].Tag.ID != tag {
			t.Fatalf("classify non-project task %s: result=%+v error=%v", taskID, taskResult, err)
		}
	}
	for _, taskID := range []string{projectTask, phaseTask} {
		value, err := service.Get(ctx, tagging.GetCommand{Principal: principal, Target: tagging.TargetRef{MSPID: msp, ClientID: client, ObjectType: tagging.ObjectTask, ObjectID: taskID}})
		if err != nil || len(value.Inherited) != 1 || value.Inherited[0].SourceObjectType != tagging.ObjectProject || value.Inherited[0].SourceObjectID != project {
			t.Fatalf("task inheritance=%+v err=%v", value, err)
		}
		var copied int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM object_tag_assignments WHERE msp_id=$1 AND client_id=$2 AND object_type='task' AND object_id=$3`, msp, client, taskID).Scan(&copied); err != nil || copied != 0 {
			t.Fatalf("copied task tags=%d err=%v", copied, err)
		}
	}
	emptyAutomatic := tagging.ReplaceCommand{
		Principal:             principal,
		Target:                tagging.TargetRef{MSPID: msp, ClientID: client, ObjectType: tagging.ObjectTask, ObjectID: projectTask},
		ExpectedObjectVersion: 1, Source: tagging.SourceAIAutomatic,
		Reason: "retain inherited classification", IdempotencyKey: "empty-automatic",
		CorrelationID: id.New(),
	}
	emptyAutomaticResult, err := service.ReplaceDirect(ctx, emptyAutomatic)
	if err != nil || len(emptyAutomaticResult.Direct) != 0 || len(emptyAutomaticResult.Inherited) != 1 || emptyAutomaticResult.ClassificationState != "classified" {
		t.Fatalf("empty automatic inherited no-op=%+v err=%v", emptyAutomaticResult, err)
	}
	inherited := emptyAutomaticResult.Inherited[0]
	if inherited.ID == "" || inherited.Source != tagging.SourceHuman || inherited.AssignedAt.IsZero() || inherited.AssignedBy != actor || inherited.SourceObjectType != tagging.ObjectProject || inherited.SourceObjectID != project {
		t.Fatalf("inherited assignment=%+v, want persisted project evidence", inherited)
	}
	emptyAutomaticReplay, err := service.ReplaceDirect(ctx, emptyAutomatic)
	if err != nil || emptyAutomaticReplay.ObjectVersion != emptyAutomaticResult.ObjectVersion || !sameAssignmentRecords(emptyAutomaticReplay.Inherited, emptyAutomaticResult.Inherited) {
		t.Fatalf("empty automatic replay=%+v err=%v, want %+v", emptyAutomaticReplay, err, emptyAutomaticResult)
	}
	emptyAutomaticGet, err := service.Get(ctx, tagging.GetCommand{Principal: principal, Target: emptyAutomatic.Target})
	if err != nil || emptyAutomaticGet.ObjectVersion != emptyAutomaticResult.ObjectVersion || !sameAssignmentRecords(emptyAutomaticGet.Inherited, emptyAutomaticResult.Inherited) {
		t.Fatalf("empty automatic Get=%+v err=%v, want %+v", emptyAutomaticGet, err, emptyAutomaticResult)
	}
	// A former Phase period and the current Project period both suppress Project
	// evidence only while an identical direct Task tag is effective.
	historyBase := time.Now().UTC().Add(time.Hour)
	exec(`UPDATE tasks SET parent_type='project', parent_id=$1, created_at=$2 WHERE id=$3 AND msp_id=$4 AND client_id=$5`, project, historyBase.Add(-time.Minute), phaseTask, msp, client)
	exec(`INSERT INTO task_movement_history (id,task_id,msp_id,client_id,from_parent_type,from_parent_id,to_parent_type,to_parent_id,previous_version,accepted_version,moved_at,moved_by,correlation_id) VALUES ($1,$2,$3,$4,'phase',$5,'project',$6,1,2,$7,$8,$9)`, id.New(), phaseTask, msp, client, phase, project, historyBase.Add(5*time.Minute), actor, id.New())
	insertHistoryEvent := func(eventID, objectType, objectID, operation string, at time.Time) {
		t.Helper()
		exec(`INSERT INTO tag_assignment_events (id,msp_id,client_id,assignment_id,object_type,object_id,target_version,tag_id,operation,assignment_source,actor_type,actor_id,occurred_at,idempotency_key,correlation_id) VALUES ($1,$2,$3,$4,$5,$6,1,$7,$8,'human','technician',$9,$10,$11,$12)`, eventID, msp, client, id.New(), objectType, objectID, tag, operation, actor, at, "history-"+eventID, id.New())
	}
	directAddBefore, hiddenProjectAdd := id.New(), id.New()
	directRemoveBefore, shownProjectAdd := id.New(), id.New()
	directAddDuring, hiddenProjectRemove := id.New(), id.New()
	directRemoveDuring, shownProjectRemove := id.New(), id.New()
	insertHistoryEvent(directAddBefore, "task", phaseTask, "added", historyBase)
	insertHistoryEvent(hiddenProjectAdd, "project", project, "added", historyBase.Add(time.Minute))
	insertHistoryEvent(directRemoveBefore, "task", phaseTask, "removed", historyBase.Add(2*time.Minute))
	insertHistoryEvent(shownProjectAdd, "project", project, "added", historyBase.Add(3*time.Minute))
	insertHistoryEvent(directAddDuring, "task", phaseTask, "added", historyBase.Add(6*time.Minute))
	insertHistoryEvent(hiddenProjectRemove, "project", project, "removed", historyBase.Add(7*time.Minute))
	insertHistoryEvent(directRemoveDuring, "task", phaseTask, "removed", historyBase.Add(8*time.Minute))
	insertHistoryEvent(shownProjectRemove, "project", project, "removed", historyBase.Add(9*time.Minute))
	history, err := service.History(ctx, tagging.HistoryCommand{Principal: principal, Target: tagging.TargetRef{MSPID: msp, ClientID: client, ObjectType: tagging.ObjectTask, ObjectID: phaseTask}})
	if err != nil {
		t.Fatalf("shadowed task history: %v", err)
	}
	inheritedEvents := map[string]bool{}
	for _, entry := range history {
		if entry.Inherited {
			inheritedEvents[entry.ID] = true
		}
	}
	if inheritedEvents[hiddenProjectAdd] || inheritedEvents[hiddenProjectRemove] || !inheritedEvents[shownProjectAdd] || !inheritedEvents[shownProjectRemove] {
		t.Fatalf("inherited shadow history=%v, hidden=(%s,%s) shown=(%s,%s)", inheritedEvents, hiddenProjectAdd, hiddenProjectRemove, shownProjectAdd, shownProjectRemove)
	}
	catalog := tagging.NewCatalogService(repo, time.Now, id.New)
	manager := authorization.Principal{ID: actor, Scope: scope.Principal{MSPID: msp, ClientID: client}, Capabilities: authorization.NewCapabilitySet("classification.manage")}
	firstHealth, err := catalog.Health(ctx, tagging.HealthCommand{Principal: manager, Target: scope.Target{MSPID: msp, ClientID: client}})
	if err != nil || firstHealth.ByObjectType[tagging.ObjectWorkRecord].Meaningful == 0 {
		t.Fatalf("first health=%+v err=%v", firstHealth, err)
	}
	manager.Scope.ClientID = other
	secondHealth, err := catalog.Health(ctx, tagging.HealthCommand{Principal: manager, Target: scope.Target{MSPID: msp, ClientID: other}})
	if err != nil || secondHealth.ByObjectType[tagging.ObjectWorkRecord].Meaningful != 0 {
		t.Fatalf("second health=%+v err=%v", secondHealth, err)
	}
}

func sameAssignmentRecords(left, right []tagging.Assignment) bool {
	if len(left) != len(right) {
		return false
	}
	byTag := make(map[string]tagging.Assignment, len(left))
	for _, assignment := range left {
		byTag[assignment.Tag.ID] = assignment
	}
	for _, assignment := range right {
		other, found := byTag[assignment.Tag.ID]
		if !found || other.ID != assignment.ID || other.Source != assignment.Source ||
			!other.AssignedAt.Equal(assignment.AssignedAt) || other.AssignedBy != assignment.AssignedBy ||
			other.Inherited != assignment.Inherited || other.SourceObjectType != assignment.SourceObjectType ||
			other.SourceObjectID != assignment.SourceObjectID {
			return false
		}
	}
	return true
}

func TestTaggingRepositoryCatalogLifecycleAgainstPostgres(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for PostgreSQL tagging verification")
	}
	ctx := context.Background()
	if err := store.Migrate(ctx, databaseURL); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect database: %v", err)
	}
	t.Cleanup(pool.Close)

	mspID, actorID := id.New(), id.New()
	if _, err := pool.Exec(ctx, `
INSERT INTO msp_organizations (
  id, display_id, name, lifecycle_state, version,
  created_at, created_by, updated_at, updated_by
) VALUES ($1, $2, 'Tagging integration', 'active', 1, now(), $3, now(), $3)
`, mspID, "TAG-"+mspID, actorID); err != nil {
		t.Fatalf("create MSP fixture: %v", err)
	}

	service := tagging.NewCatalogService(
		psa.NewTaggingRepositoryFromPool(pool),
		time.Now,
		id.New,
	)
	principal := authorization.Principal{
		ID: actorID,
		Scope: scope.Principal{
			MSPID: mspID,
		},
		Capabilities: authorization.NewCapabilitySet("classification.manage"),
	}
	unclassified, err := service.EnsureSystemCatalog(ctx, mspID, actorID)
	if err != nil || !unclassified.SystemManaged {
		t.Fatalf("EnsureSystemCatalog() tag=%+v error=%v", unclassified, err)
	}
	repeated, err := service.EnsureSystemCatalog(ctx, mspID, actorID)
	if err != nil || repeated.ID != unclassified.ID {
		t.Fatalf(
			"repeated EnsureSystemCatalog() tag=%+v error=%v",
			repeated,
			err,
		)
	}
	var ensuredSubjectID string
	if err := pool.QueryRow(ctx, `
SELECT subject_id::text
FROM audit_ledger
WHERE msp_id = $1 AND action = 'classification.system_catalog.ensured'
ORDER BY occurred_at DESC, id DESC
LIMIT 1
`, mspID).Scan(&ensuredSubjectID); err != nil {
		t.Fatalf("read system-catalog audit: %v", err)
	}
	if ensuredSubjectID != unclassified.ID {
		t.Fatalf(
			"system-catalog audit subject=%s, want %s",
			ensuredSubjectID,
			unclassified.ID,
		)
	}
	group, err := service.CreateGroup(ctx, tagging.CreateGroupCommand{
		Principal: principal, Label: "Technology", Position: 2,
	})
	if err != nil {
		t.Fatalf("CreateGroup() error=%v", err)
	}
	survivor, err := service.CreateTag(ctx, tagging.CreateTagCommand{
		Principal: principal, GroupID: group.ID, Label: "Microsoft 365",
		Synonyms: []string{"M365", "Office 365"},
	})
	if err != nil {
		t.Fatalf("CreateTag(survivor) error=%v", err)
	}
	survivor, err = service.UpdateTag(ctx, tagging.UpdateTagCommand{
		Principal: principal, ID: survivor.ID, GroupID: group.ID,
		Label: "M365", Synonyms: []string{"Office 365"},
		ExpectedVersion: survivor.Version,
	})
	if err != nil || survivor.Label != "M365" {
		t.Fatalf("UpdateTag() tag=%+v error=%v", survivor, err)
	}
	retired, err := service.CreateTag(ctx, tagging.CreateTagCommand{
		Principal: principal, GroupID: group.ID, Label: "Microsoft Cloud",
	})
	if err != nil {
		t.Fatalf("CreateTag(retired) error=%v", err)
	}
	retired, err = service.Merge(ctx, tagging.MergeCommand{
		Principal: principal, TagID: retired.ID, SurvivorTagID: survivor.ID,
		ExpectedVersion: retired.Version, Reason: "Duplicate catalog term",
	})
	if err != nil || retired.State != tagging.StateMerged {
		t.Fatalf("Merge() tag=%+v error=%v", retired, err)
	}
	archivable, err := service.CreateTag(ctx, tagging.CreateTagCommand{
		Principal: principal, GroupID: group.ID, Label: "Legacy VPN",
	})
	if err != nil {
		t.Fatalf("CreateTag(archivable) error=%v", err)
	}
	archived, err := service.Archive(ctx, tagging.ArchiveCommand{
		Principal: principal, TagID: archivable.ID,
		ExpectedVersion: archivable.Version, Reason: "Technology retired",
	})
	if err != nil || archived.State != tagging.StateArchived {
		t.Fatalf("Archive() tag=%+v error=%v", archived, err)
	}
}
