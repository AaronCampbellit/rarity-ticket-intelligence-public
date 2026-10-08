package psa_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/clientresources"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/id"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/knowledge"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/projects"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/routing"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sla"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/store"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/store/psa"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tasks"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/timeentries"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workflow"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workrecords"
)

// TestInitialTagAssignmentsAgainstPostgres executes every supported object
// repository against PostgreSQL. It is deliberately database-gated: the SQL
// mock tests protect statement order, while this test protects the actual
// transaction boundary, UUID casts, provenance columns, and constraints.
func TestInitialTagAssignmentsAgainstPostgres(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for Task 5 PostgreSQL verification")
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

	fixture := newInitialTagFixture(t, ctx, pool)
	create := func(objectType tagging.ObjectType, objectID string, fallback bool, call func(tagging.InitialAssignmentSet) error) {
		t.Helper()
		initial := fixture.initial(objectType, fallback)
		if err := call(initial); err != nil {
			t.Fatalf("create %s fallback=%t: %v", objectType, fallback, err)
		}
		fixture.assertCreated(t, objectType, objectID, fallback)
	}

	workDirect, workFallback := id.New(), id.New()
	create(tagging.ObjectWorkRecord, workDirect, false, func(initial tagging.InitialAssignmentSet) error {
		return fixture.workRecords.CreateAtomic(ctx, fixture.workMutation(workDirect, initial))
	})
	create(tagging.ObjectWorkRecord, workFallback, true, func(initial tagging.InitialAssignmentSet) error {
		return fixture.workRecords.CreateAtomic(ctx, fixture.workMutation(workFallback, initial))
	})

	taskDirect, taskFallback := id.New(), id.New()
	create(tagging.ObjectTask, taskDirect, false, func(initial tagging.InitialAssignmentSet) error {
		return fixture.tasks.CreateAtomic(ctx, fixture.taskMutation(taskDirect, workDirect, 1, initial))
	})
	create(tagging.ObjectTask, taskFallback, true, func(initial tagging.InitialAssignmentSet) error {
		return fixture.tasks.CreateAtomic(ctx, fixture.taskMutation(taskFallback, workFallback, 1, initial))
	})

	projectDirect, projectFallback := id.New(), id.New()
	create(tagging.ObjectProject, projectDirect, false, func(initial tagging.InitialAssignmentSet) error {
		return fixture.projects.CreateAtomic(ctx, fixture.projectMutation(projectDirect, initial))
	})
	create(tagging.ObjectProject, projectFallback, true, func(initial tagging.InitialAssignmentSet) error {
		return fixture.projects.CreateAtomic(ctx, fixture.projectMutation(projectFallback, initial))
	})
	// Project evidence remains inherited at read time; creating a Project task
	// must write only its own direct assignment, never a copied Project row.
	projectTask := id.New()
	create(tagging.ObjectTask, projectTask, false, func(initial tagging.InitialAssignmentSet) error {
		return fixture.tasks.CreateAtomic(ctx, fixture.projectTaskMutation(projectTask, projectDirect, initial))
	})
	var copied int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM object_tag_assignments WHERE msp_id=$1 AND client_id=$2 AND object_type='task' AND object_id=$3`, fixture.msp, fixture.client, projectTask).Scan(&copied); err != nil || copied != 1 {
		t.Fatalf("project task direct rows=%d err=%v, want exactly its one direct assignment", copied, err)
	}

	assetDirect, assetFallback := id.New(), id.New()
	create(tagging.ObjectAsset, assetDirect, false, func(initial tagging.InitialAssignmentSet) error {
		return fixture.assets.CreateAtomic(ctx, fixture.assetMutation(assetDirect, initial))
	})
	create(tagging.ObjectAsset, assetFallback, true, func(initial tagging.InitialAssignmentSet) error {
		return fixture.assets.CreateAtomic(ctx, fixture.assetMutation(assetFallback, initial))
	})

	articleDirect, articleFallback := id.New(), id.New()
	create(tagging.ObjectKnowledgeArticle, articleDirect, false, func(initial tagging.InitialAssignmentSet) error {
		return fixture.knowledge.CreateDraftAtomic(ctx, fixture.articleMutation(articleDirect, initial))
	})
	create(tagging.ObjectKnowledgeArticle, articleFallback, true, func(initial tagging.InitialAssignmentSet) error {
		return fixture.knowledge.CreateDraftAtomic(ctx, fixture.articleMutation(articleFallback, initial))
	})

	entryDirect, entryFallback := id.New(), id.New()
	create(tagging.ObjectTimeEntry, entryDirect, false, func(initial tagging.InitialAssignmentSet) error {
		return fixture.entries.CreateAtomic(ctx, fixture.entryMutation(entryDirect, workDirect, initial))
	})
	create(tagging.ObjectTimeEntry, entryFallback, true, func(initial tagging.InitialAssignmentSet) error {
		return fixture.entries.CreateAtomic(ctx, fixture.entryMutation(entryFallback, workFallback, initial))
	})

	// A missing tag makes insertInitialTagAssignments fail after the asset
	// insert. The rejected transaction may not leave an object, audit, or outbox
	// row behind.
	badAsset := id.New()
	bad := fixture.initial(tagging.ObjectAsset, false)
	bad.Direct[0].Tag.ID = id.New()
	err = fixture.assets.CreateAtomic(ctx, fixture.assetMutation(badAsset, bad))
	if !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("bad initial assignment error=%v, want not found", err)
	}
	fixture.assertRollback(t, badAsset)
}

type initialTagFixture struct {
	pool                                *pgxpool.Pool
	msp, client, actor, technician, tag string
	unclassified                        string
	at                                  time.Time
	workRecords                         *psa.WorkRecordRepository
	tasks                               *psa.TaskRepository
	projects                            *psa.ProjectRepository
	assets                              *psa.AssetRepository
	knowledge                           *psa.KnowledgeRepository
	entries                             *psa.TimeEntryRepository
	proposalVersion, routingSet         string
	workflowID, policyID, calendarID    string
	queueID, routingRuleID              string
}

func newInitialTagFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) initialTagFixture {
	t.Helper()
	f := initialTagFixture{pool: pool, msp: id.New(), client: id.New(), actor: id.New(), technician: id.New(), tag: id.New(), unclassified: id.New(), at: time.Now().UTC().Truncate(time.Microsecond), proposalVersion: id.New(), routingSet: id.New(), workflowID: id.New(), policyID: id.New(), calendarID: id.New(), queueID: id.New(), routingRuleID: id.New()}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatalf("Task 5 fixture: %v", err)
		}
	}
	exec(`INSERT INTO msp_organizations (id,display_id,name,created_by,updated_by) VALUES ($1,$2,'Task 5 live',$3,$3)`, f.msp, "T5-"+f.msp, f.actor)
	exec(`INSERT INTO client_organizations (id,msp_id,display_id,name,created_by,updated_by) VALUES ($1,$2,$3,'Task 5 client',$4,$4)`, f.client, f.msp, "T5-"+f.client, f.actor)
	exec(`INSERT INTO technicians (id,msp_id,email,display_name) VALUES ($1,$2,$3,'Task 5 technician')`, f.technician, f.msp, f.technician+"@example.test")
	exec(`INSERT INTO tag_groups (id,msp_id,internal_key,label,position,created_by,updated_by) VALUES ($1,$2,'task5-live','Task 5',1,$3,$3)`, id.New(), f.msp, f.actor)
	var group string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM tag_groups WHERE msp_id=$1 AND internal_key='task5-live'`, f.msp).Scan(&group); err != nil {
		t.Fatalf("read fixture tag group: %v", err)
	}
	exec(`INSERT INTO tags (id,msp_id,group_id,internal_key,label,created_by,updated_by) VALUES ($1,$2,$3,'task5-live-tag','Live meaningful',$4,$4)`, f.tag, f.msp, group, f.actor)
	exec(`INSERT INTO tags (id,msp_id,group_id,internal_key,label,system_tag,created_by,updated_by) VALUES ($1,$2,$3,'system.unclassified','Unclassified',true,$4,$4)`, f.unclassified, f.msp, group, f.actor)

	// Work Record's routing/workflow/SLA snapshots have real FKs, so populate
	// their minimal published records rather than weakening the repository test.
	exec(`INSERT INTO queues (id,msp_id,key,name) VALUES ($1,$2,'task5','Task 5')`, f.queueID, f.msp)
	exec(`INSERT INTO routing_rule_sets (id,msp_id,current_version,created_at,created_by,updated_at,updated_by) VALUES ($1,$2,1,$3,$4,$3,$4)`, f.routingSet, f.msp, f.at, f.actor)
	exec(`INSERT INTO routing_rule_set_versions (rule_set_id,msp_id,version,published_at,published_by) VALUES ($1,$2,1,$3,$4)`, f.routingSet, f.msp, f.at, f.actor)
	exec(`INSERT INTO routing_rule_versions (rule_set_id,msp_id,version,rule_id,position,queue_id) VALUES ($1,$2,1,$3,1,$4)`, f.routingSet, f.msp, f.routingRuleID, f.queueID)
	exec(`INSERT INTO workflows (id,msp_id,key,name,created_at,created_by,updated_at,updated_by) VALUES ($1,$2,'task5','Task 5',$3,$4,$3,$4)`, f.workflowID, f.msp, f.at, f.actor)
	exec(`INSERT INTO workflow_versions (workflow_id,msp_id,version,definition,published_at,published_by) VALUES ($1,$2,1,'{}',$3,$4)`, f.workflowID, f.msp, f.at, f.actor)
	exec(`INSERT INTO business_calendars (id,msp_id,key,name,timezone,weekly_schedule) VALUES ($1,$2,'task5','Task 5','UTC','{}')`, f.calendarID, f.msp)
	exec(`INSERT INTO business_calendar_versions (calendar_id,msp_id,version,timezone,weekly_schedule,holidays,published_at,published_by) VALUES ($1,$2,1,'UTC','{}','[]',$3,$4)`, f.calendarID, f.msp, f.at, f.actor)
	exec(`INSERT INTO sla_policies (id,msp_id,key,name,calendar_id,response_target_seconds,resolution_target_seconds) VALUES ($1,$2,'task5','Task 5',$3,60,120)`, f.policyID, f.msp, f.calendarID)
	exec(`INSERT INTO sla_policy_versions (policy_id,msp_id,version,calendar_id,calendar_version,conditions,response_target_seconds,resolution_target_seconds,warning_percent,pause_states,enabled,priority,stable_order,fallback,published_at,published_by) VALUES ($1,$2,1,$3,1,'{}',60,120,80,'[]',true,0,0,false,$4,$5)`, f.policyID, f.msp, f.calendarID, f.at, f.actor)

	pipeline, stage, opportunity, proposal := id.New(), id.New(), id.New(), id.New()
	exec(`INSERT INTO pipelines (id,msp_id,key,name,created_by,updated_by) VALUES ($1,$2,'task5','Task 5',$3,$3)`, pipeline, f.msp, f.actor)
	exec(`INSERT INTO pipeline_stages (id,pipeline_id,msp_id,key,name,position,probability,forecast_category) VALUES ($1,$2,$3,'won','Won',1,100,'weighted')`, stage, pipeline, f.msp)
	exec(`INSERT INTO opportunities (id,msp_id,client_id,pipeline_id,stage_id,display_id,name,currency,created_by,updated_by) VALUES ($1,$2,$3,$4,$5,'T5-OPP','Task 5','USD',$6,$6)`, opportunity, f.msp, f.client, pipeline, stage, f.actor)
	exec(`INSERT INTO proposals (id,msp_id,client_id,opportunity_id,display_id,current_version,state,created_by,updated_by) VALUES ($1,$2,$3,$4,'T5-PROP',1,'draft',$5,$5)`, proposal, f.msp, f.client, opportunity, f.actor)
	exec(`INSERT INTO proposal_versions (id,proposal_id,msp_id,version,currency,subtotal_minor,tax_minor,total_minor,cost_minor,margin_minor,issued_by,pdf_snapshot_id) VALUES ($1,$2,$3,1,'USD',0,0,0,0,0,$4,$5)`, f.proposalVersion, proposal, f.msp, f.actor, id.New())
	f.workRecords = psa.NewWorkRecordRepositoryFromPool(pool)
	f.tasks = psa.NewTaskRepositoryFromPool(pool)
	f.projects = psa.NewProjectRepositoryFromPool(pool)
	f.assets = psa.NewAssetRepositoryFromPool(pool)
	f.knowledge = psa.NewKnowledgeRepositoryFromPool(pool)
	f.entries = psa.NewTimeEntryRepositoryFromPool(pool)
	return f
}

