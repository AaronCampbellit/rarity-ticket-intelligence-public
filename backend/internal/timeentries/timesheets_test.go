package timeentries

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
)

func TestTimesheetWeekUsesMSPTimezoneAndMondayBoundary(t *testing.T) {
	repository := &timesheetRepositoryStub{
		timezone: "America/Chicago",
	}
	service := NewTimesheetService(
		repository,
		func() time.Time {
			return time.Date(2026, time.March, 11, 18, 0, 0, 0, time.UTC)
		},
		nextTimesheetID(),
		tagging.NewCreationPreparer(classificationRepository{}),
	)
	found, err := service.ListWeek(
		context.Background(),
		ListWeekCommand{
			Principal: technicianPrincipal("timesheet.read_own"),
			Target:    scope.Target{MSPID: "msp", ClientID: "client"},
		},
	)
	if err != nil {
		t.Fatalf("ListWeek() error = %v", err)
	}
	if found.Week.Timezone != "America/Chicago" ||
		!found.Week.StartsAt.Equal(
			time.Date(2026, time.March, 9, 5, 0, 0, 0, time.UTC),
		) ||
		!found.Week.EndsAt.Equal(
			time.Date(2026, time.March, 16, 5, 0, 0, 0, time.UTC),
		) {
		t.Fatalf("week = %+v", found.Week)
	}
	if repository.technicianID != "technician" {
		t.Fatalf("technician = %q", repository.technicianID)
	}
}

func TestGetEntryLoadsAnExactOffWeekEntryWithinAuthorizedClient(t *testing.T) {
	repository := &timesheetRepositoryStub{row: TimesheetRow{Entry: Entry{
		ID: "entry-old", MSPID: "msp", ClientID: "client", TechnicianID: "technician",
	}}}
	service := NewTimesheetService(repository, time.Now, nextTimesheetID(), nil)
	found, err := service.Get(context.Background(), GetCommand{
		Principal: technicianPrincipal("timesheet.read_own"), EntryID: "entry-old",
	})
	if err != nil || found.Entry.ID != "entry-old" {
		t.Fatalf("Get() found=%+v error=%v", found, err)
	}
}

func TestApprovedEntryRequiresReversalAndReplacement(t *testing.T) {
	startedAt := time.Date(2026, time.August, 4, 18, 0, 0, 0, time.UTC)
	repository := &timesheetRepositoryStub{
		timezone: "America/Chicago",
		row: TimesheetRow{
			Entry: Entry{
				ID: "approved", MSPID: "msp", ClientID: "client",
				WorkRecordID: "ticket", TechnicianID: "technician",
				StartedAt: startedAt, EndedAt: startedAt.Add(time.Hour),
				DurationSeconds: 3600, Version: 2,
			},
			ApprovalState: "approved",
		},
	}
	service := NewTimesheetService(
		repository,
		func() time.Time { return startedAt.Add(24 * time.Hour) },
		nextTimesheetID(),
		tagging.NewCreationPreparer(classificationRepository{}),
	)
	principal := technicianPrincipal("time_entry.amend")

	_, err := service.Amend(
		context.Background(),
		AmendCommand{
			Principal: principal,
			Target:    scope.Target{MSPID: "msp", ClientID: "client"},
			EntryID:   "approved", ExpectedVersion: 2,
			StartedAt: startedAt, EndedAt: startedAt.Add(90 * time.Minute),
			Billable: true, Note: "Corrected",
			Reason: "correct duration", ActorID: "technician", Source: "api",
		},
	)
	if !errors.Is(err, ErrApprovedEntryImmutable) {
		t.Fatalf("Amend() error = %v", err)
	}

	result, err := service.ReverseAndReplace(
		context.Background(),
		ReverseCommand{
			Principal: principal,
			Target:    scope.Target{MSPID: "msp", ClientID: "client"},
			EntryID:   "approved", ExpectedVersion: 2,
			Reason: "correct duration", ActorID: "technician", Source: "api",
			Replacement: Replacement{
				StartedAt: startedAt,
				EndedAt:   startedAt.Add(90 * time.Minute),
				Billable:  true,
				Note:      "Corrected",
				TagIDs:    []string{"tag-id"},
			},
		},
	)
	if err != nil {
		t.Fatalf("ReverseAndReplace() error = %v", err)
	}
	if result.Original.ReversedAt == nil ||
		result.Original.ReplacementTimeEntryID != result.Replacement.Entry.ID ||
		result.Replacement.Entry.DurationSeconds != 5400 ||
		repository.reversal.Original.Entry.Version != 3 {
		t.Fatalf("result=%+v mutation=%+v", result, repository.reversal)
	}
}

func TestReverseAndReplaceFailsClosedWhenMandatoryDependenciesAreMissing(t *testing.T) {
	started := time.Now().UTC()
	_, err := (&TimesheetService{}).ReverseAndReplace(context.Background(), ReverseCommand{
		Target: scope.Target{MSPID: "msp", ClientID: "client"}, EntryID: "entry",
		ExpectedVersion: 1, Reason: "correction", ActorID: "technician", Source: "api",
		Replacement: Replacement{StartedAt: started, EndedAt: started.Add(time.Minute)},
	})
	if !errors.Is(err, ErrInvalidTimesheet) {
		t.Fatalf("ReverseAndReplace() error=%v, want invalid timesheet", err)
	}
}

type timesheetRepositoryStub struct {
	timezone     string
	technicianID string
	row          TimesheetRow
	amendment    AmendmentMutation
	reversal     ReversalMutation
}

func (r *timesheetRepositoryStub) LoadMSPTimezone(
	context.Context,
	string,
) (string, error) {
	return r.timezone, nil
}

func (r *timesheetRepositoryStub) ListTimesheetRows(
	_ context.Context,
	_ scope.Target,
	technicianID string,
	_ time.Time,
	_ time.Time,
) ([]TimesheetRow, error) {
	r.technicianID = technicianID
	return nil, nil
}

func (r *timesheetRepositoryStub) LoadTimesheetRow(
	context.Context,
	scope.Target,
	string,
) (TimesheetRow, error) {
	return r.row, nil
}

func (r *timesheetRepositoryStub) AmendTimeEntryAtomic(
	_ context.Context,
	mutation AmendmentMutation,
) (TimesheetRow, error) {
	r.amendment = mutation
	return mutation.Result, nil
}

func (r *timesheetRepositoryStub) ReverseTimeEntryAtomic(
	_ context.Context,
	mutation ReversalMutation,
) (ReversalResult, error) {
	r.reversal = mutation
	return ReversalResult{
		Original:    mutation.Original,
		Replacement: mutation.Replacement,
	}, nil
}

func technicianPrincipal(capabilities ...string) authorization.Principal {
	return authorization.Principal{
		ID: "technician",
		Scope: scope.Principal{
			MSPID: "msp", ClientID: "client",
		},
		Capabilities: authorization.NewCapabilitySet(capabilities...),
	}
}

func nextTimesheetID() func() string {
	ids := []string{
		"replacement", "amendment", "audit", "event", "correlation",
		"replacement-audit", "replacement-event",
	}
	return func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	}
}
