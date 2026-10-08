package automation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type deadLetterRepository struct {
	letter   DeadLetter
	accepted DeadLetterMutation
}

func (r *deadLetterRepository) List(
	_ context.Context,
	target scope.Target,
) ([]DeadLetter, error) {
	if target.MSPID != r.letter.MSPID || target.ClientID != r.letter.ClientID {
		return nil, scope.ErrNotFound
	}
	return []DeadLetter{r.letter}, nil
}

func (r *deadLetterRepository) Get(
	_ context.Context,
	target scope.Target,
	_ string,
) (DeadLetter, error) {
	if target.MSPID != r.letter.MSPID || target.ClientID != r.letter.ClientID {
		return DeadLetter{}, scope.ErrNotFound
	}
	return r.letter, nil
}
func (r *deadLetterRepository) Apply(_ context.Context, mutation DeadLetterMutation) error {
	r.accepted = mutation
	return nil
}

func TestDeadLetterActionRequiresScopedCapabilityAndReason(t *testing.T) {
	now := time.Date(2026, time.July, 30, 1, 0, 0, 0, time.UTC)
	repository := &deadLetterRepository{letter: DeadLetter{
		ID: "dead-id", RunID: "run-id", MSPID: "msp-id", ClientID: "client-alpha",
		State: "open",
	}}
	service := NewDeadLetterService(repository, func() time.Time { return now }, sequenceAutomationIDs("action-id", "event-id", "correlation-id"))
	principal := authorization.Principal{
		ID:           "technician-id",
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-alpha"},
		Capabilities: authorization.NewCapabilitySet("automation.dead_letter.manage"),
	}
	result, err := service.Act(context.Background(), DeadLetterCommand{
		Principal: principal, ID: "dead-id", Action: DeadLetterRetry,
		Reason: "connection repaired",
	})
	if err != nil {
		t.Fatalf("Act() error = %v", err)
	}
	if result.State != "retrying" ||
		repository.accepted.Audit.Action != "automation.dead_letter.retry" ||
		repository.accepted.Audit.Reason != "connection repaired" {
		t.Fatalf("dead-letter action lacks evidence: result=%+v mutation=%+v", result, repository.accepted)
	}

	principal.Scope.ClientID = "client-bravo"
	_, err = service.Act(context.Background(), DeadLetterCommand{
		Principal: principal, ID: "dead-id", Action: DeadLetterDismiss, Reason: "not applicable",
	})
	if !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("cross-client dead letter error = %v", err)
	}
}
