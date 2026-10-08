package aiassist

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCalendarRecommendationFeatureIsValidForModelAndPolicy(t *testing.T) {
	model := ModelProfile{
		ID: "model", MSPID: "msp", ConnectionID: "connection",
		ProviderModelID: "calendar-model", DisplayName: "Calendar model",
		SupportedFeatures: []Feature{FeatureCalendarRecommendation}, ContextLimit: 4096,
		OutputLimit: 512, Enabled: true, Version: 1,
	}
	if err := ValidateModelProfile(model); err != nil {
		t.Fatalf("ValidateModelProfile() error = %v", err)
	}
	policy := Policy{
		MSPID: "msp", Enabled: true, ProviderDisclosureAccepted: true,
		AllowedFeatures:                      []Feature{FeatureCalendarRecommendation},
		CalendarRecommendationModelProfileID: "model", Version: 1,
	}
	if err := ValidateManagementPolicy(policy); err != nil {
		t.Fatalf("ValidateManagementPolicy() error = %v", err)
	}
	policy.CalendarRecommendationModelProfileID = ""
	if err := ValidateManagementPolicy(policy); !errors.Is(err, ErrInvalidProviderConfiguration) {
		t.Fatalf("missing calendar model error = %v", err)
	}
}

func TestCalendarProviderContractUsesDedicatedInstructionAndStrictOutput(t *testing.T) {
	model := ModelProfile{ProviderModelID: "model", ContextLimit: 4096, OutputLimit: 512, Enabled: true}
	body, err := PrepareProviderRequest(AdapterOpenAICompatible, model, ProviderRequest{
		Feature:                FeatureCalendarRecommendation,
		Fields:                 []ContextField{{Name: "calendar_context", Value: `{"projection_id":"event"}`, Classification: ContextStandard}},
		AuthorizedCandidateIDs: []string{"tech-a"}, MaxOutputUnits: 512,
	})
	if err != nil || !strings.Contains(string(body), "technician_id") || !strings.Contains(string(body), "starts_at") {
		t.Fatalf("calendar request body=%s error=%v", body, err)
	}
	start := time.Date(2026, 8, 17, 14, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	candidates, err := ParseCalendarRecommendationOutput([]byte(`{"candidates":[{"technician_id":"tech-a","starts_at":"2026-08-17T14:00:00Z","ends_at":"2026-08-17T15:00:00Z","timezone":"UTC","explanation":"Best available interval","tradeoffs":["Later than requested"]}]}`), []string{"tech-a"})
	if err != nil || len(candidates) != 1 || !candidates[0].StartsAt.Equal(start) || !candidates[0].EndsAt.Equal(end) {
		t.Fatalf("ParseCalendarRecommendationOutput() candidates=%+v error=%v", candidates, err)
	}
}

func TestCalendarProviderContractRejectsUnauthorizedUnknownAndUnboundedOutput(t *testing.T) {
	for _, body := range [][]byte{
		[]byte(`{"candidates":[],"apply":true}`),
		[]byte(`{"candidates":[{"technician_id":"tech-secret","starts_at":"2026-08-17T14:00:00Z","ends_at":"2026-08-17T15:00:00Z","timezone":"UTC","explanation":"x","tradeoffs":[]}]}`),
		[]byte(`{"candidates":[{"technician_id":"tech-a","starts_at":"2026-08-17T15:00:00Z","ends_at":"2026-08-17T14:00:00Z","timezone":"UTC","explanation":"x","tradeoffs":[]}]}`),
	} {
		if _, err := ParseCalendarRecommendationOutput(body, []string{"tech-a"}); !errors.Is(err, ErrInvalidAIOutput) {
			t.Fatalf("ParseCalendarRecommendationOutput(%s) error=%v", body, err)
		}
	}
}
