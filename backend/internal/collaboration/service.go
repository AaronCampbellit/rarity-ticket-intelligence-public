package collaboration

import (
	"context"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mentions"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type Service struct {
	repository Repository
	mentions   MentionPreparer
	now        func() time.Time
	newID      func() string
}

func NewService(
	repository Repository,
	mentionPreparer MentionPreparer,
	now func() time.Time,
	newID func() string,
) *Service {
	return &Service{repository: repository, mentions: mentionPreparer, now: now, newID: newID}
}

func (s *Service) CreateComment(ctx context.Context, command CreateCommand) (Source, error) {
	return s.upsert(ctx, UpsertCommand{
		Principal: command.Principal, Parent: command.Parent, Kind: mentions.SourceComment,
		Body: command.Body, Tokens: command.Tokens,
		ConfirmedTeamSnapshots: command.ConfirmedTeamSnapshots,
		IdempotencyKey:         command.IdempotencyKey, Source: command.Source,
	}, operationCreate)
}

func (s *Service) CreateNote(ctx context.Context, command CreateCommand) (Source, error) {
	return s.upsert(ctx, UpsertCommand{
		Principal: command.Principal, Parent: command.Parent, Kind: mentions.SourceNote,
		Body: command.Body, Tokens: command.Tokens,
		ConfirmedTeamSnapshots: command.ConfirmedTeamSnapshots,
		IdempotencyKey:         command.IdempotencyKey, Source: command.Source,
	}, operationCreate)
}

func (s *Service) PutDetails(ctx context.Context, command UpsertCommand) (Source, error) {
	if command.Kind != mentions.SourceDetails {
		return Source{}, ErrInvalid
	}
	return s.upsert(ctx, command, operationPutDetails)
}

func (s *Service) Edit(ctx context.Context, command UpsertCommand) (Source, error) {
	return s.upsert(ctx, command, operationEdit)
}

func (s *Service) Redact(ctx context.Context, command RedactCommand) (Source, error) {
	if !s.valid() || !validPrincipal(command.Principal.ID, command.Principal.Scope) ||
		!validParent(command.Parent) || !validSourceKind(command.Kind) ||
		!validID(command.SourceID) || command.ExpectedVersion < 1 ||
		command.ExpectedVersion == math.MaxInt64 || !validID(command.IdempotencyKey) ||
		!validID(command.Source) {
		return Source{}, ErrInvalid
	}
	lookup := mentions.SourceRef{
		MSPID: command.Principal.Scope.MSPID, ClientID: command.Principal.Scope.ClientID,
		ParentType: command.Parent.Type, ParentID: command.Parent.ID, SourceKind: command.Kind,
	}
	var result Source
	err := s.repository.WithTransaction(ctx, lookup, func(tx Transaction) error {
		loaded, trusted, err := s.loadAndAuthorize(ctx, tx, lookup, command.SourceID, command.Principal.ID, command.Principal.Scope)
		if err != nil {
			return err
		}
		loadedIsRedacted := loaded.LifecycleState == SourceRedacted
		if loadedIsRedacted {
			if err := validateLoadedRedactedSource(loaded, trusted, command.SourceID); err != nil {
				return err
			}
		} else if err := validateLoadedSource(loaded, trusted, command.SourceID); err != nil {
			return err
		}
		if replay, found, err := tx.LoadIdempotentSource(ctx, command.IdempotencyKey); err != nil {
			return err
		} else if found {
			if !validReplaySource(replay, trusted, command.SourceID, SourceRedacted) {
				return ErrInvalid
			}
			result = cloneSource(replay)
			return nil
		}
		if loadedIsRedacted {
			return ErrSourceRedacted
		}
		if err := object.RequireVersion(loaded.Version, command.ExpectedVersion); err != nil {
			return err
		}
		now := s.now().UTC()
		if now.IsZero() {
			return ErrInvalid
		}
		redactedAt := now
		result = cloneSource(loaded)
		result.Body = ""
		result.Tokens = []mentions.Token{}
		result.LifecycleState = SourceRedacted
		result.RedactedAt = &redactedAt
		result.Version++
		result.UpdatedAt = now
		return tx.Save(ctx, Mutation{
			Source: cloneSource(result), Mentions: emptyMentionMutation(), IdempotencyKey: command.IdempotencyKey,
		})
	})
	if err != nil {
		return Source{}, err
	}
	return result, nil
}

type upsertOperation int

const (
	operationCreate upsertOperation = iota
	operationPutDetails
	operationEdit
)

