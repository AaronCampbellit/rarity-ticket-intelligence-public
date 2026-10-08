package routing

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

type RuleSet struct {
	ID          string    `json:"id"`
	MSPID       string    `json:"msp_id"`
	Version     int64     `json:"version"`
	Rules       []Rule    `json:"rules"`
	PublishedAt time.Time `json:"published_at"`
	PublishedBy string    `json:"published_by"`
}

type PublishCommand struct {
	Principal       authorization.Principal
	ExpectedVersion int64
	Rules           []Rule
	ActorID         string
	Source          string
}

type PublishMutation struct {
	RuleSet         RuleSet
	ExpectedVersion int64
	Created         bool
	Audit           mutation.AuditRecord
	Event           mutation.EventRecord
}

type ManagementRepository interface {
	LoadCurrent(context.Context, string) (RuleSet, error)
	ValidateDestinations(context.Context, scope.Target, []Rule) error
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

func (s *ManagementService) Current(
	ctx context.Context,
	principal authorization.Principal,
) (RuleSet, error) {
	target := scope.Target{MSPID: principal.Scope.MSPID}
	if target.MSPID == "" {
		return RuleSet{}, ErrInvalidRules
	}
	if err := authorization.Authorize(principal, "routing.manage", target); err != nil {
		return RuleSet{}, err
	}
	return s.repository.LoadCurrent(ctx, target.MSPID)
}

func (s *ManagementService) Publish(
	ctx context.Context,
	command PublishCommand,
) (RuleSet, error) {
	target := scope.Target{MSPID: command.Principal.Scope.MSPID}
	if target.MSPID == "" || command.ExpectedVersion < 0 ||
		command.ActorID == "" || command.Source == "" {
		return RuleSet{}, ErrInvalidRules
	}
	if err := authorization.Authorize(command.Principal, "routing.manage", target); err != nil {
		return RuleSet{}, err
	}
	current, err := s.repository.LoadCurrent(ctx, target.MSPID)
	created := errors.Is(err, scope.ErrNotFound)
	if err != nil && !created {
		return RuleSet{}, err
	}
	if created {
		if command.ExpectedVersion != 0 {
			return RuleSet{}, scope.ErrNotFound
		}
		current = RuleSet{ID: s.newID(), MSPID: target.MSPID}
	} else if err := object.RequireVersion(current.Version, command.ExpectedVersion); err != nil {
		return RuleSet{}, err
	}

	rules := append([]Rule(nil), command.Rules...)
	for index := range rules {
		rules[index].ClientID = strings.TrimSpace(rules[index].ClientID)
		rules[index].RecordType = strings.TrimSpace(rules[index].RecordType)
		rules[index].Priority = strings.TrimSpace(rules[index].Priority)
		rules[index].QueueID = strings.TrimSpace(rules[index].QueueID)
		if rules[index].ID == "" {
			rules[index].ID = s.newID()
		}
	}
	if _, err := NewEngine(rules); err != nil {
		return RuleSet{}, err
	}
	if err := s.repository.ValidateDestinations(ctx, target, rules); err != nil {
		return RuleSet{}, err
	}
	now := s.now().UTC()
	published := RuleSet{
		ID: current.ID, MSPID: target.MSPID, Version: current.Version + 1,
		Rules: rules, PublishedAt: now, PublishedBy: command.ActorID,
	}
	auditID, eventID, correlationID := s.newID(), s.newID(), s.newID()
	accepted := PublishMutation{
		RuleSet: published, ExpectedVersion: command.ExpectedVersion, Created: created,
		Audit: mutation.AuditRecord{
			ID: auditID, OccurredAt: now, MSPID: target.MSPID,
			ActorType: "technician", ActorID: command.ActorID,
			Action: "routing.rules.published", SubjectType: "routing_rule_set",
			SubjectID: published.ID, SubjectVersion: published.Version,
			Source: command.Source, CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: eventID, EventType: "routing.rules.published", SchemaVersion: 1,
			OccurredAt: now, MSPID: target.MSPID,
			ActorType: "technician", ActorID: command.ActorID,
			SubjectType: "routing_rule_set", SubjectID: published.ID,
			SubjectVersion: published.Version, Source: command.Source,
			CorrelationID: correlationID,
		},
	}
	if err := s.repository.PublishAtomic(ctx, accepted); err != nil {
		return RuleSet{}, err
	}
	return published, nil
}
