package timeentries

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

var ErrInvalidLaborRole = errors.New("invalid labor role")

var nonLaborRoleKeyCharacter = regexp.MustCompile(`[^a-z0-9]+`)

type LaborRole struct {
	ID             string           `json:"id"`
	MSPID          string           `json:"msp_id"`
	Key            string           `json:"key"`
	Version        int64            `json:"version"`
	CurrentVersion LaborRoleVersion `json:"current_version"`
}

type LaborRoleVersion struct {
	ID                string     `json:"id"`
	LaborRoleID       string     `json:"labor_role_id"`
	MSPID             string     `json:"msp_id"`
	Name              string     `json:"name"`
	InternalCostMinor int64      `json:"internal_cost_minor"`
	BillRateMinor     int64      `json:"bill_rate_minor"`
	Currency          string     `json:"currency"`
	EffectiveFrom     time.Time  `json:"effective_from"`
	EffectiveUntil    *time.Time `json:"effective_until,omitempty"`
	Enabled           bool       `json:"enabled"`
	CreatedAt         time.Time  `json:"created_at"`
	CreatedBy         string     `json:"created_by"`
}

type RateSnapshot struct {
	LaborRoleVersionID string `json:"labor_role_version_id"`
	InternalCostMinor  int64  `json:"internal_cost_minor"`
	BillRateMinor      int64  `json:"bill_rate_minor"`
	Currency           string `json:"currency"`
}

type CreateLaborRoleCommand struct {
	Principal         authorization.Principal
	Key               string
	Name              string
	InternalCostMinor int64
	BillRateMinor     int64
	Currency          string
	EffectiveFrom     time.Time
	ActorID           string
	Source            string
}

type VersionLaborRoleCommand struct {
	Principal         authorization.Principal
	LaborRoleID       string
	ExpectedVersion   int64
	Name              string
	InternalCostMinor int64
	BillRateMinor     int64
	Currency          string
	EffectiveFrom     time.Time
	EffectiveUntil    *time.Time
	Enabled           bool
	Reason            string
	ActorID           string
	Source            string
}

type LaborRoleMutation struct {
	Role    LaborRole
	Version LaborRoleVersion
	Audit   mutation.AuditRecord
	Event   mutation.EventRecord
}

type LaborRoleVersionMutation struct {
	Role    LaborRole
	Version LaborRoleVersion
	Audit   mutation.AuditRecord
	Event   mutation.EventRecord
}

type LaborRoleRepository interface {
	ListLaborRoles(context.Context, string, time.Time) ([]LaborRole, error)
	ListLaborRolesForManagement(context.Context, string) ([]LaborRole, error)
	CreateLaborRoleAtomic(context.Context, LaborRoleMutation) error
	VersionLaborRoleAtomic(context.Context, LaborRoleVersionMutation) error
	ResolveLaborRate(
		context.Context,
		string,
		string,
		string,
		time.Time,
	) (RateSnapshot, error)
}

type LaborRoleService struct {
	repository LaborRoleRepository
	now        func() time.Time
	newID      func() string
}

func NewLaborRoleService(
	repository LaborRoleRepository,
	now func() time.Time,
	newID func() string,
) *LaborRoleService {
	return &LaborRoleService{repository: repository, now: now, newID: newID}
}

func (s *LaborRoleService) ListForManagement(
	ctx context.Context,
	principal authorization.Principal,
) ([]LaborRole, error) {
	target := scope.Target{MSPID: principal.Scope.MSPID}
	if target.MSPID == "" {
		return nil, ErrInvalidLaborRole
	}
	if err := authorization.Authorize(
		principal,
		"organization.manage",
		target,
	); err != nil {
		return nil, err
	}
	return s.repository.ListLaborRolesForManagement(ctx, target.MSPID)
}

func (s *LaborRoleService) List(
	ctx context.Context,
	principal authorization.Principal,
) ([]LaborRole, error) {
	target := scope.Target{
		MSPID: principal.Scope.MSPID, ClientID: principal.Scope.ClientID,
	}
	if target.MSPID == "" || target.ClientID == "" {
		return nil, ErrInvalidLaborRole
	}
	if err := authorization.Authorize(
		principal,
		"time_entry.create",
		target,
	); err != nil {
		return nil, err
	}
	return s.repository.ListLaborRoles(ctx, target.MSPID, s.now().UTC())
}

func (s *LaborRoleService) Create(
	ctx context.Context,
	command CreateLaborRoleCommand,
) (LaborRole, error) {
	target := scope.Target{MSPID: command.Principal.Scope.MSPID}
	key := normalizeLaborRoleKey(command.Key)
	name := strings.TrimSpace(command.Name)
	currency := strings.ToUpper(strings.TrimSpace(command.Currency))
	actorID := strings.TrimSpace(command.ActorID)
	source := strings.TrimSpace(command.Source)
	if target.MSPID == "" || key == "" || name == "" ||
		command.InternalCostMinor < 0 || command.BillRateMinor < 0 ||
		len(currency) != 3 || command.EffectiveFrom.IsZero() ||
		actorID == "" || source == "" {
		return LaborRole{}, ErrInvalidLaborRole
	}
	if err := authorization.Authorize(
		command.Principal,
		"organization.manage",
		target,
	); err != nil {
		return LaborRole{}, err
	}

	now := s.now().UTC()
	role := LaborRole{
		ID: s.newID(), MSPID: target.MSPID, Key: key, Version: 1,
	}
	role.CurrentVersion = LaborRoleVersion{
		ID: s.newID(), LaborRoleID: role.ID, MSPID: role.MSPID,
		Name: name, InternalCostMinor: command.InternalCostMinor,
		BillRateMinor: command.BillRateMinor, Currency: currency,
		EffectiveFrom: command.EffectiveFrom.UTC(), Enabled: true,
		CreatedAt: now, CreatedBy: actorID,
	}
	auditID, eventID, correlationID := s.newID(), s.newID(), s.newID()
	accepted := LaborRoleMutation{
		Role: role, Version: role.CurrentVersion,
		Audit: mutation.AuditRecord{
			ID: auditID, OccurredAt: now, MSPID: role.MSPID,
			ActorType: "technician", ActorID: actorID,
			Action: "labor_role.created", SubjectType: "labor_role",
			SubjectID: role.ID, SubjectVersion: role.Version,
			Source: source, CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: eventID, EventType: "labor_role.created",
			SchemaVersion: 1, OccurredAt: now, MSPID: role.MSPID,
			ActorType: "technician", ActorID: actorID,
			SubjectType: "labor_role", SubjectID: role.ID,
			SubjectVersion: role.Version, Source: source,
			CorrelationID: correlationID,
		},
	}
	if err := s.repository.CreateLaborRoleAtomic(ctx, accepted); err != nil {
		return LaborRole{}, err
	}
	return role, nil
}

