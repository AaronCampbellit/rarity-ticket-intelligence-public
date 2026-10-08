package adapters

import (
	"context"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/commitments"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/customfields"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/projects"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workforce"
)

type workLoaderStub struct {
	work WorkRecordSource
	task TaskSource
}

type revisionWorkLoader struct {
	workLoaderStub
	revision int64
}

func (s revisionWorkLoader) ResolveCalendarProjectionRevision(context.Context, calendar.SourceRef, int64) (int64, error) {
	return s.revision, nil
}

func (s workLoaderStub) LoadWorkRecord(context.Context, calendar.SourceRef) (WorkRecordSource, error) {
	return s.work, nil
}
func (s workLoaderStub) LoadTask(context.Context, calendar.SourceRef) (TaskSource, error) {
	return s.task, nil
}

func TestWorkRecordAdapterSeparatesScheduleDatesAndSLARoles(t *testing.T) {
	start := time.Date(2026, 8, 10, 13, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	due := time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC)
	follow := due.AddDate(0, 0, 1)
	response := start.Add(2 * time.Hour)
	resolution := start.Add(4 * time.Hour)
	adapter := NewWorkRecordAdapter(workLoaderStub{work: WorkRecordSource{MSPID: "msp", ClientID: "client", ID: "work", Title: "Printer down", RecordType: "incident", TeamID: "team", ServiceID: "service", Priority: "urgent", SLAID: "sla", Version: 7, OwnerID: "tech", PlannedMinutes: 60, SchedulingMode: calendar.FixedBlock, ScheduledStartsAt: &start, ScheduledEndsAt: &end, Timezone: "America/New_York", DueOn: &due, FollowUpOn: &follow, SLAResponseDueAt: &response, SLAResolutionDueAt: &resolution}})
	got, err := adapter.Project(context.Background(), calendar.SourceRef{MSPID: "msp", ClientID: "client", Type: "work_record", ID: "work"})
	if err != nil {
		t.Fatal(err)
	}
	assertRoles(t, got, "due", "follow_up", "scheduled_work", "sla_resolution_deadline", "sla_response_deadline")
	for _, projection := range got {
		if projection.EventRole == "scheduled_work" && (!projection.CapacityBearing || projection.AssigneeID != "tech" || projection.PlannedMinutes != 60) {
			t.Fatalf("scheduled projection = %+v", projection)
		}
		if len(projection.Dimensions.TechnicianIDs) != 1 || projection.Dimensions.TechnicianIDs[0] != "tech" || len(projection.Dimensions.TicketTypes) != 1 || projection.Dimensions.TicketTypes[0] != "incident" || len(projection.Dimensions.TeamIDs) != 1 || projection.Dimensions.TeamIDs[0] != "team" || len(projection.Dimensions.TechnologyIDs) != 1 || projection.Dimensions.TechnologyIDs[0] != "service" {
			t.Fatalf("work dimensions=%+v", projection.Dimensions)
		}
		if projection.EventRole[:3] == "sla" && (projection.SchedulingMode != calendar.Informational || projection.CapacityBearing) {
			t.Fatalf("SLA projection is schedulable: %+v", projection)
		}
	}
}

func TestTaskDueCarriesOwnerDimension(t *testing.T) {
	due := time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC)
	source := TaskSource{MSPID: "msp", ClientID: "client", ID: "task", Title: "Task", OwnerID: "tech", Version: 2, DueOn: &due}
	got, err := NewTaskAdapter(workLoaderStub{task: source}).Project(context.Background(), calendar.SourceRef{MSPID: "msp", ClientID: "client", Type: "task", ID: "task"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || len(got[0].Dimensions.TechnicianIDs) != 1 || got[0].Dimensions.TechnicianIDs[0] != "tech" {
		t.Fatalf("due=%+v", got)
	}
}

func TestWorkAdapterUsesRevisionCoveringClassificationAndSLAInputs(t *testing.T) {
	due := time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC)
	loader := revisionWorkLoader{workLoaderStub: workLoaderStub{work: WorkRecordSource{MSPID: "msp", ClientID: "client", ID: "work", Title: "Work", Version: 2, DueOn: &due}}, revision: 9}
	got, err := NewWorkRecordAdapter(loader).Project(context.Background(), calendar.SourceRef{MSPID: "msp", ClientID: "client", Type: "work_record", ID: "work"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].SourceRevision != 9 {
		t.Fatalf("projection revision=%+v", got)
	}
}

func TestScheduledWorkNeedsAssignmentAndPositiveEffortForCapacity(t *testing.T) {
	start := time.Date(2026, 8, 10, 13, 0, 0, 0, time.UTC)
	for _, source := range []WorkRecordSource{{MSPID: "msp", ClientID: "client", ID: "work", Title: "Work", Version: 1, ScheduledStartsAt: &start, Timezone: "UTC", SchedulingMode: calendar.FixedBlock, PlannedMinutes: 30}, {MSPID: "msp", ClientID: "client", ID: "work", Title: "Work", Version: 1, ScheduledStartsAt: &start, Timezone: "UTC", SchedulingMode: calendar.FixedBlock, OwnerID: "tech"}} {
		got, err := NewWorkRecordAdapter(workLoaderStub{work: source}).Project(context.Background(), calendar.SourceRef{MSPID: "msp", ClientID: "client", Type: "work_record", ID: "work"})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].CapacityBearing {
			t.Fatalf("projection = %+v", got)
		}
	}
}

