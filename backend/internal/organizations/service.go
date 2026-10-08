// Package organizations owns MSP and client organization application
// behavior.
package organizations

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/clientidentity"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

var (
	ErrForbidden              = errors.New("forbidden")
	ErrInvalid                = errors.New("invalid client organization")
	ErrClientIdentityConflict = clientidentity.ErrConflict
)

type Actor struct {
	Type   string
	ID     string
	Source string
}

type Client struct {
	object.Envelope
	Name string
}

type CreateClientCommand struct {
	Principal     authorization.Principal
	Actor         Actor
	ClientID      string
	CorrelationID string
	DisplayID     string
	Name          string
}

type CreateClientMutation struct {
	Client Client
	Audit  mutation.AuditRecord
	Event  mutation.EventRecord
}

type Repository interface {
	// CreateClientAtomic commits all three records in one database
	// transaction or commits none of them.
	CreateClientAtomic(context.Context, CreateClientMutation) error
	ClientIdentityConflict(
		context.Context,
		scope.Target,
		string,
		string,
	) (bool, error)
}

type Service struct {
	repository Repository
	now        func() time.Time
	newID      func() string
}

func NewService(repository Repository, now func() time.Time, newID func() string) *Service {
	return &Service{repository: repository, now: now, newID: newID}
}

func (s *Service) HasClientIdentityConflict(
	ctx context.Context,
	principal authorization.Principal,
	name string,
	displayID string,
) (bool, error) {
	normalizedName := clientidentity.Normalize(name)
	normalizedDisplayID := clientidentity.Normalize(displayID)
	if strings.TrimSpace(principal.Scope.MSPID) == "" ||
		normalizedName == "" ||
		normalizedDisplayID == "" ||
		s == nil ||
		s.repository == nil {
		return false, ErrInvalid
	}
	if principal.Scope.ClientID != "" {
		return false, ErrForbidden
	}
	target := scope.Target{MSPID: principal.Scope.MSPID}
	if err := authorization.Authorize(
		principal,
		"client.create",
		target,
	); err != nil {
		return false, err
	}
	return s.repository.ClientIdentityConflict(
		ctx, target, normalizedName, normalizedDisplayID,
	)
}

func (s *Service) CreateClient(ctx context.Context, command CreateClientCommand) (Client, error) {
	clientID := strings.TrimSpace(command.ClientID)
	correlationID := strings.TrimSpace(command.CorrelationID)
	if command.Principal.Scope.MSPID == "" ||
		command.Actor.Type == "" ||
		command.Actor.ID == "" ||
		command.Actor.Source == "" ||
		strings.TrimSpace(command.DisplayID) == "" ||
		strings.TrimSpace(command.Name) == "" ||
		(clientID != "" && uuid.Validate(clientID) != nil) ||
		(correlationID != "" && uuid.Validate(correlationID) != nil) {
		return Client{}, ErrInvalid
	}
	if command.Principal.Scope.ClientID != "" {
		return Client{}, ErrForbidden
	}
	if err := authorization.Authorize(
		command.Principal,
		"client.create",
		scope.Target{MSPID: command.Principal.Scope.MSPID},
	); err != nil {
		return Client{}, err
	}

	now := s.now().UTC()
	if clientID == "" {
		clientID = s.newID()
	}
	auditID := s.newID()
	eventID := s.newID()
	if correlationID == "" {
		correlationID = s.newID()
	}
	client := Client{
		Envelope: object.Envelope{
			ID:             clientID,
			ObjectType:     "client_organization",
			MSPID:          command.Principal.Scope.MSPID,
			ClientID:       clientID,
			DisplayID:      strings.TrimSpace(command.DisplayID),
			LifecycleState: "active",
			Version:        1,
			CreatedAt:      now,
			CreatedBy:      command.Actor.ID,
			UpdatedAt:      now,
			UpdatedBy:      command.Actor.ID,
		},
		Name: strings.TrimSpace(command.Name),
	}
	accepted := CreateClientMutation{
		Client: client,
		Audit: mutation.AuditRecord{
			ID:             auditID,
			OccurredAt:     now,
			MSPID:          client.MSPID,
			ClientID:       client.ID,
			ActorType:      command.Actor.Type,
			ActorID:        command.Actor.ID,
			Action:         "client.created",
			SubjectType:    client.ObjectType,
			SubjectID:      client.ID,
			SubjectVersion: client.Version,
			Source:         command.Actor.Source,
			CorrelationID:  correlationID,
		},
		Event: mutation.EventRecord{
			EventID:        eventID,
			EventType:      "client.created",
			SchemaVersion:  1,
			OccurredAt:     now,
			MSPID:          client.MSPID,
			ClientID:       client.ID,
			ActorType:      command.Actor.Type,
			ActorID:        command.Actor.ID,
			SubjectType:    client.ObjectType,
			SubjectID:      client.ID,
			SubjectVersion: client.Version,
			Source:         command.Actor.Source,
			CorrelationID:  correlationID,
		},
	}
	if err := s.repository.CreateClientAtomic(ctx, accepted); err != nil {
		return Client{}, err
	}
	return client, nil
}
