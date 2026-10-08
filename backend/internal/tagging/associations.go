package tagging

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/id"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

var (
	ErrInvalidAssociation    = errors.New("invalid tag association request")
	ErrMeaningfulTagRequired = errors.New("at least one meaningful tag is required")
	ErrAutomaticReplacement  = errors.New("automatic classification may only add tags")
)

type TargetRef struct {
	ObjectType ObjectType `json:"object_type"`
	ObjectID   string     `json:"object_id"`
	MSPID      string     `json:"msp_id"`
	ClientID   string     `json:"client_id,omitempty"`
}

type Assignment struct {
	ID               string     `json:"id"`
	Tag              Tag        `json:"tag"`
	Source           Source     `json:"source"`
	AssignedAt       time.Time  `json:"assigned_at,omitempty"`
	AssignedBy       string     `json:"assigned_by,omitempty"`
	Inherited        bool       `json:"inherited"`
	SourceObjectType ObjectType `json:"source_object_type,omitempty"`
	SourceObjectID   string     `json:"source_object_id,omitempty"`
}

type TaggedObject struct {
	Target              TargetRef    `json:"target"`
	ObjectVersion       int64        `json:"object_version"`
	Direct              []Assignment `json:"direct"`
	Inherited           []Assignment `json:"inherited"`
	Effective           []Assignment `json:"effective"`
	ClassificationState string       `json:"classification_state"`
}

type HistoryEntry struct {
	ID               string     `json:"id"`
	Operation        string     `json:"operation"`
	Assignment       Assignment `json:"assignment"`
	TargetVersion    int64      `json:"target_version"`
	OccurredAt       time.Time  `json:"occurred_at"`
	ActorID          string     `json:"actor_id,omitempty"`
	Inherited        bool       `json:"inherited"`
	SourceObjectType ObjectType `json:"source_object_type,omitempty"`
	SourceObjectID   string     `json:"source_object_id,omitempty"`
	CorrelationID    string     `json:"correlation_id"`
	CausationID      string     `json:"causation_id,omitempty"`
}

type GetCommand struct {
	Principal authorization.Principal
	Target    TargetRef
}

type HistoryCommand struct {
	Principal authorization.Principal
	Target    TargetRef
}

type ReplaceCommand struct {
	Principal             authorization.Principal
	Target                TargetRef
	ExpectedObjectVersion int64
	TagIDs                []string
	Source                Source
	Reason                string
	IdempotencyKey        string
	CorrelationID         string
	CausationID           string
}

type BulkCommand struct {
	Principal authorization.Principal
	Items     []ReplaceCommand
}

type BulkResult struct {
	Target TargetRef     `json:"target"`
	Object *TaggedObject `json:"object,omitempty"`
	// Cause preserves the typed domain failure for the transport boundary.
	// Error remains a human-readable compatibility field for in-process callers.
	Cause error  `json:"-"`
	Error string `json:"-"`
}

type GuardCommand struct {
	Principal authorization.Principal
	Target    TargetRef
}

type AssociationMutation struct {
	Before                TaggedObject
	Direct                []Assignment
	Source                Source
	ExpectedObjectVersion int64
	Reason                string
	IdempotencyKey        string
	CorrelationID         string
	CausationID           string
	Audit                 mutation.AuditRecord
	Event                 mutation.EventRecord
}

type AssociationRepository interface {
	Get(context.Context, TargetRef) (TaggedObject, error)
	ResolveTags(context.Context, string, []string) ([]Tag, error)
	Accepted(context.Context, TargetRef, string) (TaggedObject, bool, error)
	ReplaceDirect(context.Context, AssociationMutation) (TaggedObject, error)
	History(context.Context, TargetRef) ([]HistoryEntry, error)
}

type AssociationService struct {
	repository AssociationRepository
	now        func() time.Time
	newID      func() string
}

func NewAssociationService(
	repository AssociationRepository,
) *AssociationService {
	return &AssociationService{
		repository: repository,
		now:        time.Now,
		newID:      id.New,
	}
}

func NewAssociationServiceWithClock(
	repository AssociationRepository,
	now func() time.Time,
	newID func() string,
) *AssociationService {
	return &AssociationService{repository: repository, now: now, newID: newID}
}

func (s *AssociationService) Get(
	ctx context.Context,
	command GetCommand,
) (TaggedObject, error) {
	if !s.valid() || !validTarget(command.Target) {
		return TaggedObject{}, ErrInvalidAssociation
	}
	if err := authorizeAssociation(command.Principal, command.Target); err != nil {
		return TaggedObject{}, err
	}
	found, err := s.repository.Get(ctx, command.Target)
	if err != nil {
		return TaggedObject{}, err
	}
	return normalizedTaggedObject(found), nil
}

