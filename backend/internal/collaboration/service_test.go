package collaboration

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mentions"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

func TestCreateInternalCommentCommitsSourceAndMentionProjectionTogether(t *testing.T) {
	repository := newAtomicRepository(mentions.SourceComment)
	repository.direct = []mentions.MemberAccess{eligibleCollaborationMember("tech-2")}
	service := newTestService(repository)
	source, err := service.CreateComment(context.Background(), CreateCommand{
		Principal: internalAuthor(), Parent: workParent(), Body: "@Mira please review",
		Tokens:         []mentions.Token{collaborationStaffToken("token-1", "tech-2", "@Mira", 0, 5)},
		IdempotencyKey: "request-1", Source: "work.comment",
	})
	if err != nil || source.ID == "" || repository.commits != 1 {
		t.Fatalf("source=%+v commits=%d error=%v", source, repository.commits, err)
	}
	accepted := repository.accepted
	if !reflect.DeepEqual(accepted.Source, source) || len(accepted.Mentions.Occurrences) != 1 ||
		len(accepted.Mentions.Items) != 1 || accepted.Mentions.Items[0].State != mentions.Unread ||
		accepted.Mentions.Event == nil || accepted.IdempotencyKey != "request-1" {
		t.Fatalf("accepted=%+v", accepted)
	}
	if source.MSPID != "msp-a" || source.ClientID != "client-a" || source.Version != 1 ||
		source.Kind != mentions.SourceComment || source.Body != "@Mira please review" {
		t.Fatalf("source=%+v", source)
	}
	if repository.loadedSourceID != "" || repository.lookup.ClientID != "" ||
		repository.authorizeCalls == 0 {
		t.Fatalf("lookup=%+v source id=%q authorize=%d", repository.lookup, repository.loadedSourceID, repository.authorizeCalls)
	}
	if !reflect.DeepEqual(repository.calls, []string{"load-source", "authorize", "load-idempotent", "authorize", "save"}) {
		t.Fatalf("calls=%v", repository.calls)
	}
	source.Tokens[0].ID = "caller-mutated"
	if repository.accepted.Source.Tokens[0].ID != "token-1" {
		t.Fatalf("returned source aliases accepted mutation: %+v", repository.accepted.Source.Tokens)
	}
}

func TestInternalDetailsNoteEditAndRedactUseExactLockedRevision(t *testing.T) {
	t.Run("put details", func(t *testing.T) {
		repository := newAtomicRepository(mentions.SourceDetails)
		source, err := newTestService(repository).PutDetails(context.Background(), UpsertCommand{
			Principal: internalAuthor(), Parent: workParent(), Kind: mentions.SourceDetails,
			Body: "Internal details", IdempotencyKey: "details-1", Source: "work.details",
		})
		if err != nil || source.Kind != mentions.SourceDetails || source.Version != 1 || repository.commits != 1 {
			t.Fatalf("source=%+v commits=%d error=%v", source, repository.commits, err)
		}
		if !reflect.DeepEqual(repository.calls, []string{"load-source", "authorize", "load-idempotent", "save"}) {
			t.Fatalf("calls=%v", repository.calls)
		}
	})

	t.Run("create note", func(t *testing.T) {
		repository := newAtomicRepository(mentions.SourceNote)
		source, err := newTestService(repository).CreateNote(context.Background(), CreateCommand{
			Principal: internalAuthor(), Parent: workParent(), Body: "Internal note",
			IdempotencyKey: "note-1", Source: "work.note",
		})
		if err != nil || source.Kind != mentions.SourceNote || source.Version != 1 || repository.commits != 1 {
			t.Fatalf("source=%+v commits=%d error=%v", source, repository.commits, err)
		}
	})

	t.Run("edit", func(t *testing.T) {
		repository := newAtomicRepository(mentions.SourceComment)
		repository.loaded = existingSource(mentions.SourceComment, 7)
		source, err := newTestService(repository).Edit(context.Background(), UpsertCommand{
			Principal: internalAuthor(), Parent: workParent(), SourceID: "source-1",
			Kind: mentions.SourceComment, Body: "Edited internal comment", ExpectedVersion: 7,
			IdempotencyKey: "edit-1", Source: "work.comment.edit",
		})
		if err != nil || source.Version != 8 || source.Body != "Edited internal comment" ||
			repository.loadedSourceID != "source-1" || repository.commits != 1 {
			t.Fatalf("source=%+v commits=%d loaded=%q error=%v", source, repository.commits, repository.loadedSourceID, err)
		}
	})

	t.Run("redact", func(t *testing.T) {
		repository := newAtomicRepository(mentions.SourceNote)
		repository.loaded = existingSource(mentions.SourceNote, 3)
		repository.loaded.Body = "@Mira secret"
		repository.loaded.Tokens = []mentions.Token{collaborationStaffToken("token-1", "tech-2", "@Mira", 0, 5)}
		source, err := newTestService(repository).Redact(context.Background(), RedactCommand{
			Principal: internalAuthor(), Parent: workParent(), SourceID: "source-1",
			Kind: mentions.SourceNote, ExpectedVersion: 3, IdempotencyKey: "redact-1", Source: "work.note.redact",
		})
		if err != nil || source.Version != 4 || source.LifecycleState != SourceRedacted ||
			source.Body != "" || len(source.Tokens) != 0 || source.RedactedAt == nil ||
			len(repository.accepted.Mentions.Occurrences) != 0 || repository.accepted.Mentions.Event != nil {
			t.Fatalf("source=%+v accepted=%+v error=%v", source, repository.accepted, err)
		}
	})
}

