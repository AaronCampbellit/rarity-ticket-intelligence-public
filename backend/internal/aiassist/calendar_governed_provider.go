package aiassist

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type CalendarControlPlane interface {
	GetPolicy(context.Context, scope.Target) (Policy, error)
	FindModels(context.Context, scope.Target, []string) ([]ModelProfile, error)
	GetConnection(context.Context, scope.Target, string) (ProviderConnection, error)
}

type GovernedCalendarProvider struct {
	control     CalendarControlPlane
	registry    *AdapterRegistry
	credentials CredentialProvider
}

func NewGovernedCalendarProvider(control CalendarControlPlane, registry *AdapterRegistry, credentials CredentialProvider) *GovernedCalendarProvider {
	return &GovernedCalendarProvider{control: control, registry: registry, credentials: credentials}
}

func (p *GovernedCalendarProvider) RecommendCalendar(ctx context.Context, safe CalendarRecommendationContext) ([]CalendarProviderCandidate, error) {
	if p == nil || p.control == nil || p.registry == nil || strings.TrimSpace(safe.MSPID) == "" ||
		strings.TrimSpace(safe.ProjectionID) == "" || len(safe.AuthorizedTechnicianIDs) == 0 {
		return nil, ErrAIDenied
	}
	target := scope.Target{MSPID: safe.MSPID}
	policy, err := p.control.GetPolicy(ctx, target)
	if err != nil || policy.MSPID != safe.MSPID || !policy.Enabled || !policy.ProviderDisclosureAccepted ||
		strings.TrimSpace(policy.PromptVersion) == "" || !policyAllowsFeature(policy, FeatureCalendarRecommendation) ||
		strings.TrimSpace(policy.CalendarRecommendationModelProfileID) == "" {
		return nil, ErrAIDenied
	}
	models, err := p.control.FindModels(ctx, target, []string{policy.CalendarRecommendationModelProfileID})
	if err != nil || len(models) != 1 {
		return nil, ErrAIDenied
	}
	model := models[0]
	if model.ID != policy.CalendarRecommendationModelProfileID || model.MSPID != safe.MSPID || !model.Enabled ||
		ValidateModelProfile(model) != nil || !modelSupportsFeature(model, FeatureCalendarRecommendation) ||
		!model.ZeroCost {
		return nil, ErrAIDenied
	}
	connection, err := p.control.GetConnection(ctx, target, model.ConnectionID)
	if err != nil || connection.MSPID != safe.MSPID || !connection.Enabled || connection.Health != HealthHealthy ||
		connection.DisclosureAcceptedAt == nil || ValidateConnection(connection) != nil {
		return nil, ErrAIDenied
	}
	adapter, ok := p.registry.Lookup(connection.Adapter)
	if !ok {
		return nil, ErrAIDenied
	}
	encodedContext, err := json.Marshal(safe)
	if err != nil {
		return nil, ErrAIDenied
	}
	request := ProviderRequest{
		Feature: FeatureCalendarRecommendation, Provider: string(connection.Adapter), Model: model.ProviderModelID,
		PromptVersion: policy.PromptVersion, MSPID: safe.MSPID, WorkRecordID: safe.ProjectionID,
		Fields:                 []ContextField{{Name: "calendar_context", Value: string(encodedContext), Classification: ContextStandard}},
		AuthorizedCandidateIDs: append([]string(nil), safe.AuthorizedTechnicianIDs...), MaxOutputUnits: model.OutputLimit,
	}
	encodedRequest, err := PrepareProviderRequest(adapter.Type(), model, request)
	if err != nil || int64(len(encodedRequest))+model.OutputLimit > model.ContextLimit || int64(len(encodedRequest)) > connection.RequestLimitBytes {
		return nil, ErrAIDenied
	}
	var generated GenerationResult
	generate := func(credential []byte) error {
		privateCredential := append([]byte(nil), credential...)
		defer wipeCredential(privateCredential)
		result, generateErr := adapter.Generate(ctx, connection, model, request, privateCredential)
		generated = result
		return generateErr
	}
	if connection.CredentialConfigured {
		if p.credentials == nil || p.credentials.UseCredential(ctx, connection, generate) != nil {
			return nil, ErrAIDenied
		}
	} else if err := generate(nil); err != nil {
		return nil, err
	}
	if generated.CalendarCandidates == nil || !calendarCandidateSubset(generated.CalendarCandidates, safe.AuthorizedTechnicianIDs) {
		return nil, ErrInvalidAIOutput
	}
	return append([]CalendarProviderCandidate(nil), generated.CalendarCandidates...), nil
}

func policyAllowsFeature(policy Policy, wanted Feature) bool {
	for _, feature := range policy.AllowedFeatures {
		if feature == wanted {
			return true
		}
	}
	return false
}

func calendarCandidateSubset(candidates []CalendarProviderCandidate, authorized []string) bool {
	ids := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.TechnicianID)
	}
	return candidateSubset(ids, authorized)
}
