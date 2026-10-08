// Package auditlog exposes the append-only audit ledger through a read-only,
// authorization-aware query boundary.
package auditlog

import (
	"context"
	"errors"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

var ErrInvalidQuery = errors.New("invalid audit query")

type Entry struct {
	ID             string    `json:"id"`
	OccurredAt     time.Time `json:"occurred_at"`
	ClientID       string    `json:"client_id,omitempty"`
	ActorType      string    `json:"actor_type"`
	ActorID        string    `json:"actor_id"`
	Action         string    `json:"action"`
	SubjectType    string    `json:"subject_type"`
	SubjectID      string    `json:"subject_id"`
	SubjectVersion int64     `json:"subject_version"`
	Source         string    `json:"source"`
	Reason         string    `json:"reason,omitempty"`
	CorrelationID  string    `json:"correlation_id"`
}

type Repository interface {
	List(context.Context, scope.Target, int) ([]Entry, error)
}

type Service struct {
	repository Repository
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

func (s *Service) List(
	ctx context.Context,
	principal authorization.Principal,
	limit int,
) ([]Entry, error) {
	target := scope.Target{
		MSPID: principal.Scope.MSPID, ClientID: principal.Scope.ClientID,
	}
	if limit < 1 || limit > 100 {
		return nil, ErrInvalidQuery
	}
	if err := authorization.Authorize(principal, "audit.read", target); err != nil {
		return nil, err
	}
	return s.repository.List(ctx, target, limit)
}