func (f initialTagFixture) initial(objectType tagging.ObjectType, fallback bool) tagging.InitialAssignmentSet {
	if fallback {
		return tagging.InitialAssignmentSet{Direct: []tagging.Assignment{{Tag: tagging.Tag{ID: f.unclassified}, Source: tagging.SourceSystemFallback}}, ActorType: "system", OccurredAt: f.at, CorrelationID: id.New()}
	}
	return tagging.InitialAssignmentSet{Direct: []tagging.Assignment{{Tag: tagging.Tag{ID: f.tag}, Source: tagging.SourceHuman}}, ActorType: "technician", ActorID: f.actor, OccurredAt: f.at, CorrelationID: id.New()}
}

func (f initialTagFixture) facts(objectType tagging.ObjectType, objectID string, fallback bool) (mutation.AuditRecord, mutation.EventRecord) {
	actorType, actorID, source := "technician", f.actor, "api"
	if fallback {
		// Object facts retain the trusted automation actor; only the fallback
		// assignment itself is system/NULL because no technician selected it.
		actorType, source = "system", "automation"
	}
	correlation := id.New()
	return mutation.AuditRecord{ID: id.New(), OccurredAt: f.at, MSPID: f.msp, ClientID: f.client, ActorType: actorType, ActorID: actorID, Action: objectType.String() + ".created", SubjectType: objectType.String(), SubjectID: objectID, SubjectVersion: 1, Source: source, CorrelationID: correlation}, mutation.EventRecord{EventID: id.New(), EventType: objectType.String() + ".created", SchemaVersion: 1, OccurredAt: f.at, MSPID: f.msp, ClientID: f.client, ActorType: actorType, ActorID: actorID, SubjectType: objectType.String(), SubjectID: objectID, SubjectVersion: 1, Source: source, CorrelationID: correlation}
}