type projectLoaderStub struct {
	project      projects.Project
	phase        projects.Phase
	milestone    projects.Milestone
	resourcePlan projects.ResourcePlan
}

func (s projectLoaderStub) LoadProject(context.Context, calendar.SourceRef) (projects.Project, error) {
	return s.project, nil
}
func (s projectLoaderStub) LoadPhase(context.Context, calendar.SourceRef) (projects.Phase, error) {
	return s.phase, nil
}
func (s projectLoaderStub) LoadMilestone(context.Context, calendar.SourceRef) (projects.Milestone, error) {
	return s.milestone, nil
}
func (s projectLoaderStub) LoadResourcePlan(context.Context, calendar.SourceRef) (projects.ResourcePlan, error) {
	return s.resourcePlan, nil
}

func TestResourcePlanAdapterProjectsAndPreparesTypedAllocation(t *testing.T) {
	start := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 5)
	plan := projects.ResourcePlan{ID: "plan", ProjectID: "project", MSPID: "msp", ClientID: "client", TeamID: "team", StartsOn: start, EndsOn: end, PlannedMinutes: 1200, Version: 3}
	adapter := NewResourcePlanAdapter(projectLoaderStub{resourcePlan: plan})
	ref := calendar.SourceRef{MSPID: "msp", ClientID: "client", Type: "resource_plan", ID: "plan"}
	projected, err := adapter.Project(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(projected) != 1 || projected[0].EventRole != "allocation" || projected[0].SchedulingMode != calendar.EffortAllocation || projected[0].PlannedMinutes != 1200 {
		t.Fatalf("projection=%+v", projected)
	}
	newStart, newEnd := start.AddDate(0, 0, 1), end.AddDate(0, 0, 1)
	prepared, err := adapter.Prepare(context.Background(), authorization.Principal{Scope: scope.Principal{MSPID: "msp"}, Capabilities: authorization.NewCapabilitySet("project.edit")}, calendar.RequestedChange{ProjectionID: projected[0].ID, Source: ref, EventRole: "allocation", SourceRevision: 3, AllDay: true, StartsOn: &newStart, EndsOn: &newEnd})
	if err != nil {
		t.Fatal(err)
	}
	mutation, ok := prepared.Mutation.(projects.ScheduleMutation)
	if !ok || mutation.SourceType != "resource_plan" || mutation.SourceID != "plan" || mutation.Interval.StartsOn == nil || !mutation.Interval.StartsOn.Equal(newStart) {
		t.Fatalf("prepared=%+v", prepared)
	}
}

type workforceLoaderStub struct {
	schedule workforce.Schedule
	pto      workforce.PTORequest
}

func (s workforceLoaderStub) LoadSchedule(context.Context, calendar.SourceRef) (workforce.Schedule, error) {
	return s.schedule, nil
}
func (s workforceLoaderStub) LoadPTO(context.Context, calendar.SourceRef) (workforce.PTORequest, error) {
	return s.pto, nil
}

