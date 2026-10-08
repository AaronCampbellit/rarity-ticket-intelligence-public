// Package links creates explicit same-client object relationships.
package links

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

var ErrInvalid = errors.New("invalid relationship")

type registryKey struct {
	SourceType string
	TargetType string
	LinkType   string
}

var registry = map[registryKey]struct{}{
	{"work_record", "asset", "affected_asset"}:              {},
	{"work_record", "service", "affected_service"}:          {},
	{"work_record", "contract", "covered_by_contract"}:      {},
	{"asset", "contract", "covered_by_contract"}:            {},
	{"service", "contract", "covered_by_contract"}:          {},
	{"work_record", "work_record", "parent_of"}:             {},
	{"work_record", "work_record", "related_to"}:            {},
	{"work_record", "work_record", "caused_by_problem"}:     {},
	{"work_record", "work_record", "implemented_by_change"}: {},
	{"asset", "asset", "depends_on"}:                        {},
	{"asset", "service", "depends_on"}:                      {},
	{"service", "asset", "depends_on"}:                      {},
	{"service", "service", "depends_on"}:                    {},
	{"service", "asset", "runs_on"}:                         {},
	{"asset", "asset", "runs_on"}:                           {},
	{"asset", "asset", "connects_through"}:                  {},
	{"asset", "service", "connects_through"}:                {},
	{"service", "asset", "connects_through"}:                {},
	{"service", "service", "connects_through"}:              {},
	{"asset", "service", "authenticates_through"}:           {},
	{"service", "asset", "authenticates_through"}:           {},
	{"asset", "asset", "backed_up_by"}:                      {},
	{"asset", "service", "backed_up_by"}:                    {},
	{"service", "asset", "backed_up_by"}:                    {},
	{"service", "service", "backed_up_by"}:                  {},
}

type Ref struct {
	Type     string
	ID       string
	MSPID    string
	ClientID string
}

type Link struct {
	ID        string
	MSPID     string
	ClientID  string
	Source    Ref
	Target    Ref
	LinkType  string
	Version   int64
	CreatedAt time.Time
	CreatedBy string
}

type CreateCommand struct {
	Principal     authorization.Principal
	Source        Ref
	Target        Ref
	LinkType      string
	ActorID       string
	RequestSource string
}

type CreateMutation struct {
	Link  Link
	Audit mutation.AuditRecord
	Event mutation.EventRecord
}

type Repository interface {
	CreateAtomic(context.Context, CreateMutation) error
}

type Service struct {
	repository Repository
	now        func() time.Time
	newID      func() string
}

func NewService(repository Repository, now func() time.Time, newID func() string) *Service {
	return &Service{repository: repository, now: now, newID: newID}
}

func (s *Service) Create(ctx context.Context, command CreateCommand) (Link, error) {
	command.Source.Type = strings.TrimSpace(command.Source.Type)
	command.Target.Type = strings.TrimSpace(command.Target.Type)
	command.LinkType = strings.TrimSpace(command.LinkType)
	if command.Source.Type == "" ||
		command.Source.ID == "" ||
		command.Target.Type == "" ||
		command.Target.ID == "" ||
		command.LinkType == "" ||
		command.ActorID == "" ||
		command.RequestSource == "" ||
		(command.Source.Type == command.Target.Type && command.Source.ID == command.Target.ID) {
		return Link{}, ErrInvalid
	}
	if _, ok := registry[registryKey{
		SourceType: command.Source.Type,
		TargetType: command.Target.Type,
		LinkType:   command.LinkType,
	}]; !ok {
		return Link{}, ErrInvalid
	}
	if command.Source.MSPID != command.Target.MSPID ||
		command.Source.ClientID == "" ||
		command.Source.ClientID != command.Target.ClientID {
		return Link{}, scope.ErrNotFound
	}
	target := scope.Target{MSPID: command.Source.MSPID, ClientID: command.Source.ClientID}
	if err := authorization.Authorize(command.Principal, "relationship.create", target); err != nil {
		return Link{}, err
	}
	if command.LinkType == "related_to" &&
		command.Source.Type == command.Target.Type &&
		command.Source.ID > command.Target.ID {
		command.Source, command.Target = command.Target, command.Source
	}
	now := s.now().UTC()
	linkID := s.newID()
	auditID := s.newID()
	eventID := s.newID()
	correlationID := s.newID()
	link := Link{
		ID: linkID, MSPID: target.MSPID, ClientID: target.ClientID,
		Source: command.Source, Target: command.Target,
		LinkType: command.LinkType, Version: 1,
		CreatedAt: now, CreatedBy: command.ActorID,
	}
	accepted := CreateMutation{
		Link: link,
		Audit: mutation.AuditRecord{
			ID: auditID, OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID,
			ActorType: "technician", ActorID: command.ActorID,
			Action: "relationship.created", SubjectType: "relationship",
			SubjectID: link.ID, SubjectVersion: link.Version,
			Source: command.RequestSource, CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: eventID, EventType: "relationship.created", SchemaVersion: 1,
			OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID,
			ActorType: "technician", ActorID: command.ActorID,
			SubjectType: "relationship", SubjectID: link.ID, SubjectVersion: link.Version,
			Source: command.RequestSource, CorrelationID: correlationID,
		},
	}
	if err := s.repository.CreateAtomic(ctx, accepted); err != nil {
		return Link{}, err
	}
	return link, nil
}