func (s *Service) upsert(ctx context.Context, command UpsertCommand, operation upsertOperation) (Source, error) {
	if !s.valid() || !validUpsertCommand(command, operation) {
		return Source{}, ErrInvalid
	}
	lookup := mentions.SourceRef{
		MSPID: command.Principal.Scope.MSPID, ClientID: command.Principal.Scope.ClientID,
		ParentType: command.Parent.Type, ParentID: command.Parent.ID, SourceKind: command.Kind,
	}
	var result Source
	err := s.repository.WithTransaction(ctx, lookup, func(tx Transaction) error {
		loaded, trusted, err := s.loadAndAuthorize(ctx, tx, lookup, command.SourceID, command.Principal.ID, command.Principal.Scope)
		if err != nil {
			return err
		}
		if loaded.ID != "" {
			if err := validateLoadedSource(loaded, trusted, command.SourceID); err != nil {
				return err
			}
		}
		if replay, found, err := tx.LoadIdempotentSource(ctx, command.IdempotencyKey); err != nil {
			return err
		} else if found {
			if !validReplaySource(replay, trusted, command.SourceID, SourceActive) {
				return ErrInvalid
			}
			result = cloneSource(replay)
			return nil
		}

		if operation == operationCreate && loaded.ID != "" {
			return ErrInvalid
		}
		if err := object.RequireVersion(loaded.Version, command.ExpectedVersion); err != nil {
			return err
		}

		now := s.now().UTC()
		if now.IsZero() {
			return ErrInvalid
		}
		correlationID := s.newID()
		if !validID(correlationID) {
			return ErrInvalid
		}
		result = cloneSource(loaded)
		if loaded.ID == "" {
			result.ID = s.newID()
			if !validID(result.ID) || result.ID == correlationID {
				return ErrInvalid
			}
			result.MSPID = trusted.MSPID
			result.ClientID = trusted.ClientID
			result.Parent = command.Parent
			result.Kind = command.Kind
			result.AuthorID = command.Principal.ID
			result.LifecycleState = SourceActive
			result.Version = 1
			result.CreatedAt = now
		} else {
			result.Version++
		}
		result.Body = command.Body
		result.Tokens = append([]mentions.Token(nil), command.Tokens...)
		result.UpdatedAt = now
		result.RedactedAt = nil

		prepared, err := s.mentions.PrepareMutation(ctx, tx, mentions.PrepareCommand{
			Author: command.Principal, Source: trusted, SourceID: result.ID,
			PriorTokens: loaded.Tokens, SubmittedBody: result.Body,
			SubmittedTokens: result.Tokens, ConfirmedTeams: command.ConfirmedTeamSnapshots,
			SourceRevision: result.Version, CorrelationID: correlationID, SourceSystem: command.Source,
		})
		if err != nil {
			return err
		}
		return tx.Save(ctx, Mutation{
			Source: cloneSource(result), Mentions: prepared, IdempotencyKey: command.IdempotencyKey,
		})
	})
	if err != nil {
		return Source{}, err
	}
	return result, nil
}

func (s *Service) loadAndAuthorize(
	ctx context.Context,
	tx Transaction,
	lookup mentions.SourceRef,
	sourceID string,
	authorID string,
	principalScope scope.Principal,
) (Source, mentions.SourceRef, error) {
	loaded, err := tx.LoadSourceForUpdate(ctx, lookup, sourceID)
	if err != nil {
		return Source{}, mentions.SourceRef{}, err
	}
	if !validID(loaded.MSPID) || !validID(loaded.ClientID) ||
		loaded.Parent.Type != lookup.ParentType || loaded.Parent.ID != lookup.ParentID ||
		loaded.Kind != lookup.SourceKind || (sourceID != "" && loaded.ID != sourceID) {
		return Source{}, mentions.SourceRef{}, ErrInvalid
	}
	trusted := mentions.SourceRef{
		MSPID: loaded.MSPID, ClientID: loaded.ClientID, ParentType: loaded.Parent.Type,
		ParentID: loaded.Parent.ID, SourceKind: loaded.Kind,
	}
	if err := scope.Authorize(principalScope, scope.Target{MSPID: trusted.MSPID, ClientID: trusted.ClientID}); err != nil {
		return Source{}, mentions.SourceRef{}, err
	}
	rows, err := tx.ListCandidates(ctx, mentions.CandidateQuery{
		AuthorID: authorID, Source: trusted, AuthorizationOnly: true,
	})
	if err != nil {
		return Source{}, mentions.SourceRef{}, err
	}
	if len(rows) != 0 {
		return Source{}, mentions.SourceRef{}, mentions.ErrInvalidAccessData
	}
	return loaded, trusted, nil
}

func validUpsertCommand(command UpsertCommand, operation upsertOperation) bool {
	if !validPrincipal(command.Principal.ID, command.Principal.Scope) ||
		!validParent(command.Parent) || !validSourceKind(command.Kind) ||
		strings.TrimSpace(command.Body) == "" || len(command.Body) > maximumBodyBytes ||
		!utf8.ValidString(command.Body) || !validID(command.IdempotencyKey) ||
		!validID(command.Source) || mentions.ValidateTokens(command.Body, command.Tokens) != nil ||
		!validConfirmations(command.Tokens, command.ConfirmedTeamSnapshots) {
		return false
	}
	switch operation {
	case operationCreate:
		return command.SourceID == "" && command.ExpectedVersion == 0 &&
			(command.Kind == mentions.SourceComment || command.Kind == mentions.SourceNote)
	case operationPutDetails:
		return command.Kind == mentions.SourceDetails &&
			(command.SourceID == "" || validID(command.SourceID)) &&
			command.ExpectedVersion >= 0 && command.ExpectedVersion != math.MaxInt64
	case operationEdit:
		return validID(command.SourceID) && command.ExpectedVersion >= 1 &&
			command.ExpectedVersion != math.MaxInt64
	default:
		return false
	}
}

