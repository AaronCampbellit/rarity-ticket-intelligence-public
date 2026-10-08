package tagging

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

var (
	ErrInvalidCatalog = errors.New("invalid classification catalog request")
	ErrDuplicateTerm  = errors.New("tag label or synonym already exists")
	ErrSystemManaged  = errors.New("system-managed classification cannot be changed")
	ErrReasonRequired = errors.New("classification change reason required")
	ErrInactiveTarget = errors.New("classification target must be active")
)

type ListCatalogCommand struct {
	Principal authorization.Principal
}

type HealthCommand struct {
	Principal authorization.Principal
	Target    scope.Target
}

type MigrationHistoryCommand struct {
	Principal authorization.Principal
}

type CreateGroupCommand struct {
	Principal   authorization.Principal
	Label       string
	Description string
	Position    int
}

type UpdateGroupCommand struct {
	Principal       authorization.Principal
	ID              string
	Label           string
	Description     string
	Position        int
	State           State
	ExpectedVersion int64
}

type CreateTagCommand struct {
	Principal   authorization.Principal
	GroupID     string
	Label       string
	Description string
	Color       string
	Synonyms    []string
}

type UpdateTagCommand struct {
	Principal       authorization.Principal
	ID              string
	GroupID         string
	Label           string
	Description     string
	Color           string
	Synonyms        []string
	ExpectedVersion int64
}

type ImpactCommand struct {
	Principal        authorization.Principal
	TagID            string
	Operation        ImpactOperation
	ReplacementTagID string
}

type MergeCommand struct {
	Principal       authorization.Principal
	TagID           string
	SurvivorTagID   string
	ExpectedVersion int64
	Reason          string
}

type ArchiveCommand struct {
	Principal        authorization.Principal
	TagID            string
	ReplacementTagID string
	ExpectedVersion  int64
	Reason           string
}

type CatalogMutation struct {
	Group           *Group
	Tag             *Tag
	ExpectedVersion int64
	Audit           mutation.AuditRecord
	Event           mutation.EventRecord
}

type SystemCatalogMutation struct {
	CatalogMutation
	Group Group
	Tag   Tag
}

type MergeMutation struct {
	Retired         Tag
	Survivor        Tag
	ExpectedVersion int64
	Reason          string
	Impact          Impact
	Audit           mutation.AuditRecord
	Event           mutation.EventRecord
}

type ArchiveMutation struct {
	Tag              Tag
	UnclassifiedTag  Tag
	ReplacementTagID string
	ExpectedVersion  int64
	Reason           string
	Impact           Impact
	Audit            mutation.AuditRecord
	Event            mutation.EventRecord
}

type CatalogRepository interface {
	EnsureSystemCatalog(context.Context, SystemCatalogMutation) (Tag, error)
	List(context.Context, string) (Catalog, error)
	Health(context.Context, scope.Target) (Health, error)
	MigrationHistory(context.Context, string) ([]MigrationRun, error)
	FindGroup(context.Context, string, string) (Group, error)
	FindTag(context.Context, string, string) (Tag, error)
	FindUnclassified(context.Context, string) (Tag, error)
	TermsAvailable(context.Context, string, string, []string) error
	CreateGroup(context.Context, CatalogMutation) error
	UpdateGroup(context.Context, CatalogMutation) error
	CreateTag(context.Context, CatalogMutation) error
	UpdateTag(context.Context, CatalogMutation) error
	PreviewImpact(
		context.Context,
		string,
		string,
		ImpactOperation,
		string,
	) (Impact, error)
	Merge(context.Context, MergeMutation) error
	Archive(context.Context, ArchiveMutation) error
}

type CatalogService struct {
	repository CatalogRepository
	now        func() time.Time
	newID      func() string
}

func NewCatalogService(
	repository CatalogRepository,
	now func() time.Time,
	newID func() string,
) *CatalogService {
	return &CatalogService{repository: repository, now: now, newID: newID}
}

