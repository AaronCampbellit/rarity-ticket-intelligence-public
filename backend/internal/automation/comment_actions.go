package automation

import (
	"context"
	"strings"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/collaboration"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/comments"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mentions"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

// CommentCommand contains only plain comment inputs. Structured mention tokens
// are deliberately absent, so automation payloads cannot create deliveries.
type CommentCommand struct {
	Principal      authorization.Principal
	WorkRecordID   string
	Visibility     comments.Visibility
	Body           string
	CausationID    string
	IdempotencyKey string
}

type PublicCommentActions interface {
	Create(context.Context, comments.CreateCommand) (comments.Comment, error)
}

type InternalCommentActions interface {
	CreateComment(context.Context, collaboration.CreateCommand) (collaboration.Source, error)
}

// AutomationCommentActorResolver replaces the runtime automation principal
// with the persisted human creator for native internal comments. The resolver
// must evaluate the creator's current lifecycle and definition scope; final
// author permissions remain collaboration's responsibility.
type AutomationCommentActorResolver interface {
	ResolveAutomationCommentActor(context.Context, authorization.Principal) (authorization.Principal, error)
}

type RuntimeCommentActions struct {
	public        PublicCommentActions
	internal      InternalCommentActions
	actorResolver AutomationCommentActorResolver
}

var _ CommentActions = (*RuntimeCommentActions)(nil)

func NewRuntimeCommentActions(
	public PublicCommentActions,
	internal InternalCommentActions,
	actorResolver AutomationCommentActorResolver,
) *RuntimeCommentActions {
	return &RuntimeCommentActions{
		public: public, internal: internal, actorResolver: actorResolver,
	}
}

func (a *RuntimeCommentActions) Create(ctx context.Context, command CommentCommand) (string, error) {
	if a == nil || a.public == nil || a.internal == nil || a.actorResolver == nil ||
		command.Principal.ID == "" ||
		command.Principal.Scope.MSPID == "" || command.Principal.Scope.ClientID == "" ||
		strings.TrimSpace(command.WorkRecordID) == "" || strings.TrimSpace(command.Body) == "" ||
		strings.TrimSpace(command.CausationID) == "" || strings.TrimSpace(command.IdempotencyKey) == "" {
		return "", ErrActionFailed
	}
	switch command.Visibility {
	case comments.ClientVisible:
		created, err := a.public.Create(ctx, comments.CreateCommand{
			Principal: command.Principal,
			Target: scope.Target{
				MSPID: command.Principal.Scope.MSPID, ClientID: command.Principal.Scope.ClientID,
			},
			WorkRecordID: command.WorkRecordID, Visibility: comments.ClientVisible,
			Body: command.Body, ActorID: command.Principal.ID,
			ActorType: "automation", Source: "automation", CausationID: command.CausationID,
		})
		return created.ID, err
	case comments.Internal:
		// The published definition's immutable capability allowlist is the
		// automation authority. Check it before resolving a human author; the
		// creator's current staff/client roles are enforced independently by
		// collaboration below.
		if !command.Principal.Capabilities.Has("comment.internal.create") {
			return "", ErrActionFailed
		}
		actor, err := a.actorResolver.ResolveAutomationCommentActor(ctx, command.Principal)
		if err != nil {
			return "", err
		}
		if actor.ID == "" || actor.Scope != command.Principal.Scope {
			return "", scope.ErrNotFound
		}
		created, err := a.internal.CreateComment(ctx, collaboration.CreateCommand{
			Principal: actor,
			Parent: collaboration.ParentRef{
				Type: mentions.ParentWorkRecord, ID: command.WorkRecordID,
			},
			Body: command.Body, Tokens: []mentions.Token{},
			ConfirmedTeamSnapshots: map[string]mentions.TeamConfirmation{},
			IdempotencyKey:         command.IdempotencyKey, Source: "automation",
		})
		return created.ID, err
	default:
		return "", ErrActionFailed
	}
}
