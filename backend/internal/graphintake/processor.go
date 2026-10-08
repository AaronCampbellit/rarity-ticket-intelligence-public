package graphintake

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
)

var (
	ErrInvalidDeltaPage = errors.New("invalid Graph delta page")
	ticketTokenPattern  = regexp.MustCompile(`(?i)\bRTY-[0-9]+-[A-Z0-9]{26}\b`)
)

type Message struct {
	ID                string
	ConversationID    string
	InternetMessageID string
	InReplyTo         string
	References        []string
	Subject           string
	Sender            string
	ReceivedAt        time.Time
	RawMIMERef        string
	AttachmentRefs    []string
}

type DeltaPage struct {
	Messages   []Message
	NextCursor string
}

type GraphSource interface {
	FetchDelta(context.Context, string, string, string) (DeltaPage, error)
	GetMessage(context.Context, string, string) (Message, error)
}

type MatchMethod string

const (
	MatchNone              MatchMethod = "none"
	MatchGraphConversation MatchMethod = "graph_conversation"
	MatchInternetHeader    MatchMethod = "internet_message_header"
	MatchTicketToken       MatchMethod = "ticket_token"
)

type ThreadResolution struct {
	WorkRecordID string
	MatchedBy    MatchMethod
	CreateNew    bool
}

type ThreadIndex interface {
	ByConversationID(context.Context, string) (string, bool)
	ByInternetMessageID(context.Context, string) (string, bool)
	ByTicketToken(context.Context, string) (string, bool)
}

func ResolveThread(ctx context.Context, index ThreadIndex, message Message) ThreadResolution {
	if index == nil {
		return ThreadResolution{MatchedBy: MatchNone, CreateNew: true}
	}
	if value := strings.TrimSpace(message.ConversationID); value != "" {
		if workRecordID, ok := index.ByConversationID(ctx, value); ok {
			return ThreadResolution{WorkRecordID: workRecordID, MatchedBy: MatchGraphConversation}
		}
	}
	headers := append([]string{message.InReplyTo}, message.References...)
	for _, value := range headers {
		if value = strings.TrimSpace(value); value != "" {
			if workRecordID, ok := index.ByInternetMessageID(ctx, value); ok {
				return ThreadResolution{WorkRecordID: workRecordID, MatchedBy: MatchInternetHeader}
			}
		}
	}
	if token := strings.ToUpper(ticketTokenPattern.FindString(message.Subject)); token != "" {
		if workRecordID, ok := index.ByTicketToken(ctx, token); ok {
			return ThreadResolution{WorkRecordID: workRecordID, MatchedBy: MatchTicketToken}
		}
	}
	return ThreadResolution{MatchedBy: MatchNone, CreateNew: true}
}

type NormalizedMessage struct {
	ExternalID        string
	ConversationID    string
	InternetMessageID string
	InReplyTo         string
	References        []string
	Subject           string
	Sender            string
	ReceivedAt        time.Time
	RawMIMERef        string
	AttachmentRefs    []string
	Thread            ThreadResolution
}

type IntakeBatch struct {
	Mailbox     string
	Folder      string
	Messages    []NormalizedMessage
	NextCursor  string
	CommittedAt time.Time
}

type IntakeRepository interface {
	Cursor(context.Context, string, string) (string, error)
	Commit(context.Context, IntakeBatch) error
	CommitMessage(context.Context, string, NormalizedMessage) error
}

type Processor struct {
	source     GraphSource
	repository IntakeRepository
	threads    ThreadIndex
	now        func() time.Time
}

func NewProcessor(
	source GraphSource,
	repository IntakeRepository,
	threads ThreadIndex,
	now func() time.Time,
) *Processor {
	return &Processor{source: source, repository: repository, threads: threads, now: now}
}

type ReconcileResult struct {
	Processed  int
	NextCursor string
}

func (p *Processor) Reconcile(
	ctx context.Context,
	mailbox string,
	folder string,
) (ReconcileResult, error) {
	if p.source == nil || p.repository == nil || p.now == nil ||
		strings.TrimSpace(mailbox) == "" || strings.TrimSpace(folder) == "" {
		return ReconcileResult{}, ErrInvalidDeltaPage
	}
	cursor, err := p.repository.Cursor(ctx, mailbox, folder)
	if err != nil {
		return ReconcileResult{}, err
	}
	page, err := p.source.FetchDelta(ctx, mailbox, folder, cursor)
	if err != nil {
		return ReconcileResult{}, err
	}
	if strings.TrimSpace(page.NextCursor) == "" {
		return ReconcileResult{}, ErrInvalidDeltaPage
	}
	messages := make([]NormalizedMessage, 0, len(page.Messages))
	for _, message := range page.Messages {
		if strings.TrimSpace(message.ID) == "" {
			return ReconcileResult{}, ErrInvalidDeltaPage
		}
		messages = append(messages, normalizeMessage(
			message,
			ResolveThread(ctx, p.threads, message),
		))
	}
	batch := IntakeBatch{
		Mailbox: mailbox, Folder: folder, Messages: messages,
		NextCursor: page.NextCursor, CommittedAt: p.now().UTC(),
	}
	if err := p.repository.Commit(ctx, batch); err != nil {
		return ReconcileResult{}, err
	}
	return ReconcileResult{Processed: len(messages), NextCursor: page.NextCursor}, nil
}

func (p *Processor) Retrieve(
	ctx context.Context,
	mailbox string,
	messageID string,
) (NormalizedMessage, error) {
	if p.source == nil || p.repository == nil ||
		strings.TrimSpace(mailbox) == "" || strings.TrimSpace(messageID) == "" {
		return NormalizedMessage{}, ErrInvalidNotification
	}
	message, err := p.source.GetMessage(ctx, mailbox, messageID)
	if err != nil {
		return NormalizedMessage{}, err
	}
	if message.ID != messageID {
		return NormalizedMessage{}, ErrInvalidNotification
	}
	normalized := normalizeMessage(message, ResolveThread(ctx, p.threads, message))
	if err := p.repository.CommitMessage(ctx, mailbox, normalized); err != nil {
		return NormalizedMessage{}, err
	}
	return normalized, nil
}

func normalizeMessage(message Message, thread ThreadResolution) NormalizedMessage {
	return NormalizedMessage{
		ExternalID: message.ID, ConversationID: message.ConversationID,
		InternetMessageID: message.InternetMessageID, Subject: message.Subject,
		InReplyTo:  message.InReplyTo,
		References: append([]string(nil), message.References...),
		Sender:     message.Sender, ReceivedAt: message.ReceivedAt.UTC(),
		RawMIMERef:     message.RawMIMERef,
		AttachmentRefs: append([]string(nil), message.AttachmentRefs...),
		Thread:         thread,
	}
}
