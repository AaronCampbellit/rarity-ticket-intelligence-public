package aiassist

import (
	"context"
	"errors"
	"testing"
)

type knowledgeStoreStub struct {
	query    string
	audience ProductAudience
	limit    int
	results  []Citation
}

func (s *knowledgeStoreStub) SearchProductKnowledge(_ context.Context, query string, audience ProductAudience, limit int) ([]Citation, error) {
	s.query, s.audience, s.limit = query, audience, limit
	return s.results, nil
}

func TestProductKnowledgeRejectsProtectedSourcesAndBoundsSearch(t *testing.T) {
	for _, document := range []ProductDocument{
		{SourceKey: "docs/recovery-access.md", SourceVersion: "v1", Section: "Reset", Audience: ProductAudienceAdministrators, Body: "instructions"},
		{SourceKey: "docs/guide.md", SourceVersion: "v1", Section: "Token", Audience: ProductAudienceAllUsers, Body: "paste the secret token"},
		{SourceKey: ".env", SourceVersion: "v1", Section: "Runtime", Audience: ProductAudienceAdministrators, Body: "configuration"},
	} {
		if err := ValidateProductDocument(document); !errors.Is(err, ErrUnsafeKnowledgeSource) {
			t.Fatalf("ValidateProductDocument(%q) error=%v", document.SourceKey, err)
		}
	}

	store := &knowledgeStoreStub{results: []Citation{{SourceKey: "docs/03-user-guide/work.md"}}}
	service := NewProductKnowledgeService(store)
	results, err := service.Search(context.Background(), "  ticket routing  ", ProductAudienceAllUsers, 500)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || store.query != "ticket routing" || store.limit != 20 {
		t.Fatalf("results=%+v query=%q limit=%d", results, store.query, store.limit)
	}
}
