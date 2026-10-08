package aiassist

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
)

var (
	ErrUnsafeKnowledgeSource = errors.New("unsafe product knowledge source")
	ErrInvalidKnowledgeQuery = errors.New("invalid product knowledge query")
)

type ProductAudience string

const (
	ProductAudienceAllUsers       ProductAudience = "all_users"
	ProductAudienceAdministrators ProductAudience = "administrators"
)

type ProductDocument struct {
	ID            string
	SourceKey     string
	SourceVersion string
	Section       string
	Audience      ProductAudience
	Body          string
}

type Citation struct {
	SourceKey     string `json:"source_key"`
	SourceVersion string `json:"source_version"`
	Section       string `json:"section"`
	Excerpt       string `json:"excerpt"`
}

type ProductKnowledgeStore interface {
	SearchProductKnowledge(context.Context, string, ProductAudience, int) ([]Citation, error)
}

type ProductKnowledgeService struct {
	store ProductKnowledgeStore
}

func NewProductKnowledgeService(store ProductKnowledgeStore) *ProductKnowledgeService {
	return &ProductKnowledgeService{store: store}
}

func ValidateProductDocument(document ProductDocument) error {
	source := strings.ToLower(filepath.ToSlash(strings.TrimSpace(document.SourceKey)))
	body := strings.ToLower(document.Body)
	if (!strings.HasPrefix(source, "docs/") && source != "readme.md") ||
		containsAny(source, "recovery", "break-glass", "secret", "credential", "token", ".env") ||
		containsAny(body, "secret token", "api key", "client secret", "password=", "credential ciphertext") ||
		strings.TrimSpace(document.SourceVersion) == "" ||
		strings.TrimSpace(document.Section) == "" ||
		strings.TrimSpace(document.Body) == "" ||
		(document.Audience != ProductAudienceAllUsers && document.Audience != ProductAudienceAdministrators) {
		return ErrUnsafeKnowledgeSource
	}
	return nil
}

func (s *ProductKnowledgeService) Search(
	ctx context.Context,
	query string,
	audience ProductAudience,
	limit int,
) ([]Citation, error) {
	query = strings.TrimSpace(query)
	if s == nil || s.store == nil || query == "" ||
		(audience != ProductAudienceAllUsers && audience != ProductAudienceAdministrators) {
		return nil, ErrInvalidKnowledgeQuery
	}
	if limit <= 0 {
		limit = 10
	}
	if limit > 20 {
		limit = 20
	}
	return s.store.SearchProductKnowledge(ctx, query, audience, limit)
}

func containsAny(value string, fragments ...string) bool {
	for _, fragment := range fragments {
		if strings.Contains(value, fragment) {
			return true
		}
	}
	return false
}
