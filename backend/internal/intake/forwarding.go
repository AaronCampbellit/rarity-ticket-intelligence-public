// Package intake normalizes authenticated external inputs before domain work
// creation. It retains references to raw payloads rather than embedding
// untrusted content in queue records.
package intake

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"time"
)

var ErrInvalidForwardingConfiguration = errors.New("invalid forwarding configuration")

type Source string

const (
	SourceForwardedEmail Source = "forwarded_email"
	SourceDirectAPI      Source = "direct_api"
	SourceInboundWebhook Source = "inbound_webhook"
)

type ProcessingState string

const (
	StateReceived    ProcessingState = "received"
	StateQuarantined ProcessingState = "quarantined"
)

type AuthenticationResult string

const (
	AuthenticationPassed AuthenticationResult = "passed"
	AuthenticationFailed AuthenticationResult = "failed"
)

type QuarantineReason string

const (
	QuarantineNone           QuarantineReason = ""
	QuarantineSender         QuarantineReason = "sender_not_allowed"
	QuarantineAuthentication QuarantineReason = "sender_authentication_failed"
	QuarantineRecipient      QuarantineReason = "recipient_mismatch"
	QuarantinePayload        QuarantineReason = "payload_reference_missing"
	QuarantineSize           QuarantineReason = "message_too_large"
	QuarantineRateLimit      QuarantineReason = "rate_limited"
)

type SenderAuthentication struct {
	SPF   bool
	DKIM  bool
	DMARC bool
}

type ForwardedMessage struct {
	EnvelopeFrom   string
	From           string
	To             string
	MessageID      string
	RawMIMERef     string
	SizeBytes      int64
	Authentication SenderAuthentication
}

type ForwardingPolicy struct {
	ConnectionID         string
	MSPID                string
	IntakeAddress        string
	AllowedSenderDomains []string
	MaxMessageBytes      int64
}

type InboundEvent struct {
	ID                     string               `json:"id"`
	MSPID                  string               `json:"msp_id"`
	ClientID               string               `json:"client_id,omitempty"`
	ConnectionID           string               `json:"connection_id,omitempty"`
	ForwardingConnectionID string               `json:"forwarding_connection_id,omitempty"`
	Source                 Source               `json:"source"`
	ExternalID             string               `json:"external_id"`
	ReceivedAt             time.Time            `json:"received_at"`
	AuthenticationResult   AuthenticationResult `json:"authentication_result"`
	RawPayloadRef          string               `json:"raw_payload_ref"`
	ProcessingState        ProcessingState      `json:"processing_state"`
	QuarantineReason       QuarantineReason     `json:"quarantine_reason,omitempty"`
	Sender                 string               `json:"sender,omitempty"`
	Recipient              string               `json:"recipient,omitempty"`
}

type RateLimiter interface {
	Allow(context.Context, string, time.Time) (bool, error)
}

type EventStore interface {
	Record(context.Context, InboundEvent) error
}

type ForwardingResult struct {
	EventID          string           `json:"event_id"`
	State            ProcessingState  `json:"state"`
	QuarantineReason QuarantineReason `json:"quarantine_reason,omitempty"`
}

type ForwardingGateway struct {
	policy  ForwardingPolicy
	limiter RateLimiter
	store   EventStore
	now     func() time.Time
	newID   func() string
}

func NewForwardingGateway(
	policy ForwardingPolicy,
	limiter RateLimiter,
	store EventStore,
	now func() time.Time,
	newID func() string,
) *ForwardingGateway {
	return &ForwardingGateway{
		policy: policy, limiter: limiter, store: store, now: now, newID: newID,
	}
}

