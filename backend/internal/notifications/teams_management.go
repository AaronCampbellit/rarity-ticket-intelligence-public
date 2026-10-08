package notifications

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

var (
	ErrInvalidTeamsConnection     = errors.New("invalid Teams connection management request")
	ErrTeamsConnectionTestFailed  = errors.New("Teams connection test failed")
	ErrTeamsCredentialUnavailable = errors.New("Teams connection credential unavailable")
)

const teamsConnectionTestFailedCode = "connection_test_failed"

// ManagedTeamsConnection is deliberately credential-free.  It is safe to
// serialize for management views; webhook URL material is write-only.
type ManagedTeamsConnection struct {
	ID                   string      `json:"id"`
	MSPID                string      `json:"-"`
	ClientID             string      `json:"client_id,omitempty"`
	Name                 string      `json:"name"`
	CredentialConfigured bool        `json:"credential_configured"`
	Enabled              bool        `json:"enabled"`
	Health               TeamsHealth `json:"health"`
	LastTestedAt         *time.Time  `json:"last_tested_at,omitempty"`
	LastSuccessAt        *time.Time  `json:"last_success_at,omitempty"`
	LastErrorCode        string      `json:"last_error_code,omitempty"`
	Version              int64       `json:"version"`
}

type TeamsHealth string

const (
	TeamsHealthPending  TeamsHealth = "pending"
	TeamsHealthHealthy  TeamsHealth = "healthy"
	TeamsHealthDegraded TeamsHealth = "degraded"
	TeamsHealthFailed   TeamsHealth = "failed"
	TeamsHealthDisabled TeamsHealth = "disabled"
)

type TeamsConnectionMutation struct {
	Connection          ManagedTeamsConnection
	ExpectedVersion     int64
	PlaintextWebhookURL []byte `json:"-"`
	Audit               mutation.AuditRecord
	Event               mutation.EventRecord
}

type TeamsConnectionHealthMutation struct {
	ConnectionID    string
	MSPID           string
	ExpectedVersion int64
	Health          TeamsHealth
	TestedAt        time.Time
	SucceededAt     *time.Time
	LastErrorCode   string
	Audit           mutation.AuditRecord
	Event           mutation.EventRecord
}

// TeamsConnectionManagementRepository owns plaintext webhook buffers during
// mutation calls and must overwrite them before returning.  UseWebhook opens
// the exact persisted snapshot only inside the supplied callback.
type TeamsConnectionManagementRepository interface {
	CreateTeamsConnection(context.Context, TeamsConnectionMutation) error
	ListTeamsConnections(context.Context, scope.Target) ([]ManagedTeamsConnection, error)
	GetTeamsConnection(context.Context, scope.Target, string) (ManagedTeamsConnection, error)
	UpdateTeamsConnectionMetadata(context.Context, TeamsConnectionMutation) error
	ReplaceTeamsConnectionCredential(context.Context, TeamsConnectionMutation) error
	SetTeamsConnectionEnabled(context.Context, TeamsConnectionMutation) error
	RecordTeamsConnectionHealth(context.Context, TeamsConnectionHealthMutation) error
	UseTeamsWebhook(context.Context, scope.Target, ManagedTeamsConnection, func([]byte) error) error
}

// TeamsConnectionTester is an intentionally narrow I/O seam.  HTTP transport
// hardening remains outside this lifecycle slice.
type TeamsConnectionTester interface {
	TestTeamsWebhook(context.Context, ManagedTeamsConnection, []byte) error
}

type TeamsConnectionManagementService struct {
	repository TeamsConnectionManagementRepository
	tester     TeamsConnectionTester
	now        func() time.Time
	newID      func() string
}

func NewTeamsConnectionManagementService(repository TeamsConnectionManagementRepository, tester TeamsConnectionTester, now func() time.Time, newID func() string) *TeamsConnectionManagementService {
	return &TeamsConnectionManagementService{repository: repository, tester: tester, now: now, newID: newID}
}

