package aiassist

import (
	"context"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type calendarControlPlaneStub struct {
	policy     Policy
	model      ModelProfile
	connection ProviderConnection
}

func (s calendarControlPlaneStub) GetPolicy(context.Context, scope.Target) (Policy, error) {
	return s.policy, nil
}
func (s calendarControlPlaneStub) FindModels(context.Context, scope.Target, []string) ([]ModelProfile, error) {
	return []ModelProfile{s.model}, nil
}
func (s calendarControlPlaneStub) GetConnection(context.Context, scope.Target, string) (ProviderConnection, error) {
	return s.connection, nil
}

type calendarAdapterStub struct {
	calls   int
	request ProviderRequest
	result  GenerationResult
}

func (s *calendarAdapterStub) Type() AdapterType { return AdapterOpenAICompatible }
func (s *calendarAdapterStub) Discover(context.Context, ProviderConnection, []byte) ([]DiscoveredModel, error) {
	return nil, nil
}
func (s *calendarAdapterStub) Generate(_ context.Context, _ ProviderConnection, _ ModelProfile, request ProviderRequest, _ []byte) (GenerationResult, error) {
	s.calls++
	s.request = request
	return s.result, nil
}

func TestGovernedCalendarProviderUsesConfiguredHealthyModelAndSafeContext(t *testing.T) {
	start := time.Date(2026, 8, 17, 14, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	adapter := &calendarAdapterStub{result: GenerationResult{CalendarCandidates: []CalendarProviderCandidate{{
		TechnicianID: "tech-a", StartsAt: &start, EndsAt: &end, Timezone: "UTC",
		Explanation: "Available and qualified", Tradeoffs: []string{},
	}}}}
	registry, err := NewAdapterRegistry(adapter)
	if err != nil {
		t.Fatal(err)
	}
	provider := NewGovernedCalendarProvider(calendarControlPlaneStub{
		policy:     Policy{MSPID: "msp", Enabled: true, ProviderDisclosureAccepted: true, PromptVersion: "calendar-v1", AllowedFeatures: []Feature{FeatureCalendarRecommendation}, CalendarRecommendationModelProfileID: "model", Version: 1},
		model:      ModelProfile{ID: "model", MSPID: "msp", ConnectionID: "connection", ProviderModelID: "provider-model", DisplayName: "Calendar", SupportedFeatures: []Feature{FeatureCalendarRecommendation}, ContextLimit: 4096, OutputLimit: 512, ZeroCost: true, Enabled: true, Version: 1},
		connection: ProviderConnection{ID: "connection", MSPID: "msp", Name: "Provider", BaseURL: "https://ai.example.com", Adapter: AdapterOpenAICompatible, Network: NetworkRemote, Enabled: true, Timeout: time.Minute, RequestLimitBytes: 1 << 20, ResponseLimitBytes: 1 << 20, DisclosureAcceptedAt: &start, Health: HealthHealthy, Version: 1},
	}, registry, nil)
	candidates, err := provider.RecommendCalendar(context.Background(), CalendarRecommendationContext{
		MSPID: "msp", ProjectionID: "event", AuthorizedTechnicianIDs: []string{"tech-a"},
		AvailableIntervals: []calendar.TimeInterval{{Start: start, End: end}}, WorkloadMinutes: map[string]int64{"tech-a": 30},
	})
	if err != nil || len(candidates) != 1 || adapter.calls != 1 {
		t.Fatalf("RecommendCalendar() candidates=%+v calls=%d error=%v", candidates, adapter.calls, err)
	}
	if adapter.request.Feature != FeatureCalendarRecommendation || len(adapter.request.Fields) != 1 || adapter.request.AuthorizedCandidateIDs[0] != "tech-a" {
		t.Fatalf("provider request=%+v", adapter.request)
	}
}

func TestGovernedCalendarProviderFailsClosedBeforeIO(t *testing.T) {
	adapter := &calendarAdapterStub{}
	registry, _ := NewAdapterRegistry(adapter)
	provider := NewGovernedCalendarProvider(calendarControlPlaneStub{
		policy:     Policy{MSPID: "msp", Enabled: true, ProviderDisclosureAccepted: true, PromptVersion: "calendar-v1", AllowedFeatures: []Feature{FeatureCalendarRecommendation}, CalendarRecommendationModelProfileID: "model", CostLimitEnabled: true, Version: 1},
		model:      ModelProfile{ID: "model", MSPID: "msp", ConnectionID: "connection", ProviderModelID: "provider-model", DisplayName: "Calendar", SupportedFeatures: []Feature{FeatureCalendarRecommendation}, ContextLimit: 4096, OutputLimit: 512, Enabled: true, Version: 1},
		connection: ProviderConnection{ID: "connection", MSPID: "msp", Name: "Provider", BaseURL: "https://ai.example.com", Adapter: AdapterOpenAICompatible, Network: NetworkRemote, Enabled: true, Timeout: time.Minute, RequestLimitBytes: 1 << 20, ResponseLimitBytes: 1 << 20, Health: HealthHealthy, Version: 1},
	}, registry, nil)
	if _, err := provider.RecommendCalendar(context.Background(), CalendarRecommendationContext{MSPID: "msp", ProjectionID: "event", AuthorizedTechnicianIDs: []string{"tech-a"}}); err == nil || adapter.calls != 0 {
		t.Fatalf("RecommendCalendar() calls=%d error=%v", adapter.calls, err)
	}
}

func TestGovernedCalendarProviderRejectsPaidModelWithoutUsageAccounting(t *testing.T) {
	adapter := &calendarAdapterStub{}
	registry, _ := NewAdapterRegistry(adapter)
	price := int64(100)
	provider := NewGovernedCalendarProvider(calendarControlPlaneStub{
		policy:     Policy{MSPID: "msp", Enabled: true, ProviderDisclosureAccepted: true, PromptVersion: "calendar-v1", AllowedFeatures: []Feature{FeatureCalendarRecommendation}, CalendarRecommendationModelProfileID: "model", Version: 1},
		model:      ModelProfile{ID: "model", MSPID: "msp", ConnectionID: "connection", ProviderModelID: "provider-model", DisplayName: "Calendar", SupportedFeatures: []Feature{FeatureCalendarRecommendation}, ContextLimit: 4096, OutputLimit: 512, InputCostPerMillionMinor: &price, OutputCostPerMillionMinor: &price, Enabled: true, Version: 1},
		connection: ProviderConnection{ID: "connection", MSPID: "msp", Name: "Provider", BaseURL: "https://ai.example.com", Adapter: AdapterOpenAICompatible, Network: NetworkRemote, Enabled: true, Timeout: time.Minute, RequestLimitBytes: 1 << 20, ResponseLimitBytes: 1 << 20, DisclosureAcceptedAt: &time.Time{}, Health: HealthHealthy, Version: 1},
	}, registry, nil)
	if _, err := provider.RecommendCalendar(context.Background(), CalendarRecommendationContext{MSPID: "msp", ProjectionID: "event", AuthorizedTechnicianIDs: []string{"tech-a"}}); err == nil || adapter.calls != 0 {
		t.Fatalf("paid synchronous generation calls=%d error=%v", adapter.calls, err)
	}
}