func (g *ForwardingGateway) Accept(
	ctx context.Context,
	message ForwardedMessage,
) (ForwardingResult, error) {
	if g.limiter == nil || g.store == nil || g.now == nil || g.newID == nil ||
		strings.TrimSpace(g.policy.MSPID) == "" ||
		strings.TrimSpace(g.policy.ConnectionID) == "" ||
		strings.TrimSpace(g.policy.IntakeAddress) == "" ||
		g.policy.MaxMessageBytes <= 0 {
		return ForwardingResult{}, ErrInvalidForwardingConfiguration
	}
	now := g.now().UTC()
	reason := g.quarantineReason(message)
	if reason == QuarantineNone {
		key := strings.ToLower(strings.TrimSpace(message.EnvelopeFrom))
		allowed, err := g.limiter.Allow(ctx, g.policy.MSPID+"|"+key, now)
		if err != nil {
			return ForwardingResult{}, err
		}
		if !allowed {
			reason = QuarantineRateLimit
		}
	}
	state := StateReceived
	authentication := AuthenticationPassed
	if reason != QuarantineNone {
		state = StateQuarantined
		if reason == QuarantineAuthentication || reason == QuarantineSender {
			authentication = AuthenticationFailed
		}
	}
	event := InboundEvent{
		ID: g.newID(), MSPID: g.policy.MSPID, Source: SourceForwardedEmail,
		ForwardingConnectionID: g.policy.ConnectionID,
		ExternalID:             strings.TrimSpace(message.MessageID), ReceivedAt: now,
		AuthenticationResult: authentication, RawPayloadRef: strings.TrimSpace(message.RawMIMERef),
		ProcessingState: state, QuarantineReason: reason,
		Sender:    forwardingMetadata(message.From),
		Recipient: forwardingMetadata(message.To),
	}
	if err := g.store.Record(ctx, event); err != nil {
		return ForwardingResult{}, err
	}
	return ForwardingResult{EventID: event.ID, State: state, QuarantineReason: reason}, nil
}

func (g *ForwardingGateway) quarantineReason(message ForwardedMessage) QuarantineReason {
	return forwardingQuarantineReason(g.policy, message)
}

func forwardingQuarantineReason(
	policy ForwardingPolicy,
	message ForwardedMessage,
) QuarantineReason {
	if len(strings.TrimSpace(message.EnvelopeFrom)) > maxForwardingHeaderBytes ||
		len(strings.TrimSpace(message.From)) > maxForwardingHeaderBytes ||
		len(strings.TrimSpace(message.To)) > maxForwardingHeaderBytes {
		return QuarantinePayload
	}
	if !sameAddress(message.To, policy.IntakeAddress) {
		return QuarantineRecipient
	}
	if strings.TrimSpace(message.MessageID) == "" ||
		len(strings.TrimSpace(message.MessageID)) > maxForwardingExternalIDBytes ||
		strings.TrimSpace(message.RawMIMERef) == "" {
		return QuarantinePayload
	}
	if message.SizeBytes < 0 || message.SizeBytes > policy.MaxMessageBytes {
		return QuarantineSize
	}
	if !allowedSender(message.EnvelopeFrom, message.From, policy.AllowedSenderDomains) {
		return QuarantineSender
	}
	if !message.Authentication.DMARC ||
		(!message.Authentication.SPF && !message.Authentication.DKIM) {
		return QuarantineAuthentication
	}
	return QuarantineNone
}

func sameAddress(left, right string) bool {
	return strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right))
}

func allowedSender(envelopeFrom, from string, domains []string) bool {
	envelope, envelopeOK := addressDomain(envelopeFrom)
	header, headerOK := addressDomain(from)
	if !envelopeOK || !headerOK || envelope != header {
		return false
	}
	for _, domain := range domains {
		if strings.EqualFold(envelope, strings.TrimSpace(domain)) {
			return true
		}
	}
	return false
}

func addressDomain(value string) (string, bool) {
	address, err := mail.ParseAddress(strings.TrimSpace(value))
	if err != nil {
		return "", false
	}
	parts := strings.Split(address.Address, "@")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", false
	}
	return strings.ToLower(parts[1]), true
}
