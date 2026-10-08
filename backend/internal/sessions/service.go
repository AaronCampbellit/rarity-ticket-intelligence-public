// Package sessions manages opaque browser session secrets. Only SHA-256 token
// digests cross the persistence boundary.
package sessions

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net"
	"time"
)

var (
	ErrInvalidSession = errors.New("invalid session")
	ErrInvalidIssue   = errors.New("invalid session issue command")
)

const RotationInterval = 15 * time.Minute

type Record struct {
	ID            string
	MSPID         string
	TechnicianID  string
	TokenHash     [32]byte
	CreatedAt     time.Time
	RotatedAt     time.Time
	LastSeenAt    time.Time
	IdleExpiresAt time.Time
	IdleTimeout   time.Duration
	ExpiresAt     time.Time
	RevokedAt     *time.Time
	UserAgentHash [32]byte
	IPPrefix      string
}

type Authenticated struct {
	SessionID    string
	MSPID        string
	TechnicianID string
	ExpiresAt    time.Time
}

type Issued struct {
	Record Record
	Token  string
}

type IssueCommand struct {
	MSPID        string
	TechnicianID string
	TTL          time.Duration
	IdleTimeout  time.Duration
	UserAgent    string
	RemoteIP     string
}

type Store interface {
	Create(context.Context, Record) error
	FindByTokenHash(context.Context, [32]byte) (Record, error)
	Touch(context.Context, [32]byte, time.Time, time.Time) error
	Rotate(context.Context, [32]byte, [32]byte, time.Time, time.Time) error
	RevokeByTokenHash(context.Context, [32]byte, time.Time, string) error
	ListActive(context.Context, string, string, time.Time) ([]Record, error)
	RevokeByID(context.Context, string, string, string, time.Time, string) error
}

func (s *Service) Revoke(ctx context.Context, token, actorID string) error {
	if token == "" || actorID == "" {
		return ErrInvalidSession
	}
	if err := s.store.RevokeByTokenHash(
		ctx, HashToken(token), s.now().UTC(), actorID,
	); err != nil {
		return ErrInvalidSession
	}
	return nil
}

type Service struct {
	store       Store
	now         func() time.Time
	randomBytes func(int) ([]byte, error)
	newID       func() string
}

func NewService(
	store Store,
	now func() time.Time,
	randomBytes func(int) ([]byte, error),
	newID func() string,
) *Service {
	if randomBytes == nil {
		randomBytes = cryptoRandomBytes
	}
	return &Service{store: store, now: now, randomBytes: randomBytes, newID: newID}
}

