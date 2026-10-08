// Package collaboration owns internal-only source content and its atomic
// mention projection. Public/customer content has no command type in this
// package and must pass ValidatePublicContentTokens at its route boundary.
package collaboration

import (
	"context"
	"errors"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mentions"
)

const maximumBodyBytes = 256 * 1024

var (
	ErrInvalid                 = errors.New("invalid internal collaboration mutation")
	ErrPublicMentionsForbidden = errors.New("structured mentions are forbidden in public content")
	ErrSourceRedacted          = errors.New("internal collaboration source is redacted")
)

type ParentRef struct {
	Type mentions.ParentType
	ID   string
}

type SourceKind = mentions.SourceKind

type SourceLifecycle string

const (
	SourceActive   SourceLifecycle = "active"
	SourceRedacted SourceLifecycle = "redacted"
)

type Source struct {
	ID             string
	MSPID          string
	ClientID       string
	Parent         ParentRef
	Kind           SourceKind
	Body           string
	Tokens         []mentions.Token
	AuthorID       string
	LifecycleState SourceLifecycle
	Version        int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
	RedactedAt     *time.Time
}

type CreateCommand struct {
	Principal              authorization.Principal
	Parent                 ParentRef
	Body                   string
	Tokens                 []mentions.Token
	ConfirmedTeamSnapshots map[string]mentions.TeamConfirmation
	IdempotencyKey         string
	Source                 string
}

type UpsertCommand struct {
	Principal              authorization.Principal
	Parent                 ParentRef
	SourceID               string
	Kind                   SourceKind
	Body                   string
	Tokens                 []mentions.Token
	ConfirmedTeamSnapshots map[string]mentions.TeamConfirmation
	ExpectedVersion        int64
	IdempotencyKey         string
	Source                 string
}

type RedactCommand struct {
	Principal       authorization.Principal
	Parent          ParentRef
	SourceID        string
	Kind            SourceKind
	ExpectedVersion int64
	IdempotencyKey  string
	Source          string
}

type Mutation struct {
	Source         Source
	Mentions       mentions.PreparedMutation
	IdempotencyKey string
}

type Repository interface {
	WithTransaction(
		context.Context,
		mentions.SourceRef,
		func(Transaction) error,
	) error
}

type Transaction interface {
	mentions.AccessRepository
	// LoadSourceForUpdate locks the parent and selected source. For creation it
	// returns a zero-ID Source carrying the trusted MSP/client parent scope.
	LoadSourceForUpdate(context.Context, mentions.SourceRef, string) (Source, error)
	// LoadIdempotentSource is scoped by the surrounding transaction's trusted
	// MSP, client, parent and source kind; implementations must never search by
	// request key alone.
	LoadIdempotentSource(context.Context, string) (Source, bool, error)
	Save(context.Context, Mutation) error
}

type MentionPreparer interface {
	PrepareMutation(
		context.Context,
		mentions.AccessRepository,
		mentions.PrepareCommand,
	) (mentions.PreparedMutation, error)
}

func ValidatePublicContentTokens(tokens []mentions.Token) error {
	if len(tokens) != 0 {
		return ErrPublicMentionsForbidden
	}
	return nil
}