type CreateTeamsConnectionCommand struct {
	Principal           authorization.Principal
	Target              scope.Target
	ClientID            string
	Name                string
	PlaintextWebhookURL []byte `json:"-"`
	Reason              string
}

type ListTeamsConnectionsCommand struct {
	Principal authorization.Principal
	Target    scope.Target
}

type UpdateTeamsConnectionMetadataCommand struct {
	Principal       authorization.Principal
	Target          scope.Target
	ID              string
	Name            string
	ExpectedVersion int64
	Reason          string
}

type ReplaceTeamsConnectionCredentialCommand struct {
	Principal           authorization.Principal
	Target              scope.Target
	ID                  string
	ExpectedVersion     int64
	PlaintextWebhookURL []byte `json:"-"`
	Reason              string
}

type SetTeamsConnectionEnabledCommand struct {
	Principal       authorization.Principal
	Target          scope.Target
	ID              string
	ExpectedVersion int64
	Enabled         bool
	Reason          string
}

type TestTeamsConnectionCommand struct {
	Principal authorization.Principal
	Target    scope.Target
	ID        string
	Reason    string
}

func (s *TeamsConnectionManagementService) Create(ctx context.Context, command CreateTeamsConnectionCommand) (ManagedTeamsConnection, error) {
	target, err := s.authorize(command.Principal, command.Target)
	if err != nil {
		return ManagedTeamsConnection{}, err
	}
	if !validTeamsReason(command.Reason) || !validTeamsName(command.Name) || !validTeamsWebhook(command.PlaintextWebhookURL) || s.newID == nil || s.now == nil {
		return ManagedTeamsConnection{}, ErrInvalidTeamsConnection
	}
	clientID := strings.TrimSpace(command.ClientID)
	if clientID != target.ClientID {
		return ManagedTeamsConnection{}, scope.ErrNotFound
	}
	now := s.now().UTC()
	connection := ManagedTeamsConnection{ID: s.newID(), MSPID: target.MSPID, ClientID: clientID, Name: strings.TrimSpace(command.Name), CredentialConfigured: true, Enabled: false, Health: TeamsHealthPending, Version: 1}
	if err := validateManagedTeamsConnection(connection); err != nil {
		return ManagedTeamsConnection{}, err
	}
	accepted := TeamsConnectionMutation{Connection: connection, PlaintextWebhookURL: cloneTeamsBytes(command.PlaintextWebhookURL)}
	accepted.Audit, accepted.Event = s.facts(command.Principal, target, now, "teams.connection.created", connection.ID, connection.Version, command.Reason)
	defer wipeTeamsPlaintext(accepted.PlaintextWebhookURL)
	if err := s.repository.CreateTeamsConnection(ctx, accepted); err != nil {
		return ManagedTeamsConnection{}, err
	}
	return connection, nil
}

func (s *TeamsConnectionManagementService) List(ctx context.Context, command ListTeamsConnectionsCommand) ([]ManagedTeamsConnection, error) {
	target, err := s.authorize(command.Principal, command.Target)
	if err != nil {
		return nil, err
	}
	connections, err := s.repository.ListTeamsConnections(ctx, target)
	if err != nil {
		return nil, err
	}
	return append([]ManagedTeamsConnection(nil), connections...), nil
}

func (s *TeamsConnectionManagementService) UpdateMetadata(ctx context.Context, command UpdateTeamsConnectionMetadataCommand) (ManagedTeamsConnection, error) {
	target, err := s.authorize(command.Principal, command.Target)
	if err != nil {
		return ManagedTeamsConnection{}, err
	}
	if !validTeamsID(command.ID) || command.ExpectedVersion < 1 || !validTeamsName(command.Name) || !validTeamsReason(command.Reason) {
		return ManagedTeamsConnection{}, ErrInvalidTeamsConnection
	}
	connection, err := s.repository.GetTeamsConnection(ctx, target, strings.TrimSpace(command.ID))
	if err != nil {
		return ManagedTeamsConnection{}, err
	}
	if err := object.RequireVersion(connection.Version, command.ExpectedVersion); err != nil {
		return ManagedTeamsConnection{}, err
	}
	connection.Name, connection.Version = strings.TrimSpace(command.Name), connection.Version+1
	now := s.now().UTC()
	accepted := TeamsConnectionMutation{Connection: connection, ExpectedVersion: command.ExpectedVersion}
	accepted.Audit, accepted.Event = s.facts(command.Principal, target, now, "teams.connection.updated", connection.ID, connection.Version, command.Reason)
	if err := s.repository.UpdateTeamsConnectionMetadata(ctx, accepted); err != nil {
		return ManagedTeamsConnection{}, err
	}
	return connection, nil
}

