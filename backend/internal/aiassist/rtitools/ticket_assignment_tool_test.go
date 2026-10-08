package rtitools

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workrecords"
)

const ticketAssignPublicRequest = `{
  "client": "Northwind Legal",
  "ticket": "INC-2042",
  "technician": "alex@example.test",
  "expected_version": 4,
  "reason": "Assign to network escalation owner"
}`

type ticketAssignmentResolverStub struct {
	target    scope.Target
	reference string
	limit     int
	records   []workrecords.Record
	err       error
}

func (s *ticketAssignmentResolverStub) ResolveWorkRecordForAssignment(_ context.Context, target scope.Target, reference string, limit int) ([]workrecords.Record, error) {
	s.target, s.reference, s.limit = target, reference, limit
	return s.records, s.err
}

type ticketAssignmentDirectoryStub struct {
	target     scope.Target
	reference  string
	limit      int
	candidates []workrecords.AssignmentCandidate
	identities map[string]workrecords.AssignmentCandidate
	err        error
}

func (s *ticketAssignmentDirectoryStub) ResolveAssignmentCandidates(_ context.Context, target scope.Target, reference string, limit int) ([]workrecords.AssignmentCandidate, error) {
	s.target, s.reference, s.limit = target, reference, limit
	return s.candidates, s.err
}

func (s *ticketAssignmentDirectoryStub) FindAssignmentIdentity(_ context.Context, _ scope.Target, id string) (workrecords.AssignmentCandidate, error) {
	if s.err != nil {
		return workrecords.AssignmentCandidate{}, s.err
	}
	identity, ok := s.identities[id]
	if !ok {
		return workrecords.AssignmentCandidate{}, scope.ErrNotFound
	}
	return identity, nil
}

type ticketAssignerStub struct {
	command workrecords.AssignCommand
	calls   int
}

func (s *ticketAssignerStub) Assign(_ context.Context, command workrecords.AssignCommand) (workrecords.Record, error) {
	s.calls++
	s.command = command
	return workrecords.Record{Envelope: object.Envelope{ID: command.WorkRecordID, MSPID: command.Target.MSPID, ClientID: command.Target.ClientID, DisplayID: "INC-2042", Version: command.ExpectedVersion + 1}, PrimaryOwnerID: command.OwnerID}, nil
}

func ticketAssignmentRecord() workrecords.Record {
	return workrecords.Record{Envelope: object.Envelope{ID: "ticket-1", MSPID: "msp-1", ClientID: "client-1", DisplayID: "INC-2042", LifecycleState: "active", Version: 4}, Title: "VPN unavailable", PrimaryOwnerID: "owner-1"}
}

func ticketAssignmentCandidate() workrecords.AssignmentCandidate {
	return workrecords.AssignmentCandidate{ID: "technician-2", MSPID: "msp-1", DisplayName: "Alex Morgan", Email: "alex@example.test", Version: 6}
}

func ticketAssignmentCurrentOwner() workrecords.AssignmentCandidate {
	return workrecords.AssignmentCandidate{ID: "owner-1", MSPID: "msp-1", DisplayName: "Casey Kim", Email: "casey@example.test", Version: 3}
}

func activeTicketAssignmentDirectory() *ticketAssignmentDirectoryStub {
	return &ticketAssignmentDirectoryStub{
		candidates: []workrecords.AssignmentCandidate{ticketAssignmentCandidate()},
		identities: map[string]workrecords.AssignmentCandidate{"owner-1": ticketAssignmentCurrentOwner()},
	}
}

func activeTicketAssignmentClientDirectory() *resourceDirectoryStub {
	directory := activeResourceDirectory()
	directory.resolved.Version = 1
	return directory
}

func ticketAssignmentPrincipal() authorization.Principal {
	return operationalPrincipal("work_record.assign")
}

func TestTicketAssignToolPreparesCanonicalTicketAndTechnician(t *testing.T) {
	resolver := &ticketAssignmentResolverStub{records: []workrecords.Record{ticketAssignmentRecord()}}
	directory := activeTicketAssignmentDirectory()
	tool := NewTicketAssignTool(activeTicketAssignmentClientDirectory(), resolver, directory, &ticketAssignerStub{})
	prepared, err := tool.(aiassist.ToolInputPreparer).Prepare(context.Background(), ticketAssignmentPrincipal(), json.RawMessage(ticketAssignPublicRequest))
	if err != nil {
		t.Fatalf("Prepare() error=%v", err)
	}
	if resolver.reference != "inc-2042" || resolver.limit != 2 || resolver.target != (scope.Target{MSPID: "msp-1", ClientID: "client-1"}) ||
		directory.reference != "alex@example.test" || directory.limit != 2 || directory.target != resolver.target {
		t.Fatalf("ticket=%+v technician=%+v", resolver, directory)
	}
	for _, fragment := range []string{`"ticket_id":"ticket-1"`, `"ticket_display_id":"INC-2042"`, `"technician_id":"technician-2"`, `"technician_display_name":"Alex Morgan"`, `"technician_version":6`} {
		if !strings.Contains(string(prepared), fragment) {
			t.Fatalf("prepared input omitted canonical identity %s: %s", fragment, prepared)
		}
	}
}

