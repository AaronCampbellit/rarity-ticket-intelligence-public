package tagging

import (
	"context"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type CreationPolicy string

const (
	CreationRequireMeaningful CreationPolicy = "require_meaningful"
	CreationAllowFallback     CreationPolicy = "allow_fallback"
)

type PrepareCreationCommand struct {
	MSPID      string
	ClientID   string
	ObjectType ObjectType
	TagIDs     []string
	Source     Source
	Policy     CreationPolicy
	// Parent is the canonical Project reference for a Task created under a
	// Project or Phase. Its assignments remain derived evidence, never rows on
	// the new Task.
	Parent     *TargetRef
	ParentType string
	ParentID   string
	Inherited  []Assignment
}

type InitialAssignmentSet struct {
	Direct        []Assignment   `json:"direct"`
	Inherited     []Assignment   `json:"inherited"`
	Effective     []Assignment   `json:"effective"`
	ActorType     string         `json:"-"`
	ActorID       string         `json:"-"`
	OccurredAt    time.Time      `json:"-"`
	CorrelationID string         `json:"-"`
	Evidence      map[string]any `json:"-"`
}

// InitialAssignmentProvenance is copied from the accepted object mutation so
// assignment history preserves the same actor, time and correlation evidence.
type InitialAssignmentProvenance struct {
	ActorType     string
	ActorID       string
	OccurredAt    time.Time
	CorrelationID string
	Evidence      map[string]any
}

func (initial InitialAssignmentSet) WithProvenance(provenance InitialAssignmentProvenance) InitialAssignmentSet {
	initial.ActorType = provenance.ActorType
	initial.ActorID = provenance.ActorID
	initial.OccurredAt = provenance.OccurredAt
	initial.CorrelationID = provenance.CorrelationID
	initial.Evidence = provenance.Evidence
	return initial
}

type CreationRepository interface {
	ResolveTags(context.Context, string, []string) ([]Tag, error)
	FindUnclassified(context.Context, string) (Tag, error)
}

type creationParentRepository interface {
	Get(context.Context, TargetRef) (TaggedObject, error)
}

type creationTaskParentRepository interface {
	ResolveTaskProject(context.Context, string, string, string, string) (TargetRef, error)
}

type CreationPreparer struct {
	repository CreationRepository
}

func NewCreationPreparer(repository CreationRepository) *CreationPreparer {
	return &CreationPreparer{repository: repository}
}

func (p *CreationPreparer) Prepare(
	ctx context.Context,
	command PrepareCreationCommand,
) (InitialAssignmentSet, error) {
	target := TargetRef{
		MSPID: command.MSPID, ClientID: command.ClientID,
		ObjectType: command.ObjectType, ObjectID: "pending",
	}
	if p == nil || p.repository == nil || !validTarget(target) ||
		!validSource(command.Source) ||
		(command.Policy != CreationRequireMeaningful &&
			command.Policy != CreationAllowFallback) ||
		(command.Policy == CreationAllowFallback &&
			!trustedFallbackSource(command.Source)) {
		return InitialAssignmentSet{}, ErrInvalidAssociation
	}
	inherited, err := p.derivedParentAssignments(ctx, command)
	if err != nil {
		return InitialAssignmentSet{}, err
	}
	tags, err := p.repository.ResolveTags(
		ctx, command.MSPID, uniqueStrings(command.TagIDs),
	)
	if err != nil {
		return InitialAssignmentSet{}, err
	}
	direct := make([]Assignment, 0, len(tags))
	for _, tag := range tags {
		if tag.State != StateActive {
			return InitialAssignmentSet{}, ErrInactiveTarget
		}
		direct = append(direct, Assignment{Tag: tag, Source: command.Source})
	}
	if hasMeaningful(direct) {
		direct = withoutUnclassified(direct)
	}
	effective := effectiveAssignments(direct, inherited)
	if !hasMeaningful(effective) {
		if command.Policy == CreationRequireMeaningful {
			return InitialAssignmentSet{}, ErrMeaningfulTagRequired
		}
		fallback, err := p.repository.FindUnclassified(ctx, command.MSPID)
		if err != nil {
			if err == scope.ErrNotFound {
				return InitialAssignmentSet{}, ErrMeaningfulTagRequired
			}
			return InitialAssignmentSet{}, err
		}
		direct = []Assignment{{
			Tag: fallback, Source: SourceSystemFallback,
		}}
		effective = effectiveAssignments(direct, inherited)
	}
	return InitialAssignmentSet{
		Direct: direct, Inherited: inherited, Effective: effective,
	}, nil
}

func (p *CreationPreparer) derivedParentAssignments(
	ctx context.Context,
	command PrepareCreationCommand,
) ([]Assignment, error) {
	// Inherited evidence is derived only through this trusted repository path.
	// The legacy command field is deliberately ignored so callers cannot forge
	// meaningful classification on non-task objects.
	inherited := []Assignment{}
	if command.ParentType == "" && command.ParentID == "" {
		return inherited, nil
	}
	if command.ObjectType != ObjectTask || command.ParentID == "" {
		return nil, ErrInvalidAssociation
	}
	// Only Project and Phase parents contribute derived classification. Tasks
	// under work records or opportunities remain directly classified.
	if command.ParentType != "project" && command.ParentType != "phase" {
		return inherited, nil
	}
	parentResolver, ok := p.repository.(creationTaskParentRepository)
	if !ok {
		return nil, ErrInvalidAssociation
	}
	parent, err := parentResolver.ResolveTaskProject(ctx, command.MSPID, command.ClientID, command.ParentType, command.ParentID)
	if err != nil {
		return nil, err
	}
	repository, ok := p.repository.(creationParentRepository)
	if !ok {
		return nil, ErrInvalidAssociation
	}
	found, err := repository.Get(ctx, parent)
	if err != nil {
		return nil, err
	}
	for _, assignment := range found.Effective {
		assignment.Inherited = true
		assignment.SourceObjectType = ObjectProject
		assignment.SourceObjectID = parent.ObjectID
		inherited = append(inherited, assignment)
	}
	return effectiveAssignments(nil, inherited), nil
}

func trustedFallbackSource(source Source) bool {
	return source == SourceAutomation || source == SourceIntegration ||
		source == SourceMigration || source == SourceAIAutomatic
}

type TerminalGuard struct {
	service *AssociationService
}

func NewTerminalGuard(service *AssociationService) *TerminalGuard {
	return &TerminalGuard{service: service}
}

func (g *TerminalGuard) RequireMeaningful(
	ctx context.Context,
	command GuardCommand,
) error {
	if g == nil || g.service == nil {
		return ErrInvalidAssociation
	}
	return g.service.RequireMeaningful(ctx, command)
}
