package intake

import (
	"errors"
	"testing"
	"time"
)

func TestNormalizeSystemIntakeRequiresAuthenticatedScopedIdentity(t *testing.T) {
	now := time.Date(2026, time.July, 29, 20, 30, 0, 0, time.UTC)
	event, err := NormalizeSystem(SystemInput{
		ID: "event-id", MSPID: "msp-id", ClientID: "client-id",
		ConnectionID: "connection-id",
		Source:       SourceInboundWebhook, ExternalID: "remote-event-id",
		RawPayloadRef: "objects/inbound/remote-event-id", Authenticated: true,
		ReceivedAt: now,
	})
	if err != nil {
		t.Fatalf("NormalizeSystem() error = %v", err)
	}
	if event.AuthenticationResult != AuthenticationPassed ||
		event.ProcessingState != StateReceived ||
		event.ClientID != "client-id" ||
		event.ConnectionID != "connection-id" {
		t.Fatalf("system intake widened or lost scope: %+v", event)
	}
}

func TestNormalizeSystemIntakeRejectsMissingIdentityAndPayloadReference(t *testing.T) {
	base := SystemInput{
		ID: "event-id", MSPID: "msp-id", ClientID: "client-id",
		Source: SourceDirectAPI, ExternalID: "remote-id",
		RawPayloadRef: "objects/inbound/remote-id", Authenticated: true,
		ReceivedAt: time.Now(),
	}
	for _, mutate := range []func(*SystemInput){
		func(value *SystemInput) { value.MSPID = "" },
		func(value *SystemInput) { value.Authenticated = false },
		func(value *SystemInput) { value.RawPayloadRef = "" },
		func(value *SystemInput) { value.Source = SourceForwardedEmail },
	} {
		input := base
		mutate(&input)
		if _, err := NormalizeSystem(input); !errors.Is(err, ErrInvalidSystemIntake) {
			t.Fatalf("invalid input error = %v for %+v", err, input)
		}
	}
}
