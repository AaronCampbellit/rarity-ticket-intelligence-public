package aiassist

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type conversationStoreStub struct {
	conversation Conversation
}

func (s *conversationStoreStub) CreateConversation(context.Context, Conversation) error { return nil }
func (s *conversationStoreStub) GetConversation(context.Context, scope.Target, string) (Conversation, error) {
	return s.conversation, nil
}
func (s *conversationStoreStub) ListConversations(context.Context, scope.Target, string, bool, int) ([]Conversation, error) {
	return []Conversation{s.conversation}, nil
}
func (s *conversationStoreStub) ArchiveConversation(context.Context, scope.Target, string, string, int64, time.Time) error {
	return nil
}
func (s *conversationStoreStub) AppendMessage(context.Context, Message) error { return nil }
func (s *conversationStoreStub) ListMessages(context.Context, scope.Target, string, int) ([]Message, error) {
	return nil, nil
}

func TestConversationCannotCrossPrincipalOrClient(t *testing.T) {
	store := &conversationStoreStub{conversation: Conversation{
		ID: "conversation-1", MSPID: "msp-1", ClientID: "client-1",
		PrincipalID: "technician-1", Title: "Ticket review", Version: 1,
	}}
	service := NewConversationService(store, time.Now, func() string { return "id" })

	tests := []authorization.Principal{
		{ID: "technician-2", Scope: scope.Principal{MSPID: "msp-1", ClientID: "client-1"}},
		{ID: "technician-1", Scope: scope.Principal{MSPID: "msp-1", ClientID: "client-2"}},
		{ID: "technician-1", Scope: scope.Principal{MSPID: "msp-2"}},
	}
	for _, principal := range tests {
		_, err := service.Get(context.Background(), principal, "conversation-1")
		if !errors.Is(err, scope.ErrNotFound) {
			t.Fatalf("principal=%+v error=%v", principal, err)
		}
	}
}

func TestConversationCreateUsesTrustedPrincipalScope(t *testing.T) {
	store := &conversationStoreStub{}
	now := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	service := NewConversationService(store, func() time.Time { return now }, func() string { return "conversation-id" })
	principal := authorization.Principal{
		ID:    "technician-1",
		Scope: scope.Principal{MSPID: "msp-1", ClientID: "client-1"},
	}

	conversation, err := service.Create(context.Background(), principal, "  Investigate ticket  ")
	if err != nil {
		t.Fatal(err)
	}
	if conversation.ID != "conversation-id" || conversation.MSPID != "msp-1" ||
		conversation.ClientID != "client-1" || conversation.PrincipalID != "technician-1" ||
		conversation.Title != "Investigate ticket" || !conversation.CreatedAt.Equal(now) {
		t.Fatalf("conversation=%+v", conversation)
	}
}