func (f initialTagFixture) workMutation(objectID string, initial tagging.InitialAssignmentSet) workrecords.CreateMutation {
	audit, event := f.facts(tagging.ObjectWorkRecord, objectID, initial.Direct[0].Source == tagging.SourceSystemFallback)
	return workrecords.CreateMutation{Record: workrecords.Record{Envelope: object.Envelope{ID: objectID, MSPID: f.msp, ClientID: f.client, DisplayID: "WR-" + objectID, LifecycleState: "active", Version: 1, CreatedAt: f.at, CreatedBy: f.actor, UpdatedAt: f.at, UpdatedBy: f.actor}, Type: workrecords.Incident, Title: "Task 5", Status: "new", Priority: "normal"}, Routing: workrecords.RoutingSelection{RuleSetID: f.routingSet, RuleSetVersion: 1, DecidedAt: f.at, Decision: routing.Decision{RuleID: f.routingRuleID, QueueID: f.queueID, Explanation: "Task 5"}}, Workflow: workflow.Selection{WorkflowID: f.workflowID, Version: 1, EvaluatedAt: f.at}, SLA: workrecords.AppliedSLA{ID: id.New(), PolicyID: f.policyID, PolicyVersion: 1, CalendarID: f.calendarID, CalendarVersion: 1, ResponseWarningAt: f.at.Add(time.Minute), ResponseDueAt: f.at.Add(2 * time.Minute), ResolutionWarningAt: f.at.Add(3 * time.Minute), ResolutionDueAt: f.at.Add(4 * time.Minute), ResponseState: sla.Running, ResolutionState: sla.Running, Version: 1}, Audit: audit, Event: event, InitialTags: initial}
}