func TestInternalEditRejectsStaleVersionWithoutSave(t *testing.T) {
	repository := newAtomicRepository(mentions.SourceComment)
	repository.loaded = existingSource(mentions.SourceComment, 2)
	_, err := newTestService(repository).Edit(context.Background(), UpsertCommand{
		Principal: internalAuthor(), Parent: workParent(), SourceID: "source-1",
		Kind: mentions.SourceComment, Body: "Stale edit", ExpectedVersion: 1,
		IdempotencyKey: "edit-1", Source: "work.comment.edit",
	})
	if !errors.Is(err, object.ErrVersionConflict) || repository.saveCalls != 0 || repository.commits != 0 {
		t.Fatalf("error=%v saves=%d commits=%d", err, repository.saveCalls, repository.commits)
	}
}

func TestInternalIdempotentReplayReturnsPriorSourceWithoutDuplicateFacts(t *testing.T) {
	repository := newAtomicRepository(mentions.SourceComment)
	prior := existingSource(mentions.SourceComment, 1)
	repository.idempotent = &prior
	idCalls := 0
	service := NewService(repository, mentions.NewService(collaborationNow, func() string {
		idCalls++
		return "mention-generated"
	}), collaborationNow, func() string {
		idCalls++
		return "source-generated"
	})
	source, err := service.CreateComment(context.Background(), CreateCommand{
		Principal: internalAuthor(), Parent: workParent(), Body: "Original retry",
		IdempotencyKey: "request-1", Source: "work.comment",
	})
	if err != nil || !reflect.DeepEqual(source, prior) || repository.saveCalls != 0 || repository.commits != 0 || idCalls != 0 {
		t.Fatalf("source=%+v prior=%+v saves=%d commits=%d id calls=%d error=%v", source, prior, repository.saveCalls, repository.commits, idCalls, err)
	}
	if repository.idempotencyKey != "request-1" || repository.authorizeCalls != 1 {
		t.Fatalf("idempotency key=%q authorize=%d", repository.idempotencyKey, repository.authorizeCalls)
	}
}

func TestInternalReplayValidatesExactLockedActiveSourceBeforeIdempotencyLookup(t *testing.T) {
	repository := newAtomicRepository(mentions.SourceComment)
	repository.loaded = existingSource(mentions.SourceComment, 2)
	repository.loaded.Body = ""
	prior := existingSource(mentions.SourceComment, 1)
	prior.Body = "stale prior content"
	repository.idempotent = &prior

	source, err := newTestService(repository).Edit(context.Background(), UpsertCommand{
		Principal: internalAuthor(), Parent: workParent(), SourceID: "source-1",
		Kind: mentions.SourceComment, Body: "Retry", ExpectedVersion: 2,
		IdempotencyKey: "edit-1", Source: "work.comment.edit",
	})

	if !errors.Is(err, ErrInvalid) || !reflect.DeepEqual(source, Source{}) ||
		repository.saveCalls != 0 || repository.commits != 0 {
		t.Fatalf("source=%+v error=%v saves=%d commits=%d", source, err, repository.saveCalls, repository.commits)
	}
	if repository.idempotencyKey != "" || !reflect.DeepEqual(repository.calls, []string{"load-source", "authorize"}) {
		t.Fatalf("idempotency key=%q calls=%v", repository.idempotencyKey, repository.calls)
	}
}