func (s *CatalogService) EnsureSystemCatalog(
	ctx context.Context,
	mspID string,
	actorID string,
) (Tag, error) {
	if !s.valid() || strings.TrimSpace(mspID) == "" ||
		strings.TrimSpace(actorID) == "" {
		return Tag{}, ErrInvalidCatalog
	}
	now := s.now().UTC()
	group := Group{
		ID: s.newID(), MSPID: mspID, InternalKey: "taxonomy.system",
		Label: "System", Description: "System-managed classification tags.",
		Position: 1, State: StateActive, SystemManaged: true, Version: 1,
	}
	tag := Tag{
		ID: s.newID(), MSPID: mspID,
		InternalKey: "taxonomy.system.unclassified",
		Label:       "Unclassified", GroupID: group.ID,
		Description: "Required fallback until a governed tag is selected.",
		State:       StateActive, SystemManaged: true, Version: 1,
	}
	facts := s.facts(
		mspID, actorID, "system", "classification.system_catalog.ensured",
		"tag", tag.ID, tag.Version, "", now,
	)
	return s.repository.EnsureSystemCatalog(ctx, SystemCatalogMutation{
		CatalogMutation: CatalogMutation{
			Group: &group, Tag: &tag, Audit: facts.audit, Event: facts.event,
		},
		Group: group,
		Tag:   tag,
	})
}

func (s *CatalogService) List(
	ctx context.Context,
	command ListCatalogCommand,
) (Catalog, error) {
	if !s.valid() {
		return Catalog{}, ErrInvalidCatalog
	}
	if err := authorizeCatalogRead(command.Principal); err != nil {
		return Catalog{}, err
	}
	return s.repository.List(ctx, command.Principal.Scope.MSPID)
}

func (s *CatalogService) Health(
	ctx context.Context,
	command HealthCommand,
) (Health, error) {
	if !s.valid() {
		return Health{}, ErrInvalidCatalog
	}
	target := command.Target
	if target.MSPID == "" {
		target = scope.Target{
			MSPID:    command.Principal.Scope.MSPID,
			ClientID: command.Principal.Scope.ClientID,
		}
	}
	if err := authorization.Authorize(
		command.Principal, "classification.manage", target,
	); err != nil {
		return Health{}, err
	}
	return s.repository.Health(ctx, target)
}

func (s *CatalogService) MigrationHistory(
	ctx context.Context,
	command MigrationHistoryCommand,
) ([]MigrationRun, error) {
	if !s.valid() {
		return nil, ErrInvalidCatalog
	}
	mspID, err := authorizeCatalogManagement(command.Principal)
	if err != nil {
		return nil, err
	}
	return s.repository.MigrationHistory(ctx, mspID)
}

func (s *CatalogService) CreateGroup(
	ctx context.Context,
	command CreateGroupCommand,
) (Group, error) {
	if !s.valid() {
		return Group{}, ErrInvalidCatalog
	}
	mspID, err := authorizeCatalogManagement(command.Principal)
	if err != nil {
		return Group{}, err
	}
	label := displayTerm(command.Label)
	if !validDisplayTerm(label) || command.Position < 1 {
		return Group{}, ErrInvalidCatalog
	}
	now := s.now().UTC()
	group := Group{
		ID: s.newID(), MSPID: mspID,
		InternalKey: "taxonomy.group." + s.newID(),
		Label:       label, Description: strings.TrimSpace(command.Description),
		Position: command.Position, State: StateActive, Version: 1,
	}
	facts := s.facts(
		mspID, command.Principal.ID, "technician",
		"classification.group.created", "tag_group", group.ID,
		group.Version, "", now,
	)
	if err := s.repository.CreateGroup(ctx, CatalogMutation{
		Group: &group, Audit: facts.audit, Event: facts.event,
	}); err != nil {
		return Group{}, err
	}
	return group, nil
}

func (s *CatalogService) UpdateGroup(
	ctx context.Context,
	command UpdateGroupCommand,
) (Group, error) {
	if !s.valid() {
		return Group{}, ErrInvalidCatalog
	}
	mspID, err := authorizeCatalogManagement(command.Principal)
	if err != nil {
		return Group{}, err
	}
	current, err := s.repository.FindGroup(ctx, mspID, command.ID)
	if err != nil {
		return Group{}, err
	}
	if current.SystemManaged {
		return Group{}, ErrSystemManaged
	}
	if err := object.RequireVersion(current.Version, command.ExpectedVersion); err != nil {
		return Group{}, err
	}
	label := displayTerm(command.Label)
	state := command.State
	if state == "" {
		state = current.State
	}
	if !validDisplayTerm(label) || command.Position < 1 ||
		(state != StateActive && state != StateArchived) {
		return Group{}, ErrInvalidCatalog
	}
	updated := current
	updated.Label = label
	updated.Description = strings.TrimSpace(command.Description)
	updated.Position = command.Position
	updated.State = state
	updated.Version++
	now := s.now().UTC()
	facts := s.facts(
		mspID, command.Principal.ID, "technician",
		"classification.group.updated", "tag_group", updated.ID,
		updated.Version, "", now,
	)
	if err := s.repository.UpdateGroup(ctx, CatalogMutation{
		Group: &updated, ExpectedVersion: command.ExpectedVersion,
		Audit: facts.audit, Event: facts.event,
	}); err != nil {
		return Group{}, err
	}
	return updated, nil
}

