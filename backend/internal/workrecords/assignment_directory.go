package workrecords

import (
	"context"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/objectidentity"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type AssignmentCandidate struct {
	ID, MSPID, DisplayName, Email string
	Version                       int64
}

type AssignmentDirectory interface {
	ResolveAssignmentCandidates(
		context.Context, scope.Target, string, int,
	) ([]AssignmentCandidate, error)
	FindAssignmentIdentity(context.Context, scope.Target, string) (AssignmentCandidate, error)
}

// ResolveAssignmentCandidate deliberately gives the same not-found response
// for zero and multiple exact matches so callers cannot enumerate technicians.
func ResolveAssignmentCandidate(
	ctx context.Context,
	directory AssignmentDirectory,
	target scope.Target,
	reference string,
) (AssignmentCandidate, error) {
	normalized := objectidentity.Normalize(reference)
	if directory == nil || target.MSPID == "" || target.ClientID == "" || normalized == "" {
		return AssignmentCandidate{}, ErrInvalid
	}
	candidates, err := directory.ResolveAssignmentCandidates(ctx, target, normalized, 2)
	if err != nil {
		return AssignmentCandidate{}, err
	}
	if len(candidates) != 1 {
		return AssignmentCandidate{}, scope.ErrNotFound
	}
	return candidates[0], nil
}