func (s *LaborRoleService) Version(
	ctx context.Context,
	command VersionLaborRoleCommand,
) (LaborRole, error) {
	target := scope.Target{MSPID: command.Principal.Scope.MSPID}
	roleID := strings.TrimSpace(command.LaborRoleID)
	name := strings.TrimSpace(command.Name)
	currency := strings.ToUpper(strings.TrimSpace(command.Currency))
	reason := strings.TrimSpace(command.Reason)
	actorID := strings.TrimSpace(command.ActorID)
	source := strings.TrimSpace(command.Source)
	if target.MSPID == "" || roleID == "" || command.ExpectedVersion < 1 ||
		name == "" || command.InternalCostMinor < 0 || command.BillRateMinor < 0 ||
		len(currency) != 3 || command.EffectiveFrom.IsZero() ||
		(command.EffectiveUntil != nil &&
			!command.EffectiveUntil.After(command.EffectiveFrom)) ||
		reason == "" || actorID == "" || source == "" {
		return LaborRole{}, ErrInvalidLaborRole
	}
	if err := authorization.Authorize(
		command.Principal,
		"organization.manage",
		target,
	); err != nil {
		return LaborRole{}, err
	}
	roles, err := s.repository.ListLaborRolesForManagement(ctx, target.MSPID)
	if err != nil {
		return LaborRole{}, err
	}
	var existing LaborRole
	for _, candidate := range roles {
		if candidate.ID == roleID {
			existing = candidate
			break
		}
	}
	if existing.ID == "" {
		return LaborRole{}, scope.ErrNotFound
	}

	now := s.now().UTC()
	role := LaborRole{
		ID: roleID, MSPID: target.MSPID, Key: existing.Key,
		Version: command.ExpectedVersion + 1,
	}
	role.CurrentVersion = LaborRoleVersion{
		ID: s.newID(), LaborRoleID: role.ID, MSPID: role.MSPID,
		Name: name, InternalCostMinor: command.InternalCostMinor,
		BillRateMinor: command.BillRateMinor, Currency: currency,
		EffectiveFrom:  command.EffectiveFrom.UTC(),
		EffectiveUntil: utcTimePointer(command.EffectiveUntil),
		Enabled:        command.Enabled, CreatedAt: now, CreatedBy: actorID,
	}
	auditID, eventID, correlationID := s.newID(), s.newID(), s.newID()
	accepted := LaborRoleVersionMutation{
		Role: role, Version: role.CurrentVersion,
		Audit: mutation.AuditRecord{
			ID: auditID, OccurredAt: now, MSPID: role.MSPID,
			ActorType: "technician", ActorID: actorID,
			Action: "labor_role.versioned", SubjectType: "labor_role",
			SubjectID: role.ID, SubjectVersion: role.Version,
			Source: source, Reason: reason, CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: eventID, EventType: "labor_role.versioned",
			SchemaVersion: 1, OccurredAt: now, MSPID: role.MSPID,
			ActorType: "technician", ActorID: actorID,
			SubjectType: "labor_role", SubjectID: role.ID,
			SubjectVersion: role.Version, Source: source,
			CorrelationID: correlationID,
		},
	}
	if err := s.repository.VersionLaborRoleAtomic(ctx, accepted); err != nil {
		return LaborRole{}, err
	}
	return role, nil
}

func (s *LaborRoleService) ResolveLaborRate(
	ctx context.Context,
	principal authorization.Principal,
	laborRoleID string,
	technicianID string,
	at time.Time,
) (RateSnapshot, error) {
	target := scope.Target{
		MSPID: principal.Scope.MSPID, ClientID: principal.Scope.ClientID,
	}
	laborRoleID = strings.TrimSpace(laborRoleID)
	technicianID = strings.TrimSpace(technicianID)
	if target.MSPID == "" || target.ClientID == "" ||
		laborRoleID == "" || technicianID == "" || at.IsZero() {
		return RateSnapshot{}, ErrInvalidLaborRole
	}
	if err := authorization.Authorize(
		principal,
		"time_entry.create",
		target,
	); err != nil {
		return RateSnapshot{}, err
	}
	return s.repository.ResolveLaborRate(
		ctx,
		target.MSPID,
		laborRoleID,
		technicianID,
		at.UTC(),
	)
}

func normalizeLaborRoleKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.Trim(nonLaborRoleKeyCharacter.ReplaceAllString(value, "_"), "_")
}

func utcTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	normalized := value.UTC()
	return &normalized
}
