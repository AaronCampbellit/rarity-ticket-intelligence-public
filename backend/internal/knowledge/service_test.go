package knowledge

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
)

type fakeRepository struct {
	article     Article
	references  []Article
	version     Version
	saved       PublishMutation
	created     DraftMutation
	revised     DraftMutation
	reviseCalls int
	calls       int
}

type knowledgeClassificationRepository struct{}

func (knowledgeClassificationRepository) ResolveTags(_ context.Context, _ string, ids []string) ([]tagging.Tag, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	return []tagging.Tag{{ID: "meaningful", InternalKey: "network", State: tagging.StateActive}}, nil
}

type knowledgeGuardRepository struct{ meaningful bool }

func (r knowledgeGuardRepository) Get(context.Context, tagging.TargetRef) (tagging.TaggedObject, error) {
	if !r.meaningful {
		return tagging.TaggedObject{}, nil
	}
	return tagging.TaggedObject{Effective: []tagging.Assignment{{Tag: tagging.Tag{ID: "meaningful", InternalKey: "network", State: tagging.StateActive}}}}, nil
}
func (knowledgeGuardRepository) ResolveTags(context.Context, string, []string) ([]tagging.Tag, error) {
	return nil, nil
}
func (knowledgeGuardRepository) Accepted(context.Context, tagging.TargetRef, string) (tagging.TaggedObject, bool, error) {
	return tagging.TaggedObject{}, false, nil
}
func (knowledgeGuardRepository) ReplaceDirect(context.Context, tagging.AssociationMutation) (tagging.TaggedObject, error) {
	return tagging.TaggedObject{}, nil
}
func (knowledgeGuardRepository) History(context.Context, tagging.TargetRef) ([]tagging.HistoryEntry, error) {
	return nil, nil
}

func knowledgeCreationPreparer() *tagging.CreationPreparer {
	return tagging.NewCreationPreparer(knowledgeClassificationRepository{})
}
func knowledgeGuard(meaningful bool) *tagging.TerminalGuard {
	return tagging.NewTerminalGuard(tagging.NewAssociationService(knowledgeGuardRepository{meaningful: meaningful}))
}
func (knowledgeClassificationRepository) FindUnclassified(_ context.Context, msp string) (tagging.Tag, error) {
	return tagging.Tag{ID: "unclassified", MSPID: msp, InternalKey: "unclassified", State: tagging.StateActive}, nil
}
func TestInitialTagsClassificationPolicyForKnowledge(t *testing.T) {
	s := &Service{creation: tagging.NewCreationPreparer(knowledgeClassificationRepository{})}
	target := scope.Target{MSPID: "msp", ClientID: "client"}
	if _, err := s.initialTags(context.Background(), target, CreateDraftCommand{Source: "api", ClassificationPolicy: tagging.CreationRequireMeaningful}); !errors.Is(err, tagging.ErrMeaningfulTagRequired) {
		t.Fatalf("interactive error=%v", err)
	}
	_, err := s.initialTags(context.Background(), target, CreateDraftCommand{Source: "automation", ClassificationPolicy: tagging.CreationAllowFallback})
	if !errors.Is(err, tagging.ErrInvalidAssociation) {
		t.Fatalf("untrusted fallback error=%v", err)
	}
}

func (r *fakeRepository) List(
	context.Context, scope.Target, string, int,
) ([]Article, error) {
	return []Article{r.article}, nil
}

func (r *fakeRepository) FindByReference(
	context.Context, scope.Target, string, int,
) ([]Article, error) {
	if r.references != nil {
		return append([]Article(nil), r.references...), nil
	}
	return []Article{r.article}, nil
}

func (r *fakeRepository) LoadDraft(_ context.Context, _ scope.Target, _ string, _ int64) (Article, Version, error) {
	return r.article, r.version, nil
}
func (r *fakeRepository) PublishAtomic(_ context.Context, mutation PublishMutation) error {
	r.calls++
	r.saved = mutation
	return nil
}
func (r *fakeRepository) CreateDraftAtomic(_ context.Context, mutation DraftMutation) error {
	r.created = mutation
	return nil
}
func (r *fakeRepository) ReviseDraftAtomic(_ context.Context, mutation DraftMutation) error {
	r.reviseCalls++
	r.revised = mutation
	return nil
}
func (r *fakeRepository) Find(_ context.Context, _ scope.Target, _ string) (ArticleDetail, error) {
	return ArticleDetail{Article: r.article, Version: r.version}, nil
}

