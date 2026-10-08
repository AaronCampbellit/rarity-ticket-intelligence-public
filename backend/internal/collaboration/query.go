package collaboration

import (
	"context"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mentions"
)

// ListQuery carries only authenticated identity and the requested parent.
// Tenant and client scope are always taken from Principal, never HTTP input.
type ListQuery struct {
	Principal authorization.Principal
	Parent    ParentRef
}

// ListedSource identifies immutable legacy rows separately from native,
// structured collaboration sources. Legacy bodies are plain text and Tokens
// is always an empty, non-nil collection.
type ListedSource struct {
	Source   Source
	ReadOnly bool
	Legacy   bool
}

type ListRepository interface {
	ListInternalContent(context.Context, ListQuery) ([]ListedSource, error)
}

type ListService struct{ repository ListRepository }

func NewListService(repository ListRepository) *ListService {
	return &ListService{repository: repository}
}

func (s *ListService) ListInternalContent(ctx context.Context, query ListQuery) ([]ListedSource, error) {
	if s == nil || s.repository == nil || !validPrincipal(query.Principal.ID, query.Principal.Scope) || !validParent(query.Parent) {
		return []ListedSource{}, ErrInvalid
	}
	rows, err := s.repository.ListInternalContent(ctx, query)
	if err != nil {
		return []ListedSource{}, err
	}
	if rows == nil {
		rows = []ListedSource{}
	}
	for index := range rows {
		if rows[index].Source.Tokens == nil {
			rows[index].Source.Tokens = []mentions.Token{}
		}
	}
	return rows, nil
}
