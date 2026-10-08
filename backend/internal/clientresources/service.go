// Package clientresources owns client-scoped locations, contacts, assets,
// operational services, and contracts.
package clientresources

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
)

var (
	ErrInvalid                  = errors.New("invalid client resource")
	ErrResourceIdentityConflict = errors.New("resource identity conflict")
)

// PreparedIdentity is a trusted adapter-only identity boundary. Public
// request DTOs intentionally do not expose these values.
type PreparedIdentity struct {
	ResourceID    string
	CorrelationID string
}

type Ref struct {
	ID       string
	MSPID    string
	ClientID string
}

type Location struct {
	object.Envelope
	Name string
}

type Contact struct {
	object.Envelope
	LocationID  string
	DisplayName string
	Email       string
	Phone       string
}

type Authority string

const (
	Discovered          Authority = "discovered"
	TechnicianConfirmed Authority = "technician_confirmed"
)

type Provenance struct {
	SourceSystem string
	ExternalID   string
	Authority    Authority
}

type Asset struct {
	object.Envelope
	LocationID string
	Name       string
	AssetType  string
	Provenance Provenance
}

type ServiceRecord struct {
	object.Envelope
	Name        string
	Criticality string
}

type Contract struct {
	object.Envelope
	Name     string
	StartsOn time.Time
	EndsOn   *time.Time
}

type CreateMutation struct {
	Kind        string
	Object      object.Envelope
	Payload     any
	Audit       mutation.AuditRecord
	Event       mutation.EventRecord
	InitialTags tagging.InitialAssignmentSet
}

type Repository interface {
	CreateAtomic(context.Context, CreateMutation) error
}

type Service struct {
	repository Repository
	now        func() time.Time
	newID      func() string
	creation   *tagging.CreationPreparer
}

func NewService(repository Repository, now func() time.Time, newID func() string, creation ...*tagging.CreationPreparer) *Service {
	service := &Service{repository: repository, now: now, newID: newID}
	if len(creation) > 0 {
		service.creation = creation[0]
	}
	return service
}

type baseCommand struct {
	Principal authorization.Principal
	Target    scope.Target
	DisplayID string
	ActorID   string
	Source    string
	Prepared  PreparedIdentity
}

type CreateLocationCommand struct {
	Principal authorization.Principal
	Target    scope.Target
	DisplayID string
	Name      string
	ActorID   string
	Source    string
	Prepared  PreparedIdentity
}

func (s *Service) CreateLocation(ctx context.Context, command CreateLocationCommand) (Location, error) {
	envelope, records, err := s.prepare(baseCommand{
		command.Principal, command.Target, command.DisplayID, command.ActorID, command.Source, command.Prepared,
	}, "location", "location.create")
	if err != nil || strings.TrimSpace(command.Name) == "" {
		if err != nil {
			return Location{}, err
		}
		return Location{}, ErrInvalid
	}
	location := Location{Envelope: envelope, Name: strings.TrimSpace(command.Name)}
	if err := s.persist(ctx, records, location); err != nil {
		return Location{}, err
	}
	return location, nil
}

type CreateContactCommand struct {
	Principal   authorization.Principal
	Target      scope.Target
	Location    Ref
	DisplayID   string
	DisplayName string
	Email       string
	Phone       string
	ActorID     string
	Source      string
	Prepared    PreparedIdentity
}

func (s *Service) CreateContact(ctx context.Context, command CreateContactCommand) (Contact, error) {
	envelope, records, err := s.prepare(baseCommand{
		command.Principal, command.Target, command.DisplayID, command.ActorID, command.Source, command.Prepared,
	}, "contact", "contact.create")
	if err != nil {
		return Contact{}, err
	}
	if strings.TrimSpace(command.DisplayName) == "" {
		return Contact{}, ErrInvalid
	}
	if command.Location.ID != "" &&
		(command.Location.MSPID != envelope.MSPID || command.Location.ClientID != envelope.ClientID) {
		return Contact{}, scope.ErrNotFound
	}
	contact := Contact{
		Envelope: envelope, LocationID: command.Location.ID,
		DisplayName: strings.TrimSpace(command.DisplayName), Email: strings.TrimSpace(command.Email), Phone: strings.TrimSpace(command.Phone),
	}
	if err := s.persist(ctx, records, contact); err != nil {
		return Contact{}, err
	}
	return contact, nil
}

