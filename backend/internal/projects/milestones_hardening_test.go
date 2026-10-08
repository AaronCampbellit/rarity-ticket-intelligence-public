package projects

import (
	"context"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar"
)

func TestMilestoneSupportsTimedRecurringInterval(t *testing.T) {
	r := &milestoneRepositoryStub{project: Project{ID: milestoneProjectID, MSPID: milestoneMSPID, ClientID: milestoneClientID}}
	s := NewMilestoneService(r, time.Now, sequenceIDs())
	due := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	start := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	found, err := s.Create(context.Background(), CreateMilestoneCommand{Principal: milestonePrincipal("project.edit", milestoneMSPID, milestoneClientID), ProjectID: milestoneProjectID, Name: "Cutover", DueOn: due, StartsAt: &start, EndsAt: &end, Timezone: "UTC", Recurrence: &calendar.RecurrenceRule{Frequency: calendar.Weekly, Interval: 1, Weekdays: []time.Weekday{time.Tuesday}, Count: 2}, ActorID: milestoneActorID, Source: "api", IdempotencyKey: "milestone-1"})
	if err != nil || found.AllDay || found.Recurrence == nil {
		t.Fatalf("found=%+v err=%v", found, err)
	}
}
