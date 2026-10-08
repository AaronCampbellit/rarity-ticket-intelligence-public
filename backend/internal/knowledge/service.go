// Package knowledge owns internal-only versioned knowledge publication.
package knowledge

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
)

var (
	ErrInvalid                  = errors.New("invalid knowledge publication")
	ErrClientVisibilityDeferred = errors.New("client-visible knowledge is deferred")
	ErrDraftStateConflict       = errors.New("knowledge draft state conflict")
	ErrIdentityConflict         = errors.New("knowledge identity conflict")
	ErrAmbiguousReference       = errors.New("ambiguous knowledge reference")
)

type State string

const (
	Draft     State = "draft"
	Published State = "published"
	Archived  State = "archived"
)

type Article struct {
	ID             string    `json:"id"`
	MSPID          string    `json:"msp_id"`
	ClientID       string    `json:"client_id"`
	DisplayID      string    `json:"display_id"`
	Title          string    `json:"title"`
	State          State     `json:"state"`
	CurrentVersion int64     `json:"current_version"`
	ClientVisible  bool      `json:"client_visible"`
	UpdatedAt      time.Time `json:"updated_at"`
	UpdatedBy      string    `json:"updated_by"`
}

type Version struct {
	ArticleID   string     `json:"article_id"`
	Version     int64      `json:"version"`
	Body        string     `json:"body"`
	State       State      `json:"state"`
	CreatedAt   time.Time  `json:"created_at"`
	CreatedBy   string     `json:"created_by"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
	PublishedBy string     `json:"published_by,omitempty"`
}

type ArticleDetail struct {
	Article Article `json:"article"`
	Version Version `json:"version"`
}

type CreateDraftCommand struct {
	Principal            authorization.Principal
	Target               scope.Target
	ArticleID            string
	DisplayID            string
	Title                string
	Body                 string
	ActorID              string
	Source               string
	TagIDs               []string
	ClassificationPolicy tagging.CreationPolicy
	CorrelationID        string
}

type ReviseDraftCommand struct {
	Principal       authorization.Principal
	Target          scope.Target
	ArticleID       string
	ExpectedVersion int64
	Title           string
	Body            string
	ActorID         string
	Source          string
	CorrelationID   string
}

type FindCommand struct {
	Principal authorization.Principal
	Target    scope.Target
	ArticleID string
}

type ResolveReferenceCommand struct {
	Principal authorization.Principal
	Target    scope.Target
	Reference string
}

type ListCommand struct {
	Principal authorization.Principal
	Target    scope.Target
	Query     string
	Limit     int
}

type DraftMutation struct {
	Article     Article
	Version     Version
	Created     bool
	Audit       mutation.AuditRecord
	Event       mutation.EventRecord
	InitialTags tagging.InitialAssignmentSet
}

type PublishCommand struct {
	Principal       authorization.Principal
	Target          scope.Target
	ArticleID       string
	ExpectedVersion int64
	// ExpectedClientVersion is adapter-owned and fences an active Client at
	// confirmation. A zero value preserves the ordinary HTTP caller contract.
	ExpectedClientVersion int64
	ActorID               string
	Source                string
	Reason                string
	CorrelationID         string
}

type PublishMutation struct {
	Article               Article
	Version               Version
	ExpectedClientVersion int64
	Audit                 mutation.AuditRecord
	Event                 mutation.EventRecord
}

type Repository interface {
	List(context.Context, scope.Target, string, int) ([]Article, error)
	FindByReference(context.Context, scope.Target, string, int) ([]Article, error)
	LoadDraft(context.Context, scope.Target, string, int64) (Article, Version, error)
	Find(context.Context, scope.Target, string) (ArticleDetail, error)
	CreateDraftAtomic(context.Context, DraftMutation) error
	ReviseDraftAtomic(context.Context, DraftMutation) error
	PublishAtomic(context.Context, PublishMutation) error
}

type Service struct {
	repository Repository
	now        func() time.Time
	newID      func() string
	creation   *tagging.CreationPreparer
	guard      *tagging.TerminalGuard
}

func NewService(repository Repository, now func() time.Time, newID func() string, classification ...any) *Service {
	service := &Service{repository: repository, now: now, newID: newID}
	for _, dependency := range classification {
		switch value := dependency.(type) {
		case *tagging.CreationPreparer:
			service.creation = value
		case *tagging.TerminalGuard:
			service.guard = value
		}
	}
	return service
}

func (s *Service) List(
	ctx context.Context,
	command ListCommand,
) ([]Article, error) {
	target := knowledgeTarget(command.Target, command.Principal)
	query := strings.TrimSpace(command.Query)
	if target.ClientID == "" || command.Limit < 1 || command.Limit > 100 ||
		len(query) > 200 {
		return nil, ErrInvalid
	}
	if err := authorization.Authorize(
		command.Principal, "knowledge.read", target,
	); err != nil {
		return nil, err
	}
	return s.repository.List(ctx, target, query, command.Limit)
}

// ResolveReference returns one exact internal article identity match, no match,
// or ErrAmbiguousReference. It never broadens a Client's knowledge enumeration
// surface.
func (s *Service) ResolveReference(
	ctx context.Context,
	command ResolveReferenceCommand,
) ([]Article, error) {
	return s.resolveReference(ctx, command, "knowledge.read")
}

// ResolveDraftReference gives the draft workflow an exact lookup without
// broadening it through the unrelated read catalog capability.
func (s *Service) ResolveDraftReference(
	ctx context.Context,
	command ResolveReferenceCommand,
) ([]Article, error) {
	return s.resolveReference(ctx, command, "knowledge.edit")
}

// ResolvePublicationReference gives the publication workflow an exact draft
// lookup under its own capability, without granting a publisher broad edit or
// read access.
func (s *Service) ResolvePublicationReference(
	ctx context.Context,
	command ResolveReferenceCommand,
) ([]Article, error) {
	return s.resolveReference(ctx, command, "knowledge.publish")
}

func (s *Service) resolveReference(
	ctx context.Context,
	command ResolveReferenceCommand,
	capability string,
) ([]Article, error) {
	target := knowledgeTarget(command.Target, command.Principal)
	if target.ClientID == "" || normalizeArticleReference(command.Reference) == "" ||
		s.repository == nil {
		return nil, ErrInvalid
	}
	if err := authorization.Authorize(command.Principal, capability, target); err != nil {
		return nil, err
	}
	matches, err := s.repository.FindByReference(ctx, target, command.Reference, 2)
	if err != nil {
		return nil, err
	}
	if len(matches) > 1 {
		return nil, ErrAmbiguousReference
	}
	return matches, nil
}

func (s *Service) FindDraft(
	ctx context.Context,
	command FindCommand,
) (ArticleDetail, error) {
	return s.find(ctx, command, "knowledge.edit")
}

// FindPublicationDraft loads the exact internal Article for publication under
// the publication capability. The AI adapter still verifies draft state and
// body before showing or confirming a proposal.
func (s *Service) FindPublicationDraft(
	ctx context.Context,
	command FindCommand,
) (ArticleDetail, error) {
	return s.find(ctx, command, "knowledge.publish")
}

func (s *Service) CreateDraft(
	ctx context.Context,
	command CreateDraftCommand,
) (ArticleDetail, error) {
	target := knowledgeTarget(command.Target, command.Principal)
	displayID := strings.TrimSpace(command.DisplayID)
	title := strings.TrimSpace(command.Title)
	body := strings.TrimSpace(command.Body)
	if target.ClientID == "" || displayID == "" || title == "" || body == "" ||
		command.ActorID == "" || command.Source == "" {
		return ArticleDetail{}, ErrInvalid
	}
	if err := authorization.Authorize(command.Principal, "knowledge.edit", target); err != nil {
		return ArticleDetail{}, err
	}
	initial, err := s.initialTags(ctx, target, command)
	if err != nil {
		return ArticleDetail{}, err
	}
	now := s.now().UTC()
	articleID := strings.TrimSpace(command.ArticleID)
	if articleID == "" {
		articleID = s.newID()
	}
	detail := ArticleDetail{
		Article: Article{
			ID: articleID, MSPID: target.MSPID, ClientID: target.ClientID,
			DisplayID: displayID, Title: title, State: Draft, CurrentVersion: 1,
			UpdatedAt: now, UpdatedBy: command.ActorID,
		},
		Version: Version{
			ArticleID: articleID, Version: 1, Body: body, State: Draft,
			CreatedAt: now, CreatedBy: command.ActorID,
		},
	}
	accepted := draftMutation(
		detail, true, "knowledge.draft.created",
		command.ActorID, command.Source, command.CorrelationID, now, s.newID,
	)
	accepted.InitialTags = initial.WithProvenance(tagging.InitialAssignmentProvenance{ActorType: accepted.Event.ActorType, ActorID: accepted.Event.ActorID, OccurredAt: accepted.Event.OccurredAt, CorrelationID: accepted.Event.CorrelationID})
	if err := s.repository.CreateDraftAtomic(ctx, accepted); err != nil {
		return ArticleDetail{}, err
	}
	return detail, nil
}

func (s *Service) ReviseDraft(
	ctx context.Context,
	command ReviseDraftCommand,
) (ArticleDetail, error) {
	target := knowledgeTarget(command.Target, command.Principal)
	title := strings.TrimSpace(command.Title)
	body := strings.TrimSpace(command.Body)
	if target.ClientID == "" || command.ArticleID == "" ||
		command.ExpectedVersion < 1 || title == "" || body == "" ||
		command.ActorID == "" || command.Source == "" {
		return ArticleDetail{}, ErrInvalid
	}
	if err := authorization.Authorize(command.Principal, "knowledge.edit", target); err != nil {
		return ArticleDetail{}, err
	}
	current, err := s.repository.Find(ctx, target, command.ArticleID)
	if err != nil {
		return ArticleDetail{}, err
	}
	if current.Article.MSPID != target.MSPID ||
		current.Article.ClientID != target.ClientID {
		return ArticleDetail{}, scope.ErrNotFound
	}
	if current.Article.ClientVisible {
		return ArticleDetail{}, ErrClientVisibilityDeferred
	}
	if current.Article.State != Draft || current.Version.State != Draft {
		return ArticleDetail{}, ErrDraftStateConflict
	}
	if err := object.RequireVersion(
		current.Article.CurrentVersion, command.ExpectedVersion,
	); err != nil {
		return ArticleDetail{}, err
	}
	now := s.now().UTC()
	current.Article.Title = title
	current.Article.State = Draft
	current.Article.CurrentVersion++
	current.Article.UpdatedAt = now
	current.Article.UpdatedBy = command.ActorID
	current.Version = Version{
		ArticleID: current.Article.ID, Version: current.Article.CurrentVersion,
		Body: body, State: Draft, CreatedAt: now, CreatedBy: command.ActorID,
	}
	accepted := draftMutation(
		current, false, "knowledge.draft.revised",
		command.ActorID, command.Source, command.CorrelationID, now, s.newID,
	)
	if err := s.repository.ReviseDraftAtomic(ctx, accepted); err != nil {
		return ArticleDetail{}, err
	}
	return current, nil
}

func (s *Service) Find(
	ctx context.Context,
	command FindCommand,
) (ArticleDetail, error) {
	return s.find(ctx, command, "knowledge.read")
}

func (s *Service) find(
	ctx context.Context,
	command FindCommand,
	capability string,
) (ArticleDetail, error) {
	target := knowledgeTarget(command.Target, command.Principal)
	if target.ClientID == "" || command.ArticleID == "" {
		return ArticleDetail{}, ErrInvalid
	}
	if err := authorization.Authorize(command.Principal, capability, target); err != nil {
		return ArticleDetail{}, err
	}
	detail, err := s.repository.Find(ctx, target, command.ArticleID)
	if err != nil {
		return ArticleDetail{}, err
	}
	if detail.Article.MSPID != target.MSPID ||
		detail.Article.ClientID != target.ClientID ||
		detail.Article.ClientVisible {
		return ArticleDetail{}, scope.ErrNotFound
	}
	return detail, nil
}

func (s *Service) Publish(ctx context.Context, command PublishCommand) (Version, error) {
	target := knowledgeTarget(command.Target, command.Principal)
	reason := strings.TrimSpace(command.Reason)
	if target.ClientID == "" ||
		command.ArticleID == "" ||
		command.ExpectedVersion < 1 ||
		command.ExpectedClientVersion < 0 ||
		command.ActorID == "" ||
		command.Source == "" || reason == "" {
		return Version{}, ErrInvalid
	}
	if err := authorization.Authorize(command.Principal, "knowledge.publish", target); err != nil {
		return Version{}, err
	}
	article, version, err := s.repository.LoadDraft(
		ctx, target, command.ArticleID, command.ExpectedVersion,
	)
	if err != nil {
		return Version{}, err
	}
	if article.MSPID != target.MSPID || article.ClientID != target.ClientID {
		return Version{}, scope.ErrNotFound
	}
	if article.ClientVisible {
		return Version{}, ErrClientVisibilityDeferred
	}
	if article.State != Draft || version.State != Draft || version.Body == "" {
		return Version{}, ErrInvalid
	}
	if s.guard != nil {
		if err := s.guard.RequireMeaningful(ctx, tagging.GuardCommand{Principal: command.Principal, Target: tagging.TargetRef{MSPID: target.MSPID, ClientID: target.ClientID, ObjectType: tagging.ObjectKnowledgeArticle, ObjectID: article.ID}}); err != nil {
			return Version{}, err
		}
	}
	if err := object.RequireVersion(article.CurrentVersion, command.ExpectedVersion); err != nil {
		return Version{}, err
	}
	now := s.now().UTC()
	article.State = Published
	article.UpdatedAt = now
	article.UpdatedBy = command.ActorID
	version.State = Published
	version.PublishedAt = &now
	version.PublishedBy = command.ActorID
	auditID := s.newID()
	eventID := s.newID()
	correlationID := strings.TrimSpace(command.CorrelationID)
	if correlationID == "" {
		correlationID = s.newID()
	}
	accepted := PublishMutation{
		Article: article, Version: version,
		ExpectedClientVersion: command.ExpectedClientVersion,
		Audit: mutation.AuditRecord{
			ID: auditID, OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID,
			ActorType: "technician", ActorID: command.ActorID,
			Action: "knowledge.published", SubjectType: "knowledge_article",
			SubjectID: article.ID, SubjectVersion: version.Version,
			Source: command.Source, Reason: reason, CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: eventID, EventType: "knowledge.published", SchemaVersion: 1,
			OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID,
			ActorType: "technician", ActorID: command.ActorID,
			SubjectType: "knowledge_article", SubjectID: article.ID,
			SubjectVersion: version.Version, Source: command.Source,
			CorrelationID: correlationID,
		},
	}
	if err := s.repository.PublishAtomic(ctx, accepted); err != nil {
		return Version{}, err
	}
	return version, nil
}

func (s *Service) initialTags(ctx context.Context, target scope.Target, command CreateDraftCommand) (tagging.InitialAssignmentSet, error) {
	if len(command.TagIDs) == 0 && command.ClassificationPolicy == "" {
		return tagging.InitialAssignmentSet{}, nil
	}
	if s == nil || s.creation == nil {
		return tagging.InitialAssignmentSet{}, ErrInvalid
	}
	return s.creation.Prepare(ctx, tagging.PrepareCreationCommand{MSPID: target.MSPID, ClientID: target.ClientID, ObjectType: tagging.ObjectKnowledgeArticle, TagIDs: command.TagIDs, Source: tagging.SourceHuman, Policy: command.ClassificationPolicy})
}

func knowledgeTarget(
	target scope.Target,
	principal authorization.Principal,
) scope.Target {
	if target.MSPID == "" {
		return scope.Target{
			MSPID: principal.Scope.MSPID, ClientID: principal.Scope.ClientID,
		}
	}
	return target
}

func ArticleReferenceMatches(reference, title, displayID string) bool {
	normalizedReference := normalizeArticleReference(reference)
	return normalizedReference != "" &&
		(normalizedReference == normalizeArticleReference(title) ||
			normalizedReference == normalizeArticleReference(displayID))
}

func normalizeArticleReference(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func draftMutation(
	detail ArticleDetail,
	created bool,
	action string,
	actorID string,
	source string,
	correlationID string,
	at time.Time,
	newID func() string,
) DraftMutation {
	correlationID = strings.TrimSpace(correlationID)
	if correlationID == "" {
		correlationID = newID()
	}
	return DraftMutation{
		Article: detail.Article, Version: detail.Version, Created: created,
		Audit: mutation.AuditRecord{
			ID: newID(), OccurredAt: at, MSPID: detail.Article.MSPID,
			ClientID: detail.Article.ClientID, ActorType: "technician",
			ActorID: actorID, Action: action, SubjectType: "knowledge_article",
			SubjectID:      detail.Article.ID,
			SubjectVersion: detail.Article.CurrentVersion,
			Source:         source, CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: newID(), EventType: action, SchemaVersion: 1,
			OccurredAt: at, MSPID: detail.Article.MSPID,
			ClientID: detail.Article.ClientID, ActorType: "technician",
			ActorID: actorID, SubjectType: "knowledge_article",
			SubjectID:      detail.Article.ID,
			SubjectVersion: detail.Article.CurrentVersion,
			Source:         source, CorrelationID: correlationID,
		},
	}
}
