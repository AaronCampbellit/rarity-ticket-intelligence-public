package datto

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

var ErrInvalidConnectionManagement = errors.New("invalid Datto connection management request")

type ManagedConnection struct {
	ID                   string     `json:"id"`
	Name                 string     `json:"name"`
	APIURL               string     `json:"api_url"`
	CredentialConfigured bool       `json:"credential_configured"`
	SyncIntervalSeconds  int64      `json:"sync_interval_seconds"`
	Enabled              bool       `json:"enabled"`
	HealthState          string     `json:"health_state"`
	LastCompletedAt      *time.Time `json:"last_completed_at,omitempty"`
	LastErrorCode        string     `json:"last_error_code,omitempty"`
	Version              int64      `json:"version"`
	UpdatedAt            time.Time  `json:"updated_at"`
}
type ConnectionMutation struct {
	Connection          ManagedConnection
	MSPID               string
	ExpectedVersion     int64
	ProtectedCredential []byte `json:"-"`
	Audit               mutation.AuditRecord
	Event               mutation.EventRecord
}
type ConnectionManagementRepository interface {
	ListManagedConnections(context.Context, string) ([]ManagedConnection, error)
	GetManagedConnection(context.Context, string, string) (ManagedConnection, error)
	CreateManagedConnection(context.Context, ConnectionMutation) error
	UpdateManagedConnection(context.Context, ConnectionMutation) error
	ReplaceManagedCredential(context.Context, ConnectionMutation) error
}
type ConnectionManagementService struct {
	repository ConnectionManagementRepository
	now        func() time.Time
	newID      func() string
}

func NewConnectionManagementService(repository ConnectionManagementRepository, now func() time.Time, newID func() string) *ConnectionManagementService {
	return &ConnectionManagementService{repository: repository, now: now, newID: newID}
}

type CreateConnectionCommand struct {
	Principal    authorization.Principal
	Name, APIURL string
	APIKey       []byte
	APISecret    []byte
	SyncInterval time.Duration
	Reason       string
}
type UpdateConnectionCommand struct {
	Principal       authorization.Principal
	ID              string
	ExpectedVersion int64
	Name            string
	SyncInterval    time.Duration
	Enabled         *bool
	Reason          string
}
type ReplaceCredentialCommand struct {
	Principal       authorization.Principal
	ID              string
	ExpectedVersion int64
	APIURL          string
	APIKey          []byte
	APISecret       []byte
	Reason          string
}