func validateLoadedSource(source Source, trusted mentions.SourceRef, expectedID string) error {
	if !validID(source.ID) || (expectedID != "" && source.ID != expectedID) ||
		source.MSPID != trusted.MSPID || source.ClientID != trusted.ClientID ||
		source.Parent.Type != trusted.ParentType || source.Parent.ID != trusted.ParentID ||
		source.Kind != trusted.SourceKind || !validID(source.AuthorID) ||
		source.LifecycleState != SourceActive || source.Version < 1 ||
		source.Version == math.MaxInt64 || source.CreatedAt.IsZero() || source.UpdatedAt.IsZero() ||
		!validActiveSourceContent(source) {
		if source.LifecycleState == SourceRedacted {
			return ErrSourceRedacted
		}
		return ErrInvalid
	}
	return nil
}

func validateLoadedRedactedSource(source Source, trusted mentions.SourceRef, expectedID string) error {
	if !validReplaySource(source, trusted, expectedID, SourceRedacted) {
		return ErrInvalid
	}
	return nil
}

func validReplaySource(
	source Source,
	trusted mentions.SourceRef,
	expectedID string,
	expectedLifecycle SourceLifecycle,
) bool {
	if !validID(source.ID) || (expectedID != "" && source.ID != expectedID) ||
		source.MSPID != trusted.MSPID || source.ClientID != trusted.ClientID ||
		source.Parent.Type != trusted.ParentType || source.Parent.ID != trusted.ParentID ||
		source.Kind != trusted.SourceKind || !validID(source.AuthorID) || source.Version < 1 ||
		source.CreatedAt.IsZero() || source.UpdatedAt.IsZero() {
		return false
	}
	if expectedLifecycle == SourceRedacted {
		if source.LifecycleState != SourceRedacted {
			return false
		}
		return source.Body == "" && len(source.Tokens) == 0 && source.RedactedAt != nil
	}
	return source.LifecycleState == SourceActive && validActiveSourceContent(source)
}

func validActiveSourceContent(source Source) bool {
	return strings.TrimSpace(source.Body) != "" && len(source.Body) <= maximumBodyBytes &&
		utf8.ValidString(source.Body) && source.RedactedAt == nil &&
		mentions.ValidateTokens(source.Body, source.Tokens) == nil
}

func validConfirmations(tokens []mentions.Token, values map[string]mentions.TeamConfirmation) bool {
	teams := make(map[string]struct{})
	for _, token := range tokens {
		if token.TargetType == mentions.TargetTeam {
			teams[token.TargetID] = struct{}{}
		}
	}
	for teamID, confirmation := range values {
		if !validID(teamID) || confirmation.TeamVersion < 1 || len(confirmation.EligibleMemberIDs) > 500 {
			return false
		}
		if _, exists := teams[teamID]; !exists {
			return false
		}
		previous := ""
		for _, memberID := range confirmation.EligibleMemberIDs {
			if !validID(memberID) || (previous != "" && memberID <= previous) {
				return false
			}
			previous = memberID
		}
	}
	return true
}

func validPrincipal(id string, principal scope.Principal) bool {
	return validID(id) && validID(principal.MSPID) &&
		(principal.ClientID == "" || validID(principal.ClientID))
}

func validParent(parent ParentRef) bool {
	return validID(parent.ID) &&
		(parent.Type == mentions.ParentWorkRecord || parent.Type == mentions.ParentTask || parent.Type == mentions.ParentProject)
}

func validSourceKind(kind mentions.SourceKind) bool {
	return kind == mentions.SourceDetails || kind == mentions.SourceComment || kind == mentions.SourceNote
}

func validID(value string) bool {
	if value == "" || len(value) > 128 || !utf8.ValidString(value) {
		return false
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			strings.ContainsRune("-_.:", rune(character)) {
			continue
		}
		return false
	}
	return true
}

func cloneSource(source Source) Source {
	cloned := source
	cloned.Tokens = append([]mentions.Token(nil), source.Tokens...)
	if source.RedactedAt != nil {
		value := *source.RedactedAt
		cloned.RedactedAt = &value
	}
	return cloned
}

func emptyMentionMutation() mentions.PreparedMutation {
	return mentions.PreparedMutation{
		Occurrences: []mentions.Occurrence{}, Resolutions: []mentions.ResolutionRecord{},
		Items: []mentions.Item{},
	}
}

func (s *Service) valid() bool {
	return s != nil && s.repository != nil && s.mentions != nil && s.now != nil && s.newID != nil
}