func (f initialTagFixture) taskMutation(objectID, workID string, position int, initial tagging.InitialAssignmentSet) tasks.CreateMutation {
	audit, event := f.facts(tagging.ObjectTask, objectID, initial.Direct[0].Source == tagging.SourceSystemFallback)
	return tasks.CreateMutation{Task: tasks.Task{ID: objectID, MSPID: f.msp, ClientID: f.client, Parent: tasks.Ref{Type: tasks.ParentWorkRecord, ID: workID, MSPID: f.msp, ClientID: f.client}, WorkRecordID: workID, Title: "Task 5", Status: "new", Position: position, Version: 1, CreatedBy: f.actor}, Audit: audit, Event: event, InitialTags: initial}
}

func (f initialTagFixture) projectTaskMutation(objectID, projectID string, initial tagging.InitialAssignmentSet) tasks.CreateMutation {
	audit, event := f.facts(tagging.ObjectTask, objectID, false)
	return tasks.CreateMutation{Task: tasks.Task{ID: objectID, MSPID: f.msp, ClientID: f.client, Parent: tasks.Ref{Type: tasks.ParentProject, ID: projectID, MSPID: f.msp, ClientID: f.client}, Title: "Project task", Status: "new", Position: 1, Version: 1, CreatedBy: f.actor}, Audit: audit, Event: event, InitialTags: initial}
}

