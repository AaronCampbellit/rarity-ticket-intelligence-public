package mentions

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/observability"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

var ErrInvalidMutation = errors.New("invalid mention mutation")

type PrepareCommand struct {
	Author          authorization.Principal
	Source          SourceRef
	SourceID        string
	PriorTokens     []Token
	SubmittedBody   string
	SubmittedTokens []Token
	ConfirmedTeams  map[string]TeamConfirmation
	SourceRevision  int64
	CorrelationID   string
	SourceSystem    string
}

type Occurrence struct {
	ID             string
	MSPID          string
	ClientID       string
	SourceID       string
	SourceRevision int64
	ParentType     ParentType
	ParentID       string
	TokenID        string
	AuthorID       string
	TargetType     TargetType
	TargetID       string
	MentionedAt    time.Time
	CorrelationID  string
}

type ResolutionDecision string

const (
	DecisionEligible ResolutionDecision = "eligible"
	DecisionExcluded ResolutionDecision = "excluded"
)

type ResolutionRecord struct {
	ID                  string
	MSPID               string
	ClientID            string
	OccurrenceID        string
	RecipientID         string
	Decision            ResolutionDecision
	Path                ResolutionPath
	ContributingTeamIDs []string
	DecidedAt           time.Time
	ReasonCode          string
}

type PreparedMutation struct {
	Occurrences []Occurrence
	Resolutions []ResolutionRecord
	Items       []Item
	Audits      []mutation.AuditRecord
	Event       *mutation.EventRecord
	Telemetry   []observability.MentionMetric
}

type Service struct {
	now       func() time.Time
	newID     func() string
	telemetry *observability.MentionTelemetry
}

func NewService(now func() time.Time, newID func() string) *Service {
	return &Service{now: now, newID: newID}
}

func (s *Service) WithTelemetry(telemetry *observability.MentionTelemetry) *Service {
	if s != nil {
		s.telemetry = telemetry
	}
	return s
}