func TestCreateAndReviseDraftPreserveClientScopeAndVersionHistory(t *testing.T) {
	repository := &fakeRepository{}
	at := time.Date(2026, time.July, 29, 18, 0, 0, 0, time.UTC)
	ids := []string{
		"article-id", "audit-create", "event-create", "correlation-create",
		"audit-revise", "event-revise", "correlation-revise",
	}
	service := NewService(repository, func() time.Time { return at }, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	}, knowledgeCreationPreparer(), knowledgeGuard(true))
	principal := authorization.Principal{
		ID: "actor", Scope: scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet("knowledge.edit"),
	}
	created, err := service.CreateDraft(context.Background(), CreateDraftCommand{
		Principal: principal, DisplayID: "KB-100", Title: "Reset a token",
		Body: "# Resolution\nReset token.", ActorID: "actor", Source: "api",
		TagIDs: []string{"meaningful"}, ClassificationPolicy: tagging.CreationRequireMeaningful,
	})
	if err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}
	if created.Article.ID != "article-id" || created.Article.ClientID != "client-id" ||
		created.Version.Version != 1 || created.Version.State != Draft ||
		repository.created.Event.EventType != "knowledge.draft.created" {
		t.Fatalf("draft creation incomplete: created=%+v mutation=%+v", created, repository.created)
	}

	repository.article = created.Article
	repository.version = created.Version
	revised, err := service.ReviseDraft(context.Background(), ReviseDraftCommand{
		Principal: principal, ArticleID: "article-id", ExpectedVersion: 1,
		Title: "Reset an access token", Body: "# Resolution\nRotate token.",
		ActorID: "actor", Source: "api",
	})
	if err != nil {
		t.Fatalf("ReviseDraft() error = %v", err)
	}
	if revised.Article.CurrentVersion != 2 || revised.Version.Version != 2 ||
		revised.Version.Body != "# Resolution\nRotate token." ||
		repository.revised.Event.EventType != "knowledge.draft.revised" {
		t.Fatalf("draft revision incomplete: revised=%+v mutation=%+v", revised, repository.revised)
	}
}

func TestCreateDraftRejectsInteractiveMissingTagsBeforePersistence(t *testing.T) {
	repository := &fakeRepository{}
	service := NewService(repository, time.Now, func() string { return "id" }, knowledgeCreationPreparer(), knowledgeGuard(true))
	principal := authorization.Principal{Scope: scope.Principal{MSPID: "msp-id", ClientID: "client-id"}, Capabilities: authorization.NewCapabilitySet("knowledge.edit")}
	_, err := service.CreateDraft(context.Background(), CreateDraftCommand{Principal: principal, DisplayID: "KB-100", Title: "Reset token", Body: "Reset it.", ActorID: "actor", Source: "api", ClassificationPolicy: tagging.CreationRequireMeaningful})
	if !errors.Is(err, tagging.ErrMeaningfulTagRequired) || repository.created.Article.ID != "" {
		t.Fatalf("CreateDraft() error=%v mutation=%+v", err, repository.created)
	}
}

func TestKnowledgeDraftWritesAcceptOnlyTrustedPreparedIdentityAndCorrelation(t *testing.T) {
	repository := &fakeRepository{}
	at := time.Date(2026, time.August, 6, 14, 0, 0, 0, time.UTC)
	service := NewService(repository, func() time.Time { return at }, func() string {
		return "generated-id"
	})
	principal := authorization.Principal{
		ID: "actor", Scope: scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet("knowledge.edit"),
	}
	created, err := service.CreateDraft(context.Background(), CreateDraftCommand{
		Principal: principal, Target: scope.Target{MSPID: "msp-id", ClientID: "client-id"},
		ArticleID: "prepared-article", DisplayID: "KB-200", Title: "VPN recovery",
		Body: "Exact supplied body.", ActorID: "actor", Source: "ai_workspace",
		CorrelationID: "proposal-correlation",
	})
	if err != nil {
		t.Fatalf("CreateDraft() error=%v", err)
	}
	if created.Article.ID != "prepared-article" ||
		repository.created.Audit.CorrelationID != "proposal-correlation" ||
		repository.created.Event.CorrelationID != "proposal-correlation" ||
		repository.created.Audit.Source != "ai_workspace" {
		t.Fatalf("created=%+v mutation=%+v", created, repository.created)
	}

	repository.article, repository.version = created.Article, created.Version
	_, err = service.ReviseDraft(context.Background(), ReviseDraftCommand{
		Principal: principal, Target: scope.Target{MSPID: "msp-id", ClientID: "client-id"},
		ArticleID: "prepared-article", ExpectedVersion: 1, Title: "VPN recovery revised",
		Body: "Exact revised body.", ActorID: "actor", Source: "ai_workspace",
		CorrelationID: "proposal-revise-correlation",
	})
	if err != nil {
		t.Fatalf("ReviseDraft() error=%v", err)
	}
	if repository.revised.Audit.CorrelationID != "proposal-revise-correlation" ||
		repository.revised.Event.CorrelationID != "proposal-revise-correlation" {
		t.Fatalf("mutation=%+v", repository.revised)
	}
}

