package automation

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/webhooks"
)

var (
	ErrInvalidManagement        = errors.New("invalid automation management request")
	ErrInactiveCatalogReference = errors.New("automation references an inactive catalog identity")
)

type ManagedDefinition struct {
	Definition
	Name          string `json:"name"`
	RecordVersion int64  `json:"record_version"`
	VersionID     string `json:"version_id"`
}

type DefinitionMutation struct {
	Managed               ManagedDefinition
	VersionID             string
	ExpectedRecordVersion int64
	Audit                 mutation.AuditRecord
	Event                 mutation.EventRecord
}

type ConnectionMutation struct {
	Connection ExternalConnection
	Audit      mutation.AuditRecord
	Event      mutation.EventRecord
}

type ManagementRepository interface {
	ListDefinitions(
		context.Context,
		scope.Target,
	) ([]ManagedDefinition, error)
	FindDefinition(
		context.Context,
		scope.Target,
		string,
		int64,
	) (ManagedDefinition, error)
	CreateDefinition(context.Context, DefinitionMutation) error
	ReviseDefinition(context.Context, DefinitionMutation) error
	PublishDefinition(context.Context, DefinitionMutation) error
	CreateConnection(context.Context, ConnectionMutation) error
}

func (s *ManagementService) ListDefinitions(
	ctx context.Context,
	principal authorization.Principal,
) ([]ManagedDefinition, error) {
	target, err := s.authorize(principal)
	if err != nil {
		return nil, err
	}
	return s.repository.ListDefinitions(ctx, target)
}

type ManagementService struct {
	repository ManagementRepository
	now        func() time.Time
	newID      func() string
}

func NewManagementService(
	repository ManagementRepository,
	now func() time.Time,
	newID func() string,
) *ManagementService {
	return &ManagementService{
		repository: repository, now: now, newID: newID,
	}
}

type CreateDefinitionCommand struct {
	Principal    authorization.Principal
	Name         string
	Trigger      Trigger
	Capabilities []string
	Steps        []Step
}

type ReviseDefinitionCommand struct {
	Principal             authorization.Principal
	ID                    string
	Version               int64
	ExpectedRecordVersion int64
	Trigger               Trigger
	Capabilities          []string
	Steps                 []Step
}

type PublishDefinitionCommand struct {
	Principal             authorization.Principal
	ID                    string
	Version               int64
	ExpectedRecordVersion int64
}

type CreateExternalConnectionCommand struct {
	Principal        authorization.Principal
	Name             string
	Endpoint         string
	SigningSecretRef string
}

func (s *ManagementService) CreateDefinition(
	ctx context.Context,
	command CreateDefinitionCommand,
) (Definition, error) {
	target, err := s.authorize(command.Principal)
	if err != nil {
		return Definition{}, err
	}
	name := strings.TrimSpace(command.Name)
	definition := Definition{
		ID: s.newID(), MSPID: target.MSPID, Version: 1, State: Draft,
		ClientScopes: []string{target.ClientID},
		Capabilities: normalizedStrings(command.Capabilities),
		Trigger:      command.Trigger,
		Steps:        cloneSteps(command.Steps),
	}
	if name == "" || Validate(definition) != nil {
		return Definition{}, ErrInvalidManagement
	}
	versionID := s.newID()
	now := s.now().UTC()
	auditID, eventID, correlationID := s.newID(), s.newID(), s.newID()
	managed := ManagedDefinition{
		Definition: definition, Name: name,
		RecordVersion: 1, VersionID: versionID,
	}
	accepted := DefinitionMutation{
		Managed: managed, VersionID: versionID,
		Audit: mutation.AuditRecord{
			ID: auditID, OccurredAt: now, MSPID: target.MSPID,
			ClientID: target.ClientID, ActorType: "technician",
			ActorID:     command.Principal.ID,
			Action:      "automation.definition.created",
			SubjectType: "automation_definition",
			SubjectID:   definition.ID, SubjectVersion: 1,
			Source: "api", CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID:       eventID,
			EventType:     "automation.definition.created",
			SchemaVersion: 1, OccurredAt: now, MSPID: target.MSPID,
			ClientID: target.ClientID, ActorType: "technician",
			ActorID:     command.Principal.ID,
			SubjectType: "automation_definition",
			SubjectID:   definition.ID, SubjectVersion: 1,
			Source: "api", CorrelationID: correlationID,
			Data: map[string]any{
				"name": name, "automation_version": definition.Version,
				"state": definition.State,
			},
		},
	}
	if err := s.repository.CreateDefinition(ctx, accepted); err != nil {
		return Definition{}, err
	}
	return definition, nil
}

