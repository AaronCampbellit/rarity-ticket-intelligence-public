package identity

import (
	"context"
	"errors"
	"net"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidBreakGlassAccount  = errors.New("invalid break-glass account")
	ErrLastBreakGlassAccount     = errors.New("cannot disable the final enabled break-glass account")
	ErrBreakGlassVersionConflict = errors.New("local administrator version conflict")
)

type ManagedBreakGlassAccount struct {
	ID           string     `json:"id"`
	TechnicianID string     `json:"technician_id"`
	Username     string     `json:"username"`
	AllowedCIDRs []string   `json:"allowed_cidrs"`
	Enabled      bool       `json:"enabled"`
	CreatedAt    time.Time  `json:"created_at"`
	LastUsedAt   *time.Time `json:"last_used_at,omitempty"`
	LastUsedIP   string     `json:"last_used_ip,omitempty"`
	Version      int64      `json:"version"`
}

type BreakGlassAccountMutation struct {
	Account      ManagedBreakGlassAccount
	MSPID        string
	PasswordHash string
	CreatedBy    string
	Email        string
	DisplayName  string
	AssignmentID string
	Audit        mutation.AuditRecord
	Event        mutation.EventRecord
}

type BreakGlassManagementRepository interface {
	CreateBreakGlassAccountAtomic(context.Context, BreakGlassAccountMutation) error
	ListBreakGlassAccounts(context.Context, string) ([]ManagedBreakGlassAccount, error)
	ResetBreakGlassPasswordAtomic(context.Context, BreakGlassAccountMutation) error
	UpdateBreakGlassNetworksAtomic(context.Context, BreakGlassAccountMutation) error
	DisableBreakGlassAccountAtomic(context.Context, BreakGlassAccountMutation) error
}

type CreateBreakGlassAccountCommand struct {
	Principal    authorization.Principal
	Email        string
	DisplayName  string
	Username     string
	Password     string
	AllowedCIDRs []string
	Reason       string
	Source       string
}

type DisableBreakGlassAccountCommand struct {
	Principal       authorization.Principal
	AccountID       string
	ExpectedVersion int64
	Reason          string
	Source          string
}

type ResetBreakGlassPasswordCommand struct {
	Principal       authorization.Principal
	AccountID       string
	ExpectedVersion int64
	Password        string
	Reason          string
	Source          string
}

type UpdateBreakGlassNetworksCommand struct {
	Principal       authorization.Principal
	AccountID       string
	ExpectedVersion int64
	AllowedCIDRs    []string
	Reason          string
	Source          string
}

type BreakGlassManagementService struct {
	repository BreakGlassManagementRepository
	now        func() time.Time
	newID      func() string
}

func NewBreakGlassManagementService(repository BreakGlassManagementRepository, now func() time.Time, newID func() string) *BreakGlassManagementService {
	if now == nil {
		now = time.Now
	}
	return &BreakGlassManagementService{repository: repository, now: now, newID: newID}
}

func (s *BreakGlassManagementService) Create(ctx context.Context, command CreateBreakGlassAccountCommand) (ManagedBreakGlassAccount, error) {
	if err := authorizeBreakGlassManagement(command.Principal); err != nil {
		return ManagedBreakGlassAccount{}, err
	}
	username := strings.ToLower(strings.TrimSpace(command.Username))
	reason := strings.TrimSpace(command.Reason)
	source := strings.TrimSpace(command.Source)
	password := []byte(command.Password)
	cidrs, ok := normalizeCIDRs(command.AllowedCIDRs)
	if username == "" || len(username) > 128 || len(password) < 16 || len(password) > 72 ||
		reason == "" || source == "" || !ok || s.repository == nil || s.newID == nil {
		return ManagedBreakGlassAccount{}, ErrInvalidBreakGlassAccount
	}
	hash, err := bcrypt.GenerateFromPassword(password, MinBreakGlassBcryptCost)
	if err != nil {
		return ManagedBreakGlassAccount{}, ErrInvalidBreakGlassAccount
	}
	now := s.now().UTC()
	technicianID := command.Principal.ID
	email, displayName := strings.TrimSpace(command.Email), strings.TrimSpace(command.DisplayName)
	assignmentID := ""
	if email != "" || displayName != "" {
		if email == "" || displayName == "" {
			return ManagedBreakGlassAccount{}, ErrInvalidBreakGlassAccount
		}
		technicianID = s.newID()
		assignmentID = s.newID()
	}
	account := ManagedBreakGlassAccount{
		ID: s.newID(), TechnicianID: technicianID, Username: username,
		AllowedCIDRs: cidrs, Enabled: true, CreatedAt: now, Version: 1,
	}
	mutation := s.mutation(command.Principal, account, "security.local_admin.created", reason, source, now)
	mutation.PasswordHash = string(hash)
	mutation.CreatedBy = command.Principal.ID
	mutation.Email = email
	mutation.DisplayName = displayName
	mutation.AssignmentID = assignmentID
	if err := s.repository.CreateBreakGlassAccountAtomic(ctx, mutation); err != nil {
		return ManagedBreakGlassAccount{}, err
	}
	return account, nil
}

func (s *BreakGlassManagementService) List(ctx context.Context, principal authorization.Principal) ([]ManagedBreakGlassAccount, error) {
	if err := authorizeBreakGlassManagement(principal); err != nil {
		return nil, err
	}
	if s.repository == nil {
		return nil, ErrInvalidBreakGlassAccount
	}
	return s.repository.ListBreakGlassAccounts(ctx, principal.Scope.MSPID)
}

