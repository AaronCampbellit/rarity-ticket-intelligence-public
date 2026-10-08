package graphintake

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeGraphSource struct {
	page      DeltaPage
	message   Message
	fetchErr  error
	requested string
	messageID string
}

func (s *fakeGraphSource) FetchDelta(_ context.Context, mailbox, folder, cursor string) (DeltaPage, error) {
	s.requested = mailbox + "|" + folder + "|" + cursor
	return s.page, s.fetchErr
}

func (s *fakeGraphSource) GetMessage(_ context.Context, mailbox, messageID string) (Message, error) {
	s.requested = mailbox
	s.messageID = messageID
	return s.message, s.fetchErr
}

type fakeIntakeRepository struct {
	cursor    string
	committed IntakeBatch
	message   NormalizedMessage
	commitErr error
}

func (r *fakeIntakeRepository) Cursor(context.Context, string, string) (string, error) {
	return r.cursor, nil
}

func (r *fakeIntakeRepository) Commit(_ context.Context, batch IntakeBatch) error {
	r.committed = batch
	return r.commitErr
}

func (r *fakeIntakeRepository) CommitMessage(
	_ context.Context,
	_ string,
	message NormalizedMessage,
) error {
	r.message = message
	return r.commitErr
}

type fakeThreadIndex struct {
	byConversation map[string]string
	byMessage      map[string]string
	byToken        map[string]string
}

func (i fakeThreadIndex) ByConversationID(_ context.Context, value string) (string, bool) {
	result, ok := i.byConversation[value]
	return result, ok
}
func (i fakeThreadIndex) ByInternetMessageID(_ context.Context, value string) (string, bool) {
	result, ok := i.byMessage[value]
	return result, ok
}
func (i fakeThreadIndex) ByTicketToken(_ context.Context, value string) (string, bool) {
	result, ok := i.byToken[value]
	return result, ok
}

func TestResolveThreadPrefersGraphThenMessageHeadersThenTicketToken(t *testing.T) {
	const replyToken = "RTY-1042-7N4P6Q2R8T3V5W9XK2M6C8D4F1"
	index := fakeThreadIndex{
		byConversation: map[string]string{"conversation-id": "work-graph"},
		byMessage:      map[string]string{"<parent@example.com>": "work-header"},
		byToken:        map[string]string{replyToken: "work-token"},
	}
	message := Message{
		ConversationID: "conversation-id", InReplyTo: "<parent@example.com>",
		Subject: "Re: [" + replyToken + "] unrelated wording",
	}
	resolution := ResolveThread(context.Background(), index, message)
	if resolution.WorkRecordID != "work-graph" || resolution.MatchedBy != MatchGraphConversation {
		t.Fatalf("Graph precedence failed: %+v", resolution)
	}

	message.ConversationID = ""
	resolution = ResolveThread(context.Background(), index, message)
	if resolution.WorkRecordID != "work-header" || resolution.MatchedBy != MatchInternetHeader {
		t.Fatalf("header precedence failed: %+v", resolution)
	}

	message.InReplyTo = ""
	resolution = ResolveThread(context.Background(), index, message)
	if resolution.WorkRecordID != "work-token" || resolution.MatchedBy != MatchTicketToken {
		t.Fatalf("ticket-token fallback failed: %+v", resolution)
	}
}

func TestResolveThreadRejectsPredictableDisplayIDAsTicketToken(t *testing.T) {
	index := fakeThreadIndex{byToken: map[string]string{"RTY-1042": "work-token"}}
	resolution := ResolveThread(context.Background(), index, Message{
		Subject: "Re: [RTY-1042] guessed ticket",
		Sender:  "attacker@example.net",
	})
	if !resolution.CreateNew || resolution.MatchedBy != MatchNone || resolution.WorkRecordID != "" {
		t.Fatalf("predictable display ID resolved a thread: %+v", resolution)
	}
}

func TestResolveThreadNeverMatchesSubjectAlone(t *testing.T) {
	index := fakeThreadIndex{byToken: map[string]string{}}
	resolution := ResolveThread(context.Background(), index, Message{
		Subject: "VPN unavailable at Dallas",
	})
	if !resolution.CreateNew || resolution.WorkRecordID != "" {
		t.Fatalf("subject-only message attached to existing work: %+v", resolution)
	}
}

func TestReconcileCommitsMessagesAndNextCursorTogether(t *testing.T) {
	source := &fakeGraphSource{page: DeltaPage{
		Messages:   []Message{{ID: "message-id", InternetMessageID: "<message@example.com>"}},
		NextCursor: "cursor-2",
	}}
	repository := &fakeIntakeRepository{cursor: "cursor-1"}
	processor := NewProcessor(source, repository, fakeThreadIndex{}, func() time.Time {
		return time.Date(2026, time.July, 29, 19, 0, 0, 0, time.UTC)
	})

	result, err := processor.Reconcile(context.Background(), "support@example.com", "inbox")
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if source.requested != "support@example.com|inbox|cursor-1" ||
		repository.committed.NextCursor != "cursor-2" ||
		len(repository.committed.Messages) != 1 ||
		result.Processed != 1 {
		t.Fatalf("delta reconciliation was not atomic: request=%q batch=%+v result=%+v", source.requested, repository.committed, result)
	}
}

func TestReconcileDoesNotAdvanceCursorAfterFetchOrCommitFailure(t *testing.T) {
	source := &fakeGraphSource{fetchErr: errors.New("Graph unavailable")}
	repository := &fakeIntakeRepository{cursor: "cursor-1"}
	processor := NewProcessor(source, repository, fakeThreadIndex{}, time.Now)
	if _, err := processor.Reconcile(context.Background(), "support@example.com", "inbox"); err == nil {
		t.Fatal("fetch failure was ignored")
	}
	if repository.committed.NextCursor != "" {
		t.Fatal("cursor advanced after fetch failure")
	}

	source.fetchErr = nil
	source.page = DeltaPage{NextCursor: "cursor-2"}
	repository.commitErr = errors.New("database unavailable")
	if _, err := processor.Reconcile(context.Background(), "support@example.com", "inbox"); err == nil {
		t.Fatal("commit failure was ignored")
	}
}

func TestRetrieveLoadsMessageContentServerSideAndCommitsNormalizedRecord(t *testing.T) {
	source := &fakeGraphSource{message: Message{
		ID: "message-id", ConversationID: "conversation-id",
		InternetMessageID: "<message@example.com>", RawMIMERef: "objects/mime/message-id",
		InReplyTo:      "<parent@example.com>",
		References:     []string{"<first@example.com>", "<parent@example.com>"},
		AttachmentRefs: []string{"objects/attachments/attachment-id"},
	}}
	repository := &fakeIntakeRepository{}
	processor := NewProcessor(source, repository, fakeThreadIndex{}, time.Now)

	result, err := processor.Retrieve(
		context.Background(),
		"support@example.com",
		"message-id",
	)
	if err != nil {
		t.Fatalf("Retrieve() error = %v", err)
	}
	if source.messageID != "message-id" ||
		repository.message.ExternalID != "message-id" ||
		repository.message.RawMIMERef == "" ||
		repository.message.InReplyTo != "<parent@example.com>" ||
		len(repository.message.References) != 2 ||
		len(repository.message.AttachmentRefs) != 1 ||
		result.ExternalID != "message-id" {
		t.Fatalf("message retrieval was incomplete: source=%+v stored=%+v", source, repository.message)
	}
}
