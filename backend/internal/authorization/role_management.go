package authorization

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

var ErrInvalidRole = errors.New("invalid role")
var ErrLastRoleManager = errors.New("cannot remove the final role manager grant")

var capabilityPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)
var roleKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,63}$`)

type Role struct {
	ID           string   `json:"id"`
	Key          string   `json:"key"`
	Name         string   `json:"name"`
	SystemRole   bool     `json:"system_role"`
	Capabilities []string `json:"capabilities"`
	Version      int64    `json:"version"`
}

type RoleAssignment struct {
	ID           string    `json:"id"`
	MSPID        string    `json:"msp_id"`
	ClientID     string    `json:"client_id,omitempty"`
	TechnicianID string    `json:"technician_id"`
	RoleID       string    `json:"role_id"`
	RoleKey      string    `json:"role_key"`
	GrantedAt    time.Time `json:"granted_at"`
	GrantedBy    string    `json:"granted_by"`
}

type CreateRoleCommand struct {
	Key          string
	Name         string
	Capabilities []string
	Reason       string
	ActorID      string
	Source       string
}

type AssignRoleCommand struct {
	PrincipalID string
	RoleKey     string
	ClientID    string
	Reason      string
	ActorID     string
	Source      string
}

type RoleRepository interface {
	ListRoles(context.Context, string) ([]Role, error)
	CreateRoleAtomic(
		context.Context,
		string,
		Role,
		mutation.AuditRecord,
		mutation.EventRecord,
	) error
	AssignRoleAtomic(
		context.Context,
		RoleAssignment,
		mutation.AuditRecord,
		mutation.EventRecord,
	) error
	ReplaceRoleCapabilities(
		context.Context,
		string,
		Role,
		mutation.AuditRecord,
		mutation.EventRecord,
	) error
}

type RoleManagementService struct {
	repository RoleRepository
	now        func() time.Time
	newID      func() string
}

func NewRoleManagementService(
	repository RoleRepository,
	now func() time.Time,
	newID func() string,
) *RoleManagementService {
	return &RoleManagementService{repository: repository, now: now, newID: newID}
}

func (s *RoleManagementService) List(
	ctx context.Context,
	principal Principal,
) ([]Role, error) {
	target := scope.Target{MSPID: principal.Scope.MSPID}
	if err := Authorize(principal, "role.manage", target); err != nil {
		return nil, err
	}
	return s.repository.ListRoles(ctx, target.MSPID)
}

func (s *RoleManagementService) Create(
	ctx context.Context,
	principal Principal,
	command CreateRoleCommand,
) (Role, error) {
	target := scope.Target{MSPID: principal.Scope.MSPID}
	if err := Authorize(principal, "role.manage", target); err != nil {
		return Role{}, err
	}
	key := strings.ToLower(strings.TrimSpace(command.Key))
	name := strings.TrimSpace(command.Name)
	reason := strings.TrimSpace(command.Reason)
	normalized, err := normalizeCapabilities(command.Capabilities)
	if err != nil || !roleKeyPattern.MatchString(key) || name == "" ||
		reason == "" || command.ActorID != principal.ID ||
		strings.TrimSpace(command.Source) == "" {
		return Role{}, ErrInvalidRole
	}
	roles, err := s.repository.ListRoles(ctx, target.MSPID)
	if err != nil {
		return Role{}, err
	}
	if slices.ContainsFunc(roles, func(role Role) bool { return role.Key == key }) {
		return Role{}, ErrInvalidRole
	}
	now := s.now().UTC()
	role := Role{
		ID: s.newID(), Key: key, Name: name,
		SystemRole: false, Capabilities: normalized, Version: 1,
	}
	correlationID, auditID, eventID := s.newID(), s.newID(), s.newID()
	audit := mutation.AuditRecord{
		ID: auditID, OccurredAt: now, MSPID: target.MSPID,
		ActorType: "technician", ActorID: command.ActorID,
		Action: "role.created", SubjectType: "role",
		SubjectID: role.ID, SubjectVersion: role.Version,
		Source: command.Source, Reason: reason,
		CorrelationID: correlationID,
	}
	event := mutation.EventRecord{
		EventID: eventID, EventType: "role.created", SchemaVersion: 1,
		OccurredAt: now, MSPID: target.MSPID,
		ActorType: "technician", ActorID: command.ActorID,
		SubjectType: "role", SubjectID: role.ID,
		SubjectVersion: role.Version, Source: command.Source,
		CorrelationID: correlationID,
	}
	if err := s.repository.CreateRoleAtomic(
		ctx,
		target.MSPID,
		role,
		audit,
		event,
	); err != nil {
		return Role{}, err
	}
	return role, nil
}

func (s *RoleManagementService) Assign(
	ctx context.Context,
	principal Principal,
	command AssignRoleCommand,
) (RoleAssignment, error) {
	clientID := strings.TrimSpace(command.ClientID)
	target := scope.Target{
		MSPID: principal.Scope.MSPID, ClientID: clientID,
	}
	if err := Authorize(principal, "role.manage", target); err != nil {
		return RoleAssignment{}, err
	}
	principalID := strings.TrimSpace(command.PrincipalID)
	roleKey := strings.ToLower(strings.TrimSpace(command.RoleKey))
	reason := strings.TrimSpace(command.Reason)
	if principalID == "" || !roleKeyPattern.MatchString(roleKey) ||
		reason == "" || command.ActorID != principal.ID ||
		strings.TrimSpace(command.Source) == "" {
		return RoleAssignment{}, ErrInvalidRole
	}
	roles, err := s.repository.ListRoles(ctx, target.MSPID)
	if err != nil {
		return RoleAssignment{}, err
	}
	var selected Role
	for _, role := range roles {
		if role.Key == roleKey {
			selected = role
			break
		}
	}
	if selected.ID == "" {
		return RoleAssignment{}, scope.ErrNotFound
	}
	now := s.now().UTC()
	assignment := RoleAssignment{
		ID: s.newID(), MSPID: target.MSPID, ClientID: clientID,
		TechnicianID: principalID, RoleID: selected.ID,
		RoleKey: selected.Key, GrantedAt: now, GrantedBy: command.ActorID,
	}
	correlationID, auditID, eventID := s.newID(), s.newID(), s.newID()
	audit := mutation.AuditRecord{
		ID: auditID, OccurredAt: now, MSPID: target.MSPID,
		ClientID: clientID, ActorType: "technician",
		ActorID: command.ActorID, Action: "role.assigned",
		SubjectType: "role_assignment", SubjectID: assignment.ID,
		SubjectVersion: 1, Source: command.Source, Reason: reason,
		CorrelationID: correlationID,
	}
	event := mutation.EventRecord{
		EventID: eventID, EventType: "role.assigned", SchemaVersion: 1,
		OccurredAt: now, MSPID: target.MSPID, ClientID: clientID,
		ActorType: "technician", ActorID: command.ActorID,
		SubjectType: "role_assignment", SubjectID: assignment.ID,
		SubjectVersion: 1, Source: command.Source,
		CorrelationID: correlationID,
	}
	if err := s.repository.AssignRoleAtomic(
		ctx,
		assignment,
		audit,
		event,
	); err != nil {
		return RoleAssignment{}, err
	}
	return assignment, nil
}

func (s *RoleManagementService) ReplaceCapabilities(
	ctx context.Context,
	principal Principal,
	roleID string,
	expectedVersion int64,
	capabilities []string,
	reason string,
) (Role, error) {
	target := scope.Target{MSPID: principal.Scope.MSPID}
	if err := Authorize(principal, "role.manage", target); err != nil {
		return Role{}, err
	}
	roleID = strings.TrimSpace(roleID)
	reason = strings.TrimSpace(reason)
	normalized, err := normalizeCapabilities(capabilities)
	if err != nil || roleID == "" || expectedVersion < 1 || reason == "" {
		return Role{}, ErrInvalidRole
	}
	roles, err := s.repository.ListRoles(ctx, target.MSPID)
	if err != nil {
		return Role{}, err
	}
	var current Role
	for _, role := range roles {
		if role.ID == roleID {
			current = role
			break
		}
	}
	if current.ID == "" {
		return Role{}, scope.ErrNotFound
	}
	if current.Version != expectedVersion {
		return Role{}, object.ErrVersionConflict
	}
	current.Capabilities = normalized
	current.Version++
	now := s.now().UTC()
	correlationID := s.newID()
	audit := mutation.AuditRecord{
		ID: s.newID(), OccurredAt: now, MSPID: target.MSPID,
		ActorType: "technician", ActorID: principal.ID,
		Action: "role.capabilities_replaced", SubjectType: "role",
		SubjectID: roleID, SubjectVersion: current.Version,
		Source: "api", Reason: reason, CorrelationID: correlationID,
	}
	event := mutation.EventRecord{
		EventID: s.newID(), EventType: "role.capabilities_replaced",
		SchemaVersion: 1, OccurredAt: now, MSPID: target.MSPID,
		ActorType: "technician", ActorID: principal.ID,
		SubjectType: "role", SubjectID: roleID,
		SubjectVersion: current.Version, Source: "api",
		CorrelationID: correlationID,
	}
	if err := s.repository.ReplaceRoleCapabilities(
		ctx, target.MSPID, current, audit, event,
	); err != nil {
		return Role{}, err
	}
	return current, nil
}

func normalizeCapabilities(values []string) ([]string, error) {
	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if !capabilityPattern.MatchString(value) {
			return nil, ErrInvalidRole
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	if len(normalized) == 0 {
		return nil, ErrInvalidRole
	}
	slices.Sort(normalized)
	return normalized, nil
}