func (s *BreakGlassManagementService) Disable(ctx context.Context, command DisableBreakGlassAccountCommand) (ManagedBreakGlassAccount, error) {
	if err := authorizeBreakGlassManagement(command.Principal); err != nil {
		return ManagedBreakGlassAccount{}, err
	}
	reason, source := strings.TrimSpace(command.Reason), strings.TrimSpace(command.Source)
	if strings.TrimSpace(command.AccountID) == "" || command.ExpectedVersion < 1 ||
		reason == "" || source == "" || s.repository == nil || s.newID == nil {
		return ManagedBreakGlassAccount{}, ErrInvalidBreakGlassAccount
	}
	account := ManagedBreakGlassAccount{ID: command.AccountID, Enabled: false, Version: command.ExpectedVersion + 1}
	accepted := s.mutation(command.Principal, account, "security.local_admin.disabled", reason, source, s.now().UTC())
	if err := s.repository.DisableBreakGlassAccountAtomic(ctx, accepted); err != nil {
		return ManagedBreakGlassAccount{}, err
	}
	return account, nil
}

func (s *BreakGlassManagementService) ResetPassword(ctx context.Context, command ResetBreakGlassPasswordCommand) (ManagedBreakGlassAccount, error) {
	if err := authorizeBreakGlassManagement(command.Principal); err != nil {
		return ManagedBreakGlassAccount{}, err
	}
	reason, source := strings.TrimSpace(command.Reason), strings.TrimSpace(command.Source)
	password := []byte(command.Password)
	if strings.TrimSpace(command.AccountID) == "" || command.ExpectedVersion < 1 ||
		len(password) < 16 || len(password) > 72 || reason == "" || source == "" ||
		s.repository == nil || s.newID == nil {
		return ManagedBreakGlassAccount{}, ErrInvalidBreakGlassAccount
	}
	hash, err := bcrypt.GenerateFromPassword(password, MinBreakGlassBcryptCost)
	if err != nil {
		return ManagedBreakGlassAccount{}, ErrInvalidBreakGlassAccount
	}
	account := ManagedBreakGlassAccount{
		ID: command.AccountID, Enabled: true, Version: command.ExpectedVersion + 1,
	}
	accepted := s.mutation(command.Principal, account, "security.local_admin.password_reset", reason, source, s.now().UTC())
	accepted.PasswordHash = string(hash)
	if err := s.repository.ResetBreakGlassPasswordAtomic(ctx, accepted); err != nil {
		return ManagedBreakGlassAccount{}, err
	}
	return account, nil
}

func (s *BreakGlassManagementService) UpdateNetworks(ctx context.Context, command UpdateBreakGlassNetworksCommand) (ManagedBreakGlassAccount, error) {
	if err := authorizeBreakGlassManagement(command.Principal); err != nil {
		return ManagedBreakGlassAccount{}, err
	}
	reason, source := strings.TrimSpace(command.Reason), strings.TrimSpace(command.Source)
	cidrs, ok := normalizeCIDRs(command.AllowedCIDRs)
	if strings.TrimSpace(command.AccountID) == "" || command.ExpectedVersion < 1 ||
		reason == "" || source == "" || !ok || s.repository == nil || s.newID == nil {
		return ManagedBreakGlassAccount{}, ErrInvalidBreakGlassAccount
	}
	account := ManagedBreakGlassAccount{
		ID: command.AccountID, AllowedCIDRs: cidrs, Enabled: true,
		Version: command.ExpectedVersion + 1,
	}
	accepted := s.mutation(command.Principal, account, "security.local_admin.networks_updated", reason, source, s.now().UTC())
	if err := s.repository.UpdateBreakGlassNetworksAtomic(ctx, accepted); err != nil {
		return ManagedBreakGlassAccount{}, err
	}
	return account, nil
}

func authorizeBreakGlassManagement(principal authorization.Principal) error {
	return authorization.Authorize(principal, "organization.manage", scope.Target{MSPID: principal.Scope.MSPID})
}

func normalizeCIDRs(values []string) ([]string, bool) {
	if len(values) == 0 || len(values) > 16 {
		return nil, false
	}
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		_, network, err := net.ParseCIDR(strings.TrimSpace(value))
		if err != nil {
			return nil, false
		}
		canonical := network.String()
		if _, exists := seen[canonical]; !exists {
			seen[canonical] = struct{}{}
			result = append(result, canonical)
		}
	}
	return result, len(result) > 0
}

func (s *BreakGlassManagementService) mutation(principal authorization.Principal, account ManagedBreakGlassAccount, action, reason, source string, occurredAt time.Time) BreakGlassAccountMutation {
	correlationID := s.newID()
	return BreakGlassAccountMutation{
		Account: account, MSPID: principal.Scope.MSPID,
		Audit: mutation.AuditRecord{
			ID: s.newID(), OccurredAt: occurredAt, MSPID: principal.Scope.MSPID,
			ActorType: "technician", ActorID: principal.ID, Action: action,
			SubjectType: "local_administrator", SubjectID: account.ID,
			SubjectVersion: account.Version, Source: source, Reason: reason,
			CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: s.newID(), EventType: action, SchemaVersion: 1,
			OccurredAt: occurredAt, MSPID: principal.Scope.MSPID,
			ActorType: "technician", ActorID: principal.ID,
			SubjectType: "local_administrator", SubjectID: account.ID,
			SubjectVersion: account.Version, Source: source, CorrelationID: correlationID,
		},
	}
}
