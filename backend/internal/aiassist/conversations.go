package aiassist

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

var ErrInvalidConversation = errors.New("invalid AI conversation")

type Conversation struct {
	ID          string     `json:"id"`
	MSPID       string     `json:"-"`
	ClientID    string     `json:"client_id,omitempty"`
	PrincipalID string     `json:"-"`
	Title       string     `json:"title"`
	ArchivedAt  *time.Time `json:"archived_at,omitempty"`
	Version     int64      `json:"version"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type MessageRole string

const (
	MessageRoleUser      MessageRole = "user"
	MessageRoleAssistant MessageRole = "assistant"
	MessageRoleTool      MessageRole = "tool"
	MessageRoleSystem    MessageRole = "system"
)

type Message struct {
	ID                string         `json:"id"`
	MSPID             string         `json:"-"`
	ClientID          string         `json:"client_id,omitempty"`
	ConversationID    string         `json:"conversation_id"`
	Role              MessageRole    `json:"role"`
	SafeText          string         `json:"text"`
	ReferencedObjects map[string]any `json:"referenced_objects,omitempty"`
	ProviderJobID     string         `json:"provider_job_id,omitempty"`
	CreatedAt         time.Time      `json:"created_at"`
}

type ConversationStore interface {
	CreateConversation(context.Context, Conversation) error
	GetConversation(context.Context, scope.Target, string) (Conversation, error)
	ListConversations(context.Context, scope.Target, string, bool, int) ([]Conversation, error)
	ArchiveConversation(context.Context, scope.Target, string, string, int64, time.Time) error
	AppendMessage(context.Context, Message) error
	ListMessages(context.Context, scope.Target, string, int) ([]Message, error)
}

type ConversationService struct {
	store ConversationStore
	now   func() time.Time
	newID func() string
}

func NewConversationService(store ConversationStore, now func() time.Time, newID func() string) *ConversationService {
	return &ConversationService{store: store, now: now, newID: newID}
}

func (s *ConversationService) Create(
	ctx context.Context,
	principal authorization.Principal,
	title string,
) (Conversation, error) {
	title = strings.TrimSpace(title)
	if !s.valid() || strings.TrimSpace(principal.ID) == "" ||
		strings.TrimSpace(principal.Scope.MSPID) == "" || title == "" || len(title) > 160 {
		return Conversation{}, ErrInvalidConversation
	}
	now := s.now().UTC()
	conversation := Conversation{
		ID: s.newID(), MSPID: principal.Scope.MSPID, ClientID: principal.Scope.ClientID,
		PrincipalID: principal.ID, Title: title, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if strings.TrimSpace(conversation.ID) == "" {
		return Conversation{}, ErrInvalidConversation
	}
	if err := s.store.CreateConversation(ctx, conversation); err != nil {
		return Conversation{}, err
	}
	return conversation, nil
}

func (s *ConversationService) Get(
	ctx context.Context,
	principal authorization.Principal,
	id string,
) (Conversation, error) {
	if !s.validPrincipal(principal) || strings.TrimSpace(id) == "" {
		return Conversation{}, ErrInvalidConversation
	}
	target := scope.Target{MSPID: principal.Scope.MSPID, ClientID: principal.Scope.ClientID}
	conversation, err := s.store.GetConversation(ctx, target, id)
	if err != nil {
		return Conversation{}, err
	}
	if conversation.MSPID != principal.Scope.MSPID ||
		conversation.ClientID != principal.Scope.ClientID ||
		conversation.PrincipalID != principal.ID {
		return Conversation{}, scope.ErrNotFound
	}
	return conversation, nil
}

func (s *ConversationService) List(
	ctx context.Context,
	principal authorization.Principal,
	includeArchived bool,
	limit int,
) ([]Conversation, error) {
	if !s.validPrincipal(principal) {
		return nil, ErrInvalidConversation
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	target := scope.Target{MSPID: principal.Scope.MSPID, ClientID: principal.Scope.ClientID}
	return s.store.ListConversations(ctx, target, principal.ID, includeArchived, limit)
}

func (s *ConversationService) Archive(
	ctx context.Context,
	principal authorization.Principal,
	id string,
	expectedVersion int64,
) error {
	conversation, err := s.Get(ctx, principal, id)
	if err != nil {
		return err
	}
	if expectedVersion <= 0 || conversation.Version != expectedVersion {
		return ErrInvalidConversation
	}
	return s.store.ArchiveConversation(
		ctx,
		scope.Target{MSPID: principal.Scope.MSPID, ClientID: principal.Scope.ClientID},
		id,
		principal.ID,
		expectedVersion,
		s.now().UTC(),
	)
}

func (s *ConversationService) Append(
	ctx context.Context,
	principal authorization.Principal,
	conversationID string,
	role MessageRole,
	safeText string,
	references map[string]any,
) (Message, error) {
	conversation, err := s.Get(ctx, principal, conversationID)
	if err != nil {
		return Message{}, err
	}
	safeText = strings.TrimSpace(safeText)
	if safeText == "" || len(safeText) > 64_000 || !validMessageRole(role) {
		return Message{}, ErrInvalidConversation
	}
	message := Message{
		ID: s.newID(), MSPID: conversation.MSPID, ClientID: conversation.ClientID,
		ConversationID: conversation.ID, Role: role, SafeText: safeText,
		ReferencedObjects: references, CreatedAt: s.now().UTC(),
	}
	if strings.TrimSpace(message.ID) == "" {
		return Message{}, ErrInvalidConversation
	}
	if err := s.store.AppendMessage(ctx, message); err != nil {
		return Message{}, err
	}
	return message, nil
}

func (s *ConversationService) Messages(
	ctx context.Context,
	principal authorization.Principal,
	conversationID string,
	limit int,
) ([]Message, error) {
	conversation, err := s.Get(ctx, principal, conversationID)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	return s.store.ListMessages(
		ctx,
		scope.Target{MSPID: conversation.MSPID, ClientID: conversation.ClientID},
		conversation.ID,
		limit,
	)
}

func (s *ConversationService) valid() bool {
	return s != nil && s.store != nil && s.now != nil && s.newID != nil
}

func (s *ConversationService) validPrincipal(principal authorization.Principal) bool {
	return s.valid() && strings.TrimSpace(principal.ID) != "" && strings.TrimSpace(principal.Scope.MSPID) != ""
}

func validMessageRole(role MessageRole) bool {
	return role == MessageRoleUser || role == MessageRoleAssistant ||
		role == MessageRoleTool || role == MessageRoleSystem
}
