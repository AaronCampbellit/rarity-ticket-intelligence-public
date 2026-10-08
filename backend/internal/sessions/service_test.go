package sessions

import (
	"context"
	"errors"
	"testing"
	"time"
)

type memoryStore struct {
	created Record
	records map[[32]byte]Record
}

func (s *memoryStore) Create(_ context.Context, record Record) error {
	s.created = record
	if s.records == nil {
		s.records = map[[32]byte]Record{}
	}
	s.records[record.TokenHash] = record
	return nil
}

func (s *memoryStore) FindByTokenHash(_ context.Context, hash [32]byte) (Record, error) {
	record, ok := s.records[hash]
	if !ok {
		return Record{}, ErrInvalidSession
	}
	return record, nil
}
func (s *memoryStore) Touch(
	_ context.Context,
	hash [32]byte,
	lastSeenAt time.Time,
	idleExpiresAt time.Time,
) error {
	record, ok := s.records[hash]
	if !ok {
		return ErrInvalidSession
	}
	record.LastSeenAt = lastSeenAt
	record.IdleExpiresAt = idleExpiresAt
	s.records[hash] = record
	return nil
}
func (s *memoryStore) Rotate(
	_ context.Context,
	oldHash [32]byte,
	newHash [32]byte,
	rotatedAt time.Time,
	idleExpiresAt time.Time,
) error {
	record, ok := s.records[oldHash]
	if !ok || record.RevokedAt != nil || !record.ExpiresAt.After(rotatedAt) {
		return ErrInvalidSession
	}
	delete(s.records, oldHash)
	record.TokenHash = newHash
	record.RotatedAt = rotatedAt
	record.LastSeenAt = rotatedAt
	record.IdleExpiresAt = idleExpiresAt
	s.records[newHash] = record
	return nil
}
func (s *memoryStore) RevokeByTokenHash(_ context.Context, hash [32]byte, at time.Time, actor string) error {
	record, ok := s.records[hash]
	if !ok || actor == "" {
		return ErrInvalidSession
	}
	record.RevokedAt = &at
	s.records[hash] = record
	return nil
}
func (s *memoryStore) ListActive(
	_ context.Context,
	mspID string,
	technicianID string,
	now time.Time,
) ([]Record, error) {
	var found []Record
	for _, record := range s.records {
		if record.MSPID == mspID && record.TechnicianID == technicianID &&
			record.RevokedAt == nil && record.ExpiresAt.After(now) &&
			(record.IdleExpiresAt.IsZero() || record.IdleExpiresAt.After(now)) {
			found = append(found, record)
		}
	}
	return found, nil
}
func (s *memoryStore) RevokeByID(
	_ context.Context,
	mspID string,
	technicianID string,
	sessionID string,
	at time.Time,
	_ string,
) error {
	for hash, record := range s.records {
		if record.ID == sessionID && record.MSPID == mspID &&
			record.TechnicianID == technicianID {
			record.RevokedAt = &at
			s.records[hash] = record
			return nil
		}
	}
	return ErrInvalidSession
}

func TestIssueStoresOnlyTokenHashAndReturnsBearerSecretOnce(t *testing.T) {
	store := &memoryStore{}
	now := time.Date(2026, time.July, 29, 14, 0, 0, 0, time.UTC)
	service := NewService(store, func() time.Time { return now }, func(size int) ([]byte, error) {
		return make([]byte, size), nil
	}, func() string { return "session-id" })

	issued, err := service.Issue(context.Background(), IssueCommand{
		MSPID: "msp-id", TechnicianID: "technician-id", TTL: 8 * time.Hour,
		UserAgent: "Rarity Browser/1.0", RemoteIP: "192.0.2.47",
	})
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	if issued.Token == "" {
		t.Fatal("Issue() returned an empty bearer token")
	}
	if store.created.ID != "session-id" || store.created.ExpiresAt != now.Add(8*time.Hour) {
		t.Fatalf("unexpected stored record: %+v", store.created)
	}
	if string(store.created.TokenHash[:]) == issued.Token {
		t.Fatal("session store retained the plaintext bearer token")
	}
	if store.created.UserAgentHash == ([32]byte{}) ||
		store.created.IPPrefix != "192.0.2.0/24" {
		t.Fatalf("session device evidence was not privacy bounded: %+v", store.created)
	}
}

