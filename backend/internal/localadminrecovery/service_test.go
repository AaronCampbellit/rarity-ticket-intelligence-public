package localadminrecovery

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/identity"
	"golang.org/x/crypto/bcrypt"
)

type repositoryStub struct {
	accepted Mutation
	result   Result
	err      error
}

func (r *repositoryStub) ResetPasswordAtomic(_ context.Context, accepted Mutation) (Result, error) {
	r.accepted = accepted
	return r.result, r.err
}

func TestServiceResetsPasswordWithSystemAuditFacts(t *testing.T) {
	repository := &repositoryStub{result: Result{
		MSPDisplayID: "MSP-001", Username: "local-admin", Version: 4, SessionsRevoked: 2,
	}}
	now := time.Date(2026, time.August, 3, 20, 0, 0, 0, time.UTC)
	ids := []string{
		"019fca00-0000-4000-8000-000000000001",
		"019fca00-0000-4000-8000-000000000002",
		"019fca00-0000-4000-8000-000000000003",
	}
	index := 0
	service := NewService(repository, func() time.Time { return now }, func() string {
		value := ids[index]
		index++
		return value
	})
	password := []byte("correct horse battery staple")
	originalPassword := append([]byte(nil), password...)

	result, err := service.ResetPassword(context.Background(), Command{
		MSPDisplayID: " MSP-001 ",
		Username:     " Local-Admin ",
		Password:     password,
		Reason:       " restore production access ",
	})
	if err != nil {
		t.Fatalf("reset password: %v", err)
	}
	if result != repository.result {
		t.Fatalf("result = %+v, want %+v", result, repository.result)
	}
	if repository.accepted.MSPDisplayID != "MSP-001" ||
		repository.accepted.Username != "local-admin" ||
		repository.accepted.Reason != "restore production access" {
		t.Fatalf("normalized mutation = %+v", repository.accepted)
	}
	if repository.accepted.ActorType != "system" ||
		repository.accepted.ActorID != SystemActorID ||
		repository.accepted.Source != "operator_cli" ||
		repository.accepted.Action != PasswordResetAction {
		t.Fatalf("security facts = %+v", repository.accepted)
	}
	if repository.accepted.OccurredAt != now || repository.accepted.AuditID == "" ||
		repository.accepted.EventID == "" || repository.accepted.CorrelationID == "" {
		t.Fatalf("mutation metadata = %+v", repository.accepted)
	}
	if strings.Contains(repository.accepted.PasswordHash, string(originalPassword)) {
		t.Fatal("mutation contains plaintext password")
	}
	if cost, err := bcrypt.Cost([]byte(repository.accepted.PasswordHash)); err != nil ||
		cost < identity.MinBreakGlassBcryptCost {
		t.Fatalf("bcrypt cost = %d, err = %v", cost, err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(repository.accepted.PasswordHash), originalPassword); err != nil {
		t.Fatalf("password hash does not match: %v", err)
	}
	for index, value := range password {
		if value != 0 {
			t.Fatalf("password byte %d was not cleared", index)
		}
	}
}

func TestServiceRejectsInvalidRecoveryRequestBeforeRepository(t *testing.T) {
	repository := &repositoryStub{}
	service := NewService(repository, time.Now, func() string { return "id" })
	for name, command := range map[string]Command{
		"missing MSP":      {Username: "admin", Password: []byte("0123456789abcdef"), Reason: "incident"},
		"missing username": {MSPDisplayID: "MSP", Password: []byte("0123456789abcdef"), Reason: "incident"},
		"short password":   {MSPDisplayID: "MSP", Username: "admin", Password: []byte("too-short"), Reason: "incident"},
		"missing reason":   {MSPDisplayID: "MSP", Username: "admin", Password: []byte("0123456789abcdef")},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := service.ResetPassword(context.Background(), command); !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("error = %v, want ErrInvalidRequest", err)
			}
		})
	}
	if repository.accepted.PasswordHash != "" {
		t.Fatal("repository was called for an invalid request")
	}
}
