package calendar_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar/adapters"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type writeWorkSource struct{ task adapters.TaskSource }

func (s writeWorkSource) LoadWorkRecord(context.Context, calendar.SourceRef) (adapters.WorkRecordSource, error) {
	return adapters.WorkRecordSource{}, errors.New("unused")
}
func (s writeWorkSource) LoadTask(context.Context, calendar.SourceRef) (adapters.TaskSource, error) {
	return s.task, nil
}

func TestTaskWriteAdapterPreparesTypedSourceMutation(t *testing.T) {
	start := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	source := writeWorkSource{task: adapters.TaskSource{MSPID: "msp", ClientID: "client", ID: "task", Version: 3, Title: "Task", Status: "open", OwnerID: "tech", PlannedMinutes: 60, SchedulingMode: calendar.FixedBlock}}
	a := adapters.NewTaskAdapter(source)
	principal := authorization.Principal{ID: "actor", Scope: scope.Principal{MSPID: "msp"}, Capabilities: authorization.NewCapabilitySet("calendar.schedule", "task.edit")}
	prepared, err := a.Prepare(context.Background(), principal, calendar.RequestedChange{ProjectionID: "projection", Source: calendar.SourceRef{MSPID: "msp", ClientID: "client", Type: "task", ID: "task"}, EventRole: "scheduled_work", SourceRevision: 3, StartsAt: &start, EndsAt: &end, Timezone: "UTC"})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Mutation == nil || prepared.ExpectedSourceRevision != 3 {
		t.Fatalf("prepared=%+v", prepared)
	}
}

func TestDerivedAndInformationalRolesAreReadOnly(t *testing.T) {
	a := adapters.NewTaskAdapter(writeWorkSource{task: adapters.TaskSource{MSPID: "msp", ClientID: "client", ID: "task", Version: 1}})
	principal := authorization.Principal{ID: "actor", Scope: scope.Principal{MSPID: "msp"}, Capabilities: authorization.NewCapabilitySet("calendar.schedule", "task.edit")}
	date := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	_, err := a.Prepare(context.Background(), principal, calendar.RequestedChange{ProjectionID: "projection", Source: calendar.SourceRef{MSPID: "msp", ClientID: "client", Type: "task", ID: "task"}, EventRole: "due", SourceRevision: 1, AllDay: true, StartsOn: &date})
	if !errors.Is(err, calendar.ErrReadOnlyEventRole) {
		t.Fatalf("error=%v", err)
	}
}
