package webhooks

import (
	"context"
	"errors"
	"testing"
	"time"
)

type memoryReplayStore struct {
	claimed map[string]time.Time
}

func (s *memoryReplayStore) Claim(_ context.Context, key string, expiresAt time.Time) (bool, error) {
	if s.claimed == nil {
		s.claimed = map[string]time.Time{}
	}
	if _, exists := s.claimed[key]; exists {
		return false, nil
	}
	s.claimed[key] = expiresAt
	return true, nil
}

func TestSignAndVerifyAuthenticatesExactTimestampEventAndBody(t *testing.T) {
	now := time.Date(2026, time.July, 29, 16, 0, 0, 0, time.UTC)
	secret := []byte("integration-secret")
	body := []byte(`{"event":"work_record.created"}`)
	signature := Sign(secret, now, "event-id", body)

	verified, err := Verify(context.Background(), VerifyCommand{
		Secret: secret, Timestamp: now.Format(time.RFC3339), EventID: "event-id",
		Signature: signature, Body: body, Now: now,
		MaxSkew: 5 * time.Minute, Replay: &memoryReplayStore{},
	})
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if verified.EventID != "event-id" || verified.Timestamp != now {
		t.Fatalf("unexpected verification result: %+v", verified)
	}
}

func TestVerifyRejectsTamperingStaleTimestampsAndReplay(t *testing.T) {
	now := time.Date(2026, time.July, 29, 16, 0, 0, 0, time.UTC)
	secret := []byte("integration-secret")
	body := []byte(`{"event":"work_record.created"}`)
	store := &memoryReplayStore{}
	valid := VerifyCommand{
		Secret: secret, Timestamp: now.Format(time.RFC3339), EventID: "event-id",
		Signature: Sign(secret, now, "event-id", body), Body: body, Now: now,
		MaxSkew: 5 * time.Minute, Replay: store,
	}
	if _, err := Verify(context.Background(), valid); err != nil {
		t.Fatalf("first Verify() error = %v", err)
	}
	if _, err := Verify(context.Background(), valid); !errors.Is(err, ErrReplay) {
		t.Fatalf("replay error = %v", err)
	}

	tampered := valid
	tampered.EventID = "other-event"
	tampered.Replay = &memoryReplayStore{}
	if _, err := Verify(context.Background(), tampered); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("tampered event error = %v", err)
	}

	stale := valid
	stale.Timestamp = now.Add(-6 * time.Minute).Format(time.RFC3339)
	stale.Signature = Sign(secret, now.Add(-6*time.Minute), stale.EventID, body)
	stale.Replay = &memoryReplayStore{}
	if _, err := Verify(context.Background(), stale); !errors.Is(err, ErrStaleTimestamp) {
		t.Fatalf("stale timestamp error = %v", err)
	}
}

func TestVerifyRejectsMalformedInputsWithoutClaimingReplayKey(t *testing.T) {
	now := time.Date(2026, time.July, 29, 16, 0, 0, 0, time.UTC)
	store := &memoryReplayStore{}
	cases := []VerifyCommand{
		{Now: now, Replay: store},
		{Secret: []byte("secret"), Timestamp: "invalid", EventID: "event", Signature: "v1=bad", Now: now, Replay: store},
		{Secret: []byte("secret"), Timestamp: now.Format(time.RFC3339), EventID: "event", Signature: "v2=abc", Now: now, Replay: store},
	}
	for _, command := range cases {
		if _, err := Verify(context.Background(), command); !errors.Is(err, ErrInvalidSignature) {
			t.Fatalf("malformed command error = %v", err)
		}
	}
	if len(store.claimed) != 0 {
		t.Fatalf("invalid requests claimed replay keys: %+v", store.claimed)
	}
}

func TestAuthenticateValidatesExactBodyWithoutClaimingReplay(t *testing.T) {
	now := time.Date(2026, time.July, 29, 16, 0, 0, 0, time.UTC)
	body := []byte(`{"event":"monitor.alert"}`)
	secret := []byte("integration-secret")
	verified, err := Authenticate(VerifyCommand{
		Secret: secret, Timestamp: now.Format(time.RFC3339),
		EventID: "event-id", Signature: Sign(secret, now, "event-id", body),
		Body: body, Now: now, MaxSkew: 5 * time.Minute,
	})
	if err != nil || verified.EventID != "event-id" {
		t.Fatalf("Authenticate() result=%+v error=%v", verified, err)
	}
	tampered := append([]byte(nil), body...)
	tampered[len(tampered)-2] = 'X'
	if _, err := Authenticate(VerifyCommand{
		Secret: secret, Timestamp: now.Format(time.RFC3339),
		EventID: "event-id", Signature: Sign(secret, now, "event-id", body),
		Body: tampered, Now: now, MaxSkew: 5 * time.Minute,
	}); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("tampered body error=%v", err)
	}
}
