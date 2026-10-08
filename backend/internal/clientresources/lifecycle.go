package clientresources

import (
	"context"
	"errors"
	"math"
	"net/mail"
	"strings"
	"time"
	"unicode"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

var (
	ErrLifecycleConflict         = errors.New("client resource lifecycle conflict")
	ErrResourceInUse             = errors.New("client resource is in use")
	ErrResourceAuthorityConflict = errors.New("client resource authority conflict")
)

// UpdatePatch exposes only the approved mutable business fields. A non-nil
// string pointer is explicit, including an empty string used to clear an
// optional value. ClearEndsOn distinguishes an end-date clear from omission.
type UpdatePatch struct {
	Name        *string
	DisplayName *string
	Email       *string
	Phone       *string
	LocationID  *string
	AssetType   *string
	Criticality *string
	StartsOn    *time.Time
	EndsOn      *time.Time
	ClearEndsOn bool
}

type UpdateCommand struct {
	Principal       authorization.Principal
	Target          scope.Target
	Kind            Kind
	ResourceID      string
	ExpectedVersion int64
	Patch           UpdatePatch
	Reason          string
	ActorID         string
	Source          string
	CorrelationID   string
}

type LifecycleCommand struct {
	Principal       authorization.Principal
	Target          scope.Target
	Kind            Kind
	ResourceID      string
	ExpectedVersion int64
	Reason          string
	ActorID         string
	Source          string
	CorrelationID   string
}

type LifecyclePreflightOperation string

const (
	PreflightUpdate     LifecyclePreflightOperation = "update"
	PreflightDeactivate LifecyclePreflightOperation = "deactivate"
	PreflightReactivate LifecyclePreflightOperation = "reactivate"
)

type LifecyclePreflightCommand struct {
	Principal       authorization.Principal
	Target          scope.Target
	Kind            Kind
	ResourceID      string
	ExpectedVersion int64
	Patch           UpdatePatch
	Operation       LifecyclePreflightOperation
}

type LifecyclePreflightMutation struct {
	Target          scope.Target
	Kind            Kind
	ResourceID      string
	ExpectedVersion int64
	Patch           UpdatePatch
	Operation       LifecyclePreflightOperation
}

type LifecyclePreflight struct {
	Resource           ResourceDetail
	DependencyStatus   string
	RelationshipStatus string
}

// UpdateMutation is accepted domain state. Repository entry points remain
// fixed per resource kind so callers cannot provide a table or SQL shape.
type UpdateMutation struct {
	Target          scope.Target
	ResourceID      string
	ExpectedVersion int64
	Patch           UpdatePatch
	ActorID         string
	UpdatedAt       time.Time
	Audit           mutation.AuditRecord
	Event           mutation.EventRecord
}

type LifecycleMutation struct {
	Target          scope.Target
	ResourceID      string
	ExpectedVersion int64
	FromState       string
	ToState         string
	ActorID         string
	UpdatedAt       time.Time
	Audit           mutation.AuditRecord
	Event           mutation.EventRecord
}

type locationUpdateRepository interface {
	UpdateLocation(context.Context, UpdateMutation) (ResourceDetail, error)
}
type contactUpdateRepository interface {
	UpdateContact(context.Context, UpdateMutation) (ResourceDetail, error)
}
type assetUpdateRepository interface {
	UpdateAsset(context.Context, UpdateMutation) (ResourceDetail, error)
}
type serviceUpdateRepository interface {
	UpdateService(context.Context, UpdateMutation) (ResourceDetail, error)
}
type contractUpdateRepository interface {
	UpdateContract(context.Context, UpdateMutation) (ResourceDetail, error)
}

type locationLifecycleRepository interface {
	DeactivateLocation(context.Context, LifecycleMutation) (ResourceDetail, error)
	ReactivateLocation(context.Context, LifecycleMutation) (ResourceDetail, error)
}
type contactLifecycleRepository interface {
	DeactivateContact(context.Context, LifecycleMutation) (ResourceDetail, error)
	ReactivateContact(context.Context, LifecycleMutation) (ResourceDetail, error)
}
type assetLifecycleRepository interface {
	DeactivateAsset(context.Context, LifecycleMutation) (ResourceDetail, error)
	ReactivateAsset(context.Context, LifecycleMutation) (ResourceDetail, error)
}
type serviceLifecycleRepository interface {
	DeactivateService(context.Context, LifecycleMutation) (ResourceDetail, error)
	ReactivateService(context.Context, LifecycleMutation) (ResourceDetail, error)
}
type contractLifecycleRepository interface {
	DeactivateContract(context.Context, LifecycleMutation) (ResourceDetail, error)
	ReactivateContract(context.Context, LifecycleMutation) (ResourceDetail, error)
}

type lifecyclePreflightRepository interface {
	Preflight(context.Context, LifecyclePreflightMutation) (LifecyclePreflight, error)
}

func (s *Service) Preflight(ctx context.Context, command LifecyclePreflightCommand) (LifecyclePreflight, error) {
	resourceID := strings.TrimSpace(command.ResourceID)
	capability := string(command.Kind) + ".lifecycle"
	patch := UpdatePatch{}
	switch command.Operation {
	case PreflightUpdate:
		capability = string(command.Kind) + ".update"
		normalized, err := normalizeUpdatePatch(command.Kind, command.Patch)
		if err != nil {
			return LifecyclePreflight{}, err
		}
		patch = normalized
	case PreflightDeactivate, PreflightReactivate:
	default:
		return LifecyclePreflight{}, ErrInvalid
	}
	if s == nil || s.repository == nil || !validKind(command.Kind) ||
		strings.TrimSpace(command.Target.MSPID) == "" ||
		strings.TrimSpace(command.Target.ClientID) == "" ||
		resourceID == "" || command.ExpectedVersion < 1 ||
		command.ExpectedVersion == math.MaxInt64 {
		return LifecyclePreflight{}, ErrInvalid
	}
	if err := authorization.Authorize(command.Principal, capability, command.Target); err != nil {
		return LifecyclePreflight{}, err
	}
	repository, ok := s.repository.(lifecyclePreflightRepository)
	if !ok {
		return LifecyclePreflight{}, ErrInvalid
	}
	return repository.Preflight(ctx, LifecyclePreflightMutation{
		Target: command.Target, Kind: command.Kind, ResourceID: resourceID,
		ExpectedVersion: command.ExpectedVersion, Patch: patch, Operation: command.Operation,
	})
}

func (s *Service) Update(ctx context.Context, command UpdateCommand) (ResourceDetail, error) {
	patch, err := normalizeUpdatePatch(command.Kind, command.Patch)
	if err != nil {
		return ResourceDetail{}, err
	}
	target, resourceID, reason, actorID, source, correlationID, err :=
		s.validateMutation(command.Principal, command.Target, command.Kind,
			command.ResourceID, command.ExpectedVersion, command.Reason,
			command.ActorID, command.Source, command.CorrelationID,
			string(command.Kind)+".update")
	if err != nil {
		return ResourceDetail{}, err
	}
	now := s.now().UTC()
	action := string(command.Kind) + ".updated"
	audit, event := s.mutationFacts(target, command.Kind, resourceID,
		command.ExpectedVersion+1, action, reason, actorID, source, correlationID, now)
	accepted := UpdateMutation{
		Target: target, ResourceID: resourceID, ExpectedVersion: command.ExpectedVersion,
		Patch: patch, ActorID: actorID, UpdatedAt: now, Audit: audit, Event: event,
	}
	switch command.Kind {
	case LocationKind:
		repository, ok := s.repository.(locationUpdateRepository)
		if !ok {
			return ResourceDetail{}, ErrInvalid
		}
		return repository.UpdateLocation(ctx, accepted)
	case ContactKind:
		repository, ok := s.repository.(contactUpdateRepository)
		if !ok {
			return ResourceDetail{}, ErrInvalid
		}
		return repository.UpdateContact(ctx, accepted)
	case AssetKind:
		repository, ok := s.repository.(assetUpdateRepository)
		if !ok {
			return ResourceDetail{}, ErrInvalid
		}
		return repository.UpdateAsset(ctx, accepted)
	case ServiceKind:
		repository, ok := s.repository.(serviceUpdateRepository)
		if !ok {
			return ResourceDetail{}, ErrInvalid
		}
		return repository.UpdateService(ctx, accepted)
	case ContractKind:
		repository, ok := s.repository.(contractUpdateRepository)
		if !ok {
			return ResourceDetail{}, ErrInvalid
		}
		return repository.UpdateContract(ctx, accepted)
	default:
		return ResourceDetail{}, ErrInvalid
	}
}

func (s *Service) Deactivate(ctx context.Context, command LifecycleCommand) (ResourceDetail, error) {
	return s.changeLifecycle(ctx, command, "active", "inactive", "deactivated")
}

func (s *Service) Reactivate(ctx context.Context, command LifecycleCommand) (ResourceDetail, error) {
	return s.changeLifecycle(ctx, command, "inactive", "active", "reactivated")
}

func (s *Service) changeLifecycle(
	ctx context.Context,
	command LifecycleCommand,
	fromState, toState, verb string,
) (ResourceDetail, error) {
	target, resourceID, reason, actorID, source, correlationID, err :=
		s.validateMutation(command.Principal, command.Target, command.Kind,
			command.ResourceID, command.ExpectedVersion, command.Reason,
			command.ActorID, command.Source, command.CorrelationID,
			string(command.Kind)+".lifecycle")
	if err != nil {
		return ResourceDetail{}, err
	}
	now := s.now().UTC()
	action := string(command.Kind) + "." + verb
	audit, event := s.mutationFacts(target, command.Kind, resourceID,
		command.ExpectedVersion+1, action, reason, actorID, source, correlationID, now)
	accepted := LifecycleMutation{
		Target: target, ResourceID: resourceID, ExpectedVersion: command.ExpectedVersion,
		FromState: fromState, ToState: toState, ActorID: actorID, UpdatedAt: now,
		Audit: audit, Event: event,
	}
	if toState == "inactive" {
		return s.deactivate(ctx, command.Kind, accepted)
	}
	return s.reactivate(ctx, command.Kind, accepted)
}

func (s *Service) deactivate(ctx context.Context, kind Kind, accepted LifecycleMutation) (ResourceDetail, error) {
	switch kind {
	case LocationKind:
		repository, ok := s.repository.(locationLifecycleRepository)
		if !ok {
			return ResourceDetail{}, ErrInvalid
		}
		return repository.DeactivateLocation(ctx, accepted)
	case ContactKind:
		repository, ok := s.repository.(contactLifecycleRepository)
		if !ok {
			return ResourceDetail{}, ErrInvalid
		}
		return repository.DeactivateContact(ctx, accepted)
	case AssetKind:
		repository, ok := s.repository.(assetLifecycleRepository)
		if !ok {
			return ResourceDetail{}, ErrInvalid
		}
		return repository.DeactivateAsset(ctx, accepted)
	case ServiceKind:
		repository, ok := s.repository.(serviceLifecycleRepository)
		if !ok {
			return ResourceDetail{}, ErrInvalid
		}
		return repository.DeactivateService(ctx, accepted)
	case ContractKind:
		repository, ok := s.repository.(contractLifecycleRepository)
		if !ok {
			return ResourceDetail{}, ErrInvalid
		}
		return repository.DeactivateContract(ctx, accepted)
	default:
		return ResourceDetail{}, ErrInvalid
	}
}

func (s *Service) reactivate(ctx context.Context, kind Kind, accepted LifecycleMutation) (ResourceDetail, error) {
	switch kind {
	case LocationKind:
		repository, ok := s.repository.(locationLifecycleRepository)
		if !ok {
			return ResourceDetail{}, ErrInvalid
		}
		return repository.ReactivateLocation(ctx, accepted)
	case ContactKind:
		repository, ok := s.repository.(contactLifecycleRepository)
		if !ok {
			return ResourceDetail{}, ErrInvalid
		}
		return repository.ReactivateContact(ctx, accepted)
	case AssetKind:
		repository, ok := s.repository.(assetLifecycleRepository)
		if !ok {
			return ResourceDetail{}, ErrInvalid
		}
		return repository.ReactivateAsset(ctx, accepted)
	case ServiceKind:
		repository, ok := s.repository.(serviceLifecycleRepository)
		if !ok {
			return ResourceDetail{}, ErrInvalid
		}
		return repository.ReactivateService(ctx, accepted)
	case ContractKind:
		repository, ok := s.repository.(contractLifecycleRepository)
		if !ok {
			return ResourceDetail{}, ErrInvalid
		}
		return repository.ReactivateContract(ctx, accepted)
	default:
		return ResourceDetail{}, ErrInvalid
	}
}

func (s *Service) validateMutation(
	principal authorization.Principal,
	target scope.Target,
	kind Kind,
	resourceID string,
	expectedVersion int64,
	reason, actorID, source, correlationID, capability string,
) (scope.Target, string, string, string, string, string, error) {
	resourceID = strings.TrimSpace(resourceID)
	reason = strings.TrimSpace(reason)
	actorID = strings.TrimSpace(actorID)
	source = strings.TrimSpace(source)
	correlationID = strings.TrimSpace(correlationID)
	if s == nil || s.repository == nil || s.now == nil || s.newID == nil ||
		!validKind(kind) || strings.TrimSpace(target.MSPID) == "" ||
		strings.TrimSpace(target.ClientID) == "" || resourceID == "" ||
		expectedVersion < 1 || expectedVersion == math.MaxInt64 || reason == "" ||
		actorID == "" || source == "" || correlationID == "" {
		return scope.Target{}, "", "", "", "", "", ErrInvalid
	}
	if err := authorization.Authorize(principal, capability, target); err != nil {
		return scope.Target{}, "", "", "", "", "", err
	}
	return target, resourceID, reason, actorID, source, correlationID, nil
}

func (s *Service) mutationFacts(
	target scope.Target,
	kind Kind,
	resourceID string,
	version int64,
	action, reason, actorID, source, correlationID string,
	now time.Time,
) (mutation.AuditRecord, mutation.EventRecord) {
	return mutation.AuditRecord{
			ID: s.newID(), OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID,
			ActorType: "technician", ActorID: actorID, Action: action,
			SubjectType: string(kind), SubjectID: resourceID, SubjectVersion: version,
			Source: source, Reason: reason, CorrelationID: correlationID,
		}, mutation.EventRecord{
			EventID: s.newID(), EventType: action, SchemaVersion: 1,
			OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID,
			ActorType: "technician", ActorID: actorID,
			SubjectType: string(kind), SubjectID: resourceID, SubjectVersion: version,
			Source: source, CorrelationID: correlationID,
		}
}

func normalizeUpdatePatch(kind Kind, patch UpdatePatch) (UpdatePatch, error) {
	if !validKind(kind) || patch.ClearEndsOn && patch.EndsOn != nil {
		return UpdatePatch{}, ErrInvalid
	}
	fieldCount := updatePatchFieldCount(patch)
	if fieldCount == 0 {
		return UpdatePatch{}, ErrInvalid
	}
	allowed := 0
	switch kind {
	case LocationKind:
		allowed = boolCount(patch.Name != nil)
	case ContactKind:
		allowed = boolCount(patch.DisplayName != nil) + boolCount(patch.Email != nil) + boolCount(patch.Phone != nil) + boolCount(patch.LocationID != nil)
	case AssetKind:
		allowed = boolCount(patch.Name != nil) + boolCount(patch.AssetType != nil) + boolCount(patch.LocationID != nil)
	case ServiceKind:
		allowed = boolCount(patch.Name != nil) + boolCount(patch.Criticality != nil)
	case ContractKind:
		allowed = boolCount(patch.Name != nil) + boolCount(patch.StartsOn != nil) + boolCount(patch.EndsOn != nil) + boolCount(patch.ClearEndsOn)
	}
	if allowed != fieldCount {
		return UpdatePatch{}, ErrInvalid
	}
	normalized := patch
	if err := normalizeRequiredString(&normalized.Name); err != nil {
		return UpdatePatch{}, err
	}
	if err := normalizeRequiredString(&normalized.DisplayName); err != nil {
		return UpdatePatch{}, err
	}
	if err := normalizeRequiredString(&normalized.AssetType); err != nil {
		return UpdatePatch{}, err
	}
	normalizeOptionalString(&normalized.Email)
	normalizeOptionalString(&normalized.Phone)
	normalizeOptionalString(&normalized.LocationID)
	normalizeOptionalString(&normalized.Criticality)
	if normalized.Email != nil && *normalized.Email != "" && !validContactEmail(*normalized.Email) {
		return UpdatePatch{}, ErrInvalid
	}
	if normalized.Phone != nil && *normalized.Phone != "" && !validContactPhone(*normalized.Phone) {
		return UpdatePatch{}, ErrInvalid
	}
	if normalized.Criticality != nil && !validServiceCriticality(*normalized.Criticality) {
		return UpdatePatch{}, ErrInvalid
	}
	if normalized.StartsOn != nil {
		if normalized.StartsOn.IsZero() {
			return UpdatePatch{}, ErrInvalid
		}
		value := normalized.StartsOn.UTC()
		normalized.StartsOn = &value
	}
	if normalized.EndsOn != nil {
		if normalized.EndsOn.IsZero() {
			return UpdatePatch{}, ErrInvalid
		}
		value := normalized.EndsOn.UTC()
		normalized.EndsOn = &value
	}
	if normalized.StartsOn != nil && normalized.EndsOn != nil && normalized.EndsOn.Before(*normalized.StartsOn) {
		return UpdatePatch{}, ErrInvalid
	}
	return normalized, nil
}

func updatePatchFieldCount(patch UpdatePatch) int {
	return boolCount(patch.Name != nil) + boolCount(patch.DisplayName != nil) +
		boolCount(patch.Email != nil) + boolCount(patch.Phone != nil) +
		boolCount(patch.LocationID != nil) + boolCount(patch.AssetType != nil) +
		boolCount(patch.Criticality != nil) + boolCount(patch.StartsOn != nil) +
		boolCount(patch.EndsOn != nil) + boolCount(patch.ClearEndsOn)
}

func boolCount(value bool) int {
	if value {
		return 1
	}
	return 0
}

func normalizeRequiredString(value **string) error {
	if *value == nil {
		return nil
	}
	normalized := strings.TrimSpace(**value)
	if normalized == "" {
		return ErrInvalid
	}
	*value = &normalized
	return nil
}

func normalizeOptionalString(value **string) {
	if *value == nil {
		return
	}
	normalized := strings.TrimSpace(**value)
	*value = &normalized
}

func validContactEmail(value string) bool {
	address, err := mail.ParseAddress(value)
	return err == nil && strings.EqualFold(address.Address, value)
}

func validContactPhone(value string) bool {
	digits := 0
	for _, character := range value {
		if unicode.IsDigit(character) {
			digits++
			continue
		}
		if !strings.ContainsRune(" +()-xX.", character) {
			return false
		}
	}
	return digits >= 7 && len(value) <= 32
}