func (s *AssociationService) History(
	ctx context.Context,
	command HistoryCommand,
) ([]HistoryEntry, error) {
	if !s.valid() || !validTarget(command.Target) {
		return nil, ErrInvalidAssociation
	}
	if err := authorizeAssociation(command.Principal, command.Target); err != nil {
		return nil, err
	}
	return s.repository.History(ctx, command.Target)
}

func (s *AssociationService) ReplaceDirect(
	ctx context.Context,
	command ReplaceCommand,
) (TaggedObject, error) {
	if !s.valid() || !validTarget(command.Target) ||
		command.ExpectedObjectVersion < 1 ||
		!validSource(command.Source) ||
		strings.TrimSpace(command.Reason) == "" ||
		strings.TrimSpace(command.IdempotencyKey) == "" ||
		strings.TrimSpace(command.CorrelationID) == "" {
		return TaggedObject{}, ErrInvalidAssociation
	}
	if err := authorizeAssociation(command.Principal, command.Target); err != nil {
		return TaggedObject{}, err
	}
	if accepted, found, err := s.repository.Accepted(
		ctx, command.Target, command.IdempotencyKey,
	); err != nil {
		return TaggedObject{}, err
	} else if found {
		return normalizedTaggedObject(accepted), nil
	}
	before, err := s.repository.Get(ctx, command.Target)
	if err != nil {
		return TaggedObject{}, err
	}
	if err := object.RequireVersion(
		before.ObjectVersion, command.ExpectedObjectVersion,
	); err != nil {
		return TaggedObject{}, err
	}
	if command.Source == SourceAIAutomatic &&
		!containsAllTagIDs(command.TagIDs, before.Direct) {
		return TaggedObject{}, ErrAutomaticReplacement
	}
	tags, err := s.repository.ResolveTags(
		ctx, command.Target.MSPID, uniqueStrings(command.TagIDs),
	)
	if err != nil {
		return TaggedObject{}, err
	}
	direct := make([]Assignment, 0, len(tags))
	seen := map[string]struct{}{}
	for _, tag := range tags {
		if tag.State != StateActive {
			return TaggedObject{}, ErrInactiveTarget
		}
		if _, exists := seen[tag.ID]; exists {
			continue
		}
		seen[tag.ID] = struct{}{}
		direct = append(direct, Assignment{
			Tag: tag, Source: command.Source,
			AssignedBy: command.Principal.ID,
		})
	}
	if hasMeaningful(direct) {
		direct = withoutUnclassified(direct)
	}
	effective := effectiveAssignments(direct, before.Inherited)
	if !hasMeaningful(effective) {
		return TaggedObject{}, ErrMeaningfulTagRequired
	}
	now := s.now().UTC()
	nextVersion := before.ObjectVersion + 1
	audit := mutation.AuditRecord{
		ID: s.newID(), OccurredAt: now,
		MSPID: command.Target.MSPID, ClientID: command.Target.ClientID,
		ActorType: "technician", ActorID: command.Principal.ID,
		Action:      "classification.tags.replaced",
		SubjectType: command.Target.ObjectType.String(),
		SubjectID:   command.Target.ObjectID, SubjectVersion: nextVersion,
		Source: command.Source.String(), Reason: command.Reason,
		CorrelationID: command.CorrelationID,
	}
	event := mutation.EventRecord{
		EventID: s.newID(), EventType: "classification.tags.replaced",
		SchemaVersion: 1, OccurredAt: now,
		MSPID: command.Target.MSPID, ClientID: command.Target.ClientID,
		ActorType: "technician", ActorID: command.Principal.ID,
		SubjectType: command.Target.ObjectType.String(),
		SubjectID:   command.Target.ObjectID, SubjectVersion: nextVersion,
		Source: command.Source.String(), CorrelationID: command.CorrelationID,
		CausationID: command.CausationID,
		Data: map[string]any{
			"tag_count":       len(direct),
			"idempotency_key": command.IdempotencyKey,
		},
	}
	return s.repository.ReplaceDirect(ctx, AssociationMutation{
		Before: before, Direct: direct, Source: command.Source,
		ExpectedObjectVersion: command.ExpectedObjectVersion,
		Reason:                command.Reason, IdempotencyKey: command.IdempotencyKey,
		CorrelationID: command.CorrelationID,
		CausationID:   command.CausationID,
		Audit:         audit, Event: event,
	})
}