func TestInternalExistingSourceReplaySucceedsAfterLockedSourceValidation(t *testing.T) {
	repository := newAtomicRepository(mentions.SourceComment)
	repository.loaded = existingSource(mentions.SourceComment, 2)
	prior := existingSource(mentions.SourceComment, 3)
	prior.Body = "Accepted prior edit"
	repository.idempotent = &prior

	source, err := newTestService(repository).Edit(context.Background(), UpsertCommand{
		Principal: internalAuthor(), Parent: workParent(), SourceID: "source-1",
		Kind: mentions.SourceComment, Body: "Retry", ExpectedVersion: 2,
		IdempotencyKey: "edit-1", Source: "work.comment.edit",
	})

	if err != nil || !reflect.DeepEqual(source, prior) || repository.saveCalls != 0 || repository.commits != 0 {
		t.Fatalf("source=%+v prior=%+v error=%v saves=%d commits=%d", source, prior, err, repository.saveCalls, repository.commits)
	}
	if !reflect.DeepEqual(repository.calls, []string{"load-source", "authorize", "load-idempotent"}) {
		t.Fatalf("calls=%v", repository.calls)
	}
}

func TestInternalIdempotentReplayRequiresTheExactLockedSource(t *testing.T) {
	repository := newAtomicRepository(mentions.SourceComment)
	repository.loaded = existingSource(mentions.SourceComment, 1)
	repository.loaded.ID = "other-source"
	prior := existingSource(mentions.SourceComment, 2)
	repository.idempotent = &prior
	_, err := newTestService(repository).Edit(context.Background(), UpsertCommand{
		Principal: internalAuthor(), Parent: workParent(), SourceID: "source-1",
		Kind: mentions.SourceComment, Body: "Retry", ExpectedVersion: 1,
		IdempotencyKey: "edit-1", Source: "work.comment.edit",
	})
	if !errors.Is(err, ErrInvalid) || repository.saveCalls != 0 || repository.commits != 0 {
		t.Fatalf("error=%v saves=%d commits=%d", err, repository.saveCalls, repository.commits)
	}
}