type CreateAssetCommand struct {
	Principal            authorization.Principal
	Target               scope.Target
	Location             Ref
	DisplayID            string
	Name                 string
	AssetType            string
	SourceSystem         string
	ExternalID           string
	Authority            Authority
	ActorID              string
	Source               string
	TagIDs               []string
	ClassificationPolicy tagging.CreationPolicy
	Prepared             PreparedIdentity
}

func (s *Service) CreateAsset(ctx context.Context, command CreateAssetCommand) (Asset, error) {
	envelope, records, err := s.prepare(baseCommand{
		command.Principal, command.Target, command.DisplayID, command.ActorID, command.Source, command.Prepared,
	}, "asset", "asset.create")
	if err != nil {
		return Asset{}, err
	}
	if strings.TrimSpace(command.Name) == "" ||
		strings.TrimSpace(command.AssetType) == "" ||
		!validAssetExternalIdentity(command.SourceSystem, command.ExternalID) ||
		(command.Authority != Discovered && command.Authority != TechnicianConfirmed) {
		return Asset{}, ErrInvalid
	}
	if command.Location.ID != "" &&
		(command.Location.MSPID != envelope.MSPID || command.Location.ClientID != envelope.ClientID) {
		return Asset{}, scope.ErrNotFound
	}
	asset := Asset{
		Envelope: envelope, LocationID: command.Location.ID,
		Name: strings.TrimSpace(command.Name), AssetType: strings.TrimSpace(command.AssetType),
		Provenance: Provenance{
			SourceSystem: strings.TrimSpace(command.SourceSystem),
			ExternalID:   strings.TrimSpace(command.ExternalID), Authority: command.Authority,
		},
	}
	initial, err := s.initialAssetTags(ctx, envelope, command)
	if err != nil {
		return Asset{}, err
	}
	if err := s.persistWithInitialTags(ctx, records, asset, initial); err != nil {
		return Asset{}, err
	}
	return asset, nil
}

func (s *Service) initialAssetTags(ctx context.Context, envelope object.Envelope, command CreateAssetCommand) (tagging.InitialAssignmentSet, error) {
	if len(command.TagIDs) == 0 && command.ClassificationPolicy == "" {
		return tagging.InitialAssignmentSet{}, nil
	}
	if s == nil || s.creation == nil {
		return tagging.InitialAssignmentSet{}, ErrInvalid
	}
	return s.creation.Prepare(ctx, tagging.PrepareCreationCommand{MSPID: envelope.MSPID, ClientID: envelope.ClientID, ObjectType: tagging.ObjectAsset, TagIDs: command.TagIDs, Source: tagging.SourceHuman, Policy: command.ClassificationPolicy})
}

func (s *Service) persistWithInitialTags(ctx context.Context, records preparedRecords, value Asset, initial tagging.InitialAssignmentSet) error {
	accepted := CreateMutation{Kind: records.kind, Object: value.Envelope, Payload: value, Audit: records.audit, Event: records.event, InitialTags: initial.WithProvenance(tagging.InitialAssignmentProvenance{ActorType: records.event.ActorType, ActorID: records.event.ActorID, OccurredAt: records.event.OccurredAt, CorrelationID: records.event.CorrelationID})}
	return s.repository.CreateAtomic(ctx, accepted)
}

type CreateServiceCommand struct {
	Principal   authorization.Principal
	Target      scope.Target
	DisplayID   string
	Name        string
	Criticality string
	ActorID     string
	Source      string
	Prepared    PreparedIdentity
}

func (s *Service) CreateService(ctx context.Context, command CreateServiceCommand) (ServiceRecord, error) {
	envelope, records, err := s.prepare(baseCommand{
		command.Principal, command.Target, command.DisplayID, command.ActorID, command.Source, command.Prepared,
	}, "service", "service.create")
	if err != nil || strings.TrimSpace(command.Name) == "" ||
		!validServiceCriticality(command.Criticality) {
		if err != nil {
			return ServiceRecord{}, err
		}
		return ServiceRecord{}, ErrInvalid
	}
	service := ServiceRecord{
		Envelope: envelope, Name: strings.TrimSpace(command.Name),
		Criticality: strings.TrimSpace(command.Criticality),
	}
	if err := s.persist(ctx, records, service); err != nil {
		return ServiceRecord{}, err
	}
	return service, nil
}

func validAssetExternalIdentity(sourceSystem, externalID string) bool {
	return (strings.TrimSpace(sourceSystem) == "") == (strings.TrimSpace(externalID) == "")
}