func (s *ManagementService) ReviseDefinition(
	ctx context.Context,
	command ReviseDefinitionCommand,
) (Definition, error) {
	target, err := s.authorize(command.Principal)
	if err != nil {
		return Definition{}, err
	}
	if strings.TrimSpace(command.ID) == "" || command.Version < 1 ||
		command.ExpectedRecordVersion < 1 {
		return Definition{}, ErrInvalidManagement
	}
	managed, err := s.repository.FindDefinition(
		ctx, target, command.ID, command.Version,
	)
	if err != nil {
		return Definition{}, err
	}
	if err := object.RequireVersion(
		managed.RecordVersion, command.ExpectedRecordVersion,
	); err != nil {
		return Definition{}, err
	}
	revision, err := Revise(managed.Definition)
	if err != nil {
		return Definition{}, ErrInvalidManagement
	}
	revision.Trigger = command.Trigger
	revision.Capabilities = normalizedStrings(command.Capabilities)
	revision.Steps = cloneSteps(command.Steps)
	if Validate(revision) != nil {
		return Definition{}, ErrInvalidManagement
	}
	now := s.now().UTC()
	versionID := s.newID()
	auditID, eventID, correlationID := s.newID(), s.newID(), s.newID()
	accepted := DefinitionMutation{
		Managed: ManagedDefinition{
			Definition: revision, Name: managed.Name,
			RecordVersion: managed.RecordVersion + 1,
			VersionID:     versionID,
		},
		VersionID:             versionID,
		ExpectedRecordVersion: command.ExpectedRecordVersion,
		Audit: mutation.AuditRecord{
			ID: auditID, OccurredAt: now, MSPID: target.MSPID,
			ClientID: target.ClientID, ActorType: "technician",
			ActorID:        command.Principal.ID,
			Action:         "automation.definition.revised",
			SubjectType:    "automation_definition",
			SubjectID:      revision.ID,
			SubjectVersion: managed.RecordVersion + 1,
			Source:         "api", CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID:       eventID,
			EventType:     "automation.definition.revised",
			SchemaVersion: 1, OccurredAt: now, MSPID: target.MSPID,
			ClientID: target.ClientID, ActorType: "technician",
			ActorID:        command.Principal.ID,
			SubjectType:    "automation_definition",
			SubjectID:      revision.ID,
			SubjectVersion: managed.RecordVersion + 1,
			Source:         "api", CorrelationID: correlationID,
			Data: map[string]any{
				"automation_version": revision.Version,
				"state":              revision.State,
			},
		},
	}
	if err := s.repository.ReviseDefinition(ctx, accepted); err != nil {
		return Definition{}, err
	}
	return revision, nil
}