func (s *Service) PrepareMutation(
	ctx context.Context,
	access AccessRepository,
	command PrepareCommand,
) (PreparedMutation, error) {
	empty := emptyPreparedMutation()
	if s == nil || s.now == nil || s.newID == nil || access == nil ||
		!validStableID(command.Author.ID) || !validSourceRef(command.Source) ||
		!validStableID(command.SourceID) || command.SourceRevision < 1 ||
		!validStableID(command.CorrelationID) ||
		!validStableID(command.SourceSystem) {
		return empty, ErrInvalidMutation
	}
	if err := scope.Authorize(command.Author.Scope, scope.Target{
		MSPID: command.Source.MSPID, ClientID: command.Source.ClientID,
	}); err != nil {
		return empty, err
	}
	if err := ValidateTokens(command.SubmittedBody, command.SubmittedTokens); err != nil {
		return empty, ErrInvalidMutation
	}
	diff, err := DiffTokens(command.PriorTokens, command.SubmittedTokens)
	if err != nil {
		return empty, ErrInvalidMutation
	}
	resolved, err := NewResolver(access).Resolve(ctx, ResolveCommand{
		AuthorID: command.Author.ID, Source: command.Source,
		Tokens: command.SubmittedTokens, ConfirmedTeamSnapshots: command.ConfirmedTeams,
	})
	if err != nil {
		return empty, err
	}
	if len(diff.Added) == 0 {
		return empty, nil
	}
	mentionedAt := s.now().UTC()
	if mentionedAt.IsZero() {
		return empty, ErrInvalidMutation
	}

	groups := groupAddedTargets(diff.Added)
	usedIDs := make(map[string]struct{})
	nextID := func() (string, bool) {
		value := s.newID()
		if !validStableID(value) {
			return "", false
		}
		if _, duplicate := usedIDs[value]; duplicate {
			return "", false
		}
		usedIDs[value] = struct{}{}
		return value, true
	}

	prepared := emptyPreparedMutation()
	latestByRecipient := make(map[string]string)
	collapsedRecipients := 0
	for _, group := range groups {
		occurrenceID, ok := nextID()
		if !ok {
			return empty, ErrInvalidMutation
		}
		occurrence := Occurrence{
			ID: occurrenceID, MSPID: command.Source.MSPID, ClientID: command.Source.ClientID,
			SourceID: command.SourceID, SourceRevision: command.SourceRevision,
			ParentType: command.Source.ParentType, ParentID: command.Source.ParentID,
			TokenID: group.representative.ID, AuthorID: command.Author.ID,
			TargetType: group.targetType, TargetID: group.targetID,
			MentionedAt:   mentionedAt,
			CorrelationID: command.CorrelationID,
		}
		prepared.Occurrences = append(prepared.Occurrences, occurrence)

		auditID, ok := nextID()
		if !ok {
			return empty, ErrInvalidMutation
		}
		prepared.Audits = append(prepared.Audits, mutation.AuditRecord{
			ID: auditID, OccurredAt: mentionedAt, MSPID: command.Source.MSPID,
			ClientID: command.Source.ClientID, ActorType: "technician", ActorID: command.Author.ID,
			Action: "mention.occurred", SubjectType: "mention_occurrence", SubjectID: occurrenceID,
			SubjectVersion: 1, Source: command.SourceSystem, CorrelationID: command.CorrelationID,
		})

		for _, recipient := range resolved.Eligible {
			if !intersectsTokenIDs(recipient.TokenIDs, group.tokenIDs) {
				continue
			}
			resolutionID, ok := nextID()
			if !ok {
				return empty, ErrInvalidMutation
			}
			path := ResolutionDirect
			teamIDs := []string{}
			if group.targetType == TargetTeam {
				path = ResolutionTeam
				teamIDs = []string{group.targetID}
			}
			prepared.Resolutions = append(prepared.Resolutions, ResolutionRecord{
				ID: resolutionID, MSPID: command.Source.MSPID, ClientID: command.Source.ClientID,
				OccurrenceID: occurrenceID, RecipientID: recipient.StaffID,
				Decision: DecisionEligible, Path: path, ContributingTeamIDs: teamIDs,
				DecidedAt: mentionedAt, ReasonCode: "eligible",
			})
			if _, alreadyResolved := latestByRecipient[recipient.StaffID]; alreadyResolved {
				collapsedRecipients++
			}
			latestByRecipient[recipient.StaffID] = occurrenceID
		}
		for _, recipient := range resolved.Excluded {
			if !intersectsTokenIDs(recipient.TokenIDs, group.tokenIDs) {
				continue
			}
			resolutionID, ok := nextID()
			if !ok {
				return empty, ErrInvalidMutation
			}
			prepared.Resolutions = append(prepared.Resolutions, ResolutionRecord{
				ID: resolutionID, MSPID: command.Source.MSPID, ClientID: command.Source.ClientID,
				OccurrenceID: occurrenceID, RecipientID: recipient.StaffID,
				Decision: DecisionExcluded, Path: ResolutionTeam,
				ContributingTeamIDs: []string{group.targetID}, DecidedAt: mentionedAt,
				ReasonCode: string(recipient.ReasonCode),
			})
		}
	}

	recipientIDs := sortedMapKeys(latestByRecipient)
	for _, recipientID := range recipientIDs {
		itemID, ok := nextID()
		if !ok {
			return empty, ErrInvalidMutation
		}
		prepared.Items = append(prepared.Items, Item{
			ID: itemID, MSPID: command.Source.MSPID, ClientID: command.Source.ClientID,
			RecipientID: recipientID, ParentType: command.Source.ParentType,
			ParentID: command.Source.ParentID, LatestOccurrenceID: latestByRecipient[recipientID],
			State: Unread, LastMentionedAt: mentionedAt, Version: 1, UpdatedAt: mentionedAt,
		})
	}
	eventID, ok := nextID()
	if !ok {
		return empty, ErrInvalidMutation
	}
	latestSnapshot := make(map[string]string, len(latestByRecipient))
	for recipientID, occurrenceID := range latestByRecipient {
		latestSnapshot[recipientID] = occurrenceID
	}
	prepared.Event = &mutation.EventRecord{
		EventID: eventID, EventType: "mention.occurred", SchemaVersion: 1,
		OccurredAt: mentionedAt, MSPID: command.Source.MSPID, ClientID: command.Source.ClientID,
		ActorType: "technician", ActorID: command.Author.ID,
		SubjectType: "internal_collaboration_source", SubjectID: command.SourceID,
		SubjectVersion: command.SourceRevision, Source: command.SourceSystem,
		CorrelationID: command.CorrelationID,
		Data: map[string]any{
			"source_id": command.SourceID, "source_revision": command.SourceRevision,
			"parent_type": string(command.Source.ParentType), "parent_id": command.Source.ParentID,
			"recipient_ids": recipientIDs, "latest_occurrence_by_recipient": latestSnapshot,
		},
	}
	for _, occurrence := range prepared.Occurrences {
		prepared.Telemetry = append(prepared.Telemetry, observability.MentionMetric{
			Name: "occurrence_target", TargetType: string(occurrence.TargetType),
			ParentType: string(occurrence.ParentType), Outcome: string(occurrence.TargetType),
			MSPID: occurrence.MSPID, ClientID: occurrence.ClientID,
			ObjectID: occurrence.ParentID, OccurrenceID: occurrence.ID,
		})
	}
	for _, resolution := range prepared.Resolutions {
		prepared.Telemetry = append(prepared.Telemetry, observability.MentionMetric{
			Name: "resolution", ParentType: string(command.Source.ParentType),
			Outcome: string(resolution.Decision), MSPID: resolution.MSPID,
			ClientID: resolution.ClientID, ObjectID: command.Source.ParentID,
			OccurrenceID: resolution.OccurrenceID,
		})
	}
	for collapsed := collapsedRecipients; collapsed > 0; collapsed-- {
		prepared.Telemetry = append(prepared.Telemetry, observability.MentionMetric{
			Name: "deduplication", ParentType: string(command.Source.ParentType),
			Outcome: "collapsed", MSPID: command.Source.MSPID,
			ClientID: command.Source.ClientID, ObjectID: command.Source.ParentID,
		})
	}
	return prepared, nil
}

