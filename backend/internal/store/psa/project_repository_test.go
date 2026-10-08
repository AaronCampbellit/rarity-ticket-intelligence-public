package psa

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/projects"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tasks"
)

func projectReferenceScan(id, displayID, name string) func(...any) {
	return func(destinations ...any) {
		*destinations[0].(*string) = id
		*destinations[1].(*string) = displayID
		*destinations[2].(*string) = name
	}
}

func TestFindProjectsByReferenceIsExactDisplayIDFirstAndSQLBounded(t *testing.T) {
	db := &fakeSalesDB{queryRows: &fakeRows{scans: []func(...any){
		projectReferenceScan("name-match", "PRJ-OTHER", " prj-\u00a0501 "),
		projectReferenceScan("project-id", " prj-\u00a0501 ", "Modernization"),
	}}}
	found, err := NewProjectRepository(db).FindProjectsByReference(
		context.Background(),
		scope.Target{MSPID: "msp", ClientID: "client"},
		"\u2003PRJ-\u2003501\u2003", 2,
	)
	if err != nil || len(found) != 1 || found[0].ID != "project-id" {
		t.Fatalf("found=%+v error=%v", found, err)
	}
	for _, fragment := range []string{
		"translate(", "regexp_replace(", "[[:space:]]+",
		"CASE WHEN", "LIMIT $5",
	} {
		if !strings.Contains(db.query, fragment) {
			t.Fatalf("exact project query missing %q: %s", fragment, db.query)
		}
	}
	if len(db.args) != 5 || db.args[2] != "prj- 501" || db.args[4] != 2 {
		t.Fatalf("query args=%#v", db.args)
	}
}

func TestFindProjectsByReferenceReturnsTwoNormalizedNameMatchesForAmbiguity(t *testing.T) {
	db := &fakeSalesDB{queryRows: &fakeRows{scans: []func(...any){
		projectReferenceScan("project-b", "PRJ-B", "\u00a0VPN\u2003Rollout "),
		projectReferenceScan("project-a", "PRJ-A", " vpn rollout"),
	}}}
	found, err := NewProjectRepository(db).FindProjectsByReference(
		context.Background(),
		scope.Target{MSPID: "msp", ClientID: "client"},
		"VPN\tROLLOUT", 2,
	)
	if err != nil || len(found) != 2 {
		t.Fatalf("found=%+v error=%v", found, err)
	}
}