func TestAuthenticateRejectsUnknownExpiredAndRevokedSessions(t *testing.T) {
	now := time.Date(2026, time.July, 29, 14, 0, 0, 0, time.UTC)
	store := &memoryStore{records: map[[32]byte]Record{}}
	service := NewService(store, func() time.Time { return now }, nil, nil)

	if _, err := service.Authenticate(context.Background(), "unknown"); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("unknown token error = %v", err)
	}

	expiredHash := HashToken("expired")
	store.records[expiredHash] = Record{
		ID: "expired", MSPID: "msp-id", TechnicianID: "tech-id",
		TokenHash: expiredHash, ExpiresAt: now,
	}
	if _, err := service.Authenticate(context.Background(), "expired"); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("expired token error = %v", err)
	}

	revokedAt := now.Add(-time.Minute)
	revokedHash := HashToken("revoked")
	store.records[revokedHash] = Record{
		ID: "revoked", MSPID: "msp-id", TechnicianID: "tech-id",
		TokenHash: revokedHash, ExpiresAt: now.Add(time.Hour), RevokedAt: &revokedAt,
	}
	if _, err := service.Authenticate(context.Background(), "revoked"); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("revoked token error = %v", err)
	}
}

func TestAuthenticateReturnsTrustedSessionScope(t *testing.T) {
	now := time.Date(2026, time.July, 29, 14, 0, 0, 0, time.UTC)
	hash := HashToken("valid")
	store := &memoryStore{records: map[[32]byte]Record{
		hash: {
			ID: "session-id", MSPID: "msp-id", TechnicianID: "tech-id",
			TokenHash: hash, ExpiresAt: now.Add(time.Hour),
		},
	}}
	service := NewService(store, func() time.Time { return now }, nil, nil)

	session, err := service.Authenticate(context.Background(), "valid")
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if session.MSPID != "msp-id" || session.TechnicianID != "tech-id" {
		t.Fatalf("unexpected authenticated session: %+v", session)
	}
}

func TestAuthenticateRejectsIdleSessionAndExtendsActiveIdleDeadline(t *testing.T) {
	now := time.Date(2026, time.July, 29, 14, 0, 0, 0, time.UTC)
	idleHash := HashToken("idle")
	activeHash := HashToken("active")
	store := &memoryStore{records: map[[32]byte]Record{
		idleHash: {
			ID: "idle", MSPID: "msp-id", TechnicianID: "tech-id",
			TokenHash: idleHash, ExpiresAt: now.Add(time.Hour),
			IdleExpiresAt: now,
		},
		activeHash: {
			ID: "active", MSPID: "msp-id", TechnicianID: "tech-id",
			TokenHash: activeHash, ExpiresAt: now.Add(time.Hour),
			IdleExpiresAt: now.Add(time.Minute), IdleTimeout: 15 * time.Minute,
		},
	}}
	service := NewService(store, func() time.Time { return now }, nil, nil)

	if _, err := service.Authenticate(context.Background(), "idle"); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("idle session error=%v", err)
	}
	if _, err := service.Authenticate(context.Background(), "active"); err != nil {
		t.Fatalf("active session error=%v", err)
	}
	if got := store.records[activeHash].IdleExpiresAt; !got.Equal(now.Add(15 * time.Minute)) {
		t.Fatalf("idle deadline was not extended: %v", got)
	}
}