func TestTicketAssignToolAcceptsExactTicketNameAndTechnicianName(t *testing.T) {
	record := ticketAssignmentRecord()
	record.Title = "VPN unavailable"
	resolver := &ticketAssignmentResolverStub{records: []workrecords.Record{record}}
	directory := activeTicketAssignmentDirectory()
	tool := NewTicketAssignTool(activeTicketAssignmentClientDirectory(), resolver, directory, &ticketAssignerStub{})
	_, err := tool.(aiassist.ToolInputPreparer).Prepare(context.Background(), ticketAssignmentPrincipal(), json.RawMessage(`{"client":"Northwind Legal","ticket":"  vpn\t unavailable ","technician":" Alex  Morgan ","expected_version":4,"reason":"Assign to network escalation owner"}`))
	if err != nil || resolver.reference != "vpn unavailable" || directory.reference != "alex morgan" {
		t.Fatalf("Prepare() error=%v ticket=%q technician=%q", err, resolver.reference, directory.reference)
	}
}

func TestTicketAssignToolRejectsUnresolvedOrUnsafePreparation(t *testing.T) {
	for _, test := range []struct {
		name       string
		records    []workrecords.Record
		candidates []workrecords.AssignmentCandidate
		raw        string
	}{
		{name: "no ticket", candidates: []workrecords.AssignmentCandidate{ticketAssignmentCandidate()}, raw: ticketAssignPublicRequest},
		{name: "ambiguous ticket", records: []workrecords.Record{ticketAssignmentRecord(), ticketAssignmentRecord()}, candidates: []workrecords.AssignmentCandidate{ticketAssignmentCandidate()}, raw: ticketAssignPublicRequest},
		{name: "inactive or expired technician", records: []workrecords.Record{ticketAssignmentRecord()}, raw: ticketAssignPublicRequest},
		{name: "ambiguous technician", records: []workrecords.Record{ticketAssignmentRecord()}, candidates: []workrecords.AssignmentCandidate{ticketAssignmentCandidate(), ticketAssignmentCandidate()}, raw: ticketAssignPublicRequest},
		{name: "mismatched technician", records: []workrecords.Record{ticketAssignmentRecord()}, candidates: []workrecords.AssignmentCandidate{{ID: "technician-3", MSPID: "msp-1", DisplayName: "Taylor Jones", Email: "taylor@example.test", Version: 2}}, raw: ticketAssignPublicRequest},
		{name: "same owner", records: []workrecords.Record{func() workrecords.Record {
			record := ticketAssignmentRecord()
			record.PrimaryOwnerID = "technician-2"
			return record
		}()}, candidates: []workrecords.AssignmentCandidate{ticketAssignmentCandidate()}, raw: ticketAssignPublicRequest},
		{name: "stale ticket", records: []workrecords.Record{func() workrecords.Record { record := ticketAssignmentRecord(); record.Version = 5; return record }()}, candidates: []workrecords.AssignmentCandidate{ticketAssignmentCandidate()}, raw: ticketAssignPublicRequest},
		{name: "unknown trusted field", records: []workrecords.Record{ticketAssignmentRecord()}, candidates: []workrecords.AssignmentCandidate{ticketAssignmentCandidate()}, raw: `{"client":"Northwind Legal","ticket":"INC-2042","technician":"alex@example.test","expected_version":4,"reason":"Assign to network escalation owner","ticket_id":"spoof"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			assigner := &ticketAssignerStub{}
			directory := activeTicketAssignmentDirectory()
			directory.candidates = test.candidates
			tool := NewTicketAssignTool(activeTicketAssignmentClientDirectory(), &ticketAssignmentResolverStub{records: test.records}, directory, assigner)
			if _, err := tool.(aiassist.ToolInputPreparer).Prepare(context.Background(), ticketAssignmentPrincipal(), json.RawMessage(test.raw)); err == nil || assigner.calls != 0 {
				t.Fatalf("Prepare() error=%v assign calls=%d", err, assigner.calls)
			}
		})
	}
}

func TestTicketAssignToolPreviewAndConfirmationRecheckBeforeAssignment(t *testing.T) {
	resolver := &ticketAssignmentResolverStub{records: []workrecords.Record{ticketAssignmentRecord()}}
	directory := activeTicketAssignmentDirectory()
	assigner := &ticketAssignerStub{}
	clientDirectory := activeTicketAssignmentClientDirectory()
	clientDirectory.resolved.Version = 3
	tool := NewTicketAssignTool(clientDirectory, resolver, directory, assigner)
	principal := ticketAssignmentPrincipal()
	store := &composedProposalStore{}
	ids := []string{"proposal-1", "correlation-1"}
	registry, err := aiassist.NewRegistry([]aiassist.Tool{tool}, store, func(context.Context, authorization.Principal) (authorization.Principal, error) { return principal, nil }, func() time.Time { return time.Date(2026, time.August, 6, 13, 0, 0, 0, time.UTC) }, func() string { value := ids[0]; ids = ids[1:]; return value }, &composedTargetAuthorizer{})
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := registry.Propose(context.Background(), principal, aiassist.ToolRequest{Name: "ticket.assign", ConversationID: "conversation-1", Input: json.RawMessage(ticketAssignPublicRequest)})
	if err != nil {
		t.Fatalf("Propose() error=%v", err)
	}
	if proposal.Preview.TargetID != "ticket-1" || proposal.Preview.TargetVersion != 4 || proposal.Preview.Changes["reason"].After != "Assign to network escalation owner" ||
		!reflect.DeepEqual(proposal.Preview.Changes["owner"].Before, map[string]any{"display_name": "Casey Kim", "email": "casey@example.test"}) ||
		!reflect.DeepEqual(proposal.Preview.Changes["owner"].After, map[string]any{"display_name": "Alex Morgan", "email": "alex@example.test"}) ||
		proposal.Preview.Changes["ticket"].After.(map[string]any)["version"] != int64(5) {
		t.Fatalf("preview=%+v", proposal.Preview)
	}
	if _, err := registry.Confirm(context.Background(), principal, proposal.ID, proposal.Version); err != nil {
		t.Fatalf("Confirm() error=%v", err)
	}
	if assigner.calls != 1 || assigner.command.WorkRecordID != "ticket-1" || assigner.command.OwnerID != "technician-2" || assigner.command.ExpectedVersion != 4 ||
		assigner.command.Reason != "Assign to network escalation owner" || assigner.command.Actor.Source != "ai_workspace" || assigner.command.CausationID != "correlation-1" ||
		assigner.command.ExpectedClientVersion != 3 || assigner.command.ExpectedOwnerVersion != 6 {
		t.Fatalf("assignment=%+v calls=%d", assigner.command, assigner.calls)
	}
}

func TestTicketAssignToolPreviewLabelsUnassignedOwner(t *testing.T) {
	record := ticketAssignmentRecord()
	record.PrimaryOwnerID = ""
	tool := NewTicketAssignTool(activeTicketAssignmentClientDirectory(), &ticketAssignmentResolverStub{records: []workrecords.Record{record}}, activeTicketAssignmentDirectory(), &ticketAssignerStub{})
	prepared, err := tool.(aiassist.ToolInputPreparer).Prepare(context.Background(), ticketAssignmentPrincipal(), json.RawMessage(ticketAssignPublicRequest))
	if err != nil {
		t.Fatalf("Prepare() error=%v", err)
	}
	preview, err := tool.Preview(context.Background(), ticketAssignmentPrincipal(), prepared)
	if err != nil || preview.Changes["owner"].Before != "Unassigned" {
		t.Fatalf("Preview() preview=%+v error=%v", preview, err)
	}
}

func TestTicketAssignToolRejectsConfirmationDriftBeforeAssignment(t *testing.T) {
	for _, test := range []struct {
		name  string
		apply func(*resourceDirectoryStub, *ticketAssignmentResolverStub, *ticketAssignmentDirectoryStub)
	}{
		{name: "inactive client", apply: func(directory *resourceDirectoryStub, _ *ticketAssignmentResolverStub, _ *ticketAssignmentDirectoryStub) {
			directory.err = scope.ErrNotFound
		}},
		{name: "ticket version", apply: func(_ *resourceDirectoryStub, resolver *ticketAssignmentResolverStub, _ *ticketAssignmentDirectoryStub) {
			resolver.records[0].Version++
		}},
		{name: "technician capability", apply: func(_ *resourceDirectoryStub, _ *ticketAssignmentResolverStub, directory *ticketAssignmentDirectoryStub) {
			directory.candidates = nil
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			clientDirectory := activeTicketAssignmentClientDirectory()
			resolver := &ticketAssignmentResolverStub{records: []workrecords.Record{ticketAssignmentRecord()}}
			directory := activeTicketAssignmentDirectory()
			assigner := &ticketAssignerStub{}
			tool := NewTicketAssignTool(clientDirectory, resolver, directory, assigner)
			principal := ticketAssignmentPrincipal()
			store := &composedProposalStore{}
			ids := []string{"proposal-1", "correlation-1"}
			registry, err := aiassist.NewRegistry([]aiassist.Tool{tool}, store, func(context.Context, authorization.Principal) (authorization.Principal, error) { return principal, nil }, func() time.Time { return time.Date(2026, time.August, 6, 13, 0, 0, 0, time.UTC) }, func() string { value := ids[0]; ids = ids[1:]; return value }, &composedTargetAuthorizer{})
			if err != nil {
				t.Fatal(err)
			}
			proposal, err := registry.Propose(context.Background(), principal, aiassist.ToolRequest{Name: "ticket.assign", ConversationID: "conversation-1", Input: json.RawMessage(ticketAssignPublicRequest)})
			if err != nil {
				t.Fatal(err)
			}
			test.apply(clientDirectory, resolver, directory)
			if _, err := registry.Confirm(context.Background(), principal, proposal.ID, proposal.Version); !errors.Is(err, aiassist.ErrProposalStale) || assigner.calls != 0 {
				t.Fatalf("Confirm() error=%v assignments=%d", err, assigner.calls)
			}
		})
	}
}
