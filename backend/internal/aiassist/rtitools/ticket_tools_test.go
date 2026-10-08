package rtitools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workrecords"
)

type workRecordActionsStub struct {
	record     workrecords.Record
	create     workrecords.CreateCommand
	transition workrecords.TransitionCommand
}

func (s *workRecordActionsStub) Get(context.Context, workrecords.GetCommand) (workrecords.Record, error) {
	return s.record, nil
}
func (s *workRecordActionsStub) Transition(_ context.Context, command workrecords.TransitionCommand) (workrecords.Record, error) {
	s.transition = command
	return s.record, nil
}
func (s *workRecordActionsStub) Create(
	_ context.Context,
	command workrecords.CreateCommand,
) (workrecords.Record, error) {
	s.create = command
	return s.record, nil
}

func TestTicketTransitionToolUsesExistingServiceAndExactPreview(t *testing.T) {
	actions := &workRecordActionsStub{record: workrecords.Record{
		Envelope: testEnvelope("ticket-1", "client-1", 3),
		Status:   "open", Priority: "normal", Title: "Printer unavailable",
	}}
	tool := NewTicketTransitionTool(actions, actions)
	principal := testPrincipal("work_record.transition")
	input := json.RawMessage(`{
		"client_id":"client-1",
		"id":"ticket-1",
		"expected_version":3,
		"status":"resolved",
		"reason":"Issue fixed"
	}`)

	preview, err := tool.Preview(context.Background(), principal, input)
	if err != nil {
		t.Fatal(err)
	}
	if preview.TargetVersion != 3 || preview.Changes["status"].Before != "open" ||
		preview.Changes["status"].After != "resolved" {
		t.Fatalf("preview=%+v", preview)
	}
	if _, err := tool.Execute(context.Background(), principal, input, "correlation-1"); err != nil {
		t.Fatal(err)
	}
	if actions.transition.WorkRecordID != "ticket-1" ||
		actions.transition.ExpectedVersion != 3 ||
		actions.transition.ToStatus != "resolved" ||
		actions.transition.Actor.ID != principal.ID ||
		actions.transition.CausationID != "correlation-1" {
		t.Fatalf("transition=%+v", actions.transition)
	}
}

func testPrincipal(capabilities ...string) authorization.Principal {
	return authorization.Principal{
		ID:           "technician-1",
		Scope:        scope.Principal{MSPID: "msp-1"},
		Capabilities: authorization.NewCapabilitySet(capabilities...),
	}
}
