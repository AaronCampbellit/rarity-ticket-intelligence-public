package graphintake

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/mail"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

var ErrInvalidGraphManagement = errors.New("invalid Graph mailbox management request")

type ManagedMailbox struct {
	ID                    string     `json:"id"`
	MailboxAddress        string     `json:"mailbox_address"`
	TenantID              string     `json:"tenant_id"`
	ClientID              string     `json:"client_id"`
	CredentialConfigured  bool       `json:"credential_configured"`
	ClientStateConfigured bool       `json:"client_state_configured"`
	Enabled               bool       `json:"enabled"`
	HealthState           string     `json:"health_state"`
	LastSuccessAt         *time.Time `json:"last_success_at,omitempty"`
	LastErrorCode         string     `json:"last_error_code,omitempty"`
	Version               int64      `json:"version"`
	UpdatedAt             time.Time  `json:"updated_at"`
}

type MailboxMutation struct {
	Mailbox             ManagedMailbox
	MSPID               string
	ExpectedVersion     int64
	ProtectedCredential []byte `json:"-"`
	ClientState         []byte `json:"-"`
	Audit               mutation.AuditRecord
	Event               mutation.EventRecord
}

type ManagementRepository interface {
	ListManagedMailboxes(context.Context, string) ([]ManagedMailbox, error)
	GetManagedMailbox(context.Context, string, string) (ManagedMailbox, error)
	CreateManagedMailbox(context.Context, MailboxMutation) error
	UpdateManagedMailbox(context.Context, MailboxMutation) error
	ReplaceManagedMailboxCredential(context.Context, MailboxMutation) error
}

type ManagementService struct {
	repository ManagementRepository
	now        func() time.Time
	newID      func() string
	random     func(int) ([]byte, error)
}

func NewManagementService(
	repository ManagementRepository,
	now func() time.Time,
	newID func() string,
	randomBytes func(int) ([]byte, error),
) *ManagementService {
	if randomBytes == nil {
		randomBytes = graphRandomBytes
	}
	return &ManagementService{repository: repository, now: now, newID: newID, random: randomBytes}
}

type CreateMailboxCommand struct {
	Principal    authorization.Principal
	Mailbox      string
	TenantID     string
	ClientID     string
	ClientSecret []byte
	Reason       string
}

type UpdateMailboxCommand struct {
	Principal       authorization.Principal
	ID              string
	ExpectedVersion int64
	Mailbox         string
	Enabled         *bool
	Reason          string
}

type ReplaceMailboxCredentialCommand struct {
	Principal       authorization.Principal
	ID              string
	ExpectedVersion int64
	TenantID        string
	ClientID        string
	ClientSecret    []byte
	Reason          string
}

func (s *ManagementService) List(ctx context.Context, principal authorization.Principal) ([]ManagedMailbox, error) {
	target := scope.Target{MSPID: principal.Scope.MSPID}
	if s == nil || s.repository == nil || target.MSPID == "" {
		return nil, ErrInvalidGraphManagement
	}
	if err := authorization.Authorize(principal, "integration.manage", target); err != nil {
		return nil, err
	}
	return s.repository.ListManagedMailboxes(ctx, target.MSPID)
}

func (s *ManagementService) Create(ctx context.Context, command CreateMailboxCommand) (ManagedMailbox, error) {
	target := scope.Target{MSPID: command.Principal.Scope.MSPID}
	if s == nil || s.repository == nil || s.now == nil || s.newID == nil || s.random == nil ||
		target.MSPID == "" || !validMailbox(command.Mailbox) ||
		strings.TrimSpace(command.TenantID) == "" || strings.TrimSpace(command.ClientID) == "" ||
		len(command.ClientSecret) == 0 || strings.TrimSpace(command.Reason) == "" {
		return ManagedMailbox{}, ErrInvalidGraphManagement
	}
	if err := authorization.Authorize(command.Principal, "integration.manage", target); err != nil {
		return ManagedMailbox{}, err
	}
	protected, err := graphCredential(command.TenantID, command.ClientID, command.ClientSecret)
	if err != nil {
		return ManagedMailbox{}, err
	}
	defer wipeGraphBytes(protected)
	state, err := s.random(32)
	if err != nil {
		return ManagedMailbox{}, err
	}
	clientState := []byte(base64.RawURLEncoding.EncodeToString(state))
	defer wipeGraphBytes(clientState)
	now := s.now().UTC()
	mailbox := ManagedMailbox{
		ID: s.newID(), MailboxAddress: strings.ToLower(strings.TrimSpace(command.Mailbox)),
		TenantID: strings.TrimSpace(command.TenantID), ClientID: strings.TrimSpace(command.ClientID),
		CredentialConfigured: true, ClientStateConfigured: true,
		Enabled: true, HealthState: "pending", Version: 1, UpdatedAt: now,
	}
	accepted := s.mutation(command.Principal, mailbox, 0, protected, clientState, "graph.mailbox.created", command.Reason, now)
	if err := s.repository.CreateManagedMailbox(ctx, accepted); err != nil {
		return ManagedMailbox{}, err
	}
	return mailbox, nil
}