func TestKnowledgeDraftReviseRejectsPublishedCurrentStateWithoutMutation(t *testing.T) {
	repository := &fakeRepository{
		article: Article{
			ID: "article-id", MSPID: "msp-id", ClientID: "client-id",
			DisplayID: "KB-200", Title: "Published title", State: Published,
			CurrentVersion: 3,
		},
		version: Version{
			ArticleID: "article-id", Version: 3, Body: "Published body",
			State: Published,
		},
	}
	service := NewService(repository, time.Now, func() string { return "unused" })
	principal := authorization.Principal{
		ID: "actor", Scope: scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet("knowledge.edit"),
	}

	_, err := service.ReviseDraft(context.Background(), ReviseDraftCommand{
		Principal: principal, ArticleID: "article-id", ExpectedVersion: 3,
		Title: "Must not overwrite publication", Body: "Must not create version four",
		ActorID: "actor", Source: "api",
	})
	if !errors.Is(err, ErrDraftStateConflict) {
		t.Fatalf("ReviseDraft() error=%v, want ErrDraftStateConflict", err)
	}
	if repository.reviseCalls != 0 || repository.revised.Article.ID != "" {
		t.Fatalf("published draft reached mutation: calls=%d mutation=%+v", repository.reviseCalls, repository.revised)
	}
}

func TestFindRequiresKnowledgeReadAndReturnsCurrentVersion(t *testing.T) {
	repository := &fakeRepository{
		article: Article{
			ID: "article", MSPID: "msp-id", ClientID: "client-id",
			DisplayID: "KB-100", Title: "Reset token", State: Published,
			CurrentVersion: 2,
		},
		version: Version{
			ArticleID: "article", Version: 2, Body: "Published body", State: Published,
		},
	}
	service := NewService(repository, time.Now, func() string { return "unused" }, knowledgeCreationPreparer(), knowledgeGuard(true))
	principal := authorization.Principal{
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet("knowledge.read"),
	}
	detail, err := service.Find(context.Background(), FindCommand{
		Principal: principal, ArticleID: "article",
	})
	if err != nil || detail.Version.Body != "Published body" {
		t.Fatalf("Find() detail=%+v error=%v", detail, err)
	}
	principal.Capabilities = authorization.NewCapabilitySet()
	if _, err := service.Find(context.Background(), FindCommand{
		Principal: principal, ArticleID: "article",
	}); !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("unauthorized Find() error=%v", err)
	}
}

func TestListRequiresKnowledgeReadAndClientScope(t *testing.T) {
	repository := &fakeRepository{
		article: Article{
			ID: "article", MSPID: "msp-id", ClientID: "client-id",
			DisplayID: "KB-100", Title: "Reset token", State: Published,
		},
	}
	service := NewService(repository, time.Now, func() string { return "unused" }, knowledgeCreationPreparer(), knowledgeGuard(true))
	principal := authorization.Principal{
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet("knowledge.read"),
	}
	articles, err := service.List(context.Background(), ListCommand{
		Principal: principal, Query: "token", Limit: 50,
	})
	if err != nil || len(articles) != 1 || articles[0].ID != "article" {
		t.Fatalf("List() articles=%+v error=%v", articles, err)
	}
	principal.Scope.ClientID = ""
	if _, err := service.List(context.Background(), ListCommand{
		Principal: principal, Limit: 50,
	}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("MSP-global List() error=%v", err)
	}
}

func TestKnowledgeResolverDistinguishesMissingAndAmbiguousExactReferences(t *testing.T) {
	repository := &fakeRepository{references: []Article{}}
	service := NewService(repository, time.Now, func() string { return "unused" })
	principal := authorization.Principal{
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet("knowledge.read"),
	}

	found, err := service.ResolveReference(
		context.Background(),
		ResolveReferenceCommand{Principal: principal, Reference: "Missing"},
	)
	if err != nil || len(found) != 0 {
		t.Fatalf("missing found=%+v error=%v", found, err)
	}

	repository.references = []Article{
		{ID: "article-1", DisplayID: "KB-1", Title: "VPN recovery"},
		{ID: "article-2", DisplayID: "KB-2", Title: "VPN recovery"},
	}
	found, err = service.ResolveReference(
		context.Background(),
		ResolveReferenceCommand{Principal: principal, Reference: "VPN recovery"},
	)
	if !errors.Is(err, ErrAmbiguousReference) || found != nil {
		t.Fatalf(
			"ambiguous found=%+v error=%v, want ErrAmbiguousReference",
			found,
			err,
		)
	}
}