func (f initialTagFixture) projectMutation(objectID string, initial tagging.InitialAssignmentSet) projects.CreateMutation {
	audit, event := f.facts(tagging.ObjectProject, objectID, initial.Direct[0].Source == tagging.SourceSystemFallback)
	return projects.CreateMutation{Project: projects.Project{ID: projects.ProjectID(objectID), MSPID: f.msp, ClientID: f.client, DisplayID: "PRJ-" + objectID, Name: "Task 5", OriginalProposalVersionID: f.proposalVersion, LifecycleState: "planned", Version: 1, CreatedAt: f.at, CreatedBy: f.actor}, Audit: audit, Event: event, InitialTags: initial}
}

func (f initialTagFixture) assetMutation(objectID string, initial tagging.InitialAssignmentSet) clientresources.CreateMutation {
	fallback := initial.Direct[0].Source == tagging.SourceSystemFallback
	audit, event := f.facts(tagging.ObjectAsset, objectID, fallback)
	envelope := object.Envelope{ID: objectID, MSPID: f.msp, ClientID: f.client, DisplayID: "ASSET-" + objectID, LifecycleState: "active", Version: 1, CreatedAt: f.at, CreatedBy: f.actor, UpdatedAt: f.at, UpdatedBy: f.actor}
	return clientresources.CreateMutation{Kind: "asset", Object: envelope, Payload: clientresources.Asset{Envelope: envelope, Name: "Task 5", AssetType: "server", Provenance: clientresources.Provenance{SourceSystem: "task5", ExternalID: objectID, Authority: clientresources.Discovered}}, Audit: audit, Event: event, InitialTags: initial}
}

func (f initialTagFixture) articleMutation(objectID string, initial tagging.InitialAssignmentSet) knowledge.DraftMutation {
	audit, event := f.facts(tagging.ObjectKnowledgeArticle, objectID, initial.Direct[0].Source == tagging.SourceSystemFallback)
	return knowledge.DraftMutation{Created: true, Article: knowledge.Article{ID: objectID, MSPID: f.msp, ClientID: f.client, DisplayID: "KB-" + objectID, Title: "Task 5 " + objectID, State: knowledge.Draft, CurrentVersion: 1, UpdatedAt: f.at, UpdatedBy: f.actor}, Version: knowledge.Version{ArticleID: objectID, Version: 1, Body: "Task 5", State: knowledge.Draft, CreatedAt: f.at, CreatedBy: f.actor}, Audit: audit, Event: event, InitialTags: initial}
}

