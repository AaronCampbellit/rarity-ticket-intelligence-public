// Package webhooks defines the cryptographic boundary shared by inbound and
// outbound integration deliveries.
package webhooks

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

var (
	ErrInvalidSignature = errors.New("invalid webhook signature")
	ErrStaleTimestamp   = errors.New("stale webhook timestamp")
	ErrReplay           = errors.New("webhook replay")
)

type ReplayStore interface {
	Claim(context.Context, string, time.Time) (bool, error)
}

type VerifyCommand struct {
	Secret    []byte
	Timestamp string
	EventID   string
	Signature string
	Body      []byte
	Now       time.Time
	MaxSkew   time.Duration
	Replay    ReplayStore
}

type Verified struct {
	EventID   string
	Timestamp time.Time
}

func Sign(secret []byte, timestamp time.Time, eventID string, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(signingPayload(timestamp, eventID, body))
	return "v1=" + hex.EncodeToString(mac.Sum(nil))
}

func Verify(ctx context.Context, command VerifyCommand) (Verified, error) {
	verified, err := Authenticate(command)
	if err != nil {
		return Verified{}, err
	}
	if command.Replay == nil {
		return Verified{}, ErrInvalidSignature
	}
	claimed, err := command.Replay.Claim(
		ctx,
		command.EventID,
		command.Now.Add(command.MaxSkew).UTC(),
	)
	if err != nil {
		return Verified{}, err
	}
	if !claimed {
		return Verified{}, ErrReplay
	}
	return verified, nil
}

// Authenticate verifies the signed exact body and freshness without claiming
// replay state. Callers that persist an inbound event use this form so the
// replay claim can commit in the same transaction as that event.
func Authenticate(command VerifyCommand) (Verified, error) {
	if len(command.Secret) == 0 || strings.TrimSpace(command.EventID) == "" ||
		command.MaxSkew <= 0 {
		return Verified{}, ErrInvalidSignature
	}
	timestamp, err := time.Parse(time.RFC3339, command.Timestamp)
	if err != nil {
		return Verified{}, ErrInvalidSignature
	}
	provided, ok := signatureBytes(command.Signature)
	if !ok {
		return Verified{}, ErrInvalidSignature
	}
	expected := Sign(command.Secret, timestamp, command.EventID, command.Body)
	expectedBytes, _ := signatureBytes(expected)
	if !hmac.Equal(provided, expectedBytes) {
		return Verified{}, ErrInvalidSignature
	}
	skew := command.Now.Sub(timestamp)
	if skew < 0 {
		skew = -skew
	}
	if skew > command.MaxSkew {
		return Verified{}, ErrStaleTimestamp
	}
	return Verified{EventID: command.EventID, Timestamp: timestamp.UTC()}, nil
}

func signingPayload(timestamp time.Time, eventID string, body []byte) []byte {
	prefix := timestamp.UTC().Format(time.RFC3339Nano) + "\n" + eventID + "\n"
	payload := make([]byte, 0, len(prefix)+len(body))
	payload = append(payload, prefix...)
	payload = append(payload, body...)
	return payload
}

func signatureBytes(signature string) ([]byte, bool) {
	if !strings.HasPrefix(signature, "v1=") {
		return nil, false
	}
	value, err := hex.DecodeString(strings.TrimPrefix(signature, "v1="))
	return value, err == nil && len(value) == sha256.Size
}