func (s *ConnectionManagementService) List(ctx context.Context, p authorization.Principal) ([]ManagedConnection, error) {
	target := scope.Target{MSPID: p.Scope.MSPID}
	if s == nil || s.repository == nil || target.MSPID == "" {
		return nil, ErrInvalidConnectionManagement
	}
	if err := authorization.Authorize(p, "integration.manage", target); err != nil {
		return nil, err
	}
	return s.repository.ListManagedConnections(ctx, target.MSPID)
}
func (s *ConnectionManagementService) Create(ctx context.Context, c CreateConnectionCommand) (ManagedConnection, error) {
	target := scope.Target{MSPID: c.Principal.Scope.MSPID}
	if s == nil || s.repository == nil || s.now == nil || s.newID == nil || target.MSPID == "" || strings.TrimSpace(c.Name) == "" || !validDattoURL(c.APIURL) || len(c.APIKey) == 0 || len(c.APISecret) == 0 || c.SyncInterval < time.Minute || c.SyncInterval > 24*time.Hour || strings.TrimSpace(c.Reason) == "" {
		return ManagedConnection{}, ErrInvalidConnectionManagement
	}
	if err := authorization.Authorize(c.Principal, "integration.manage", target); err != nil {
		return ManagedConnection{}, err
	}
	defer wipeDattoCredential(c.APIKey)
	defer wipeDattoCredential(c.APISecret)
	protected, err := dattoCredential(c.APIURL, c.APIKey, c.APISecret)
	if err != nil {
		return ManagedConnection{}, err
	}
	defer wipeDattoCredential(protected)
	now := s.now().UTC()
	item := ManagedConnection{ID: s.newID(), Name: strings.TrimSpace(c.Name), APIURL: strings.TrimRight(strings.TrimSpace(c.APIURL), "/"), CredentialConfigured: true, SyncIntervalSeconds: int64(c.SyncInterval / time.Second), Enabled: true, HealthState: "pending", Version: 1, UpdatedAt: now}
	m := s.mutation(c.Principal, item, 0, protected, "datto.connection.created", c.Reason, now)
	if err := s.repository.CreateManagedConnection(ctx, m); err != nil {
		return ManagedConnection{}, err
	}
	return item, nil
}
func (s *ConnectionManagementService) Update(ctx context.Context, c UpdateConnectionCommand) (ManagedConnection, error) {
	target := scope.Target{MSPID: c.Principal.Scope.MSPID}
	if s == nil || s.repository == nil || s.now == nil || s.newID == nil || target.MSPID == "" || c.ExpectedVersion < 1 || strings.TrimSpace(c.Reason) == "" {
		return ManagedConnection{}, ErrInvalidConnectionManagement
	}
	if err := authorization.Authorize(c.Principal, "integration.manage", target); err != nil {
		return ManagedConnection{}, err
	}
	item, err := s.repository.GetManagedConnection(ctx, target.MSPID, c.ID)
	if err != nil {
		return ManagedConnection{}, err
	}
	if item.Version != c.ExpectedVersion {
		return ManagedConnection{}, object.ErrVersionConflict
	}
	if c.Enabled != nil {
		item.Enabled = *c.Enabled
		if item.Enabled {
			item.HealthState = "pending"
		} else {
			item.HealthState = "disabled"
		}
	} else {
		if strings.TrimSpace(c.Name) == "" || c.SyncInterval < time.Minute || c.SyncInterval > 24*time.Hour {
			return ManagedConnection{}, ErrInvalidConnectionManagement
		}
		item.Name = strings.TrimSpace(c.Name)
		item.SyncIntervalSeconds = int64(c.SyncInterval / time.Second)
	}
	now := s.now().UTC()
	item.Version++
	item.UpdatedAt = now
	m := s.mutation(c.Principal, item, c.ExpectedVersion, nil, "datto.connection.updated", c.Reason, now)
	if err := s.repository.UpdateManagedConnection(ctx, m); err != nil {
		return ManagedConnection{}, err
	}
	return item, nil
}
func (s *ConnectionManagementService) ReplaceCredential(ctx context.Context, c ReplaceCredentialCommand) (ManagedConnection, error) {
	target := scope.Target{MSPID: c.Principal.Scope.MSPID}
	if s == nil || s.repository == nil || s.now == nil || s.newID == nil || target.MSPID == "" || c.ExpectedVersion < 1 || !validDattoURL(c.APIURL) || len(c.APIKey) == 0 || len(c.APISecret) == 0 || strings.TrimSpace(c.Reason) == "" {
		return ManagedConnection{}, ErrInvalidConnectionManagement
	}
	if err := authorization.Authorize(c.Principal, "integration.manage", target); err != nil {
		return ManagedConnection{}, err
	}
	item, err := s.repository.GetManagedConnection(ctx, target.MSPID, c.ID)
	if err != nil {
		return ManagedConnection{}, err
	}
	if item.Version != c.ExpectedVersion {
		return ManagedConnection{}, object.ErrVersionConflict
	}
	defer wipeDattoCredential(c.APIKey)
	defer wipeDattoCredential(c.APISecret)
	protected, err := dattoCredential(c.APIURL, c.APIKey, c.APISecret)
	if err != nil {
		return ManagedConnection{}, err
	}
	defer wipeDattoCredential(protected)
	now := s.now().UTC()
	item.APIURL = strings.TrimRight(strings.TrimSpace(c.APIURL), "/")
	item.CredentialConfigured = true
	item.Version++
	item.UpdatedAt = now
	m := s.mutation(c.Principal, item, c.ExpectedVersion, protected, "datto.connection.credential_replaced", c.Reason, now)
	if err := s.repository.ReplaceManagedCredential(ctx, m); err != nil {
		return ManagedConnection{}, err
	}
	return item, nil
}
func (s *ConnectionManagementService) mutation(p authorization.Principal, item ManagedConnection, expected int64, credential []byte, action, reason string, now time.Time) ConnectionMutation {
	a, e, c := s.newID(), s.newID(), s.newID()
	return ConnectionMutation{Connection: item, MSPID: p.Scope.MSPID, ExpectedVersion: expected, ProtectedCredential: credential, Audit: mutation.AuditRecord{ID: a, OccurredAt: now, MSPID: p.Scope.MSPID, ActorType: "technician", ActorID: p.ID, Action: action, SubjectType: "datto_connection", SubjectID: item.ID, SubjectVersion: item.Version, Source: "api", Reason: strings.TrimSpace(reason), CorrelationID: c}, Event: mutation.EventRecord{EventID: e, EventType: action, SchemaVersion: 1, OccurredAt: now, MSPID: p.Scope.MSPID, ActorType: "technician", ActorID: p.ID, SubjectType: "datto_connection", SubjectID: item.ID, SubjectVersion: item.Version, Source: "api", CorrelationID: c}}
}
func dattoCredential(apiURL string, key, secret []byte) ([]byte, error) {
	if !validDattoURL(apiURL) || len(key) == 0 || len(secret) == 0 {
		return nil, ErrInvalidConnectionManagement
	}
	return json.Marshal(map[string]string{"api_url": strings.TrimRight(strings.TrimSpace(apiURL), "/"), "api_key": strings.TrimSpace(string(key)), "api_secret": string(secret)})
}
func validDattoURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && strings.HasSuffix(strings.ToLower(u.Hostname()), ".centrastage.net")
}
func wipeDattoCredential(value []byte) {
	for i := range value {
		value[i] = 0
	}
}