func (f initialTagFixture) entryMutation(objectID, workID string, initial tagging.InitialAssignmentSet) timeentries.CreateMutation {
	audit, event := f.facts(tagging.ObjectTimeEntry, objectID, initial.Direct[0].Source == tagging.SourceSystemFallback)
	return timeentries.CreateMutation{Entry: timeentries.Entry{ID: objectID, MSPID: f.msp, ClientID: f.client, WorkRecordID: workID, TechnicianID: f.technician, StartedAt: f.at, EndedAt: f.at.Add(time.Minute), DurationSeconds: 60, Version: 1, CreatedAt: f.at, CreatedBy: f.actor}, Audit: audit, Event: event, InitialTags: initial}
}

func (f initialTagFixture) assertCreated(t *testing.T, objectType tagging.ObjectType, objectID string, fallback bool) {
	t.Helper()
	table := map[tagging.ObjectType]string{
		tagging.ObjectWorkRecord:       "work_records",
		tagging.ObjectTask:             "tasks",
		tagging.ObjectProject:          "projects",
		tagging.ObjectAsset:            "assets",
		tagging.ObjectKnowledgeArticle: "knowledge_articles",
		tagging.ObjectTimeEntry:        "time_entries",
	}[objectType]
	versionColumn := "version"
	if objectType == tagging.ObjectKnowledgeArticle {
		versionColumn = "current_version"
	}
	var objectVersion int64
	if err := f.pool.QueryRow(context.Background(), `SELECT `+versionColumn+` FROM `+table+` WHERE id=$1 AND msp_id=$2 AND client_id=$3`, objectID, f.msp, f.client).Scan(&objectVersion); err != nil || objectVersion != 1 {
		t.Fatalf("%s object version=%d err=%v, want persisted version 1", objectType, objectVersion, err)
	}
	var assignmentVersion, eventVersion int64
	var assignedBy, actorType, actorID, source string
	err := f.pool.QueryRow(context.Background(), `
SELECT a.object_version, COALESCE(a.assigned_by::text,''), e.target_version, e.actor_type, COALESCE(e.actor_id::text,''), a.assignment_source
FROM object_tag_assignments a
JOIN tag_assignment_events e ON e.assignment_id=a.id
WHERE a.msp_id=$1 AND a.client_id=$2 AND a.object_type=$3 AND a.object_id=$4 AND e.operation='added'
`, f.msp, f.client, objectType.String(), objectID).Scan(&assignmentVersion, &assignedBy, &eventVersion, &actorType, &actorID, &source)
	if err != nil {
		t.Fatalf("read %s initial evidence: %v", objectType, err)
	}
	if assignmentVersion != 1 || eventVersion != 1 {
		t.Fatalf("%s versions assignment=%d event=%d, want 1", objectType, assignmentVersion, eventVersion)
	}
	if fallback {
		if assignedBy != "" || actorID != "" || actorType != "system" || source != tagging.SourceSystemFallback.String() {
			t.Fatalf("%s fallback provenance assigned_by=%q actor=%s/%q source=%q", objectType, assignedBy, actorType, actorID, source)
		}
		return
	}
	if assignedBy != f.actor || actorID != f.actor || actorType != "technician" || source != tagging.SourceHuman.String() {
		t.Fatalf("%s human provenance assigned_by=%q actor=%s/%q source=%q", objectType, assignedBy, actorType, actorID, source)
	}
}

func (f initialTagFixture) assertRollback(t *testing.T, objectID string) {
	t.Helper()
	for table, idColumn := range map[string]string{"assets": "id", "audit_ledger": "subject_id", "event_outbox": "subject_id"} {
		var count int
		if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM `+table+` WHERE `+idColumn+`=$1`, objectID).Scan(&count); err != nil || count != 0 {
			t.Fatalf("rollback %s rows=%d err=%v, want 0", table, count, err)
		}
	}
}
