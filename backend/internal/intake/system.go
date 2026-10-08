package intake

import (
	"errors"
	"strings"
	"time"
)

var ErrInvalidSystemIntake = errors.New("invalid system intake")

type SystemInput struct {
	ID            string
	MSPID         string
	ClientID      string
	ConnectionID  string
	Source        Source
	ExternalID    string
	RawPayloadRef string
	Authenticated bool
	ReceivedAt    time.Time
}

func NormalizeSystem(input SystemInput) (InboundEvent, error) {
	if strings.TrimSpace(input.ID) == "" ||
		strings.TrimSpace(input.MSPID) == "" ||
		strings.TrimSpace(input.ExternalID) == "" ||
		strings.TrimSpace(input.RawPayloadRef) == "" ||
		!input.Authenticated ||
		(input.Source != SourceDirectAPI && input.Source != SourceInboundWebhook) ||
		(input.Source == SourceInboundWebhook && strings.TrimSpace(input.ConnectionID) == "") ||
		(input.Source != SourceInboundWebhook && strings.TrimSpace(input.ConnectionID) != "") ||
		input.ReceivedAt.IsZero() {
		return InboundEvent{}, ErrInvalidSystemIntake
	}
	return InboundEvent{
		ID: input.ID, MSPID: input.MSPID, ClientID: input.ClientID,
		ConnectionID: input.ConnectionID,
		Source:       input.Source, ExternalID: input.ExternalID,
		ReceivedAt:           input.ReceivedAt.UTC(),
		AuthenticationResult: AuthenticationPassed,
		RawPayloadRef:        input.RawPayloadRef, ProcessingState: StateReceived,
	}, nil
}