func TestInternalReplayCannotExposeContentAcrossRedactionState(t *testing.T) {
	t.Run("redaction replay must be redacted", func(t *testing.T) {
		repository := newAtomicRepository(mentions.SourceNote)
		repository.loaded = existingSource(mentions.SourceNote, 3)
		activeReplay := existingSource(mentions.SourceNote, 3)
		activeReplay.Body = "sensitive prior content"
		repository.idempotent = &activeReplay
		_, err := newTestService(repository).Redact(context.Background(), RedactCommand{
			Principal: internalAuthor(), Parent: workParent(), SourceID: "source-1",
			Kind: mentions.SourceNote, ExpectedVersion: 3,
			IdempotencyKey: "redact-1", Source: "work.note.redact",
		})
		if !errors.Is(err, ErrInvalid) || repository.saveCalls != 0 {
			t.Fatalf("error=%v saves=%d", err, repository.saveCalls)
		}
	})

	t.Run("edit replay cannot revive a redacted source", func(t *testing.T) {
		repository := newAtomicRepository(mentions.SourceComment)
		repository.loaded = existingSource(mentions.SourceComment, 4)
		repository.loaded.Body = ""
		repository.loaded.Tokens = nil
		repository.loaded.LifecycleState = SourceRedacted
		redactedAt := collaborationNow()
		repository.loaded.RedactedAt = &redactedAt
		activeReplay := existingSource(mentions.SourceComment, 3)
		activeReplay.Body = "sensitive prior content"
		repository.idempotent = &activeReplay
		_, err := newTestService(repository).Edit(context.Background(), UpsertCommand{
			Principal: internalAuthor(), Parent: workParent(), SourceID: "source-1",
			Kind: mentions.SourceComment, Body: "Retry", ExpectedVersion: 3,
			IdempotencyKey: "edit-1", Source: "work.comment.edit",
		})
		if !errors.Is(err, ErrSourceRedacted) || repository.saveCalls != 0 {
			t.Fatalf("error=%v saves=%d", err, repository.saveCalls)
		}
	})

	t.Run("valid redaction replay stays content-free", func(t *testing.T) {
		repository := newAtomicRepository(mentions.SourceNote)
		redactedAt := collaborationNow()
		repository.loaded = existingSource(mentions.SourceNote, 4)
		repository.loaded.Body = ""
		repository.loaded.Tokens = nil
		repository.loaded.LifecycleState = SourceRedacted
		repository.loaded.RedactedAt = &redactedAt
		prior := cloneSource(repository.loaded)
		repository.idempotent = &prior

		source, err := newTestService(repository).Redact(context.Background(), RedactCommand{
			Principal: internalAuthor(), Parent: workParent(), SourceID: "source-1",
			Kind: mentions.SourceNote, ExpectedVersion: 3,
			IdempotencyKey: "redact-1", Source: "work.note.redact",
		})

		if err != nil || source.Body != "" || len(source.Tokens) != 0 || source.RedactedAt == nil ||
			repository.saveCalls != 0 || repository.commits != 0 {
			t.Fatalf("source=%+v error=%v saves=%d commits=%d", source, err, repository.saveCalls, repository.commits)
		}
		if !reflect.DeepEqual(repository.calls, []string{"load-source", "authorize", "load-idempotent"}) {
			t.Fatalf("calls=%v", repository.calls)
		}
	})
}

func TestInternalMutationRollbackLeavesNoPartialFacts(t *testing.T) {
	repository := newAtomicRepository(mentions.SourceComment)
	repository.direct = []mentions.MemberAccess{eligibleCollaborationMember("tech-2")}
	repository.saveErr = errors.New("outbox failed")
	_, err := newTestService(repository).CreateComment(context.Background(), CreateCommand{
		Principal: internalAuthor(), Parent: workParent(), Body: "@Mira rollback",
		Tokens:         []mentions.Token{collaborationStaffToken("token-1", "tech-2", "@Mira", 0, 5)},
		IdempotencyKey: "request-1", Source: "work.comment",
	})
	if !errors.Is(err, repository.saveErr) || repository.commits != 0 || !reflect.DeepEqual(repository.accepted, Mutation{}) {
		t.Fatalf("error=%v commits=%d accepted=%+v", err, repository.commits, repository.accepted)
	}
}

func TestInternalGeneratedIDCollisionReturnsStableErrorBeforeSave(t *testing.T) {
	repository := newAtomicRepository(mentions.SourceComment)
	repository.direct = []mentions.MemberAccess{eligibleCollaborationMember("tech-2")}
	service := NewService(
		repository,
		mentions.NewService(collaborationNow, func() string { return "duplicate-id" }),
		collaborationNow,
		sequentialCollaborationIDs(),
	)
	_, err := service.CreateComment(context.Background(), CreateCommand{
		Principal: internalAuthor(), Parent: workParent(), Body: "@Mira collision",
		Tokens:         []mentions.Token{collaborationStaffToken("token-1", "tech-2", "@Mira", 0, 5)},
		IdempotencyKey: "request-1", Source: "work.comment",
	})
	if !errors.Is(err, mentions.ErrInvalidMutation) || repository.saveCalls != 0 || repository.commits != 0 {
		t.Fatalf("error=%v saves=%d commits=%d", err, repository.saveCalls, repository.commits)
	}
}