func TestCreateCostActualAtomicWritesCostAndFacts(t *testing.T) {
	tx := &fakeSalesTx{}
	repository := NewProjectRepository(&fakeSalesDB{tx: tx})
	at := time.Date(2026, time.July, 30, 18, 0, 0, 0, time.UTC)
	err := repository.CreateCostActualAtomic(context.Background(), projects.CostActualMutation{
		Cost: projects.CostActual{
			ID: "cost", ProjectID: "project", PhaseID: "phase",
			MSPID: "msp", ClientID: "client", CostType: "license",
			Description: "Security license",
			Amount:      projects.Money{Minor: 250000, Currency: "USD"},
			Committed:   true, IncurredAt: at, Version: 1,
		},
		Audit: validAudit(at, "project.cost_actual.created", "cost_actual", "cost"),
		Event: validEvent(at, "project.cost_actual.created", "cost_actual", "cost"),
	})
	if err != nil {
		t.Fatalf("CreateCostActualAtomic() error = %v", err)
	}
	assertQueryOrder(t, tx.queries,
		"INSERT INTO cost_actuals", "INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
}

func TestCreateAvailabilityAtomicValidatesTechnicianAndRejectsOverlapBeforeWriting(t *testing.T) {
	at := time.Date(2026, time.August, 3, 14, 0, 0, 0, time.UTC)
	tx := &fakeSalesTx{queryRows: []row{
		fakeRow{scan: func(destinations ...any) {
			*destinations[0].(*string) = "technician"
		}},
		fakeRow{err: pgx.ErrNoRows},
	}}
	repository := NewProjectRepository(&fakeSalesDB{tx: tx})
	err := repository.CreateAvailabilityAtomic(
		context.Background(),
		projects.AvailabilityMutation{
			Availability: projects.TechnicianAvailability{
				ID: "availability", MSPID: "msp", TechnicianID: "technician",
				StartsAt: at, EndsAt: at.Add(8 * time.Hour),
				AvailableMinutes: 480, Version: 1,
			},
			Audit: validAudit(
				at, "technician.availability.created",
				"technician_availability", "availability",
			),
			Event: validEvent(
				at, "technician.availability.created",
				"technician_availability", "availability",
			),
		},
	)
	if err != nil {
		t.Fatalf("CreateAvailabilityAtomic() error = %v", err)
	}
	if len(tx.calls) < 5 ||
		!strings.Contains(tx.calls[0], "FROM technicians") ||
		!strings.Contains(tx.calls[1], "FROM technician_availability_windows") {
		t.Fatalf("calls=%v", tx.calls)
	}
	assertQueryOrder(
		t, tx.queries,
		"INSERT INTO technician_availability_windows",
		"INSERT INTO audit_ledger",
		"INSERT INTO event_outbox",
	)
}

func TestCreateLaborCostRateAtomicValidatesTechnicianThenWritesFacts(t *testing.T) {
	at := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	tx := &fakeSalesTx{queryRow: fakeRow{scan: func(destinations ...any) {
		*destinations[0].(*string) = "technician"
	}}}
	err := NewProjectRepository(&fakeSalesDB{tx: tx}).
		CreateLaborCostRateAtomic(
			context.Background(),
			projects.LaborCostRateMutation{
				Rate: projects.TechnicianLaborCostRate{
					ID: "rate", MSPID: "msp", TechnicianID: "technician",
					HourlyRate:  projects.Money{Minor: 8500, Currency: "USD"},
					EffectiveAt: at, Version: 1,
				},
				Audit: validAudit(
					at, "technician.labor_cost_rate.created",
					"technician_labor_cost_rate", "rate",
				),
				Event: validEvent(
					at, "technician.labor_cost_rate.created",
					"technician_labor_cost_rate", "rate",
				),
			},
		)
	if err != nil {
		t.Fatalf("CreateLaborCostRateAtomic() error = %v", err)
	}
	if len(tx.calls) < 4 || !strings.Contains(tx.calls[0], "FROM technicians") {
		t.Fatalf("calls=%v", tx.calls)
	}
	assertQueryOrder(
		t, tx.queries,
		"INSERT INTO technician_labor_cost_rates",
		"INSERT INTO audit_ledger",
		"INSERT INTO event_outbox",
	)
}

func TestRecognizeBillableWorkAtomicWritesRecognitionAndFacts(t *testing.T) {
	at := time.Date(2026, time.August, 15, 18, 0, 0, 0, time.UTC)
	tx := &fakeSalesTx{}
	err := NewProjectRepository(&fakeSalesDB{tx: tx}).
		RecognizeBillableWorkAtomic(
			context.Background(),
			projects.BillableWorkMutation{
				Recognition: projects.BillableWorkRecognition{
					ID: "recognition", ProjectID: "project", PhaseID: "phase",
					MSPID: "msp", ClientID: "client",
					Description:  "Accepted milestone",
					Amount:       projects.Money{Minor: 1200000, Currency: "USD"},
					RecognizedAt: at, Version: 1,
				},
				Audit: validAudit(
					at, "project.billable_work.recognized",
					"recognized_billable_work", "recognition",
				),
				Event: validEvent(
					at, "project.billable_work.recognized",
					"recognized_billable_work", "recognition",
				),
			},
		)
	if err != nil {
		t.Fatalf("RecognizeBillableWorkAtomic() error = %v", err)
	}
	assertQueryOrder(
		t, tx.queries,
		"INSERT INTO recognized_billable_work",
		"INSERT INTO audit_ledger",
		"INSERT INTO event_outbox",
	)
}

func TestLoadFinancialInputsUsesBaselinesAndPersistedProjectCosts(t *testing.T) {
	db := &fakeSalesDB{queryRow: fakeRow{scan: func(destinations ...any) {
		*destinations[0].(*string) = "USD"
		*destinations[1].(*int64) = 20000
		*destinations[2].(*int64) = 24000
		*destinations[3].(*int64) = 7000
		*destinations[4].(*int64) = 6000
		*destinations[5].(*int64) = 2000
		*destinations[6].(*int64) = 1000
		*destinations[7].(*int64) = 16000
		*destinations[8].(*bool) = true
		*destinations[9].(*bool) = false
	}}}
	inputs, err := NewProjectRepository(db).LoadFinancialInputs(
		context.Background(),
		scope.Target{MSPID: "msp", ClientID: "client"},
		"project",
	)
	if err != nil {
		t.Fatalf("LoadFinancialInputs() error = %v", err)
	}
	if inputs.OriginalBudget.Minor != 20000 ||
		inputs.CurrentBudget.Minor != 24000 ||
		inputs.PlannedLabor.Minor != 7000 ||
		inputs.ActualLabor.Minor != 6000 ||
		inputs.CostActuals.Minor != 2000 ||
		inputs.CommittedCost.Minor != 1000 ||
		inputs.BillableWork.Minor != 16000 ||
		!inputs.ActualLaborComplete {
		t.Fatalf("inputs=%+v", inputs)
	}
	if !strings.Contains(db.query, "FROM project_budgets") ||
		!strings.Contains(db.query, "FROM cost_actuals") ||
		!strings.Contains(db.query, "technician_labor_cost_rates") ||
		!strings.Contains(db.query, "recognized_billable_work") {
		t.Fatalf("query=%s", db.query)
	}
}

func TestLoadPhaseFinancialInputsUsesOnlyPhaseAttributedEvidence(t *testing.T) {
	db := &fakeSalesDB{queryRows: &fakeRows{scans: []func(...any){
		func(destinations ...any) {
			*destinations[0].(*projects.PhaseID) = "phase"
			*destinations[1].(*string) = "USD"
			*destinations[2].(*int64) = 10000
			*destinations[3].(*int64) = 12000
			*destinations[4].(*int64) = 4000
			*destinations[5].(*int64) = 3000
			*destinations[6].(*int64) = 1000
			*destinations[7].(*int64) = 500
			*destinations[8].(*int64) = 9000
			*destinations[9].(*bool) = true
			*destinations[10].(*bool) = false
		},
	}}}
	found, err := NewProjectRepository(db).LoadPhaseFinancialInputs(
		context.Background(),
		scope.Target{MSPID: "msp", ClientID: "client"},
		"project",
	)
	if err != nil {
		t.Fatalf("LoadPhaseFinancialInputs() error = %v", err)
	}
	if found["phase"].ActualLabor.Minor != 3000 ||
		found["phase"].BillableWork.Minor != 9000 ||
		!found["phase"].ActualLaborComplete {
		t.Fatalf("found=%+v", found)
	}
	if !strings.Contains(db.query, "recognized.phase_id = phase.id") ||
		!strings.Contains(db.query, "cost.phase_id = phase.id") {
		t.Fatalf("query=%s", db.query)
	}
}

func TestLoadCapacityAggregatesAvailabilityAssignmentsAndActualTimeByMSP(t *testing.T) {
	const (
		mspID  = "00000000-0000-4000-8000-000000000901"
		techID = "00000000-0000-4000-8000-000000000902"
	)
	db := &capacityBridgeDB{mspID: mspID, technicianID: techID}
	window := projects.CapacityWindow{
		Start: time.Date(2026, time.August, 3, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2026, time.August, 10, 0, 0, 0, 0, time.UTC),
	}
	rows, err := NewProjectRepository(db).LoadCapacity(
		context.Background(),
		scope.Target{MSPID: mspID, ClientID: "00000000-0000-4000-8000-000000000903"},
		window,
		[]string{techID},
	)
	if err != nil {
		t.Fatalf("LoadCapacity() error = %v", err)
	}
	if len(rows) != 1 || rows[0].Name != "Alex Morgan" ||
		rows[0].Available != 40*time.Hour ||
		rows[0].Scheduled != 48*time.Hour ||
		rows[0].Actual != 12*time.Hour {
		t.Fatalf("rows=%+v", rows)
	}
	joined := strings.Join(db.queries, "\n")
	if !strings.Contains(joined, "technician_schedule_versions") ||
		!strings.Contains(joined, "calendar_event_projections") ||
		!strings.Contains(joined, "time_entries") {
		t.Fatalf("queries=%s", joined)
	}
	if strings.Contains(joined, "client_id=$") && !strings.Contains(joined, "NULLIF($5,'')::uuid IS NULL") {
		t.Fatalf("capacity aggregation accidentally narrowed to the active client: %s", joined)
	}
}

type capacityBridgeDB struct {
	mspID, technicianID string
	queries             []string
}

func (d *capacityBridgeDB) Begin(context.Context) (transaction, error) {
	return nil, errors.New("unexpected transaction")
}
func (d *capacityBridgeDB) QueryRow(context.Context, string, ...any) row {
	return fakeRow{err: errors.New("unexpected QueryRow")}
}
func (d *capacityBridgeDB) Query(_ context.Context, query string, _ ...any) (rows, error) {
	d.queries = append(d.queries, query)
	switch {
	case strings.Contains(query, "FROM technician_schedule_versions"):
		scans := []func(...any){}
		for weekday := time.Monday; weekday <= time.Friday; weekday++ {
			day := weekday
			scans = append(scans, func(v ...any) {
				*(v[0].(*string)) = "00000000-0000-4000-8000-000000000904"
				*(v[1].(*string)) = d.mspID
				*(v[2].(*string)) = d.technicianID
				*(v[3].(*string)) = "UTC"
				*(v[4].(*time.Time)) = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
				*(v[5].(**time.Time)) = nil
				*(v[6].(*int64)) = 1
				*(v[7].(*time.Time)) = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
				*(v[8].(*string)) = d.technicianID
				*(v[9].(*string)) = fmt.Sprintf("00000000-0000-4000-8000-%012d", 910+int(day))
				*(v[10].(*int)) = int(day)
				*(v[11].(*int)) = 9 * 60
				*(v[12].(*int)) = 17 * 60
				*(v[13].(*int)) = 100
			})
		}
		return &fakeRows{scans: scans}, nil
	case strings.Contains(query, "technician_schedule_exceptions"), strings.Contains(query, "FROM pto_requests"), strings.Contains(query, "FROM maintenance_windows"):
		return &fakeRows{}, nil
	case strings.Contains(query, "FROM calendar_dependencies"):
		return &fakeRows{}, nil
	case strings.Contains(query, "FROM calendar_event_projections"):
		start := time.Date(2026, time.August, 3, 0, 0, 0, 0, time.UTC)
		end := start.Add(48 * time.Hour)
		return &fakeRows{scans: []func(...any){func(v ...any) {
			*(v[0].(*string)) = "00000000-0000-4000-8000-000000000920"
			*(v[1].(*string)) = "00000000-0000-4000-8000-000000000921"
			*(v[2].(*string)) = "task"
			*(v[3].(*string)) = "00000000-0000-4000-8000-000000000922"
			*(v[4].(*string)) = "scheduled_work"
			*(v[5].(*int64)) = 1
			*(v[6].(*string)) = "Long scheduled task"
			*(v[7].(**time.Time)) = &start
			*(v[8].(**time.Time)) = &end
			*(v[9].(*string)) = "UTC"
			*(v[10].(*calendar.SchedulingMode)) = calendar.FixedBlock
			*(v[11].(*int64)) = 48 * 60
			*(v[12].(*string)) = d.technicianID
			*(v[13].(*[]byte)) = nil
		}}}, nil
	case strings.Contains(query, "FROM requested LEFT JOIN actual"):
		return &fakeRows{scans: []func(...any){func(v ...any) {
			*(v[0].(*string)) = d.technicianID
			*(v[1].(*string)) = "Alex Morgan"
			*(v[2].(*int64)) = 720
		}}}, nil
	}
	return nil, fmt.Errorf("unexpected query: %s", query)
}

func TestCreateProjectAtomicWritesPhasesTeamsAndFacts(t *testing.T) {
	tx := &fakeSalesTx{}
	repository := NewProjectRepository(&fakeSalesDB{tx: tx})
	at := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)

	err := repository.CreateAtomic(context.Background(), projects.CreateMutation{
		Project: projects.Project{
			ID: "project", MSPID: "msp", ClientID: "client", DisplayID: "PRJ-1",
			Name: "Modernization", OriginalProposalVersionID: "proposal-version",
			LifecycleState: "planned", Version: 1, CreatedAt: at, CreatedBy: "actor",
			Phases: []projects.Phase{{
				ID: "phase", ProjectID: "project", MSPID: "msp", ClientID: "client",
				Position: 1, Name: "Discovery", State: "planned",
				ParticipatingTeams: []string{"team-1", "team-2"},
				PlannedMinutes:     120, Budget: projects.Money{Minor: 50000, Currency: "USD"},
				Deliverables: []string{"assessment"}, CompletionCriteria: []string{"approved"},
				Version: 1,
			}},
		},
		Audit:       validAudit(at, "project.created", "project", "project"),
		Event:       validEvent(at, "project.created", "project", "project"),
		InitialTags: testInitialTags(at),
	})

	if err != nil {
		t.Fatalf("CreateAtomic() error = %v", err)
	}
	assertQueryOrder(t, tx.queries,
		"INSERT INTO projects", "INSERT INTO phases",
		"INSERT INTO phase_participating_teams", "INSERT INTO phase_participating_teams",
		"INSERT INTO object_tag_assignments", "INSERT INTO tag_assignment_events",
		"INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
}

func TestCreateAIWorkspaceProjectAtomicWritesProjectTasksAndFactsTogether(t *testing.T) {
	tx := &fakeSalesTx{}
	repository := NewProjectRepository(&fakeSalesDB{tx: tx})
	at := time.Date(2026, time.August, 4, 18, 30, 0, 0, time.UTC)
	project := projects.Project{
		ID: "project", MSPID: "msp", ClientID: "client", DisplayID: "PRJ-1",
		Name: "Onboarding", LifecycleState: "planned", Version: 1,
		CreatedAt: at, CreatedBy: "actor",
	}
	projectTasks := []tasks.Task{
		{
			ID: "task-a", MSPID: "msp", ClientID: "client",
			Parent: tasks.Ref{Type: tasks.ParentProject, ID: "project"},
			Title:  "A", Status: "open", Position: 1, Version: 1,
			CreatedBy: "actor",
		},
		{
			ID: "task-b", MSPID: "msp", ClientID: "client",
			Parent: tasks.Ref{Type: tasks.ParentProject, ID: "project"},
			Title:  "B", Status: "open", Position: 2, Version: 1,
			CreatedBy: "actor",
		},
	}
	audits := []mutation.AuditRecord{
		validAudit(at, "project.created", "project", "project"),
		validAudit(at, "task.created", "task", "task-a"),
		validAudit(at, "task.created", "task", "task-b"),
	}
	events := []mutation.EventRecord{
		validEvent(at, "project.created", "project", "project"),
		validEvent(at, "task.created", "task", "task-a"),
		validEvent(at, "task.created", "task", "task-b"),
	}

	err := repository.CreateAIWorkspaceProjectAtomic(
		context.Background(),
		projects.AIWorkspaceCreateMutation{
			Project: project, Tasks: projectTasks, Audits: audits, Events: events,
			InitialTags: testInitialTags(at),
		},
	)
	if err != nil {
		t.Fatalf("CreateAIWorkspaceProjectAtomic() error = %v", err)
	}
	assertQueryOrder(t, tx.queries,
		"FROM client_organizations", "INSERT INTO projects",
		"INSERT INTO tasks", "INSERT INTO tasks",
		"INSERT INTO object_tag_assignments", "INSERT INTO tag_assignment_events",
		"INSERT INTO audit_ledger", "INSERT INTO event_outbox",
		"INSERT INTO audit_ledger", "INSERT INTO event_outbox",
		"INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
	if !strings.Contains(tx.queries[1], "NULLIF($6, '')::uuid") {
		t.Fatalf("project insert must preserve an absent proposal as NULL: %s", tx.queries[1])
	}
}

func TestFindProjectRepresentsMissingProposalProvenanceAsEmpty(t *testing.T) {
	db := &fakeSalesDB{queryRow: fakeRow{err: pgx.ErrNoRows}}
	_, _ = NewProjectRepository(db).FindProject(
		context.Background(),
		scope.Target{MSPID: "msp", ClientID: "client"},
		"project",
	)
	if !strings.Contains(
		db.query,
		"COALESCE(original_proposal_version_id::text, '')",
	) {
		t.Fatalf("query must safely scan direct projects: %s", db.query)
	}
}

func TestInsertPhasePersistsEmptyDeliveryCollectionsAsJSONArrays(t *testing.T) {
	tx := &fakeSalesTx{}
	err := insertPhase(context.Background(), tx, projects.Phase{
		ID: "phase", ProjectID: "project", MSPID: "msp", ClientID: "client",
		Position: 1, Name: "Delivery", State: "planned",
		PlannedMinutes: 120, Budget: projects.Money{Minor: 50000, Currency: "USD"},
		Version: 1,
	})
	if err != nil {
		t.Fatalf("insertPhase() error = %v", err)
	}
	if got := string(tx.args[0][15].([]byte)); got != "[]" {
		t.Fatalf("deliverables = %s, want []", got)
	}
	if got := string(tx.args[0][16].([]byte)); got != "[]" {
		t.Fatalf("completion criteria = %s, want []", got)
	}
}

func TestFindPhaseTeamsReturnsEmptyArrayWhenNoTeamsAreAssigned(t *testing.T) {
	db := &fakeSalesDB{queryRows: &fakeRows{}}
	teams, err := NewProjectRepository(db).findPhaseTeams(
		context.Background(),
		scope.Target{MSPID: "msp", ClientID: "client"},
		"phase",
	)
	if err != nil {
		t.Fatalf("findPhaseTeams() error = %v", err)
	}
	if teams == nil || len(teams) != 0 {
		t.Fatalf("teams = %#v, want non-nil empty array", teams)
	}
}

func TestCreateResourcePlanAtomicWritesPlanAndFacts(t *testing.T) {
	tx := &fakeSalesTx{}
	repository := NewProjectRepository(&fakeSalesDB{tx: tx})
	at := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	err := repository.CreateResourcePlanAtomic(
		context.Background(),
		projects.ResourcePlanMutation{
			Plan: projects.ResourcePlan{
				ID: "plan", ProjectID: "project", PhaseID: "phase",
				MSPID: "msp", ClientID: "client", TeamID: "team",
				StartsOn: at, EndsOn: at.AddDate(0, 0, 5),
				PlannedMinutes: 1200, Version: 1,
			},
			Audit: validAudit(at, "resource_plan.created", "resource_plan", "plan"),
			Event: validEvent(at, "resource_plan.created", "resource_plan", "plan"),
		},
	)
	if err != nil {
		t.Fatalf("CreateResourcePlanAtomic() error=%v", err)
	}
	assertQueryOrder(t, tx.queries,
		"INSERT INTO resource_plans", "INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
}

func TestUpdatePhaseAtomicUsesOptimisticVersionAndReplacesTeams(t *testing.T) {
	tx := &fakeSalesTx{}
	repository := NewProjectRepository(&fakeSalesDB{tx: tx})
	at := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)

	err := repository.UpdatePhaseAtomic(context.Background(), projects.PhaseMutation{
		Phase: projects.Phase{
			ID: "phase", ProjectID: "project", MSPID: "msp", ClientID: "client",
			Name: "Delivery", Position: 1, State: "planned", Version: 3,
			ParticipatingTeams: []string{"team-1"},
		},
		Audit: validAudit(at, "phase.updated", "phase", "phase"),
		Event: validEvent(at, "phase.updated", "phase", "phase"),
	})

	if err != nil {
		t.Fatalf("UpdatePhaseAtomic() error = %v", err)
	}
	assertQueryOrder(t, tx.queries,
		"UPDATE phases", "DELETE FROM phase_participating_teams",
		"INSERT INTO phase_participating_teams", "INSERT INTO audit_ledger",
		"INSERT INTO event_outbox",
	)
	if !strings.Contains(tx.queries[0], "version = $5") {
		t.Fatalf("phase update lacks optimistic version predicate: %s", tx.queries[0])
	}
}

func TestUpdatePhaseAtomicReturnsVersionConflictBeforeFacts(t *testing.T) {
	tx := &fakeSalesTx{zeroRowsAt: 1}
	repository := NewProjectRepository(&fakeSalesDB{tx: tx})

	err := repository.UpdatePhaseAtomic(context.Background(), projects.PhaseMutation{
		Phase: projects.Phase{
			ID: "phase", ProjectID: "project", MSPID: "msp", ClientID: "client",
			Name: "Delivery", Version: 2,
		},
	})

	if !errors.Is(err, object.ErrVersionConflict) {
		t.Fatalf("UpdatePhaseAtomic() error = %v", err)
	}
	if len(tx.queries) != 1 || tx.committed || !tx.rolledBack {
		t.Fatalf("stale phase update wrote facts or committed: %+v", tx)
	}
}

func TestFindPhaseIsClientScoped(t *testing.T) {
	db := &fakeSalesDB{queryRow: fakeRow{err: errors.New("stop after query capture")}}
	repository := NewProjectRepository(db)

	_, _ = repository.FindPhase(
		context.Background(),
		scope.Target{MSPID: "msp", ClientID: "client"},
		"phase",
	)

	if !strings.Contains(db.query, "p.client_id = $3") {
		t.Fatalf("phase lookup is not client scoped: %s", db.query)
	}
}