func (s *Service) Issue(ctx context.Context, command IssueCommand) (Issued, error) {
	if command.MSPID == "" || command.TechnicianID == "" || command.TTL <= 0 || s.newID == nil {
		return Issued{}, ErrInvalidIssue
	}
	secret, err := s.randomBytes(32)
	if err != nil {
		return Issued{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(secret)
	now := s.now().UTC()
	idleTimeout := command.IdleTimeout
	if idleTimeout <= 0 || idleTimeout > command.TTL {
		idleTimeout = command.TTL
	}
	record := Record{
		ID:            s.newID(),
		MSPID:         command.MSPID,
		TechnicianID:  command.TechnicianID,
		TokenHash:     HashToken(token),
		CreatedAt:     now,
		RotatedAt:     now,
		LastSeenAt:    now,
		IdleExpiresAt: now.Add(idleTimeout),
		IdleTimeout:   idleTimeout,
		ExpiresAt:     now.Add(command.TTL),
		UserAgentHash: sha256.Sum256([]byte(command.UserAgent)),
		IPPrefix:      privacyPrefix(command.RemoteIP),
	}
	if err := s.store.Create(ctx, record); err != nil {
		return Issued{}, err
	}
	return Issued{Record: record, Token: token}, nil
}

func (s *Service) Rotate(ctx context.Context, token string) (Issued, error) {
	if token == "" {
		return Issued{}, ErrInvalidSession
	}
	oldHash := HashToken(token)
	record, err := s.store.FindByTokenHash(ctx, oldHash)
	now := s.now().UTC()
	if err != nil || record.RevokedAt != nil || !record.ExpiresAt.After(now) ||
		!record.IdleExpiresAt.IsZero() && !record.IdleExpiresAt.After(now) {
		return Issued{}, ErrInvalidSession
	}
	if record.RotatedAt.IsZero() {
		record.RotatedAt = record.CreatedAt
	}
	if now.Sub(record.RotatedAt) < RotationInterval {
		return Issued{Record: record}, nil
	}
	secret, err := s.randomBytes(32)
	if err != nil {
		return Issued{}, err
	}
	replacement := base64.RawURLEncoding.EncodeToString(secret)
	newHash := HashToken(replacement)
	idleExpiresAt := now.Add(record.IdleTimeout)
	if record.IdleTimeout <= 0 || idleExpiresAt.After(record.ExpiresAt) {
		idleExpiresAt = record.ExpiresAt
	}
	if err := s.store.Rotate(ctx, oldHash, newHash, now, idleExpiresAt); err != nil {
		return Issued{}, ErrInvalidSession
	}
	record.TokenHash = newHash
	record.RotatedAt = now
	record.LastSeenAt = now
	record.IdleExpiresAt = idleExpiresAt
	return Issued{Record: record, Token: replacement}, nil
}

func privacyPrefix(value string) string {
	ip := net.ParseIP(value)
	if ip == nil {
		return ""
	}
	if ipv4 := ip.To4(); ipv4 != nil {
		return (&net.IPNet{
			IP: ipv4.Mask(net.CIDRMask(24, 32)), Mask: net.CIDRMask(24, 32),
		}).String()
	}
	return (&net.IPNet{
		IP: ip.Mask(net.CIDRMask(64, 128)), Mask: net.CIDRMask(64, 128),
	}).String()
}

func (s *Service) Authenticate(ctx context.Context, token string) (Authenticated, error) {
	if token == "" {
		return Authenticated{}, ErrInvalidSession
	}
	record, err := s.store.FindByTokenHash(ctx, HashToken(token))
	now := s.now().UTC()
	if err != nil || record.RevokedAt != nil || !record.ExpiresAt.After(now) ||
		!record.IdleExpiresAt.IsZero() && !record.IdleExpiresAt.After(now) {
		return Authenticated{}, ErrInvalidSession
	}
	if record.IdleTimeout > 0 {
		idleExpiresAt := now.Add(record.IdleTimeout)
		if idleExpiresAt.After(record.ExpiresAt) {
			idleExpiresAt = record.ExpiresAt
		}
		if err := s.store.Touch(ctx, record.TokenHash, now, idleExpiresAt); err != nil {
			return Authenticated{}, ErrInvalidSession
		}
	}
	return Authenticated{
		SessionID:    record.ID,
		MSPID:        record.MSPID,
		TechnicianID: record.TechnicianID,
		ExpiresAt:    record.ExpiresAt,
	}, nil
}

func (s *Service) ListActive(ctx context.Context, token string) ([]Record, error) {
	authenticated, err := s.Authenticate(ctx, token)
	if err != nil {
		return nil, ErrInvalidSession
	}
	records, err := s.store.ListActive(
		ctx, authenticated.MSPID, authenticated.TechnicianID, s.now().UTC(),
	)
	if err != nil {
		return nil, err
	}
	return records, nil
}

func (s *Service) RevokeSession(
	ctx context.Context,
	token string,
	sessionID string,
) error {
	if sessionID == "" {
		return ErrInvalidSession
	}
	authenticated, err := s.Authenticate(ctx, token)
	if err != nil {
		return ErrInvalidSession
	}
	if err := s.store.RevokeByID(
		ctx, authenticated.MSPID, authenticated.TechnicianID,
		sessionID, s.now().UTC(), authenticated.TechnicianID,
	); err != nil {
		return ErrInvalidSession
	}
	return nil
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