func TestPublishCreatesImmutableInternalVersionWithAuditEvent(t *testing.T) {
	repository := &fakeRepository{
		article: Article{ID: "article-id", MSPID: "msp-id", ClientID: "client-id", State: Draft, CurrentVersion: 2},
		version: Version{ArticleID: "article-id", Version: 2, Body: "# Resolution\nReset token.", State: Draft},
	}
	ids := []string{"audit-id", "event-id", "correlation-id"}
	service := NewService(repository, time.Now, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	}, knowledgeCreationPreparer(), knowledgeGuard(true))
	principal := authorization.Principal{
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet("knowledge.publish"),
	}
	published, err := service.Publish(context.Background(), PublishCommand{
		Principal: principal, ArticleID: "article-id", ExpectedVersion: 2,
		ActorID: "actor-id", Source: "web", Reason: "  Reviewed for internal publication  ",
	})
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if published.State != Published || published.PublishedAt == nil {
		t.Fatalf("unexpected published version: %+v", published)
	}
	if repository.saved.Article.UpdatedBy != "actor-id" ||
		repository.saved.Article.UpdatedAt.IsZero() {
		t.Fatalf("article publication metadata missing: %+v", repository.saved.Article)
	}
	if repository.saved.Event.EventType != "knowledge.published" ||
		repository.saved.Audit.Action != "knowledge.published" ||
		repository.saved.Audit.Reason != "Reviewed for internal publication" {
		t.Fatal("knowledge publication lacks audit/event facts")
	}
}

func TestPublishRejectsBlankReasonBeforeRepositoryAccess(t *testing.T) {
	repository := &fakeRepository{}
	service := NewService(repository, time.Now, func() string { return "unused" })
	principal := authorization.Principal{
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet("knowledge.publish"),
	}
	_, err := service.Publish(context.Background(), PublishCommand{
		Principal: principal, ArticleID: "article-id", ExpectedVersion: 2,
		ActorID: "actor-id", Source: "web", Reason: " \t ",
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("Publish() error=%v, want ErrInvalid", err)
	}
	if repository.calls != 0 {
		t.Fatalf("PublishAtomic calls=%d, want 0", repository.calls)
	}
}

func TestPublishRejectsClientVisibleOrUnauthorizedPublication(t *testing.T) {
	repository := &fakeRepository{
		article: Article{ID: "article-id", MSPID: "msp-id", ClientID: "client-id", State: Draft, CurrentVersion: 1, ClientVisible: true},
		version: Version{ArticleID: "article-id", Version: 1, Body: "body", State: Draft},
	}
	service := NewService(repository, time.Now, func() string { return "unused" }, knowledgeCreationPreparer(), knowledgeGuard(true))
	principal := authorization.Principal{
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet("knowledge.publish"),
	}
	_, err := service.Publish(context.Background(), PublishCommand{
		Principal: principal, ArticleID: "article-id", ExpectedVersion: 1,
		ActorID: "actor-id", Source: "web", Reason: "Reviewed for internal use",
	})
	if !errors.Is(err, ErrClientVisibilityDeferred) {
		t.Fatalf("client-visible publish error = %v", err)
	}

	repository.article.ClientVisible = false
	principal.Capabilities = authorization.NewCapabilitySet()
	_, err = service.Publish(context.Background(), PublishCommand{
		Principal: principal, ArticleID: "article-id", ExpectedVersion: 1,
		ActorID: "actor-id", Source: "web", Reason: "Reviewed for internal use",
	})
	if !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("unauthorized publish error = %v", err)
	}
}

func TestPublishBlocksUnclassifiedArticleBeforeAtomicMutation(t *testing.T) {
	repository := &fakeRepository{
		article: Article{ID: "article-id", MSPID: "msp-id", ClientID: "client-id", State: Draft, CurrentVersion: 1},
		version: Version{ArticleID: "article-id", Version: 1, Body: "body", State: Draft},
	}
	service := NewService(repository, time.Now, func() string { return "unused" }, knowledgeCreationPreparer(), knowledgeGuard(false))
	_, err := service.Publish(context.Background(), PublishCommand{
		Principal: authorization.Principal{Scope: scope.Principal{MSPID: "msp-id", ClientID: "client-id"}, Capabilities: authorization.NewCapabilitySet("knowledge.publish")},
		ArticleID: "article-id", ExpectedVersion: 1, ActorID: "actor-id", Source: "api", Reason: "Reviewed",
	})
	if !errors.Is(err, tagging.ErrMeaningfulTagRequired) || repository.calls != 0 {
		t.Fatalf("Publish() error=%v calls=%d", err, repository.calls)
	}
}
