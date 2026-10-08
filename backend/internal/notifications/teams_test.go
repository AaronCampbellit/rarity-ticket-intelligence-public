package notifications

import (
	"context"
	"errors"
	"testing"
	"time"
)

type teamsSecrets struct {
	ref   string
	value string
}

func (s *teamsSecrets) Resolve(_ context.Context, connection TeamsConnection) (string, error) {
	s.ref = connection.WebhookSecretRef
	return s.value, nil
}

type teamsSender struct {
	url     string
	payload []byte
	err     error
}

func (s *teamsSender) Send(_ context.Context, url string, payload []byte) error {
	s.url = url
	s.payload = payload
	return s.err
}

type teamsHistory struct {
	attempt TeamsAttempt
}

func (h *teamsHistory) Record(_ context.Context, attempt TeamsAttempt) error {
	h.attempt = attempt
	return nil
}

func TestTeamsDeliveryResolvesEncryptedConnectionAndRecordsSuccess(t *testing.T) {
	now := time.Date(2026, time.July, 29, 23, 0, 0, 0, time.UTC)
	secrets := &teamsSecrets{value: "https://teams.example.test/incoming/opaque"}
	sender := &teamsSender{}
	history := &teamsHistory{}
	service := NewTeamsService(secrets, sender, history, func() time.Time { return now })

	err := service.Deliver(context.Background(), TeamsRequest{
		Connection: TeamsConnection{
			ID: "connection-id", MSPID: "msp-id", Version: 1,
			WebhookSecretRef: "secret://teams/connection-id",
		},
		EventID: "event-id", WorkRecordID: "work-id",
		Summary: WorkSummary{
			DisplayID: "INC-100", Priority: "critical", Status: "in_progress",
			Subject: "Email unavailable", SLAState: "warning",
			AuthenticatedURL: "https://rarity.example/work/INC-100",
			InternalNote:     "must not leave Rarity", EmailBody: "private body",
		},
		FirstAttemptAt: now.Add(-time.Minute), Attempt: 1,
	})
	if err != nil {
		t.Fatalf("Deliver() error = %v", err)
	}
	if secrets.ref != "secret://teams/connection-id" || sender.url != secrets.value ||
		history.attempt.State != TeamsDelivered ||
		history.attempt.ConnectionVersion != 1 ||
		history.attempt.EventID != "event-id" ||
		history.attempt.WorkRecordID != "work-id" ||
		!history.attempt.FirstAttemptAt.Equal(now.Add(-time.Minute)) {
		t.Fatalf("Teams delivery incomplete: secret=%+v sender=%+v history=%+v", secrets, sender, history)
	}
	for _, forbidden := range []string{"must not leave Rarity", "private body", "secret://teams"} {
		if contains(string(sender.payload), forbidden) {
			t.Fatalf("Teams payload leaked %q: %s", forbidden, sender.payload)
		}
	}
}

func TestTeamsDeliveryStopsAfterTwentyFourHoursAndKeepsFailureVisible(t *testing.T) {
	now := time.Date(2026, time.July, 29, 23, 0, 0, 0, time.UTC)
	secrets := &teamsSecrets{value: "https://teams.example.test/incoming/opaque"}
	sender := &teamsSender{err: errors.New("offline")}
	history := &teamsHistory{}
	service := NewTeamsService(secrets, sender, history, func() time.Time { return now })
	summary := WorkSummary{
		DisplayID: "INC-100", Priority: "critical", Status: "in_progress",
		Subject: "Email unavailable", AuthenticatedURL: "https://rarity.example/work/INC-100",
	}

	err := service.Deliver(context.Background(), TeamsRequest{
		Connection: TeamsConnection{
			ID: "connection-id", MSPID: "msp-id", WebhookSecretRef: "secret-ref", Version: 1,
		},
		EventID: "event-id", WorkRecordID: "work-id", Summary: summary,
		FirstAttemptAt: now.Add(-23 * time.Hour), Attempt: 3,
	})
	if !errors.Is(err, ErrTeamsDeliveryFailed) ||
		history.attempt.State != TeamsRetrying {
		t.Fatalf("retryable failure not recorded: err=%v attempt=%+v", err, history.attempt)
	}

	sender.url = ""
	err = service.Deliver(context.Background(), TeamsRequest{
		Connection: TeamsConnection{
			ID: "connection-id", MSPID: "msp-id", WebhookSecretRef: "secret-ref", Version: 1,
		},
		EventID: "event-id", WorkRecordID: "work-id", Summary: summary,
		FirstAttemptAt: now.Add(-24 * time.Hour), Attempt: 4,
	})
	if !errors.Is(err, ErrTeamsRetryExpired) ||
		history.attempt.State != TeamsFailed ||
		sender.url != "" {
		t.Fatalf("expired delivery retried or disappeared: err=%v sender=%+v attempt=%+v", err, sender, history.attempt)
	}
}

func TestTeamsDeliveryNeverSendsDisabledConnectionAndStillExpires(t *testing.T) {
	now := time.Date(2026, time.July, 30, 1, 0, 0, 0, time.UTC)
	secrets := &teamsSecrets{value: "https://teams.example.test/incoming/opaque"}
	sender := &teamsSender{}
	history := &teamsHistory{}
	service := NewTeamsService(secrets, sender, history, func() time.Time { return now })
	request := TeamsRequest{
		Connection: TeamsConnection{
			ID: "connection-id", MSPID: "msp-id", Version: 4, Disabled: true,
		},
		EventID: "event-id", WorkRecordID: "work-id",
		Summary: WorkSummary{
			DisplayID: "INC-100", Priority: "high", Status: "open",
			Subject:          "Service unavailable",
			AuthenticatedURL: "https://rarity.example/work/INC-100",
		},
		FirstAttemptAt: now.Add(-23 * time.Hour), Attempt: 3,
	}

	if err := service.Deliver(context.Background(), request); !errors.Is(err, ErrTeamsDeliveryFailed) ||
		history.attempt.State != TeamsRetrying || history.attempt.ErrorCode != "connection_disabled" ||
		secrets.ref != "" || sender.url != "" {
		t.Fatalf("disabled connection delivered or disappeared: err=%v secrets=%+v sender=%+v attempt=%+v", err, secrets, sender, history.attempt)
	}

	request.FirstAttemptAt = now.Add(-24 * time.Hour)
	request.Attempt = 4
	if err := service.Deliver(context.Background(), request); !errors.Is(err, ErrTeamsRetryExpired) ||
		history.attempt.State != TeamsFailed || history.attempt.ErrorCode != "retry_window_expired" {
		t.Fatalf("disabled connection did not expire: err=%v attempt=%+v", err, history.attempt)
	}
}
