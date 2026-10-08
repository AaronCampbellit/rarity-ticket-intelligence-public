// Package servicekeys manages attributable integration bearer credentials.
// Plaintext key material is returned once and never crosses the repository
// boundary.
package servicekeys

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

var (
	ErrInvalidKey     = errors.New("invalid service key")
	ErrInvalidCommand = errors.New("invalid service key command")
)

type Record struct {
	ID           string
	MSPID        string
	ClientID     string
	Name         string
	Prefix       string
	TokenHash    [32]byte
	Capabilities []string
	DataScopes   []string
	CreatedBy    string
	CreatedAt    time.Time
	ExpiresAt    time.Time
	RevokedAt    *time.Time
}

type Issued struct {
	Record Record
	Token  string
}

type Authenticated struct {
	KeyID        string
	MSPID        string
	ClientID     string
	Name         string
	Capabilities []string
	DataScopes   []string
	ExpiresAt    time.Time
}

func (a Authenticated) Principal() authorization.Principal {
	return authorization.Principal{
		ID: a.KeyID,
		Scope: scope.Principal{
			MSPID:    a.MSPID,
			ClientID: a.ClientID,
		},
		Capabilities: authorization.NewCapabilitySet(a.Capabilities...),
		DataScopes:   authorization.NewCapabilitySet(a.DataScopes...),
	}
}

func (a Authenticated) AllowsData(dataScope string) bool {
	for _, allowed := range a.DataScopes {
		if allowed == dataScope {
			return true
		}
	}
	return false
}

type IssueCommand struct {
	Principal    authorization.Principal
	Target       scope.Target
	Name         string
	Capabilities []string
	DataScopes   []string
	TTL          time.Duration
	ActorID      string
	Source       string
}

type RotateCommand struct {
	Principal authorization.Principal
	Target    scope.Target
	KeyID     string
	TTL       time.Duration
	ActorID   string
	Reason    string
	Source    string
}

type RevokeCommand struct {
	Principal authorization.Principal
	Target    scope.Target
	KeyID     string
	ActorID   string
	Reason    string
	Source    string
}

type IssueMutation struct {
	Record Record
	Audit  mutation.AuditRecord
	Event  mutation.EventRecord
}

type RevokeMutation struct {
	KeyID     string
	Prefix    string
	RevokedAt time.Time
	Audit     mutation.AuditRecord
	Event     mutation.EventRecord
}

type RotateMutation struct {
	OldKeyID    string
	OldPrefix   string
	Replacement Record
	RotatedAt   time.Time
	Audit       mutation.AuditRecord
	Event       mutation.EventRecord
}

type Repository interface {
	Create(context.Context, IssueMutation) error
	List(context.Context, scope.Target) ([]Record, error)
	FindByPrefix(context.Context, string) (Record, error)
	FindByID(context.Context, scope.Target, string) (Record, error)
	Revoke(context.Context, RevokeMutation) error
	Rotate(context.Context, RotateMutation) error
}

func (s *Service) List(
	ctx context.Context,
	principal authorization.Principal,
	target scope.Target,
) ([]Record, error) {
	target = managedTarget(principal, target)
	if s.repository == nil || target.MSPID == "" {
		return nil, ErrInvalidCommand
	}
	if err := authorization.Authorize(principal, "service_key.manage", target); err != nil {
		return nil, err
	}
	return s.repository.List(ctx, target)
}

type Service struct {
	repository  Repository
	now         func() time.Time
	randomBytes func(int) ([]byte, error)
	newID       func() string
}

func NewService(
	repository Repository,
	now func() time.Time,
	randomBytes func(int) ([]byte, error),
	newID func() string,
) *Service {
	if randomBytes == nil {
		randomBytes = cryptoRandomBytes
	}
	return &Service{
		repository: repository, now: now, randomBytes: randomBytes, newID: newID,
	}
}

