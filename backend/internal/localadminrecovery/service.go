// Package localadminrecovery implements the offline operator boundary for
// recovering a local administrator when browser authentication is unavailable.
package localadminrecovery

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/identity"
	"golang.org/x/crypto/bcrypt"
)

const (
	PasswordResetAction = "security.local_admin.password_reset"
	SystemActorID       = "00000000-0000-0000-0000-000000000000"
)

var (
	ErrInvalidRequest    = errors.New("invalid local administrator recovery request")
	ErrTargetUnavailable = errors.New("local administrator target is unavailable")
)

type Command struct {
	MSPDisplayID string
	Username     string
	Password     []byte
	Reason       string
}

type Mutation struct {
	MSPDisplayID  string
	Username      string
	PasswordHash  string
	Reason        string
	Action        string
	Source        string
	ActorType     string
	ActorID       string
	OccurredAt    time.Time
	AuditID       string
	EventID       string
	CorrelationID string
}

type Result struct {
	MSPDisplayID    string
	Username        string
	Version         int64
	SessionsRevoked int64
}

type Repository interface {
	ResetPasswordAtomic(context.Context, Mutation) (Result, error)
}

type Service struct {
	repository Repository
	now        func() time.Time
	newID      func() string
}

func NewService(repository Repository, now func() time.Time, newID func() string) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{repository: repository, now: now, newID: newID}
}

func (s *Service) ResetPassword(ctx context.Context, command Command) (Result, error) {
	defer clearBytes(command.Password)
	mspDisplayID := strings.TrimSpace(command.MSPDisplayID)
	username := strings.ToLower(strings.TrimSpace(command.Username))
	reason := strings.TrimSpace(command.Reason)
	if mspDisplayID == "" || len(mspDisplayID) > 128 ||
		username == "" || len(username) > 128 ||
		len(command.Password) < 16 || len(command.Password) > 72 ||
		reason == "" || len(reason) > 1024 ||
		s.repository == nil || s.newID == nil {
		return Result{}, ErrInvalidRequest
	}
	hash, err := bcrypt.GenerateFromPassword(command.Password, identity.MinBreakGlassBcryptCost)
	if err != nil {
		return Result{}, ErrInvalidRequest
	}
	return s.repository.ResetPasswordAtomic(ctx, Mutation{
		MSPDisplayID:  mspDisplayID,
		Username:      username,
		PasswordHash:  string(hash),
		Reason:        reason,
		Action:        PasswordResetAction,
		Source:        "operator_cli",
		ActorType:     "system",
		ActorID:       SystemActorID,
		OccurredAt:    s.now().UTC(),
		AuditID:       s.newID(),
		EventID:       s.newID(),
		CorrelationID: s.newID(),
	})
}

func clearBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
