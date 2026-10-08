package comments

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sla"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/timeentries"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workrecords"
)

type captureRepository struct {
	mutation CreateMutation
	calls    int
	sla      workrecords.AppliedSLA
}

type compositeCaptureRepository struct {
	captureRepository
	captureMutation timeentries.CaptureMutation
	compositeCalls  int
}

func (r *compositeCaptureRepository) CreateWithCaptureAtomic(
	_ context.Context,
	comment CreateMutation,
	capture timeentries.CaptureMutation,
) error {
	r.compositeCalls++
	r.mutation = comment
	r.captureMutation = capture
	return nil
}

type capturePreparerStub struct {
	command  timeentries.CaptureCommand
	mutation timeentries.CaptureMutation
}

func (s *capturePreparerStub) Prepare(
	_ context.Context,
	command timeentries.CaptureCommand,
) (timeentries.CaptureMutation, error) {
	s.command = command
	return s.mutation, nil
}

func (r *captureRepository) FindAppliedSLA(
	context.Context,
	scope.Target,
	string,
) (workrecords.AppliedSLA, error) {
	return r.sla, nil
}

func (r *captureRepository) CreateAtomic(_ context.Context, mutation CreateMutation) error {
	r.calls++
	r.mutation = mutation
	return nil
}

func TestFirstClientReplyMeetsSLAWithAtomicEvidence(t *testing.T) {
	now := time.Date(2026, time.July, 29, 18, 0, 0, 0, time.UTC)
	repository := &captureRepository{sla: workrecords.AppliedSLA{
		ID: "sla", Version: 2, ResponseDueAt: now.Add(time.Hour),
		ResponseWarningAt: now.Add(30 * time.Minute), ResponseState: sla.Running,
		CalendarDefinition: sla.CalendarDefinition{
			Timezone: "UTC",
			Weekly: map[string][]sla.Window{
				"wednesday": {{StartMinute: 0, EndMinute: 1440}},
			},
		},
	}}
	ids := []string{
		"comment", "audit", "event", "correlation", "sla-audit", "sla-event",
	}
	service := NewService(repository, func() time.Time { return now }, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	})
	principal := authorization.Principal{
		Scope:        scope.Principal{MSPID: "msp", ClientID: "client"},
		Capabilities: authorization.NewCapabilitySet("comment.public.create"),
	}
	_, err := service.Create(context.Background(), CreateCommand{
		Principal: principal, WorkRecordID: "work", Visibility: ClientVisible,
		Body: "Service restored", ActorID: "actor", Source: "web",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if !repository.mutation.SLAChanged ||
		repository.mutation.SLA.RespondedAt == nil ||
		repository.mutation.SLA.ResponseState != sla.Met ||
		repository.mutation.SLAAudit.Action != "sla.response.recorded" ||
		repository.mutation.SLAEvent.EventType != "sla.response.recorded" {
		t.Fatalf("first response SLA evidence incomplete: %+v", repository.mutation)
	}
}

func TestCreateClientReplyRequiresExplicitPublicCapability(t *testing.T) {
	repository := &captureRepository{}
	service := NewService(repository, time.Now, func() string { return "unused" })
	principal := authorization.Principal{
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet("comment.internal.create"),
	}
	_, err := service.Create(context.Background(), CreateCommand{
		Principal: principal, WorkRecordID: "work-id", Visibility: ClientVisible,
		Body: "Customer update", ActorID: "actor-id", Source: "web",
	})
	if !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("Create() error = %v, want ErrForbidden", err)
	}
	if repository.calls != 0 {
		t.Fatal("unauthorized public reply reached repository")
	}
}

func TestCreateInternalCommentIsAuditedAndScoped(t *testing.T) {
	repository := &captureRepository{}
	now := time.Date(2026, time.July, 29, 18, 0, 0, 0, time.UTC)
	ids := []string{"comment-id", "audit-id", "event-id", "correlation-id"}
	service := NewService(repository, func() time.Time { return now }, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	})
	principal := authorization.Principal{
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet("comment.internal.create"),
	}
	comment, err := service.Create(context.Background(), CreateCommand{
		Principal: principal, WorkRecordID: "work-id", Visibility: Internal,
		Body: "Investigating authentication logs", ActorID: "actor-id", Source: "web",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if comment.ClientID != "client-id" || comment.Visibility != Internal {
		t.Fatalf("unexpected comment: %+v", comment)
	}
	if repository.mutation.Event.EventType != "comment.added" ||
		repository.mutation.Audit.Action != "comment.added" {
		t.Fatal("comment mutation did not produce audit and event facts")
	}
}

func TestCreateCommentWithCaptureUsesOneAtomicRepositoryMutation(t *testing.T) {
	repository := &compositeCaptureRepository{}
	preparer := &capturePreparerStub{mutation: timeentries.CaptureMutation{
		EntryID: "entry",
	}}
	service := NewServiceWithCapture(
		repository,
		preparer,
		func() time.Time {
			return time.Date(2026, time.August, 4, 19, 0, 0, 0, time.UTC)
		},
		idSequence("comment", "audit", "event", "correlation"),
	)
	principal := authorization.Principal{
		ID: "tech",
		Scope: scope.Principal{
			MSPID: "msp", ClientID: "client",
		},
		Capabilities: authorization.NewCapabilitySet(
			"comment.internal.create",
			"time_entry.create",
		),
	}
	_, err := service.Create(context.Background(), CreateCommand{
		Principal: principal, WorkRecordID: "work", Visibility: Internal,
		Body: "Investigated logs", ActorID: "tech", Source: "api",
		TimeCapture: &TimeCapture{
			ID: "capture", ExpectedVersion: 2,
			LaborRoleID: "role", Billable: true,
		},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if repository.calls != 0 || repository.compositeCalls != 1 ||
		repository.captureMutation.EntryID != "entry" ||
		preparer.command.WorkRecordID != "work" ||
		preparer.command.Note != "Investigated logs" {
		t.Fatalf(
			"comment=%+v capture=%+v command=%+v",
			repository.mutation,
			repository.captureMutation,
			preparer.command,
		)
	}
}

func idSequence(ids ...string) func() string {
	return func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	}
}
