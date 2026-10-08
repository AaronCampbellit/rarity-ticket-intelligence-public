package workrecords

import (
	"context"
	"errors"
	"testing"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type assignmentDirectoryStub struct {
	target     scope.Target
	reference  string
	limit      int
	candidates []AssignmentCandidate
	err        error
}

func (s *assignmentDirectoryStub) ResolveAssignmentCandidates(
	_ context.Context,
	target scope.Target,
	reference string,
	limit int,
) ([]AssignmentCandidate, error) {
	s.target, s.reference, s.limit = target, reference, limit
	return s.candidates, s.err
}

func (*assignmentDirectoryStub) FindAssignmentIdentity(context.Context, scope.Target, string) (AssignmentCandidate, error) {
	return AssignmentCandidate{}, scope.ErrNotFound
}

func TestResolveAssignmentCandidateNormalizesAndRequiresOneExactCandidate(t *testing.T) {
	directory := &assignmentDirectoryStub{candidates: []AssignmentCandidate{{
		ID: "technician-id", MSPID: "msp-id", DisplayName: "Taylor Jones",
		Email: "taylor@example.com", Version: 7,
	}}}
	candidate, err := ResolveAssignmentCandidate(
		context.Background(), directory,
		scope.Target{MSPID: "msp-id", ClientID: "client-id"}, "  TAYLOR\tJONES  ",
	)
	if err != nil {
		t.Fatalf("ResolveAssignmentCandidate() error = %v", err)
	}
	if candidate.ID != "technician-id" || directory.reference != "taylor jones" ||
		directory.limit != 2 || directory.target.ClientID != "client-id" {
		t.Fatalf("candidate=%+v directory=%+v", candidate, directory)
	}
}

func TestResolveAssignmentCandidateKeepsZeroAndAmbiguousResultsEnumerationSafe(t *testing.T) {
	for name, candidates := range map[string][]AssignmentCandidate{
		"zero":      nil,
		"ambiguous": {{ID: "first"}, {ID: "second"}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ResolveAssignmentCandidate(
				context.Background(), &assignmentDirectoryStub{candidates: candidates},
				scope.Target{MSPID: "msp-id", ClientID: "client-id"}, "Taylor Jones",
			)
			if !errors.Is(err, scope.ErrNotFound) {
				t.Fatalf("ResolveAssignmentCandidate() error = %v, want enumeration-safe not found", err)
			}
		})
	}
}