func (s *ManagementService) Update(ctx context.Context, command UpdateMailboxCommand) (ManagedMailbox, error) {
	target := scope.Target{MSPID: command.Principal.Scope.MSPID}
	if s == nil || s.repository == nil || s.now == nil || s.newID == nil ||
		target.MSPID == "" || command.ExpectedVersion < 1 || strings.TrimSpace(command.Reason) == "" {
		return ManagedMailbox{}, ErrInvalidGraphManagement
	}
	if err := authorization.Authorize(command.Principal, "integration.manage", target); err != nil {
		return ManagedMailbox{}, err
	}
	current, err := s.repository.GetManagedMailbox(ctx, target.MSPID, command.ID)
	if err != nil {
		return ManagedMailbox{}, err
	}
	if current.Version != command.ExpectedVersion {
		return ManagedMailbox{}, object.ErrVersionConflict
	}
	if command.Enabled != nil {
		current.Enabled = *command.Enabled
		if current.Enabled {
			current.HealthState = "pending"
		} else {
			current.HealthState = "disabled"
		}
	} else if validMailbox(command.Mailbox) {
		current.MailboxAddress = strings.ToLower(strings.TrimSpace(command.Mailbox))
	} else {
		return ManagedMailbox{}, ErrInvalidGraphManagement
	}
	now := s.now().UTC()
	current.Version++
	current.UpdatedAt = now
	accepted := s.mutation(command.Principal, current, command.ExpectedVersion, nil, nil, "graph.mailbox.updated", command.Reason, now)
	if err := s.repository.UpdateManagedMailbox(ctx, accepted); err != nil {
		return ManagedMailbox{}, err
	}
	return current, nil
}

func (s *ManagementService) ReplaceCredential(ctx context.Context, command ReplaceMailboxCredentialCommand) (ManagedMailbox, error) {
	target := scope.Target{MSPID: command.Principal.Scope.MSPID}
	if s == nil || s.repository == nil || s.now == nil || s.newID == nil ||
		target.MSPID == "" || command.ExpectedVersion < 1 || len(command.ClientSecret) == 0 ||
		strings.TrimSpace(command.Reason) == "" {
		return ManagedMailbox{}, ErrInvalidGraphManagement
	}
	if err := authorization.Authorize(command.Principal, "integration.manage", target); err != nil {
		return ManagedMailbox{}, err
	}
	current, err := s.repository.GetManagedMailbox(ctx, target.MSPID, command.ID)
	if err != nil {
		return ManagedMailbox{}, err
	}
	if current.Version != command.ExpectedVersion {
		return ManagedMailbox{}, object.ErrVersionConflict
	}
	protected, err := graphCredential(command.TenantID, command.ClientID, command.ClientSecret)
	if err != nil {
		return ManagedMailbox{}, err
	}
	defer wipeGraphBytes(protected)
	now := s.now().UTC()
	current.TenantID, current.ClientID = strings.TrimSpace(command.TenantID), strings.TrimSpace(command.ClientID)
	current.CredentialConfigured = true
	current.Version++
	current.UpdatedAt = now
	accepted := s.mutation(command.Principal, current, command.ExpectedVersion, protected, nil, "graph.mailbox.credential_replaced", command.Reason, now)
	if err := s.repository.ReplaceManagedMailboxCredential(ctx, accepted); err != nil {
		return ManagedMailbox{}, err
	}
	return current, nil
}

func (s *ManagementService) mutation(principal authorization.Principal, mailbox ManagedMailbox, expected int64, credential, state []byte, action, reason string, now time.Time) MailboxMutation {
	auditID, eventID, correlationID := s.newID(), s.newID(), s.newID()
	return MailboxMutation{
		Mailbox: mailbox, MSPID: principal.Scope.MSPID, ExpectedVersion: expected,
		ProtectedCredential: credential, ClientState: state,
		Audit: mutation.AuditRecord{ID: auditID, OccurredAt: now, MSPID: principal.Scope.MSPID, ActorType: "technician", ActorID: principal.ID, Action: action, SubjectType: "graph_mailbox", SubjectID: mailbox.ID, SubjectVersion: mailbox.Version, Source: "api", Reason: strings.TrimSpace(reason), CorrelationID: correlationID},
		Event: mutation.EventRecord{EventID: eventID, EventType: action, SchemaVersion: 1, OccurredAt: now, MSPID: principal.Scope.MSPID, ActorType: "technician", ActorID: principal.ID, SubjectType: "graph_mailbox", SubjectID: mailbox.ID, SubjectVersion: mailbox.Version, Source: "api", CorrelationID: correlationID},
	}
}

func graphCredential(tenant, client string, secret []byte) ([]byte, error) {
	tenant, client = strings.TrimSpace(tenant), strings.TrimSpace(client)
	if tenant == "" || client == "" || len(secret) == 0 {
		return nil, ErrInvalidGraphManagement
	}
	return json.Marshal(map[string]string{"tenant_id": tenant, "client_id": client, "client_secret": string(secret)})
}
func validMailbox(value string) bool {
	address, err := mail.ParseAddress(strings.TrimSpace(value))
	return err == nil && strings.EqualFold(address.Address, strings.TrimSpace(value))
}
func graphRandomBytes(size int) ([]byte, error) {
	value := make([]byte, size)
	_, err := rand.Read(value)
	return value, err
}
func wipeGraphBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