func (s *Service) Issue(ctx context.Context, command IssueCommand) (Issued, error) {
	if !validIssue(command) || s.repository == nil || s.now == nil || s.newID == nil {
		return Issued{}, ErrInvalidCommand
	}
	target := managedTarget(command.Principal, command.Target)
	if target.MSPID == "" || command.ActorID != command.Principal.ID {
		return Issued{}, ErrInvalidCommand
	}
	if err := authorization.Authorize(command.Principal, "service_key.manage", target); err != nil {
		return Issued{}, err
	}
	record, token, err := s.newRecord(command, target)
	if err != nil {
		return Issued{}, err
	}
	now := record.CreatedAt
	auditID, eventID, correlationID := s.newID(), s.newID(), s.newID()
	accepted := IssueMutation{
		Record: record,
		Audit: mutation.AuditRecord{
			ID: auditID, OccurredAt: now, MSPID: record.MSPID, ClientID: record.ClientID,
			ActorType: "technician", ActorID: command.ActorID, Action: "service_key.created",
			SubjectType: "service_key", SubjectID: record.ID, SubjectVersion: 1,
			Source: command.Source, CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: eventID, EventType: "service_key.created", SchemaVersion: 1,
			OccurredAt: now, MSPID: record.MSPID, ClientID: record.ClientID,
			ActorType: "technician", ActorID: command.ActorID,
			SubjectType: "service_key", SubjectID: record.ID, SubjectVersion: 1,
			Source: command.Source, CorrelationID: correlationID,
		},
	}
	if err := s.repository.Create(ctx, accepted); err != nil {
		return Issued{}, err
	}
	return Issued{Record: record, Token: token}, nil
}

func (s *Service) Authenticate(ctx context.Context, token string) (Authenticated, error) {
	prefix, ok := tokenPrefix(token)
	if !ok || s.repository == nil || s.now == nil {
		return Authenticated{}, ErrInvalidKey
	}
	record, err := s.repository.FindByPrefix(ctx, prefix)
	digest := HashToken(token)
	if err != nil || record.RevokedAt != nil || !record.ExpiresAt.After(s.now()) ||
		subtle.ConstantTimeCompare(record.TokenHash[:], digest[:]) != 1 {
		return Authenticated{}, ErrInvalidKey
	}
	return authenticatedFrom(record), nil
}

func (s *Service) Rotate(ctx context.Context, command RotateCommand) (Issued, error) {
	target := managedTarget(command.Principal, command.Target)
	if !validManagedCommand(
		command.Principal, target, command.KeyID, command.ActorID,
		command.Reason, command.Source,
	) || command.TTL <= 0 || command.TTL > 365*24*time.Hour {
		return Issued{}, ErrInvalidCommand
	}
	if err := authorization.Authorize(command.Principal, "service_key.manage", target); err != nil {
		return Issued{}, err
	}
	current, err := s.recordForID(ctx, target, command.KeyID)
	if err != nil {
		return Issued{}, err
	}
	replacement, token, err := s.newRecord(IssueCommand{
		Name:         current.Name,
		Capabilities: current.Capabilities, DataScopes: current.DataScopes,
		TTL: command.TTL, ActorID: command.ActorID, Source: command.Source,
	}, scope.Target{MSPID: current.MSPID, ClientID: current.ClientID})
	if err != nil {
		return Issued{}, err
	}
	now := replacement.CreatedAt
	auditID, eventID, correlationID := s.newID(), s.newID(), s.newID()
	accepted := RotateMutation{
		OldKeyID: current.ID, OldPrefix: current.Prefix, Replacement: replacement, RotatedAt: now,
		Audit: mutation.AuditRecord{
			ID: auditID, OccurredAt: now, MSPID: current.MSPID, ClientID: current.ClientID,
			ActorType: "technician", ActorID: command.ActorID, Action: "service_key.rotated",
			SubjectType: "service_key", SubjectID: current.ID, SubjectVersion: 2,
			Source: command.Source, Reason: strings.TrimSpace(command.Reason),
			CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: eventID, EventType: "service_key.rotated", SchemaVersion: 1,
			OccurredAt: now, MSPID: current.MSPID, ClientID: current.ClientID,
			ActorType: "technician", ActorID: command.ActorID,
			SubjectType: "service_key", SubjectID: current.ID, SubjectVersion: 2,
			Source: command.Source, CorrelationID: correlationID,
		},
	}
	if err := s.repository.Rotate(ctx, accepted); err != nil {
		return Issued{}, err
	}
	return Issued{Record: replacement, Token: token}, nil
}