func validServiceCriticality(value string) bool {
	switch strings.TrimSpace(value) {
	case "", "low", "normal", "high", "critical":
		return true
	default:
		return false
	}
}

type CreateContractCommand struct {
	Principal authorization.Principal
	Target    scope.Target
	DisplayID string
	Name      string
	StartsOn  time.Time
	EndsOn    *time.Time
	ActorID   string
	Source    string
	Prepared  PreparedIdentity
}

func (s *Service) CreateContract(ctx context.Context, command CreateContractCommand) (Contract, error) {
	if strings.TrimSpace(command.Name) == "" ||
		command.StartsOn.IsZero() ||
		(command.EndsOn != nil && command.EndsOn.Before(command.StartsOn)) {
		return Contract{}, ErrInvalid
	}
	envelope, records, err := s.prepare(baseCommand{
		command.Principal, command.Target, command.DisplayID, command.ActorID, command.Source, command.Prepared,
	}, "contract", "contract.create")
	if err != nil {
		return Contract{}, err
	}
	contract := Contract{
		Envelope: envelope, Name: strings.TrimSpace(command.Name),
		StartsOn: command.StartsOn.UTC(), EndsOn: command.EndsOn,
	}
	if err := s.persist(ctx, records, contract); err != nil {
		return Contract{}, err
	}
	return contract, nil
}

type preparedRecords struct {
	kind  string
	audit mutation.AuditRecord
	event mutation.EventRecord
}

func (s *Service) prepare(command baseCommand, kind, capability string) (object.Envelope, preparedRecords, error) {
	resourceID := strings.TrimSpace(command.Prepared.ResourceID)
	correlationID := strings.TrimSpace(command.Prepared.CorrelationID)
	if (resourceID != "" && uuid.Validate(resourceID) != nil) ||
		(correlationID != "" && uuid.Validate(correlationID) != nil) {
		return object.Envelope{}, preparedRecords{}, ErrInvalid
	}
	target := command.Target
	if target.MSPID == "" {
		target = scope.Target{
			MSPID: command.Principal.Scope.MSPID, ClientID: command.Principal.Scope.ClientID,
		}
	}
	if target.ClientID == "" ||
		strings.TrimSpace(command.DisplayID) == "" ||
		command.ActorID == "" ||
		command.Source == "" {
		return object.Envelope{}, preparedRecords{}, ErrInvalid
	}
	if err := authorization.Authorize(command.Principal, capability, target); err != nil {
		return object.Envelope{}, preparedRecords{}, err
	}
	now := s.now().UTC()
	entityID := resourceID
	if entityID == "" {
		entityID = s.newID()
	}
	auditID := s.newID()
	eventID := s.newID()
	if correlationID == "" {
		correlationID = s.newID()
	}
	envelope := object.Envelope{
		ID: entityID, ObjectType: kind, MSPID: target.MSPID, ClientID: target.ClientID,
		DisplayID: strings.TrimSpace(command.DisplayID), LifecycleState: "active", Version: 1,
		CreatedAt: now, CreatedBy: command.ActorID, UpdatedAt: now, UpdatedBy: command.ActorID,
	}
	action := kind + ".created"
	return envelope, preparedRecords{
		kind: kind,
		audit: mutation.AuditRecord{
			ID: auditID, OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID,
			ActorType: "technician", ActorID: command.ActorID, Action: action,
			SubjectType: kind, SubjectID: entityID, SubjectVersion: 1,
			Source: command.Source, CorrelationID: correlationID,
		},
		event: mutation.EventRecord{
			EventID: eventID, EventType: action, SchemaVersion: 1,
			OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID,
			ActorType: "technician", ActorID: command.ActorID,
			SubjectType: kind, SubjectID: entityID, SubjectVersion: 1,
			Source: command.Source, CorrelationID: correlationID,
		},
	}, nil
}

func (s *Service) persist(ctx context.Context, records preparedRecords, payload any) error {
	var envelope object.Envelope
	switch value := payload.(type) {
	case Location:
		envelope = value.Envelope
	case Contact:
		envelope = value.Envelope
	case Asset:
		envelope = value.Envelope
	case ServiceRecord:
		envelope = value.Envelope
	case Contract:
		envelope = value.Envelope
	default:
		return ErrInvalid
	}
	return s.repository.CreateAtomic(ctx, CreateMutation{
		Kind: records.kind, Object: envelope, Payload: payload,
		Audit: records.audit, Event: records.event,
	})
}