func (s *CatalogService) CreateTag(
	ctx context.Context,
	command CreateTagCommand,
) (Tag, error) {
	if !s.valid() {
		return Tag{}, ErrInvalidCatalog
	}
	mspID, err := authorizeCatalogManagement(command.Principal)
	if err != nil {
		return Tag{}, err
	}
	group, err := s.repository.FindGroup(ctx, mspID, command.GroupID)
	if err != nil {
		return Tag{}, err
	}
	if group.State != StateActive {
		return Tag{}, ErrInactiveTarget
	}
	label, synonyms, normalized, err := validatedTerms(
		command.Label, command.Synonyms,
	)
	if err != nil {
		return Tag{}, err
	}
	if err := s.repository.TermsAvailable(
		ctx, mspID, "", normalized,
	); err != nil {
		return Tag{}, err
	}
	id := s.newID()
	tag := Tag{
		ID: id, MSPID: mspID, InternalKey: "taxonomy.custom." + id,
		Label: label, GroupID: group.ID,
		Description: strings.TrimSpace(command.Description),
		Color:       strings.TrimSpace(command.Color), State: StateActive,
		Synonyms: synonyms, Version: 1,
	}
	now := s.now().UTC()
	facts := s.facts(
		mspID, command.Principal.ID, "technician",
		"classification.tag.created", "tag", tag.ID, tag.Version, "", now,
	)
	if err := s.repository.CreateTag(ctx, CatalogMutation{
		Tag: &tag, Audit: facts.audit, Event: facts.event,
	}); err != nil {
		return Tag{}, err
	}
	return tag, nil
}

func (s *CatalogService) UpdateTag(
	ctx context.Context,
	command UpdateTagCommand,
) (Tag, error) {
	if !s.valid() {
		return Tag{}, ErrInvalidCatalog
	}
	mspID, err := authorizeCatalogManagement(command.Principal)
	if err != nil {
		return Tag{}, err
	}
	current, err := s.repository.FindTag(ctx, mspID, command.ID)
	if err != nil {
		return Tag{}, err
	}
	if current.SystemManaged {
		return Tag{}, ErrSystemManaged
	}
	if err := object.RequireVersion(current.Version, command.ExpectedVersion); err != nil {
		return Tag{}, err
	}
	groupID := command.GroupID
	if groupID == "" {
		groupID = current.GroupID
	}
	group, err := s.repository.FindGroup(ctx, mspID, groupID)
	if err != nil {
		return Tag{}, err
	}
	if group.State != StateActive {
		return Tag{}, ErrInactiveTarget
	}
	label, synonyms, normalized, err := validatedTerms(
		command.Label, command.Synonyms,
	)
	if err != nil {
		return Tag{}, err
	}
	if err := s.repository.TermsAvailable(
		ctx, mspID, current.ID, normalized,
	); err != nil {
		return Tag{}, err
	}
	updated := current
	updated.GroupID = group.ID
	updated.Label = label
	updated.Description = strings.TrimSpace(command.Description)
	updated.Color = strings.TrimSpace(command.Color)
	updated.Synonyms = synonyms
	updated.Version++
	action := "classification.tag.updated"
	if normalizedTerm(current.Label) != normalizedTerm(updated.Label) {
		action = "classification.tag.renamed"
	}
	now := s.now().UTC()
	facts := s.facts(
		mspID, command.Principal.ID, "technician", action,
		"tag", updated.ID, updated.Version, "", now,
	)
	if err := s.repository.UpdateTag(ctx, CatalogMutation{
		Tag: &updated, ExpectedVersion: command.ExpectedVersion,
		Audit: facts.audit, Event: facts.event,
	}); err != nil {
		return Tag{}, err
	}
	return updated, nil
}