func TestRevokeInvalidatesOpaqueSession(t *testing.T) {
	now := time.Date(2026, time.July, 29, 14, 0, 0, 0, time.UTC)
	hash := HashToken("valid")
	store := &memoryStore{records: map[[32]byte]Record{
		hash: {ID: "session-id", MSPID: "msp-id", TechnicianID: "tech-id",
			TokenHash: hash, ExpiresAt: now.Add(time.Hour)},
	}}
	service := NewService(store, func() time.Time { return now }, nil, nil)
	if err := service.Revoke(context.Background(), "valid", "tech-id"); err != nil {
		t.Fatalf("Revoke() error=%v", err)
	}
	if _, err := service.Authenticate(context.Background(), "valid"); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("revoked token error=%v", err)
	}
}

func TestRotateReplacesOpaqueTokenAndPreservesAbsoluteExpiry(t *testing.T) {
	now := time.Date(2026, time.July, 29, 14, 0, 0, 0, time.UTC)
	oldHash := HashToken("old-token")
	absoluteExpiry := now.Add(45 * time.Minute)
	store := &memoryStore{records: map[[32]byte]Record{
		oldHash: {
			ID: "session-id", MSPID: "msp-id", TechnicianID: "tech-id",
			TokenHash: oldHash, CreatedAt: now.Add(-time.Hour),
			RotatedAt: now.Add(-RotationInterval), LastSeenAt: now.Add(-time.Minute),
			IdleExpiresAt: now.Add(5 * time.Minute), IdleTimeout: 10 * time.Minute,
			ExpiresAt: absoluteExpiry,
		},
	}}
	service := NewService(store, func() time.Time { return now }, func(int) ([]byte, error) {
		return []byte("01234567890123456789012345678901"), nil
	}, func() string { return "unused" })

	rotated, err := service.Rotate(context.Background(), "old-token")
	if err != nil || rotated.Token == "" || rotated.Record.ExpiresAt != absoluteExpiry {
		t.Fatalf("rotated=%+v err=%v", rotated, err)
	}
	if _, err := service.Authenticate(context.Background(), "old-token"); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("old token replay error=%v", err)
	}
	if _, err := service.Authenticate(context.Background(), rotated.Token); err != nil {
		t.Fatalf("replacement token error=%v", err)
	}
}

func TestRotateHonorsCadenceWithoutChangingToken(t *testing.T) {
	now := time.Date(2026, time.July, 29, 14, 0, 0, 0, time.UTC)
	hash := HashToken("current-token")
	store := &memoryStore{records: map[[32]byte]Record{
		hash: {
			ID: "session-id", TokenHash: hash, CreatedAt: now.Add(-time.Hour),
			RotatedAt: now.Add(-time.Minute), IdleExpiresAt: now.Add(time.Minute),
			ExpiresAt: now.Add(time.Hour),
		},
	}}
	service := NewService(store, func() time.Time { return now }, nil, nil)
	rotated, err := service.Rotate(context.Background(), "current-token")
	if err != nil || rotated.Token != "" || store.records[hash].TokenHash != hash {
		t.Fatalf("rotated=%+v err=%v", rotated, err)
	}
}

func TestListAndRevokeOwnActiveSessions(t *testing.T) {
	now := time.Date(2026, time.July, 29, 14, 0, 0, 0, time.UTC)
	currentHash := HashToken("current")
	otherHash := HashToken("other")
	store := &memoryStore{records: map[[32]byte]Record{
		currentHash: {
			ID: "current-id", MSPID: "msp-id", TechnicianID: "tech-id",
			TokenHash: currentHash, ExpiresAt: now.Add(time.Hour),
		},
		otherHash: {
			ID: "other-id", MSPID: "msp-id", TechnicianID: "tech-id",
			TokenHash: otherHash, ExpiresAt: now.Add(time.Hour),
		},
	}}
	service := NewService(store, func() time.Time { return now }, nil, nil)

	found, err := service.ListActive(context.Background(), "current")
	if err != nil || len(found) != 2 {
		t.Fatalf("active sessions=%+v err=%v", found, err)
	}
	if err := service.RevokeSession(
		context.Background(), "current", "other-id",
	); err != nil {
		t.Fatal(err)
	}
	if store.records[otherHash].RevokedAt == nil {
		t.Fatal("selected session was not revoked")
	}
}