func (s *TeamsConnectionManagementService) ReplaceCredential(ctx context.Context, command ReplaceTeamsConnectionCredentialCommand) (ManagedTeamsConnection, error) {
	target, err := s.authorize(command.Principal, command.Target)
	if err != nil {
		return ManagedTeamsConnection{}, err
	}
	if !validTeamsID(command.ID) || command.ExpectedVersion < 1 || !validTeamsWebhook(command.PlaintextWebhookURL) || !validTeamsReason(command.Reason) {
		return ManagedTeamsConnection{}, ErrInvalidTeamsConnection
	}
	connection, err := s.repository.GetTeamsConnection(ctx, target, strings.TrimSpace(command.ID))
	if err != nil {
		return ManagedTeamsConnection{}, err
	}
	if err := object.RequireVersion(connection.Version, command.ExpectedVersion); err != nil {
		return ManagedTeamsConnection{}, err
	}
	connection.Version, connection.CredentialConfigured = connection.Version+1, true
	now := s.now().UTC()
	accepted := TeamsConnectionMutation{Connection: connection, ExpectedVersion: command.ExpectedVersion, PlaintextWebhookURL: cloneTeamsBytes(command.PlaintextWebhookURL)}
	accepted.Audit, accepted.Event = s.facts(command.Principal, target, now, "teams.connection.credential_replaced", connection.ID, connection.Version, command.Reason)
	defer wipeTeamsPlaintext(accepted.PlaintextWebhookURL)
	if err := s.repository.ReplaceTeamsConnectionCredential(ctx, accepted); err != nil {
		return ManagedTeamsConnection{}, err
	}
	return connection, nil
}

func (s *TeamsConnectionManagementService) SetEnabled(ctx context.Context, command SetTeamsConnectionEnabledCommand) (ManagedTeamsConnection, error) {
	target, err := s.authorize(command.Principal, command.Target)
	if err != nil {
		return ManagedTeamsConnection{}, err
	}
	if !validTeamsID(command.ID) || command.ExpectedVersion < 1 || !validTeamsReason(command.Reason) {
		return ManagedTeamsConnection{}, ErrInvalidTeamsConnection
	}
	connection, err := s.repository.GetTeamsConnection(ctx, target, strings.TrimSpace(command.ID))
	if err != nil {
		return ManagedTeamsConnection{}, err
	}
	if err := object.RequireVersion(connection.Version, command.ExpectedVersion); err != nil {
		return ManagedTeamsConnection{}, err
	}
	connection.Enabled, connection.Version = command.Enabled, connection.Version+1
	if command.Enabled && connection.Health == TeamsHealthDisabled {
		connection.Health = TeamsHealthPending
	}
	if !command.Enabled {
		connection.Health = TeamsHealthDisabled
	}
	now := s.now().UTC()
	action := "teams.connection.disabled"
	if command.Enabled {
		action = "teams.connection.enabled"
	}
	accepted := TeamsConnectionMutation{Connection: connection, ExpectedVersion: command.ExpectedVersion}
	accepted.Audit, accepted.Event = s.facts(command.Principal, target, now, action, connection.ID, connection.Version, command.Reason)
	if err := s.repository.SetTeamsConnectionEnabled(ctx, accepted); err != nil {
		return ManagedTeamsConnection{}, err
	}
	return connection, nil
}

