package datto

import (
	"context"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/routing"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sla"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workflow"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workrecords"
)

type alertWorkRecords struct {
	existing workrecords.Record
	command  workrecords.CreateCommand
}

type missingAlertWorkRecordFinder struct{}

func (missingAlertWorkRecordFinder) Find(context.Context, scope.Target, string) (workrecords.Record, error) {
	return workrecords.Record{}, scope.ErrNotFound
}

type dattoWorkRecordRepository struct{ mutation workrecords.CreateMutation }

func (*dattoWorkRecordRepository) EnsureDisplayIDAvailable(context.Context, scope.Target, string) error {
	return nil
}
func (*dattoWorkRecordRepository) ValidateReferences(context.Context, scope.Target, workrecords.ContextReferences, time.Time) error {
	return nil
}
func (r *dattoWorkRecordRepository) CreateAtomic(_ context.Context, mutation workrecords.CreateMutation) error {
	r.mutation = mutation
	return nil
}

type dattoRoutingSource struct{}

func (dattoRoutingSource) LoadCurrent(context.Context, string) (routing.RuleSet, error) {
	return routing.RuleSet{ID: "routing", Version: 1, Rules: []routing.Rule{{ID: "route", Position: 1, QueueID: "queue"}}}, nil
}

type dattoWorkflowSource struct{}

func (dattoWorkflowSource) ListPublished(context.Context, scope.Target) ([]workflow.Published, error) {
	return []workflow.Published{{ID: "workflow", Version: 1, Enabled: true, Fallback: true, Definition: workflow.Definition{States: []workflow.State{{Key: "new"}}}}}, nil
}

type dattoPolicySource struct{}

func (dattoPolicySource) ListPolicies(context.Context, scope.Target) ([]sla.PublishedPolicy, error) {
	weekly := map[string][]sla.Window{}
	for _, day := range []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"} {
		weekly[day] = []sla.Window{{StartMinute: 0, EndMinute: 1440}}
	}
	return []sla.PublishedPolicy{{
		ID: "sla", Version: 1, Enabled: true, Fallback: true,
		Calendar:              sla.PublishedCalendar{ID: "calendar", Version: 1, Definition: sla.CalendarDefinition{Timezone: "UTC", Weekly: weekly}},
		ResponseTargetSeconds: 60, ResolutionTargetSeconds: 120, WarningPercent: 80,
	}}, nil
}

type dattoClassificationRepository struct{}

func (dattoClassificationRepository) ResolveTags(context.Context, string, []string) ([]tagging.Tag, error) {
	return nil, nil
}
func (dattoClassificationRepository) FindUnclassified(_ context.Context, mspID string) (tagging.Tag, error) {
	return tagging.Tag{ID: "unclassified", MSPID: mspID, InternalKey: "taxonomy.system.unclassified", State: tagging.StateActive}, nil
}

func (w *alertWorkRecords) Find(
	context.Context,
	scope.Target,
	string,
) (workrecords.Record, error) {
	if w.existing.ID == "" {
		return workrecords.Record{}, scope.ErrNotFound
	}
	return w.existing, nil
}

func (w *alertWorkRecords) CreateDattoIncident(
	_ context.Context,
	command workrecords.CreateCommand,
) (workrecords.Record, error) {
	w.command = command
	return workrecords.Record{}, nil
}

func TestAlertIncidentWriterCreatesScopedIncidentWithStableIdentity(t *testing.T) {
	records := &alertWorkRecords{}
	writer := NewWorkRecordAlertIncidentWriter(records, records)
	incident := AlertIncident{
		ID: "incident-id", MSPID: "msp-id", ClientID: "client-id",
		ActorID: "actor-id", DisplayID: "DATTO-ALERT",
		Title: "Gateway unavailable", Description: "No heartbeat",
		Priority: "critical",
	}

	err := writer.EnsureIncident(context.Background(), incident)

	if err != nil ||
		records.command.RecordID != "incident-id" ||
		records.command.Type != workrecords.Incident ||
		records.command.Principal.Scope.MSPID != "msp-id" ||
		records.command.Principal.Scope.ClientID != "client-id" ||
		records.command.Actor.Type != "integration" ||
		records.command.Actor.Source != "datto" ||
		records.command.ClassificationPolicy != tagging.CreationAllowFallback {
		t.Fatalf("EnsureIncident() command=%+v error=%v", records.command, err)
	}
}

func TestAlertIncidentWriterLeavesExistingStableIncidentUntouched(t *testing.T) {
	records := &alertWorkRecords{
		existing: workrecords.Record{
			Envelope: object.Envelope{ID: "incident-id"},
		},
	}
	writer := NewWorkRecordAlertIncidentWriter(records, records)

	err := writer.EnsureIncident(context.Background(), AlertIncident{
		ID: "incident-id", MSPID: "msp-id", ClientID: "client-id",
		ActorID: "actor-id", Title: "Gateway unavailable", Priority: "high",
	})

	if err != nil || records.command.RecordID != "" {
		t.Fatalf("EnsureIncident() command=%+v error=%v", records.command, err)
	}
}

func TestAlertIncidentWriterComposesRealWorkRecordServiceFallback(t *testing.T) {
	repository := &dattoWorkRecordRepository{}
	ids := []string{"sla", "audit", "event", "correlation"}
	service := workrecords.NewService(
		repository, dattoRoutingSource{}, dattoWorkflowSource{}, dattoPolicySource{},
		func() time.Time { return time.Date(2026, time.August, 7, 0, 0, 0, 0, time.UTC) },
		func() string { value := ids[0]; ids = ids[1:]; return value },
		tagging.NewCreationPreparer(dattoClassificationRepository{}),
	)
	writer := NewWorkRecordAlertIncidentWriter(missingAlertWorkRecordFinder{}, service)
	err := writer.EnsureIncident(context.Background(), AlertIncident{
		ID: "incident-id", MSPID: "msp-id", ClientID: "client-id", ActorID: "datto", DisplayID: "DATTO-1", Title: "Gateway unavailable", Priority: "high",
	})
	if err != nil {
		t.Fatal(err)
	}
	if repository.mutation.Record.ID != "incident-id" || repository.mutation.Record.ClientID != "client-id" ||
		repository.mutation.Audit.Source != "datto" || repository.mutation.InitialTags.Direct[0].Tag.ID != "unclassified" ||
		repository.mutation.InitialTags.Direct[0].Source != tagging.SourceSystemFallback {
		t.Fatalf("mutation=%+v", repository.mutation)
	}
}