func containsAllTagIDs(tagIDs []string, assignments []Assignment) bool {
	requested := map[string]struct{}{}
	for _, tagID := range uniqueStrings(tagIDs) {
		requested[tagID] = struct{}{}
	}
	for _, assignment := range assignments {
		if _, exists := requested[assignment.Tag.ID]; !exists {
			return false
		}
	}
	return true
}

func (s *AssociationService) Bulk(
	ctx context.Context,
	command BulkCommand,
) ([]BulkResult, error) {
	if len(command.Items) == 0 || len(command.Items) > 500 {
		return nil, ErrInvalidAssociation
	}
	results := make([]BulkResult, 0, len(command.Items))
	for _, item := range command.Items {
		item.Principal = command.Principal
		updated, err := s.ReplaceDirect(ctx, item)
		result := BulkResult{Target: item.Target}
		if err != nil {
			result.Cause = err
			result.Error = err.Error()
		} else {
			result.Object = &updated
		}
		results = append(results, result)
	}
	return results, nil
}

func (s *AssociationService) RequireMeaningful(
	ctx context.Context,
	command GuardCommand,
) error {
	// Terminal services authorize their lifecycle operation before invoking this
	// scoped read. Requiring classification.apply here would turn a read-only
	// prerequisite into an unrelated mutation permission (e.g. time reviewers).
	if !s.valid() || !validTarget(command.Target) {
		return ErrInvalidAssociation
	}
	found, err := s.repository.Get(ctx, command.Target)
	if err != nil {
		return err
	}
	if !hasMeaningful(found.Effective) {
		return ErrMeaningfulTagRequired
	}
	return nil
}

func (s *AssociationService) valid() bool {
	return s != nil && s.repository != nil && s.now != nil && s.newID != nil
}

func authorizeAssociation(
	principal authorization.Principal,
	target TargetRef,
) error {
	return authorization.Authorize(
		principal,
		"classification.apply",
		scope.Target{MSPID: target.MSPID, ClientID: target.ClientID},
	)
}

func validTarget(target TargetRef) bool {
	if strings.TrimSpace(target.MSPID) == "" ||
		strings.TrimSpace(target.ObjectID) == "" ||
		!validObjectType(target.ObjectType) {
		return false
	}
	return target.ObjectType == ObjectKnowledgeArticle ||
		strings.TrimSpace(target.ClientID) != ""
}

func validObjectType(objectType ObjectType) bool {
	switch objectType {
	case ObjectWorkRecord, ObjectTask, ObjectProject, ObjectAsset,
		ObjectKnowledgeArticle, ObjectTimeEntry:
		return true
	default:
		return false
	}
}

func validSource(source Source) bool {
	switch source {
	case SourceHuman, SourceAIConfirmed, SourceAIAutomatic,
		SourceAutomation, SourceIntegration, SourceMigration,
		SourceSystemFallback:
		return true
	default:
		return false
	}
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func effectiveAssignments(
	direct []Assignment,
	inherited []Assignment,
) []Assignment {
	result := make([]Assignment, 0, len(direct)+len(inherited))
	index := map[string]int{}
	appendAssignment := func(assignment Assignment) {
		if assignment.Tag.State != StateActive {
			return
		}
		if _, ok := index[assignment.Tag.ID]; ok {
			return
		}
		index[assignment.Tag.ID] = len(result)
		result = append(result, assignment)
	}
	for _, assignment := range direct {
		appendAssignment(assignment)
	}
	for _, assignment := range inherited {
		appendAssignment(assignment)
	}
	return result
}

func hasMeaningful(assignments []Assignment) bool {
	for _, assignment := range assignments {
		if assignment.Tag.State == StateActive &&
			assignment.Tag.InternalKey != "taxonomy.system.unclassified" {
			return true
		}
	}
	return false
}

func withoutUnclassified(assignments []Assignment) []Assignment {
	result := make([]Assignment, 0, len(assignments))
	for _, assignment := range assignments {
		if assignment.Tag.InternalKey == "taxonomy.system.unclassified" {
			continue
		}
		result = append(result, assignment)
	}
	return result
}

func normalizedTaggedObject(value TaggedObject) TaggedObject {
	value.Direct = append([]Assignment{}, value.Direct...)
	value.Inherited = append([]Assignment{}, value.Inherited...)
	value.Effective = effectiveAssignments(value.Direct, value.Inherited)
	value.ClassificationState = classificationState(value.Effective)
	return value
}

func classificationState(assignments []Assignment) string {
	if hasMeaningful(assignments) {
		return "classified"
	}
	for _, assignment := range assignments {
		if assignment.Tag.InternalKey == "taxonomy.system.unclassified" {
			return "unclassified"
		}
	}
	return "missing"
}