func (s *CatalogService) PreviewImpact(
	ctx context.Context,
	command ImpactCommand,
) (Impact, error) {
	if !s.valid() || strings.TrimSpace(command.TagID) == "" ||
		!validImpactOperation(command.Operation) {
		return Impact{}, ErrInvalidCatalog
	}
	mspID, err := authorizeCatalogManagement(command.Principal)
	if err != nil {
		return Impact{}, err
	}
	return s.repository.PreviewImpact(
		ctx, mspID, command.TagID, command.Operation,
		command.ReplacementTagID,
	)
}

func (s *CatalogService) Merge(
	ctx context.Context,
	command MergeCommand,
) (Tag, error) {
	if !s.valid() || strings.TrimSpace(command.Reason) == "" {
		return Tag{}, ErrReasonRequired
	}
	mspID, err := authorizeCatalogManagement(command.Principal)
	if err != nil {
		return Tag{}, err
	}
	if command.TagID == command.SurvivorTagID ||
		command.TagID == "" || command.SurvivorTagID == "" {
		return Tag{}, ErrInvalidCatalog
	}
	retired, err := s.repository.FindTag(ctx, mspID, command.TagID)
	if err != nil {
		return Tag{}, err
	}
	survivor, err := s.repository.FindTag(ctx, mspID, command.SurvivorTagID)
	if err != nil {
		return Tag{}, err
	}
	if retired.SystemManaged || survivor.SystemManaged {
		return Tag{}, ErrSystemManaged
	}
	if retired.State != StateActive || survivor.State != StateActive {
		return Tag{}, ErrInactiveTarget
	}
	if err := object.RequireVersion(retired.Version, command.ExpectedVersion); err != nil {
		return Tag{}, err
	}
	impact, err := s.repository.PreviewImpact(
		ctx, mspID, retired.ID, ImpactMerge, survivor.ID,
	)
	if err != nil {
		return Tag{}, err
	}
	merged := retired
	merged.State = StateMerged
	merged.MergedIntoTagID = survivor.ID
	merged.Version++
	now := s.now().UTC()
	facts := s.facts(
		mspID, command.Principal.ID, "technician",
		"classification.tag.merged", "tag", merged.ID,
		merged.Version, command.Reason, now,
	)
	accepted := MergeMutation{
		Retired: merged, Survivor: survivor,
		ExpectedVersion: command.ExpectedVersion,
		Reason:          command.Reason, Impact: impact,
		Audit: facts.audit, Event: facts.event,
	}
	if err := s.repository.Merge(ctx, accepted); err != nil {
		return Tag{}, err
	}
	return merged, nil
}

func (s *CatalogService) Archive(
	ctx context.Context,
	command ArchiveCommand,
) (Tag, error) {
	if !s.valid() || strings.TrimSpace(command.Reason) == "" {
		return Tag{}, ErrReasonRequired
	}
	mspID, err := authorizeCatalogManagement(command.Principal)
	if err != nil {
		return Tag{}, err
	}
	current, err := s.repository.FindTag(ctx, mspID, command.TagID)
	if err != nil {
		return Tag{}, err
	}
	if current.SystemManaged {
		return Tag{}, ErrSystemManaged
	}
	if current.State != StateActive {
		return Tag{}, ErrInactiveTarget
	}
	if command.ReplacementTagID == current.ID {
		return Tag{}, ErrInvalidCatalog
	}
	if err := object.RequireVersion(current.Version, command.ExpectedVersion); err != nil {
		return Tag{}, err
	}
	unclassified, err := s.repository.FindUnclassified(ctx, mspID)
	if err != nil {
		return Tag{}, err
	}
	if command.ReplacementTagID != "" {
		replacement, findErr := s.repository.FindTag(
			ctx, mspID, command.ReplacementTagID,
		)
		if findErr != nil {
			return Tag{}, findErr
		}
		if replacement.State != StateActive || replacement.SystemManaged {
			return Tag{}, ErrInactiveTarget
		}
	}
	impact, err := s.repository.PreviewImpact(
		ctx, mspID, current.ID, ImpactArchive,
		command.ReplacementTagID,
	)
	if err != nil {
		return Tag{}, err
	}
	archived := current
	archived.State = StateArchived
	archived.Version++
	now := s.now().UTC()
	facts := s.facts(
		mspID, command.Principal.ID, "technician",
		"classification.tag.archived", "tag", archived.ID,
		archived.Version, command.Reason, now,
	)
	accepted := ArchiveMutation{
		Tag: archived, UnclassifiedTag: unclassified,
		ReplacementTagID: command.ReplacementTagID,
		ExpectedVersion:  command.ExpectedVersion,
		Reason:           command.Reason, Impact: impact,
		Audit: facts.audit, Event: facts.event,
	}
	if err := s.repository.Archive(ctx, accepted); err != nil {
		return Tag{}, err
	}
	return archived, nil
}