func TestInternalCommandsRejectMalformedInputBeforeRepositoryAccess(t *testing.T) {
	valid := CreateCommand{
		Principal: internalAuthor(), Parent: workParent(), Body: "Internal",
		IdempotencyKey: "request-1", Source: "work.comment",
	}
	tests := []struct {
		name    string
		command CreateCommand
	}{
		{name: "missing principal", command: func() CreateCommand { value := valid; value.Principal.ID = ""; return value }()},
		{name: "missing parent", command: func() CreateCommand { value := valid; value.Parent.ID = ""; return value }()},
		{name: "empty body", command: func() CreateCommand { value := valid; value.Body = " \n "; return value }()},
		{name: "oversized body", command: func() CreateCommand {
			value := valid
			value.Body = strings.Repeat("a", maximumBodyBytes+1)
			return value
		}()},
		{name: "bad idempotency", command: func() CreateCommand { value := valid; value.IdempotencyKey = "bad/key"; return value }()},
		{name: "missing source", command: func() CreateCommand { value := valid; value.Source = ""; return value }()},
		{name: "mismatched token", command: func() CreateCommand {
			value := valid
			value.Tokens = []mentions.Token{collaborationStaffToken("token-1", "tech-2", "@Mira", 0, 5)}
			return value
		}()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := newAtomicRepository(mentions.SourceComment)
			if _, err := newTestService(repository).CreateComment(context.Background(), test.command); !errors.Is(err, ErrInvalid) {
				t.Fatalf("CreateComment() error=%v", err)
			}
			if repository.transactions != 0 {
				t.Fatalf("invalid command opened %d transactions", repository.transactions)
			}
		})
	}
}

func TestPublicContentGuardRejectsStructuredMentionsWithoutRepositoryPath(t *testing.T) {
	if err := ValidatePublicContentTokens(nil); err != nil {
		t.Fatalf("plain public content error=%v", err)
	}
	if err := ValidatePublicContentTokens([]mentions.Token{{
		ID: "token-1", TargetType: mentions.TargetStaff, TargetID: "tech-2",
	}}); !errors.Is(err, ErrPublicMentionsForbidden) {
		t.Fatalf("public structured mention error=%v", err)
	}
}

func TestInternalTransactionDerivesAndEnforcesTrustedParentScope(t *testing.T) {
	repository := newAtomicRepository(mentions.SourceComment)
	repository.loaded.ClientID = "client-b"
	principal := internalAuthor()
	principal.Scope.ClientID = "client-a"
	_, err := newTestService(repository).CreateComment(context.Background(), CreateCommand{
		Principal: principal, Parent: workParent(), Body: "Internal",
		IdempotencyKey: "request-1", Source: "work.comment",
	})
	if !errors.Is(err, scope.ErrNotFound) || repository.saveCalls != 0 {
		t.Fatalf("scope error=%v saves=%d", err, repository.saveCalls)
	}
}

func TestInternalMutationRejectsMalformedLockedSourceWithoutSave(t *testing.T) {
	redactedAt := collaborationNow()
	for _, test := range []struct {
		name   string
		mutate func(*Source)
	}{
		{name: "empty active body", mutate: func(source *Source) { source.Body = "" }},
		{name: "active source with redaction timestamp", mutate: func(source *Source) { source.RedactedAt = &redactedAt }},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := newAtomicRepository(mentions.SourceComment)
			repository.loaded = existingSource(mentions.SourceComment, 2)
			test.mutate(&repository.loaded)
			_, err := newTestService(repository).Edit(context.Background(), UpsertCommand{
				Principal: internalAuthor(), Parent: workParent(), SourceID: "source-1",
				Kind: mentions.SourceComment, Body: "Replacement", ExpectedVersion: 2,
				IdempotencyKey: "edit-1", Source: "work.comment.edit",
			})
			if !errors.Is(err, ErrInvalid) || repository.saveCalls != 0 {
				t.Fatalf("error=%v saves=%d", err, repository.saveCalls)
			}
		})
	}
}

type atomicRepository struct {
	loaded        Source
	idempotent    *Source
	direct        []mentions.MemberAccess
	teams         []mentions.TeamAccess
	loadErr       error
	idempotentErr error
	authorizeErr  error
	saveErr       error

	lookup         mentions.SourceRef
	loadedSourceID string
	idempotencyKey string
	transactions   int
	authorizeCalls int
	saveCalls      int
	commits        int
	accepted       Mutation
	calls          []string
}

func newAtomicRepository(kind mentions.SourceKind) *atomicRepository {
	return &atomicRepository{loaded: Source{
		MSPID: "msp-a", ClientID: "client-a", Parent: workParent(), Kind: kind,
	}}
}