func (s *ManagementService) PublishDefinition(
	ctx context.Context,
	command PublishDefinitionCommand,
) (Definition, error) {
	target, err := s.authorize(command.Principal)
	if err != nil {
		return Definition{}, err
	}
	if strings.TrimSpace(command.ID) == "" || command.Version < 1 ||
		command.ExpectedRecordVersion < 1 {
		return Definition{}, ErrInvalidManagement
	}
	managed, err := s.repository.FindDefinition(
		ctx, target, command.ID, command.Version,
	)
	if err != nil {
		return Definition{}, err
	}
	if err := object.RequireVersion(
		managed.RecordVersion, command.ExpectedRecordVersion,
	); err != nil {
		return Definition{}, err
	}
	published, err := Publish(managed.Definition)
	if err != nil {
		return Definition{}, ErrInvalidManagement
	}
	now := s.now().UTC()
	auditID, eventID, correlationID := s.newID(), s.newID(), s.newID()
	accepted := DefinitionMutation{
		Managed: ManagedDefinition{
			Definition: published, Name: managed.Name,
			RecordVersion: managed.RecordVersion + 1,
			VersionID:     managed.VersionID,
		},
		VersionID:             managed.VersionID,
		ExpectedRecordVersion: command.ExpectedRecordVersion,
		Audit: mutation.AuditRecord{
			ID: auditID, OccurredAt: now, MSPID: target.MSPID,
			ClientID: target.ClientID, ActorType: "technician",
			ActorID:        command.Principal.ID,
			Action:         "automation.definition.published",
			SubjectType:    "automation_definition",
			SubjectID:      published.ID,
			SubjectVersion: managed.RecordVersion + 1,
			Source:         "api", CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID:       eventID,
			EventType:     "automation.definition.published",
			SchemaVersion: 1, OccurredAt: now, MSPID: target.MSPID,
			ClientID: target.ClientID, ActorType: "technician",
			ActorID:        command.Principal.ID,
			SubjectType:    "automation_definition",
			SubjectID:      published.ID,
			SubjectVersion: managed.RecordVersion + 1,
			Source:         "api", CorrelationID: correlationID,
			Data: map[string]any{
				"automation_version": published.Version,
				"state":              published.State,
			},
		},
	}
	if err := s.repository.PublishDefinition(ctx, accepted); err != nil {
		return Definition{}, err
	}
	return published, nil
}

func (s *ManagementService) CreateExternalConnection(
	ctx context.Context,
	command CreateExternalConnectionCommand,
) (ExternalConnection, error) {
	target, err := s.authorize(command.Principal)
	if err != nil {
		return ExternalConnection{}, err
	}
	name := strings.TrimSpace(command.Name)
	endpoint := strings.TrimSpace(command.Endpoint)
	secretRef := strings.TrimSpace(command.SigningSecretRef)
	if name == "" || webhooks.ValidateDestination(endpoint) != nil ||
		!ValidExternalSecretRef(secretRef) {
		return ExternalConnection{}, ErrInvalidManagement
	}
	now := s.now().UTC()
	connectionID := s.newID()
	auditID, eventID, correlationID := s.newID(), s.newID(), s.newID()
	connection := ExternalConnection{
		ID: connectionID, MSPID: target.MSPID, ClientID: target.ClientID,
		Name: name, Endpoint: endpoint, SigningSecretRef: secretRef,
	}
	accepted := ConnectionMutation{
		Connection: connection,
		Audit: mutation.AuditRecord{
			ID: auditID, OccurredAt: now, MSPID: target.MSPID,
			ClientID: target.ClientID, ActorType: "technician",
			ActorID:     command.Principal.ID,
			Action:      "automation.connection.created",
			SubjectType: "connection", SubjectID: connectionID,
			SubjectVersion: 1, Source: "api",
			CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID:       eventID,
			EventType:     "automation.connection.created",
			SchemaVersion: 1, OccurredAt: now, MSPID: target.MSPID,
			ClientID: target.ClientID, ActorType: "technician",
			ActorID:     command.Principal.ID,
			SubjectType: "connection", SubjectID: connectionID,
			SubjectVersion: 1, Source: "api",
			CorrelationID: correlationID,
			Data:          map[string]any{"name": name, "kind": "external_http"},
		},
	}
	if err := s.repository.CreateConnection(ctx, accepted); err != nil {
		return ExternalConnection{}, err
	}
	return connection, nil
}

func (s *ManagementService) authorize(
	principal authorization.Principal,
) (scope.Target, error) {
	if s == nil || s.repository == nil || s.now == nil || s.newID == nil {
		return scope.Target{}, ErrInvalidManagement
	}
	target := scope.Target{
		MSPID: principal.Scope.MSPID, ClientID: principal.Scope.ClientID,
	}
	if target.MSPID == "" || target.ClientID == "" {
		return scope.Target{}, ErrInvalidManagement
	}
	if err := authorization.Authorize(
		principal, "automation.manage", target,
	); err != nil {
		return scope.Target{}, err
	}
	return target, nil
}