func (s *CatalogService) valid() bool {
	return s != nil && s.repository != nil && s.now != nil && s.newID != nil
}

func authorizeCatalogManagement(
	principal authorization.Principal,
) (string, error) {
	target := scope.Target{MSPID: principal.Scope.MSPID}
	if err := authorization.Authorize(
		principal, "classification.manage", target,
	); err != nil {
		return "", err
	}
	return target.MSPID, nil
}

func authorizeCatalogRead(principal authorization.Principal) error {
	target := scope.Target{
		MSPID: principal.Scope.MSPID, ClientID: principal.Scope.ClientID,
	}
	if principal.Capabilities.Has("classification.manage") {
		return authorization.Authorize(
			principal, "classification.manage", target,
		)
	}
	return authorization.Authorize(principal, "classification.apply", target)
}

func displayTerm(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func normalizedTerm(value string) string {
	return strings.ToLower(displayTerm(value))
}

func validDisplayTerm(value string) bool {
	length := len([]rune(value))
	return length >= 1 && length <= 80
}

func validatedTerms(
	rawLabel string,
	rawSynonyms []string,
) (string, []string, []string, error) {
	label := displayTerm(rawLabel)
	if !validDisplayTerm(label) {
		return "", nil, nil, ErrInvalidCatalog
	}
	seen := map[string]struct{}{}
	labelKey := normalizedTerm(label)
	seen[labelKey] = struct{}{}
	normalized := []string{labelKey}
	synonyms := make([]string, 0, len(rawSynonyms))
	for _, raw := range rawSynonyms {
		synonym := displayTerm(raw)
		if !validDisplayTerm(synonym) {
			return "", nil, nil, ErrInvalidCatalog
		}
		key := normalizedTerm(synonym)
		if _, exists := seen[key]; exists {
			return "", nil, nil, ErrDuplicateTerm
		}
		seen[key] = struct{}{}
		synonyms = append(synonyms, synonym)
		normalized = append(normalized, key)
	}
	return label, synonyms, normalized, nil
}

func validImpactOperation(operation ImpactOperation) bool {
	switch operation {
	case ImpactRename, ImpactMove, ImpactMerge, ImpactArchive:
		return true
	default:
		return false
	}
}

type mutationFacts struct {
	audit mutation.AuditRecord
	event mutation.EventRecord
}

func (s *CatalogService) facts(
	mspID string,
	actorID string,
	actorType string,
	action string,
	subjectType string,
	subjectID string,
	subjectVersion int64,
	reason string,
	now time.Time,
) mutationFacts {
	correlationID := s.newID()
	audit := mutation.AuditRecord{
		ID: s.newID(), OccurredAt: now, MSPID: mspID,
		ActorType: actorType, ActorID: actorID, Action: action,
		SubjectType: subjectType, SubjectID: subjectID,
		SubjectVersion: subjectVersion, Source: "api",
		Reason: reason, CorrelationID: correlationID,
	}
	event := mutation.EventRecord{
		EventID: s.newID(), EventType: action, SchemaVersion: 1,
		OccurredAt: now, MSPID: mspID, ActorType: actorType,
		ActorID: actorID, SubjectType: subjectType,
		SubjectID: subjectID, SubjectVersion: subjectVersion,
		Source: "api", CorrelationID: correlationID,
		Data: map[string]any{"action": action},
	}
	return mutationFacts{audit: audit, event: event}
}

func (operation ImpactOperation) String() string {
	return string(operation)
}

func (state State) String() string {
	return string(state)
}

func (objectType ObjectType) String() string {
	return string(objectType)
}

func (source Source) String() string {
	return string(source)
}