func (r *atomicRepository) WithTransaction(
	ctx context.Context,
	lookup mentions.SourceRef,
	callback func(Transaction) error,
) error {
	r.transactions++
	r.lookup = lookup
	tx := &atomicTransaction{repository: r}
	if err := callback(tx); err != nil {
		return err
	}
	if tx.saved {
		r.accepted = tx.pending
		r.commits++
	}
	return nil
}

type atomicTransaction struct {
	repository *atomicRepository
	pending    Mutation
	saved      bool
}

func (tx *atomicTransaction) LoadSourceForUpdate(_ context.Context, _ mentions.SourceRef, sourceID string) (Source, error) {
	tx.repository.calls = append(tx.repository.calls, "load-source")
	tx.repository.loadedSourceID = sourceID
	return tx.repository.loaded, tx.repository.loadErr
}

func (tx *atomicTransaction) LoadIdempotentSource(_ context.Context, key string) (Source, bool, error) {
	tx.repository.calls = append(tx.repository.calls, "load-idempotent")
	tx.repository.idempotencyKey = key
	if tx.repository.idempotent == nil {
		return Source{}, false, tx.repository.idempotentErr
	}
	return *tx.repository.idempotent, true, tx.repository.idempotentErr
}

func (tx *atomicTransaction) Save(_ context.Context, accepted Mutation) error {
	tx.repository.calls = append(tx.repository.calls, "save")
	tx.repository.saveCalls++
	if tx.repository.saveErr != nil {
		return tx.repository.saveErr
	}
	tx.pending = accepted
	tx.saved = true
	return nil
}

func (tx *atomicTransaction) ListCandidates(_ context.Context, query mentions.CandidateQuery) ([]mentions.Candidate, error) {
	if query.AuthorizationOnly {
		tx.repository.calls = append(tx.repository.calls, "authorize")
		tx.repository.authorizeCalls++
	}
	return nil, tx.repository.authorizeErr
}

func (tx *atomicTransaction) LoadDirectAccess(_ context.Context, _ mentions.SourceRef, _ []string) ([]mentions.MemberAccess, error) {
	return append([]mentions.MemberAccess(nil), tx.repository.direct...), nil
}

func (tx *atomicTransaction) LoadTeamAccess(_ context.Context, _ mentions.SourceRef, _ []string) ([]mentions.TeamAccess, error) {
	return append([]mentions.TeamAccess(nil), tx.repository.teams...), nil
}

func newTestService(repository Repository) *Service {
	newID := sequentialCollaborationIDs()
	return NewService(repository, mentions.NewService(collaborationNow, newID), collaborationNow, newID)
}

func internalAuthor() authorization.Principal {
	return authorization.Principal{ID: "author", Scope: scope.Principal{MSPID: "msp-a"}}
}

func workParent() ParentRef {
	return ParentRef{Type: mentions.ParentWorkRecord, ID: "work-1"}
}

func existingSource(kind mentions.SourceKind, version int64) Source {
	return Source{
		ID: "source-1", MSPID: "msp-a", ClientID: "client-a", Parent: workParent(),
		Kind: kind, Body: "Original", AuthorID: "author", LifecycleState: SourceActive,
		Version: version, CreatedAt: collaborationNow().Add(-time.Hour), UpdatedAt: collaborationNow().Add(-time.Hour),
	}
}

func collaborationStaffToken(id, target, label string, start, end int) mentions.Token {
	return mentions.Token{ID: id, TargetType: mentions.TargetStaff, TargetID: target, Label: label, Start: start, End: end}
}

func eligibleCollaborationMember(id string) mentions.MemberAccess {
	return mentions.MemberAccess{StaffID: id, MSPID: "msp-a", Active: true, Internal: true, HasMentionRead: true, CanRead: true}
}

func collaborationNow() time.Time {
	return time.Date(2026, time.August, 8, 15, 0, 0, 0, time.FixedZone("test", -4*60*60))
}

func sequentialCollaborationIDs() func() string {
	index := 0
	return func() string {
		index++
		return "generated-" + string(rune('a'+index-1))
	}
}
