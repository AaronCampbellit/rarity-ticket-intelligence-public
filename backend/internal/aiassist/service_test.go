package aiassist

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type aiProvider struct {
	request ProviderRequest
	result  ProviderResponse
}

func (p *aiProvider) Generate(_ context.Context, request ProviderRequest) (ProviderResponse, error) {
	p.request = request
	return p.result, nil
}

type recommendationStore struct {
	record RecommendationRecord
}

func (s *recommendationStore) Record(_ context.Context, record RecommendationRecord) error {
	s.record = record
	return nil
}

func TestGenerateRequiresOptInAndBuildsMinimizedPermissionScopedContext(t *testing.T) {
	now := time.Date(2026, time.July, 30, 2, 0, 0, 0, time.UTC)
	provider := &aiProvider{result: ProviderResponse{Text: "Concise summary", Confidence: 0.82}}
	store := &recommendationStore{}
	service := NewService(provider, store, func() time.Time { return now }, sequenceIDs("recommendation-id", "usage-id", "audit-id", "event-id", "correlation-id"))
	principal := authorization.Principal{
		ID:           "technician-id",
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-alpha"},
		Capabilities: authorization.NewCapabilitySet("ai.assist"),
	}

	result, err := service.Generate(context.Background(), Command{
		Principal: principal,
		Policy: Policy{
			MSPID: "msp-id", Enabled: true, Provider: "provider-a", Model: "model-a",
			ProviderDisclosureAccepted: true, PromptVersion: "summary-v1",
			AllowedFeatures: []Feature{FeatureSummary},
		},
		Feature: FeatureSummary, ClientID: "client-alpha", WorkRecordID: "work-id",
		Fields: []ContextField{
			{Name: "subject", Value: "VPN unavailable", Classification: ContextStandard},
			{Name: "password", Value: "super-secret", Classification: ContextSecret},
			{Name: "attachment", Value: "raw-log.zip", Classification: ContextAttachment},
			{Name: "sensitive", Value: "private medical detail", Classification: ContextSensitive},
		},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if len(provider.request.Fields) != 1 || provider.request.Fields[0].Name != "subject" {
		t.Fatalf("AI context was not minimized: %+v", provider.request.Fields)
	}
	if result.State != RecommendationPendingHuman || result.Provider != "provider-a" ||
		result.Model != "model-a" || store.record.Audit.Action != "ai.recommendation.generated" {
		t.Fatalf("recommendation evidence incomplete: result=%+v record=%+v", result, store.record)
	}
	if store.record.Usage.ID == "" || store.record.Usage.RecommendationID != result.ID {
		t.Fatalf("recommendation usage was not assigned atomically: %+v", store.record.Usage)
	}
}

func TestGenerateRejectsOptOutCrossClientAndUnsupportedAutonomousFeature(t *testing.T) {
	service := NewService(&aiProvider{}, &recommendationStore{}, time.Now, sequenceIDs("id"))
	principal := authorization.Principal{
		ID:           "technician-id",
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-alpha"},
		Capabilities: authorization.NewCapabilitySet("ai.assist"),
	}
	base := Command{
		Principal: principal,
		Policy: Policy{
			MSPID: "msp-id", Enabled: true, Provider: "provider", Model: "model",
			ProviderDisclosureAccepted: true, PromptVersion: "v1",
			AllowedFeatures: []Feature{FeatureSummary, FeatureReplyDraft, FeatureSimilar},
		},
		Feature: FeatureSummary, ClientID: "client-alpha", WorkRecordID: "work-id",
		Fields: []ContextField{{Name: "subject", Value: "value", Classification: ContextStandard}},
	}
	cases := []Command{base, base, base}
	cases[0].Policy.Enabled = false
	cases[1].ClientID = "client-bravo"
	cases[2].Feature = "send_reply"
	for _, command := range cases {
		if _, err := service.Generate(context.Background(), command); !errors.Is(err, ErrAIDenied) {
			t.Fatalf("unsafe AI request error = %v for %+v", err, command)
		}
	}
}

func TestSimilarSuggestionsMustRemainWithinAuthorizedCandidateSet(t *testing.T) {
	provider := &aiProvider{result: ProviderResponse{
		Text: "Related work", CandidateIDs: []string{"work-authorized", "work-invented"},
	}}
	service := NewService(provider, &recommendationStore{}, time.Now, sequenceIDs("id", "usage", "audit", "event", "correlation"))
	principal := authorization.Principal{
		ID: "technician-id", Scope: scope.Principal{MSPID: "msp-id", ClientID: "client-alpha"},
		Capabilities: authorization.NewCapabilitySet("ai.assist"),
	}
	_, err := service.Generate(context.Background(), Command{
		Principal: principal,
		Policy: Policy{
			MSPID: "msp-id", Enabled: true, Provider: "provider", Model: "model",
			ProviderDisclosureAccepted: true, PromptVersion: "similar-v1",
			AllowedFeatures: []Feature{FeatureSimilar},
		},
		Feature: FeatureSimilar, ClientID: "client-alpha", WorkRecordID: "work-id",
		Fields:                 []ContextField{{Name: "subject", Value: "VPN", Classification: ContextStandard}},
		AuthorizedCandidateIDs: []string{"work-authorized"},
	})
	if !errors.Is(err, ErrInvalidAIOutput) {
		t.Fatalf("invented candidate accepted: %v", err)
	}
}

func sequenceIDs(values ...string) func() string {
	index := 0
	return func() string {
		value := values[index]
		index++
		return value
	}
}
