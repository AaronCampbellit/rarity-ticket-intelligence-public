package sla

import (
	"context"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type PublishPolicyCommand struct {
	Principal               authorization.Principal
	Target                  scope.Target
	PolicyID                string
	ExpectedVersion         int64
	Key                     string
	Name                    string
	CalendarID              string
	Conditions              PolicyConditions
	ResponseTargetSeconds   int
	ResolutionTargetSeconds int
	WarningPercent          int
	PauseStates             []string
	Enabled                 bool
	Priority                int
	StableOrder             int
	Fallback                bool
	ActorID                 string
	Source                  string
}

type PolicyPublishMutation struct {
	Policy          PublishedPolicy
	ExpectedVersion int64
	Created         bool
	Audit           mutation.AuditRecord
	Event           mutation.EventRecord
}

type PolicyManagementRepository interface {
	ListPolicies(context.Context, scope.Target) ([]PublishedPolicy, error)
	ResolveCalendar(context.Context, scope.Target, string) (PublishedCalendar, error)
	PublishPolicyAtomic(context.Context, PolicyPublishMutation) error
}

type PolicyManagementService struct {
	repository PolicyManagementRepository
	now        func() time.Time
	newID      func() string
}

func NewPolicyManagementService(
	repository PolicyManagementRepository,
	now func() time.Time,
	newID func() string,
) *PolicyManagementService {
	return &PolicyManagementService{repository: repository, now: now, newID: newID}
}

func (s *PolicyManagementService) ListPolicies(
	ctx context.Context,
	principal authorization.Principal,
	target scope.Target,
) ([]PublishedPolicy, error) {
	if target.MSPID == "" {
		target = scope.Target{
			MSPID: principal.Scope.MSPID, ClientID: principal.Scope.ClientID,
		}
	}
	if err := authorization.Authorize(principal, "sla.manage", target); err != nil {
		return nil, err
	}
	return s.repository.ListPolicies(ctx, target)
}

func (s *PolicyManagementService) PublishPolicy(
	ctx context.Context,
	command PublishPolicyCommand,
) (PublishedPolicy, error) {
	target := command.Target
	if target.MSPID == "" {
		target = scope.Target{
			MSPID: command.Principal.Scope.MSPID, ClientID: command.Principal.Scope.ClientID,
		}
	}
	if target.MSPID == "" || command.ExpectedVersion < 0 ||
		strings.TrimSpace(command.Key) == "" ||
		strings.TrimSpace(command.Name) == "" ||
		command.CalendarID == "" ||
		command.ActorID == "" || command.Source == "" {
		return PublishedPolicy{}, ErrInvalidPolicy
	}
	if err := authorization.Authorize(command.Principal, "sla.manage", target); err != nil {
		return PublishedPolicy{}, err
	}
	conditions := command.Conditions
	conditions.ClientID = strings.TrimSpace(conditions.ClientID)
	conditions.RecordType = strings.TrimSpace(conditions.RecordType)
	conditions.Priority = strings.TrimSpace(conditions.Priority)
	conditions.QueueID = strings.TrimSpace(conditions.QueueID)
	conditions.ServiceID = strings.TrimSpace(conditions.ServiceID)
	conditions.ContractID = strings.TrimSpace(conditions.ContractID)
	if conditions.ClientID != "" && conditions.ClientID != target.ClientID {
		return PublishedPolicy{}, scope.ErrNotFound
	}
	current, err := s.repository.ListPolicies(ctx, target)
	if err != nil {
		return PublishedPolicy{}, err
	}
	calendar, err := s.repository.ResolveCalendar(ctx, target, command.CalendarID)
	if err != nil {
		return PublishedPolicy{}, err
	}
	created := command.ExpectedVersion == 0
	policyID := command.PolicyID
	version := int64(1)
	if created {
		if policyID != "" {
			return PublishedPolicy{}, ErrInvalidPolicy
		}
		policyID = s.newID()
	} else {
		found := false
		for _, policy := range current {
			if policy.ID != policyID {
				continue
			}
			if policy.ClientID != target.ClientID {
				return PublishedPolicy{}, scope.ErrNotFound
			}
			if err := object.RequireVersion(policy.Version, command.ExpectedVersion); err != nil {
				return PublishedPolicy{}, err
			}
			version = policy.Version + 1
			found = true
			break
		}
		if !found {
			return PublishedPolicy{}, scope.ErrNotFound
		}
	}
	pauseStates := make([]string, len(command.PauseStates))
	for index, state := range command.PauseStates {
		pauseStates[index] = strings.TrimSpace(state)
	}
	now := s.now().UTC()
	published := PublishedPolicy{
		ID: policyID, MSPID: target.MSPID, ClientID: target.ClientID,
		Key: strings.TrimSpace(command.Key), Name: strings.TrimSpace(command.Name),
		Version: version, Calendar: calendar, Conditions: conditions,
		ResponseTargetSeconds:   command.ResponseTargetSeconds,
		ResolutionTargetSeconds: command.ResolutionTargetSeconds,
		WarningPercent:          command.WarningPercent, PauseStates: pauseStates,
		Enabled: command.Enabled, Priority: command.Priority,
		StableOrder: command.StableOrder, Fallback: command.Fallback,
		PublishedAt: now, PublishedBy: command.ActorID,
	}
	candidates := make([]PublishedPolicy, 0, len(current)+1)
	replaced := false
	for _, candidate := range current {
		if candidate.ID == published.ID {
			candidates = append(candidates, published)
			replaced = true
		} else {
			candidates = append(candidates, candidate)
		}
	}
	if !replaced {
		candidates = append(candidates, published)
	}
	if _, err := NewPolicyEngine(candidates); err != nil {
		return PublishedPolicy{}, err
	}
	auditID, eventID, correlationID := s.newID(), s.newID(), s.newID()
	accepted := PolicyPublishMutation{
		Policy: published, ExpectedVersion: command.ExpectedVersion, Created: created,
		Audit: mutation.AuditRecord{
			ID: auditID, OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID,
			ActorType: "technician", ActorID: command.ActorID,
			Action: "sla_policy.published", SubjectType: "sla_policy",
			SubjectID: published.ID, SubjectVersion: published.Version,
			Source: command.Source, CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: eventID, EventType: "sla_policy.published", SchemaVersion: 1,
			OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID,
			ActorType: "technician", ActorID: command.ActorID,
			SubjectType: "sla_policy", SubjectID: published.ID,
			SubjectVersion: published.Version, Source: command.Source,
			CorrelationID: correlationID,
		},
	}
	if err := s.repository.PublishPolicyAtomic(ctx, accepted); err != nil {
		return PublishedPolicy{}, err
	}
	return published, nil
}