func (s *TeamsConnectionManagementService) Test(ctx context.Context, command TestTeamsConnectionCommand) (ManagedTeamsConnection, error) {
	target, err := s.authorize(command.Principal, command.Target)
	if err != nil {
		return ManagedTeamsConnection{}, err
	}
	if !validTeamsID(command.ID) || !validTeamsReason(command.Reason) || s.tester == nil || s.now == nil {
		return ManagedTeamsConnection{}, ErrInvalidTeamsConnection
	}
	connection, err := s.repository.GetTeamsConnection(ctx, target, strings.TrimSpace(command.ID))
	if err != nil {
		return ManagedTeamsConnection{}, err
	}
	now := s.now().UTC()
	health := TeamsConnectionHealthMutation{ConnectionID: connection.ID, MSPID: target.MSPID, ExpectedVersion: connection.Version, Health: TeamsHealthHealthy, TestedAt: now, SucceededAt: &now}
	if !connection.CredentialConfigured || s.repository.UseTeamsWebhook(ctx, target, connection, func(webhook []byte) error { return s.tester.TestTeamsWebhook(ctx, connection, webhook) }) != nil {
		health.Health, health.SucceededAt, health.LastErrorCode = TeamsHealthFailed, nil, teamsConnectionTestFailedCode
	}
	connection.Version++
	connection.Health, connection.LastTestedAt, connection.LastSuccessAt, connection.LastErrorCode = health.Health, &now, health.SucceededAt, health.LastErrorCode
	health.Audit, health.Event = s.facts(command.Principal, target, now, "teams.connection.tested", connection.ID, connection.Version, command.Reason)
	if err := s.repository.RecordTeamsConnectionHealth(ctx, health); err != nil {
		return ManagedTeamsConnection{}, err
	}
	return connection, nil
}

func (s *TeamsConnectionManagementService) authorize(principal authorization.Principal, target scope.Target) (scope.Target, error) {
	if target.MSPID == "" {
		target.MSPID, target.ClientID = principal.Scope.MSPID, principal.Scope.ClientID
	}
	if err := authorization.Authorize(principal, "integration.manage", target); err != nil {
		return scope.Target{}, err
	}
	return target, nil
}

func (s *TeamsConnectionManagementService) facts(principal authorization.Principal, target scope.Target, now time.Time, action, id string, version int64, reason string) (mutation.AuditRecord, mutation.EventRecord) {
	correlationID := s.newID()
	return mutation.AuditRecord{ID: s.newID(), OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID, ActorType: "technician", ActorID: principal.ID, Action: action, SubjectType: "teams_connection", SubjectID: id, SubjectVersion: version, Source: "teams.connection.management", Reason: strings.TrimSpace(reason), CorrelationID: correlationID}, mutation.EventRecord{EventID: s.newID(), EventType: action, SchemaVersion: 1, OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID, ActorType: "technician", ActorID: principal.ID, SubjectType: "teams_connection", SubjectID: id, SubjectVersion: version, Source: "teams.connection.management", CorrelationID: correlationID}
}

func validateManagedTeamsConnection(connection ManagedTeamsConnection) error {
	if !validTeamsID(connection.ID) || !validTeamsID(connection.MSPID) || !validTeamsName(connection.Name) || connection.Version < 1 || !validTeamsHealth(connection.Health) {
		return ErrInvalidTeamsConnection
	}
	return nil
}
func validTeamsID(value string) bool { return strings.TrimSpace(value) != "" }
func validTeamsName(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && len(value) <= 120
}
func validTeamsReason(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && len(value) <= 500
}
func validTeamsHealth(value TeamsHealth) bool {
	return value == TeamsHealthPending || value == TeamsHealthHealthy || value == TeamsHealthDegraded || value == TeamsHealthFailed || value == TeamsHealthDisabled
}
func validTeamsWebhook(value []byte) bool {
	endpoint, err := url.Parse(strings.TrimSpace(string(value)))
	return err == nil && endpoint.Scheme == "https" && endpoint.Host != "" && endpoint.User == nil && endpoint.Fragment == ""
}
func cloneTeamsBytes(value []byte) []byte { return append([]byte(nil), value...) }
func wipeTeamsPlaintext(value []byte) {
	for i := range value {
		value[i] = 0
	}
}
