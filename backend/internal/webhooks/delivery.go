package webhooks

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var (
	ErrDeliveryFailed    = errors.New("webhook delivery failed")
	ErrUnsafeDestination = errors.New("unsafe webhook destination")
)

type DeliveryState string

const (
	DeliverySucceeded DeliveryState = "succeeded"
	DeliveryFailed    DeliveryState = "failed"
)

type WebhookConnection struct {
	ID        string
	MSPID     string
	ClientID  string
	Endpoint  string
	SecretRef string
	Secret    []byte
}

type OutboundEvent struct {
	ID   string
	Type string
	Body []byte
}

type DeliveryRequest struct {
	URL     string
	Headers map[string]string
	Body    []byte
}

type DeliveryAttempt struct {
	ConnectionID string
	EventID      string
	Attempt      int
	AttemptedAt  time.Time
	State        DeliveryState
	HTTPStatus   int
	ErrorCode    string
}

type Sender interface {
	Send(context.Context, DeliveryRequest) (int, error)
}

type DeliveryHistory interface {
	Record(context.Context, DeliveryAttempt) error
}

type Publisher struct {
	sender  Sender
	history DeliveryHistory
	now     func() time.Time
}

type ResolvingPublisher struct {
	secrets   InboundSecretResolver
	publisher *Publisher
}

func NewPublisher(sender Sender, history DeliveryHistory, now func() time.Time) *Publisher {
	return &Publisher{sender: sender, history: history, now: now}
}

func NewResolvingPublisher(
	secrets InboundSecretResolver,
	publisher *Publisher,
) *ResolvingPublisher {
	return &ResolvingPublisher{secrets: secrets, publisher: publisher}
}

func (p *ResolvingPublisher) PublishOutbound(
	ctx context.Context,
	job OutboundJob,
) error {
	if p == nil || p.secrets == nil || p.publisher == nil ||
		p.publisher.history == nil || p.publisher.now == nil ||
		strings.TrimSpace(job.Connection.SecretRef) == "" {
		return ErrDeliveryFailed
	}
	secret, err := p.secrets.Resolve(ctx, job.Connection.SecretRef)
	if err != nil {
		recordErr := p.publisher.history.Record(ctx, DeliveryAttempt{
			ConnectionID: job.Connection.ID, EventID: job.Event.ID,
			Attempt: job.Attempt, AttemptedAt: p.publisher.now().UTC(),
			State: DeliveryFailed, ErrorCode: "connection_unavailable",
		})
		if recordErr != nil {
			return recordErr
		}
		return ErrDeliveryFailed
	}
	defer clear(secret)
	connection := job.Connection
	connection.Secret = secret
	return p.publisher.Publish(ctx, connection, job.Event, job.Attempt)
}

func (p *Publisher) Publish(
	ctx context.Context,
	connection WebhookConnection,
	event OutboundEvent,
	attemptNumber int,
) error {
	if p.sender == nil || p.history == nil || p.now == nil ||
		connection.ID == "" || connection.MSPID == "" || len(connection.Secret) == 0 ||
		event.ID == "" || event.Type == "" || attemptNumber < 1 {
		return ErrDeliveryFailed
	}
	now := p.now().UTC()
	if err := ValidateDestination(connection.Endpoint); err != nil {
		recordErr := p.history.Record(ctx, DeliveryAttempt{
			ConnectionID: connection.ID, EventID: event.ID,
			Attempt: attemptNumber, AttemptedAt: now,
			State: DeliveryFailed, ErrorCode: "unsafe_destination",
		})
		if recordErr != nil {
			return recordErr
		}
		return err
	}
	request := DeliveryRequest{
		URL: connection.Endpoint,
		Headers: map[string]string{
			"Content-Type":        "application/json",
			"X-Rarity-Event-ID":   event.ID,
			"X-Rarity-Event-Type": event.Type,
			"X-Rarity-Timestamp":  now.Format(time.RFC3339Nano),
			"X-Rarity-Signature":  Sign(connection.Secret, now, event.ID, event.Body),
		},
		Body: append([]byte(nil), event.Body...),
	}
	status, sendErr := p.sender.Send(ctx, request)
	deliveryState := DeliverySucceeded
	errorCode := ""
	unsafeDestination := errors.Is(sendErr, ErrUnsafeDestination)
	if sendErr != nil || status < 200 || status >= 300 {
		deliveryState = DeliveryFailed
		if unsafeDestination {
			errorCode = "unsafe_destination"
		} else {
			errorCode = deliveryErrorCode(status, sendErr)
		}
	}
	recordErr := p.history.Record(ctx, DeliveryAttempt{
		ConnectionID: connection.ID, EventID: event.ID, Attempt: attemptNumber,
		AttemptedAt: now, State: deliveryState, HTTPStatus: status, ErrorCode: errorCode,
	})
	if recordErr != nil {
		return recordErr
	}
	if deliveryState == DeliveryFailed {
		if unsafeDestination {
			return ErrUnsafeDestination
		}
		return ErrDeliveryFailed
	}
	return nil
}

func ValidateDestination(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" ||
		parsed.User != nil || parsed.Fragment != "" {
		return ErrUnsafeDestination
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") ||
		strings.HasSuffix(host, ".local") {
		return ErrUnsafeDestination
	}
	if ip := net.ParseIP(host); ip != nil &&
		(ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
			ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast()) {
		return ErrUnsafeDestination
	}
	return nil
}

func deliveryErrorCode(status int, sendErr error) string {
	if sendErr != nil {
		return "transport_error"
	}
	switch {
	case status >= 500:
		return "remote_5xx"
	case status >= 400:
		return "remote_4xx"
	default:
		return "remote_status_" + strconv.Itoa(status)
	}
}