func TestPTOProjectionNeverCopiesPrivateTypeOrReason(t *testing.T) {
	start := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 2)
	source := workforce.PTORequest{ID: "pto", MSPID: "msp", TechnicianID: "tech", PTOType: "sick", DecisionReason: "private medical details", StartsOn: &start, EndsOn: &end, AllDay: true, State: workforce.Approved, Version: 3}
	got, err := NewPTOAdapter(workforceLoaderStub{pto: source}).Project(context.Background(), calendar.SourceRef{MSPID: "msp", Type: "pto", ID: "pto"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "Unavailable" {
		t.Fatalf("PTO projection leaked detail: %+v", got)
	}
}

func TestScheduleAdapterProjectsEveryWeeklyWindowInAuthoritativeTimezone(t *testing.T) {
	from := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	through := from.AddDate(0, 1, 0)
	source := workforce.Schedule{ID: "schedule", MSPID: "msp", TechnicianID: "tech", Timezone: "America/New_York", EffectiveFrom: from, EffectiveThrough: &through, Version: 4, Windows: []workforce.WeeklyWindow{{ID: "monday", Weekday: time.Monday, StartsMinute: 9 * 60, EndsMinute: 17 * 60, CapacityPercent: 100}, {ID: "tuesday", Weekday: time.Tuesday, StartsMinute: 10 * 60, EndsMinute: 14 * 60, CapacityPercent: 80}}}
	got, err := NewScheduleAdapter(workforceLoaderStub{schedule: source}).Project(context.Background(), calendar.SourceRef{MSPID: "msp", Type: "technician_schedule", ID: "schedule"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("projections=%+v", got)
	}
	for _, p := range got {
		if p.EventRole != "availability" || p.SourceRoleKey == "" || p.Timezone != "America/New_York" || p.Recurrence == nil || p.Recurrence.Frequency != calendar.Weekly {
			t.Fatalf("availability=%+v", p)
		}
	}
}

type commitmentLoaderStub struct {
	maintenance commitments.MaintenanceWindow
	commercial  commitments.CommercialCommitment
}

func (s commitmentLoaderStub) LoadMaintenance(context.Context, calendar.SourceRef) (commitments.MaintenanceWindow, error) {
	return s.maintenance, nil
}
func (s commitmentLoaderStub) LoadCommercial(context.Context, calendar.SourceRef) (commitments.CommercialCommitment, error) {
	return s.commercial, nil
}

func TestCommercialAdapterProjectsOnlyInformationalDates(t *testing.T) {
	effective := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	notice := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	expiry := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
	source := commitments.CommercialCommitment{ID: "commercial", MSPID: "msp", ClientID: "client", Title: "M365", Version: 2, EffectiveOn: effective, NoticeOn: notice, ExpirationOn: expiry, CommercialLinks: commitments.CommercialLinks{ServiceID: "service"}}
	got, err := NewCommercialAdapter(commitmentLoaderStub{commercial: source}).Project(context.Background(), calendar.SourceRef{MSPID: "msp", ClientID: "client", Type: "commercial_commitment", ID: "commercial"})
	if err != nil {
		t.Fatal(err)
	}
	assertRoles(t, got, "effective", "expiration", "notice")
	for _, p := range got {
		if p.SchedulingMode != calendar.Informational || p.CapacityBearing {
			t.Fatalf("commercial projection is schedulable: %+v", p)
		}
		if len(p.Dimensions.TechnologyIDs) != 1 || p.Dimensions.TechnologyIDs[0] != "service" {
			t.Fatalf("commercial dimensions=%+v", p.Dimensions)
		}
	}
}

func TestMaintenanceServiceScopesCarryTechnologyDimensions(t *testing.T) {
	start := time.Date(2026, 8, 10, 13, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	source := commitments.MaintenanceWindow{ID: "maintenance", MSPID: "msp", Title: "Maintenance", StartsAt: start, EndsAt: end, Timezone: "UTC", Version: 1, Scopes: []commitments.ResolvedScope{{ID: "scope", ClientID: "client", Type: commitments.ScopeService, ResourceID: "service"}}}
	got, err := NewMaintenanceAdapter(commitmentLoaderStub{maintenance: source}).Project(context.Background(), calendar.SourceRef{MSPID: "msp", Type: "maintenance_window", ID: "maintenance"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || len(got[0].Dimensions.TechnologyIDs) != 1 || got[0].Dimensions.TechnologyIDs[0] != "service" {
		t.Fatalf("maintenance=%+v", got)
	}
}

type customLoaderStub struct {
	definition customfields.DateDefinition
	value      customfields.DateValue
}

type customCapacityLoaderStub struct {
	customLoaderStub
	assignee string
	minutes  int64
}

func (s customCapacityLoaderStub) LoadCustomCapacity(context.Context, customfields.DateValue) (string, int64, error) {
	return s.assignee, s.minutes, nil
}

func (s customLoaderStub) LoadCustomDate(context.Context, calendar.SourceRef) (customfields.DateDefinition, customfields.DateValue, error) {
	return s.definition, s.value, nil
}

func TestCustomDateAdapterUsesConfiguredRoleAndStableFieldIdentity(t *testing.T) {
	date := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	def := customfields.DateDefinition{ID: "field", MSPID: "msp", InternalKey: "warranty", Label: "Warranty expiration", ObjectType: customfields.ObjectAsset, ValueKind: customfields.DateKind, LifecycleState: "active", Version: 2}
	value := customfields.DateValue{ID: "value", FieldID: "field", MSPID: "msp", ClientID: "client", ObjectID: "asset", ObjectType: customfields.ObjectAsset, SourceRevision: 5, Version: 3, DateValue: &date}
	got, err := NewCustomDateAdapter(customLoaderStub{definition: def, value: value}, map[string]calendar.EventRoleDefinition{"field": {SourceType: "custom_date", Role: "warranty_expiration", SourceRoleKey: "field", SchedulingMode: calendar.Informational, ReadOnly: true}}).Project(context.Background(), calendar.SourceRef{MSPID: "msp", ClientID: "client", Type: "custom_date", ID: "value"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].EventRole != "warranty_expiration" || got[0].SourceRoleKey != "field" || got[0].SourceRevision != 5 {
		t.Fatalf("custom projection = %+v", got)
	}
}

func TestCustomDateCapacityRequiresCurrentAssignmentAndPositiveEffort(t *testing.T) {
	at := time.Date(2026, 9, 1, 13, 0, 0, 0, time.UTC)
	def := customfields.DateDefinition{ID: "field", MSPID: "msp", InternalKey: "dispatch", Label: "Dispatch", ObjectType: customfields.ObjectTask, ValueKind: customfields.TimestampKind, LifecycleState: "active", Version: 2}
	value := customfields.DateValue{ID: "value", FieldID: "field", MSPID: "msp", ClientID: "client", ObjectID: "task", ObjectType: customfields.ObjectTask, SourceRevision: 5, Version: 3, TimestampValue: &at, Timezone: "UTC"}
	role := calendar.EventRoleDefinition{SourceType: "custom_date", Role: "dispatch", SourceRoleKey: "dispatch", SchedulingMode: calendar.EffortAllocation, CapacityBearing: true}
	got, err := NewCustomDateAdapter(customCapacityLoaderStub{customLoaderStub: customLoaderStub{definition: def, value: value}, assignee: "tech", minutes: 45}, map[string]calendar.EventRoleDefinition{"field": role}).Project(context.Background(), calendar.SourceRef{MSPID: "msp", ClientID: "client", Type: "custom_date", ID: "value"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !got[0].CapacityBearing || got[0].AssigneeID != "tech" || got[0].PlannedMinutes != 45 {
		t.Fatalf("capacity projection=%+v", got)
	}
}

func TestProductionAdapterRegistryCoversCanonicalSources(t *testing.T) {
	registry, err := NewProductionAdapterRegistry(Dependencies{Work: workLoaderStub{}, Projects: projectLoaderStub{}, Workforce: workforceLoaderStub{}, Commitments: commitmentLoaderStub{}, CustomDates: customLoaderStub{}})
	if err != nil {
		t.Fatal(err)
	}
	for _, typ := range []string{"work_record", "task", "project", "phase", "milestone", "technician_schedule", "pto", "maintenance_window", "commercial_commitment", "custom_date"} {
		if _, ok := registry.ForSource(typ); !ok {
			t.Errorf("missing adapter %s", typ)
		}
	}
}

func assertRoles(t *testing.T, projections []calendar.Projection, expected ...string) {
	t.Helper()
	got := make([]string, len(projections))
	for i, p := range projections {
		got[i] = p.EventRole
	}
	if len(got) != len(expected) {
		t.Fatalf("roles=%v want=%v", got, expected)
	}
	for i := range got {
		if got[i] != expected[i] {
			t.Fatalf("roles=%v want=%v", got, expected)
		}
	}
}

type liveCustomLoader struct {
	customLoaderStub
	roles map[string]calendar.EventRoleDefinition
}

func (s *liveCustomLoader) LoadCustomRoleDefinitions(context.Context, string) (map[string]calendar.EventRoleDefinition, error) {
	return s.roles, nil
}
func TestCustomDateAdapterReloadsDefinitionsCreatedAfterStartup(t *testing.T) {
	date := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	loader := &liveCustomLoader{customLoaderStub: customLoaderStub{definition: customfields.DateDefinition{ID: "field", MSPID: "msp", InternalKey: "follow_up", Label: "Follow up", ObjectType: customfields.ObjectWorkRecord, ValueKind: customfields.DateKind, LifecycleState: "active", Version: 1}, value: customfields.DateValue{ID: "value", FieldID: "field", MSPID: "msp", ClientID: "client", ObjectID: "record", ObjectType: customfields.ObjectWorkRecord, SourceRevision: 1, Version: 1, DateValue: &date}}, roles: map[string]calendar.EventRoleDefinition{"field": {SourceType: "custom_date", Role: "follow_up", SourceRoleKey: "follow_up", SchedulingMode: calendar.Informational, ReadOnly: true}}}
	adapter := NewCustomDateAdapter(loader, nil)
	ref := calendar.SourceRef{MSPID: "msp", ClientID: "client", Type: "custom_date", ID: "value"}
	projections, err := adapter.Project(context.Background(), ref)
	if err != nil || len(projections) != 1 {
		t.Fatalf("dynamic field=%+v err=%v", projections, err)
	}
	loader.roles = map[string]calendar.EventRoleDefinition{}
	if _, err = adapter.Project(context.Background(), ref); err == nil {
		t.Fatal("stale definition retained after removal")
	}
}
