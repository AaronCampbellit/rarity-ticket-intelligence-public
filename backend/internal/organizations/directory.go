package organizations

import (
	"context"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type Department struct {
	ID      string `json:"id"`
	MSPID   string `json:"msp_id"`
	Key     string `json:"key"`
	Name    string `json:"name"`
	Version int64  `json:"version"`
}

type Team struct {
	ID           string   `json:"id"`
	MSPID        string   `json:"msp_id"`
	DepartmentID string   `json:"department_id,omitempty"`
	Key          string   `json:"key"`
	Name         string   `json:"name"`
	Version      int64    `json:"version"`
	MemberIDs    []string `json:"member_ids"`
}

type Technician struct {
	ID          string `json:"id"`
	MSPID       string `json:"msp_id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Version     int64  `json:"version"`
}

type Queue struct {
	ID           string `json:"id"`
	MSPID        string `json:"msp_id"`
	ClientID     string `json:"client_id,omitempty"`
	DepartmentID string `json:"department_id,omitempty"`
	TeamID       string `json:"team_id,omitempty"`
	Key          string `json:"key"`
	Name         string `json:"name"`
	Version      int64  `json:"version"`
}

type Directory struct {
	Clients     []Client     `json:"clients"`
	Departments []Department `json:"departments"`
	Teams       []Team       `json:"teams"`
	Queues      []Queue      `json:"queues"`
	Technicians []Technician `json:"technicians"`
}

type DirectoryMutation struct {
	Department Department
	Team       Team
	Queue      Queue
	Audit      mutation.AuditRecord
	Event      mutation.EventRecord
}

type DirectoryRepository interface {
	CreateDepartmentAtomic(context.Context, DirectoryMutation) error
	CreateTeamAtomic(context.Context, DirectoryMutation) error
	CreateQueueAtomic(context.Context, DirectoryMutation) error
	ListDirectory(context.Context, scope.Target) (Directory, error)
}

type CreateDepartmentCommand struct {
	Principal authorization.Principal
	Key       string
	Name      string
	Reason    string
	Source    string
}

type CreateTeamCommand struct {
	Principal    authorization.Principal
	DepartmentID string
	Key          string
	Name         string
	Reason       string
	Source       string
}

type CreateQueueCommand struct {
	Principal    authorization.Principal
	ClientID     string
	DepartmentID string
	TeamID       string
	Key          string
	Name         string
	Reason       string
	Source       string
}

type ListDirectoryCommand struct {
	Principal authorization.Principal
	Target    scope.Target
}

type DirectoryService struct {
	repository           DirectoryRepository
	membershipRepository TeamMembershipRepository
	now                  func() time.Time
	newID                func() string
}

func NewDirectoryService(
	repository DirectoryRepository,
	now func() time.Time,
	newID func() string,
) *DirectoryService {
	membershipRepository, _ := repository.(TeamMembershipRepository)
	return &DirectoryService{
		repository: repository, membershipRepository: membershipRepository,
		now: now, newID: newID,
	}
}

func (s *DirectoryService) CreateDepartment(
	ctx context.Context,
	command CreateDepartmentCommand,
) (Department, error) {
	if err := authorizeDirectoryMutation(command.Principal); err != nil {
		return Department{}, err
	}
	key, name, reason, source, ok := directoryFields(
		command.Key, command.Name, command.Reason, command.Source,
	)
	if !ok || s.newID == nil {
		return Department{}, ErrInvalid
	}
	department := Department{
		ID: s.newID(), MSPID: command.Principal.Scope.MSPID,
		Key: key, Name: name, Version: 1,
	}
	accepted := s.directoryMutation(
		command.Principal, "", "department", department.ID,
		"department.created", reason, source,
	)
	accepted.Department = department
	if err := s.repository.CreateDepartmentAtomic(ctx, accepted); err != nil {
		return Department{}, err
	}
	return department, nil
}

func (s *DirectoryService) CreateTeam(
	ctx context.Context,
	command CreateTeamCommand,
) (Team, error) {
	if err := authorizeDirectoryMutation(command.Principal); err != nil {
		return Team{}, err
	}
	key, name, reason, source, ok := directoryFields(
		command.Key, command.Name, command.Reason, command.Source,
	)
	if !ok || strings.TrimSpace(command.DepartmentID) == "" || s.newID == nil {
		return Team{}, ErrInvalid
	}
	team := Team{
		ID: s.newID(), MSPID: command.Principal.Scope.MSPID,
		DepartmentID: strings.TrimSpace(command.DepartmentID),
		Key:          key, Name: name, Version: 1,
	}
	accepted := s.directoryMutation(
		command.Principal, "", "team", team.ID, "team.created", reason, source,
	)
	accepted.Team = team
	if err := s.repository.CreateTeamAtomic(ctx, accepted); err != nil {
		return Team{}, err
	}
	return team, nil
}

func (s *DirectoryService) CreateQueue(
	ctx context.Context,
	command CreateQueueCommand,
) (Queue, error) {
	if err := authorizeDirectoryMutation(command.Principal); err != nil {
		return Queue{}, err
	}
	key, name, reason, source, ok := directoryFields(
		command.Key, command.Name, command.Reason, command.Source,
	)
	if !ok || s.newID == nil {
		return Queue{}, ErrInvalid
	}
	clientID := strings.TrimSpace(command.ClientID)
	target := scope.Target{
		MSPID: command.Principal.Scope.MSPID, ClientID: clientID,
	}
	if err := authorization.Authorize(
		command.Principal, "organization.manage", target,
	); err != nil {
		return Queue{}, err
	}
	queue := Queue{
		ID: s.newID(), MSPID: target.MSPID, ClientID: clientID,
		DepartmentID: strings.TrimSpace(command.DepartmentID),
		TeamID:       strings.TrimSpace(command.TeamID),
		Key:          key, Name: name, Version: 1,
	}
	accepted := s.directoryMutation(
		command.Principal, clientID, "queue", queue.ID,
		"queue.created", reason, source,
	)
	accepted.Queue = queue
	if err := s.repository.CreateQueueAtomic(ctx, accepted); err != nil {
		return Queue{}, err
	}
	return queue, nil
}

func (s *DirectoryService) List(
	ctx context.Context,
	command ListDirectoryCommand,
) (Directory, error) {
	target := command.Target
	if target.MSPID == "" {
		target = scope.Target{
			MSPID:    command.Principal.Scope.MSPID,
			ClientID: command.Principal.Scope.ClientID,
		}
	}
	capability := "organization.read"
	if target.ClientID == "" && command.Principal.Capabilities.Has("organization.manage") {
		capability = "organization.manage"
	}
	if err := authorization.Authorize(
		command.Principal, capability, target,
	); err != nil {
		return Directory{}, err
	}
	return s.repository.ListDirectory(ctx, target)
}

// AuthorizeExecutionTarget resolves a trusted target against the active Client
// directory. The caller performs capability authorization separately; this
// boundary adds current lifecycle validation without requiring directory-read
// permission as a second, unrelated capability.
func (s *DirectoryService) AuthorizeExecutionTarget(
	ctx context.Context,
	principal authorization.Principal,
	target scope.Target,
) error {
	target.MSPID = strings.TrimSpace(target.MSPID)
	target.ClientID = strings.TrimSpace(target.ClientID)
	if s == nil || s.repository == nil {
		return ErrInvalid
	}
	if err := scope.Authorize(principal.Scope, target); err != nil {
		return err
	}
	if target.ClientID == "" {
		return nil
	}
	directory, err := s.repository.ListDirectory(ctx, target)
	if err != nil {
		return err
	}
	for _, client := range directory.Clients {
		if client.ID == target.ClientID &&
			client.MSPID == target.MSPID &&
			client.ClientID == target.ClientID &&
			client.LifecycleState == "active" {
			return nil
		}
	}
	return scope.ErrNotFound
}

// ResolveActiveClient returns one exact active Client without exposing a
// general directory read. The requested capability is checked before the
// internal lookup and again against the resolved Client target.
func (s *DirectoryService) ResolveActiveClient(
	ctx context.Context,
	principal authorization.Principal,
	capability string,
	reference string,
) (Client, error) {
	lookupTarget := scope.Target{
		MSPID: principal.Scope.MSPID, ClientID: principal.Scope.ClientID,
	}
	if s == nil || s.repository == nil || strings.TrimSpace(capability) == "" ||
		strings.TrimSpace(reference) == "" {
		return Client{}, ErrInvalid
	}
	if err := authorization.Authorize(principal, capability, lookupTarget); err != nil {
		return Client{}, err
	}
	directory, err := s.repository.ListDirectory(ctx, lookupTarget)
	if err != nil {
		return Client{}, err
	}
	active := make([]Client, 0, len(directory.Clients))
	for _, client := range directory.Clients {
		if client.ID != "" && client.MSPID == lookupTarget.MSPID &&
			client.ClientID == client.ID && client.LifecycleState == "active" {
			active = append(active, client)
		}
	}
	client, err := ResolveClientReference(reference, active)
	if err != nil {
		return Client{}, err
	}
	if err := authorization.Authorize(principal, capability, scope.Target{
		MSPID: client.MSPID, ClientID: client.ID,
	}); err != nil {
		return Client{}, err
	}
	return client, nil
}

func authorizeDirectoryMutation(principal authorization.Principal) error {
	if principal.Scope.MSPID == "" {
		return ErrInvalid
	}
	if principal.Scope.ClientID != "" {
		return ErrForbidden
	}
	return authorization.Authorize(
		principal,
		"organization.manage",
		scope.Target{MSPID: principal.Scope.MSPID},
	)
}

func directoryFields(
	rawKey, rawName, rawReason, rawSource string,
) (key, name, reason, source string, ok bool) {
	key, name = strings.TrimSpace(rawKey), strings.TrimSpace(rawName)
	reason, source = strings.TrimSpace(rawReason), strings.TrimSpace(rawSource)
	ok = key != "" && name != "" && reason != "" && source != ""
	return
}

func (s *DirectoryService) directoryMutation(
	principal authorization.Principal,
	clientID, subjectType, subjectID, action, reason, source string,
) DirectoryMutation {
	now := s.now().UTC()
	auditID, eventID := s.newID(), s.newID()
	correlationID := s.newID()
	return DirectoryMutation{
		Audit: mutation.AuditRecord{
			ID: auditID, OccurredAt: now, MSPID: principal.Scope.MSPID,
			ClientID: clientID, ActorType: "technician", ActorID: principal.ID,
			Action: action, SubjectType: subjectType, SubjectID: subjectID,
			SubjectVersion: 1, Source: source, Reason: reason,
			CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: eventID, EventType: action, SchemaVersion: 1,
			OccurredAt: now, MSPID: principal.Scope.MSPID, ClientID: clientID,
			ActorType: "technician", ActorID: principal.ID,
			SubjectType: subjectType, SubjectID: subjectID, SubjectVersion: 1,
			Source: source, CorrelationID: correlationID,
		},
	}
}
