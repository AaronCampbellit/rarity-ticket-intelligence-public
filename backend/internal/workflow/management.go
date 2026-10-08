package workflow

import (
	"context"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type PublishCommand struct {
	Principal       authorization.Principal
	Target          scope.Target
	WorkflowID      string
	ExpectedVersion int64
	Key             string
	Name            string
	Enabled         bool
	Priority        int
	StableOrder     int
	Fallback        bool
	EffectiveFrom   time.Time
	EffectiveTo     *time.Time
	Conditions      Conditions
	Definition      Definition
	ActorID         string
	Source          string
}

type PublishMutation struct {
	Workflow        Published
	ExpectedVersion int64
	Created         bool
	PublishedAt     time.Time
	PublishedBy     string
	Audit           mutation.AuditRecord
	Event           mutation.EventRecord
}

type ManagementRepository interface {
	ListPublished(context.Context, scope.Target) ([]Published, error)
	PublishAtomic(context.Context, PublishMutation) error
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
	return &ManagementService{repository: repository, now: now, newID: newID}
}

func (s *ManagementService) List(
	ctx context.Context,
	principal authorization.Principal,
	target scope.Target,
) ([]Published, error) {
	if target.MSPID == "" {
		target = scope.Target{
			MSPID: principal.Scope.MSPID, ClientID: principal.Scope.ClientID,
		}
	}
	if err := authorization.Authorize(principal, "workflow.publish", target); err != nil {
		return nil, err
	}
	return s.repository.ListPublished(ctx, target)
}

func (s *ManagementService) Publish(
	ctx context.Context,
	command PublishCommand,
) (Published, error) {
	target := command.Target
	if target.MSPID == "" {
		target = scope.Target{
			MSPID:    command.Principal.Scope.MSPID,
			ClientID: command.Principal.Scope.ClientID,
		}
	}
	if target.MSPID == "" ||
		strings.TrimSpace(command.Key) == "" ||
		strings.TrimSpace(command.Name) == "" ||
		command.ExpectedVersion < 0 ||
		command.StableOrder < 0 ||
		command.ActorID == "" ||
		command.Source == "" ||
		command.Definition.Validate() != nil ||
		command.EffectiveTo != nil &&
			!command.EffectiveFrom.IsZero() &&
			!command.EffectiveTo.After(command.EffectiveFrom) {
		return Published{}, ErrInvalidConfiguration
	}
	if err := authorization.Authorize(command.Principal, "workflow.publish", target); err != nil {
		return Published{}, err
	}
	if command.Conditions.ClientID != "" &&
		target.ClientID != "" &&
		command.Conditions.ClientID != target.ClientID {
		return Published{}, scope.ErrNotFound
	}
	current, err := s.repository.ListPublished(ctx, target)
	if err != nil {
		return Published{}, err
	}
	created := command.ExpectedVersion == 0
	workflowID := command.WorkflowID
	version := int64(1)
	if created {
		if workflowID != "" {
			return Published{}, ErrInvalidConfiguration
		}
		workflowID = s.newID()
	} else {
		found := false
		for _, published := range current {
			if published.ID != workflowID {
				continue
			}
			if err := object.RequireVersion(
				published.Version, command.ExpectedVersion,
			); err != nil {
				return Published{}, err
			}
			version = published.Version + 1
			found = true
			break
		}
		if !found {
			return Published{}, scope.ErrNotFound
		}
	}
	published := Published{
		ID: workflowID, MSPID: target.MSPID, ClientID: target.ClientID,
		Key: strings.TrimSpace(command.Key), Name: strings.TrimSpace(command.Name),
		Version: version, Priority: command.Priority,
		StableOrder: command.StableOrder, Fallback: command.Fallback,
		Enabled: command.Enabled, EffectiveFrom: command.EffectiveFrom,
		EffectiveTo: command.EffectiveTo, Conditions: command.Conditions,
		Definition: command.Definition,
	}
	candidates := make([]Published, 0, len(current)+1)
	replaced := false
	for _, candidate := range current {
		if candidate.ID == published.ID {
			candidates = append(candidates, published)
			replaced = true
			continue
		}
		candidates = append(candidates, candidate)
	}
	if !replaced {
		candidates = append(candidates, published)
	}
	if _, err := NewEngine(candidates); err != nil {
		return Published{}, err
	}
	now := s.now().UTC()
	auditID, eventID, correlationID := s.newID(), s.newID(), s.newID()
	accepted := PublishMutation{
		Workflow: published, ExpectedVersion: command.ExpectedVersion,
		Created: created, PublishedAt: now, PublishedBy: command.ActorID,
		Audit: mutation.AuditRecord{
			ID: auditID, OccurredAt: now, MSPID: target.MSPID,
			ClientID: target.ClientID, ActorType: "technician",
			ActorID: command.ActorID, Action: "workflow.published",
			SubjectType: "workflow", SubjectID: published.ID,
			SubjectVersion: published.Version, Source: command.Source,
			CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: eventID, EventType: "workflow.published", SchemaVersion: 1,
			OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID,
			ActorType: "technician", ActorID: command.ActorID,
			SubjectType: "workflow", SubjectID: published.ID,
			SubjectVersion: published.Version, Source: command.Source,
			CorrelationID: correlationID,
		},
	}
	if err := s.repository.PublishAtomic(ctx, accepted); err != nil {
		return Published{}, err
	}
	return published, nil
}