type addedTargetGroup struct {
	targetType     TargetType
	targetID       string
	representative Token
	tokenIDs       map[string]struct{}
}

func groupAddedTargets(tokens []Token) []addedTargetGroup {
	result := make([]addedTargetGroup, 0, len(tokens))
	indexByTarget := make(map[string]int, len(tokens))
	for _, token := range tokens {
		key := string(token.TargetType) + "\x00" + token.TargetID
		index, exists := indexByTarget[key]
		if !exists {
			index = len(result)
			indexByTarget[key] = index
			result = append(result, addedTargetGroup{
				targetType: token.TargetType, targetID: token.TargetID,
				representative: token, tokenIDs: make(map[string]struct{}),
			})
		}
		result[index].tokenIDs[token.ID] = struct{}{}
	}
	return result
}

func intersectsTokenIDs(values []string, wanted map[string]struct{}) bool {
	for _, value := range values {
		if _, exists := wanted[value]; exists {
			return true
		}
	}
	return false
}

func sortedMapKeys(values map[string]string) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func emptyPreparedMutation() PreparedMutation {
	return PreparedMutation{
		Occurrences: []Occurrence{}, Resolutions: []ResolutionRecord{},
		Items: []Item{}, Audits: []mutation.AuditRecord{}, Telemetry: []observability.MentionMetric{},
	}
}