func (s *Service) Revoke(ctx context.Context, command RevokeCommand) error {
	target := managedTarget(command.Principal, command.Target)
	if !validManagedCommand(
		command.Principal, target, command.KeyID, command.ActorID,
		command.Reason, command.Source,
	) {
		return ErrInvalidCommand
	}
	if err := authorization.Authorize(command.Principal, "service_key.manage", target); err != nil {
		return err
	}
	record, err := s.recordForID(ctx, target, command.KeyID)
	if err != nil {
		return err
	}
	now := s.now().UTC()
	auditID, eventID, correlationID := s.newID(), s.newID(), s.newID()
	return s.repository.Revoke(ctx, RevokeMutation{
		KeyID: record.ID, Prefix: record.Prefix, RevokedAt: now,
		Audit: mutation.AuditRecord{
			ID: auditID, OccurredAt: now, MSPID: record.MSPID, ClientID: record.ClientID,
			ActorType: "technician", ActorID: command.ActorID, Action: "service_key.revoked",
			SubjectType: "service_key", SubjectID: record.ID, SubjectVersion: 2,
			Source: command.Source, Reason: strings.TrimSpace(command.Reason),
			CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: eventID, EventType: "service_key.revoked", SchemaVersion: 1,
			OccurredAt: now, MSPID: record.MSPID, ClientID: record.ClientID,
			ActorType: "technician", ActorID: command.ActorID,
			SubjectType: "service_key", SubjectID: record.ID, SubjectVersion: 2,
			Source: command.Source, CorrelationID: correlationID,
		},
	})
}

func (s *Service) newRecord(
	command IssueCommand,
	target scope.Target,
) (Record, string, error) {
	secret, err := s.randomBytes(32)
	if err != nil {
		return Record{}, "", err
	}
	id := s.newID()
	prefixDigest := sha256.Sum256([]byte(id))
	prefix := base64.RawURLEncoding.EncodeToString(prefixDigest[:9])
	token := "rsk_" + prefix + "_" + base64.RawURLEncoding.EncodeToString(secret)
	now := s.now().UTC()
	return Record{
		ID: id, MSPID: target.MSPID, ClientID: target.ClientID,
		Name: strings.TrimSpace(command.Name), Prefix: prefix, TokenHash: HashToken(token),
		Capabilities: normalized(command.Capabilities), DataScopes: normalized(command.DataScopes),
		CreatedBy: strings.TrimSpace(command.ActorID), CreatedAt: now, ExpiresAt: now.Add(command.TTL),
	}, token, nil
}

func (s *Service) recordForID(
	ctx context.Context,
	target scope.Target,
	keyID string,
) (Record, error) {
	if s.repository == nil || s.now == nil || s.newID == nil {
		return Record{}, ErrInvalidKey
	}
	record, err := s.repository.FindByID(ctx, target, keyID)
	if err != nil || record.RevokedAt != nil {
		return Record{}, ErrInvalidKey
	}
	return record, nil
}

func authenticatedFrom(record Record) Authenticated {
	return Authenticated{
		KeyID: record.ID, MSPID: record.MSPID, ClientID: record.ClientID, Name: record.Name,
		Capabilities: append([]string(nil), record.Capabilities...),
		DataScopes:   append([]string(nil), record.DataScopes...), ExpiresAt: record.ExpiresAt,
	}
}

func validIssue(command IssueCommand) bool {
	return strings.TrimSpace(command.Name) != "" &&
		strings.TrimSpace(command.ActorID) != "" &&
		strings.TrimSpace(command.Source) != "" &&
		command.TTL > 0 && command.TTL <= 365*24*time.Hour &&
		len(normalized(command.Capabilities)) > 0 &&
		len(normalized(command.DataScopes)) > 0
}

func managedTarget(
	principal authorization.Principal,
	target scope.Target,
) scope.Target {
	if target.MSPID == "" {
		return scope.Target{
			MSPID: principal.Scope.MSPID, ClientID: principal.Scope.ClientID,
		}
	}
	return target
}

func validManagedCommand(
	principal authorization.Principal,
	target scope.Target,
	keyID, actorID, reason, source string,
) bool {
	return target.MSPID != "" &&
		strings.TrimSpace(keyID) != "" &&
		strings.TrimSpace(actorID) != "" &&
		actorID == principal.ID &&
		strings.TrimSpace(reason) != "" &&
		strings.TrimSpace(source) != ""
}

func normalized(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func tokenPrefix(token string) (string, bool) {
	parts := strings.Split(token, "_")
	returnValue := len(parts) == 3 && parts[0] == "rsk" &&
		parts[1] != "" && parts[2] != ""
	if !returnValue {
		return "", false
	}
	return parts[1], true
}

func HashToken(token string) [32]byte {
	return sha256.Sum256([]byte(token))
}

func cryptoRandomBytes(size int) ([]byte, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return nil, err
	}
	return value, nil
}
