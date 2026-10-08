// Package search exposes a client-scoped query boundary for derived indexes.
package search

import (
	"context"
	"errors"
	"strings"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

var ErrInvalidQuery = errors.New("invalid search query")

type Result struct {
	ID         string
	ObjectType string
	ClientID   string
	Title      string
	Snippet    string
}

type Query struct {
	Principal authorization.Principal
	Target    scope.Target
	Text      string
	Limit     int
}

type Repository interface {
	Search(context.Context, scope.Target, string, int) ([]Result, error)
}

type Service struct {
	repository Repository
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

func (s *Service) Search(ctx context.Context, query Query) ([]Result, error) {
	target := query.Target
	if target.MSPID == "" && query.Principal.Scope.ClientID != "" {
		target = scope.Target{
			MSPID: query.Principal.Scope.MSPID, ClientID: query.Principal.Scope.ClientID,
		}
	}
	text := strings.TrimSpace(query.Text)
	if target.MSPID == "" || target.ClientID == "" || len(text) < 2 ||
		query.Limit < 1 || query.Limit > 100 {
		return nil, ErrInvalidQuery
	}
	if err := authorization.Authorize(query.Principal, "search.read", target); err != nil {
		return nil, err
	}
	return s.repository.Search(ctx, target, text, query.Limit)
}
