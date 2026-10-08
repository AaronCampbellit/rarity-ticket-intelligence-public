// Package aiassist defines the constrained, technician-controlled AI boundary.
// It produces labeled recommendations only; it cannot mutate work, send
// messages, or start automation.
package aiassist

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

var (
	ErrAIDenied        = errors.New("AI assistance denied")
	ErrInvalidAIOutput = errors.New("invalid AI output")
)

type Feature string

const (
	FeatureSummary    Feature = "summary"
	FeatureReplyDraft Feature = "reply_draft"
	FeatureSimilar    Feature = "similar_suggestions"
	// FeatureClassification is deliberately a separate provider contract. It
	// produces tag candidates only and is never interpreted as free-form work.
	FeatureClassification Feature = "classification"
	// FeatureCalendarRecommendation returns scheduling candidates only. The
	// application validates them through deterministic calendar preview and
	// never exposes an Apply tool to the provider.
	FeatureCalendarRecommendation Feature = "calendar_recommendation"
)

// SubjectRef gives generation jobs a typed, scoped subject while keeping the
// legacy work-record field available to existing endpoints during migration.
type SubjectRef struct {
	Type     string
	ID       string
	MSPID    string
	ClientID string
}

// ClassificationCandidate is the complete, provider-owned classification
// output. The application layer validates each identifier against the active
// catalog captured for the request.
type ClassificationCandidate struct {
	TagID      string  `json:"tag_id"`
	Confidence float64 `json:"confidence"`
	Rationale  string  `json:"rationale"`
}

type Policy struct {
	MSPID                                string
	Enabled                              bool
	Provider                             string
	Model                                string
	ProviderDisclosureAccepted           bool
	PromptVersion                        string
	AllowedFeatures                      []Feature
	SummaryModelProfileID                string
	ReplyDraftModelProfileID             string
	SimilarSuggestionsModelProfileID     string
	ClassificationModelProfileID         string
	CalendarRecommendationModelProfileID string
	CostLimitEnabled                     bool
	AllowUnmeteredUnknown                bool
	MonthlyCostLimitMinor                int64
	CurrentMonthlyCostMinor              *int64
	Version                              int64
}

type ContextClassification string

const (
	ContextStandard   ContextClassification = "standard"
	ContextSensitive  ContextClassification = "sensitive"
	ContextSecret     ContextClassification = "secret"
	ContextAttachment ContextClassification = "attachment"
)

type ContextField struct {
	Name           string
	Value          string
	Classification ContextClassification
}

type Command struct {
	Principal              authorization.Principal
	Policy                 Policy
	Feature                Feature
	ClientID               string
	WorkRecordID           string
	Fields                 []ContextField
	AuthorizedCandidateIDs []string
	MaxOutputUnits         int64
}

type ProviderRequest struct {
	Feature                Feature
	Provider               string
	Model                  string
	PromptVersion          string
	MSPID                  string
	ClientID               string
	WorkRecordID           string
	Fields                 []ContextField
	AuthorizedCandidateIDs []string
	MaxOutputUnits         int64
}

type ProviderResponse struct {
	Text         string
	Confidence   float64
	CandidateIDs []string
}

type Provider interface {
	Generate(context.Context, ProviderRequest) (ProviderResponse, error)
}

type RecommendationState string

const RecommendationPendingHuman RecommendationState = "pending_human"

type Recommendation struct {
	ID             string
	Feature        Feature
	MSPID          string
	ClientID       string
	WorkRecordID   string
	Text           string
	CandidateIDs   []string
	Confidence     *float64
	Provider       string
	Model          string
	PromptVersion  string
	State          RecommendationState
	GeneratedAt    time.Time
	RelevantInputs []string
}

type Store interface {
	Record(context.Context, RecommendationRecord) error
}

type Service struct {
	provider Provider
	store    Store
	now      func() time.Time
	newID    func() string
}

func NewService(
	provider Provider,
	store Store,
	now func() time.Time,
	newID func() string,
) *Service {
	return &Service{provider: provider, store: store, now: now, newID: newID}
}

func (s *Service) Generate(ctx context.Context, command Command) (Recommendation, error) {
	if s.provider == nil || s.store == nil || s.now == nil || s.newID == nil ||
		!validPolicy(command.Policy, command.Feature) ||
		command.Policy.MSPID != command.Principal.Scope.MSPID ||
		strings.TrimSpace(command.ClientID) == "" ||
		strings.TrimSpace(command.WorkRecordID) == "" {
		return Recommendation{}, ErrAIDenied
	}
	target := scope.Target{MSPID: command.Policy.MSPID, ClientID: command.ClientID}
	if err := authorization.Authorize(command.Principal, "ai.assist", target); err != nil {
		return Recommendation{}, ErrAIDenied
	}
	fields := minimizedFields(command.Fields)
	if len(fields) == 0 {
		return Recommendation{}, ErrAIDenied
	}
	request := ProviderRequest{
		Feature: command.Feature, Provider: command.Policy.Provider,
		Model: command.Policy.Model, PromptVersion: command.Policy.PromptVersion,
		MSPID: command.Policy.MSPID, ClientID: command.ClientID,
		WorkRecordID: command.WorkRecordID, Fields: fields,
		AuthorizedCandidateIDs: append([]string(nil), command.AuthorizedCandidateIDs...),
	}
	response, err := s.provider.Generate(ctx, request)
	if err != nil {
		return Recommendation{}, err
	}
	now := s.now().UTC()
	recommendationID, usageID, auditID, eventID, correlationID :=
		s.newID(), s.newID(), s.newID(), s.newID(), s.newID()
	confidence := response.Confidence
	record, err := BuildRecommendationRecord(RecommendationCommand{
		Job: GenerationJob{ID: "legacy", MSPID: command.Policy.MSPID, ClientID: command.ClientID,
			WorkRecordID: command.WorkRecordID, RequestedBy: command.Principal.ID, Feature: command.Feature},
		Policy: command.Policy,
		Model: ModelProfile{ID: "legacy", MSPID: command.Policy.MSPID, ConnectionID: "legacy",
			ProviderModelID: command.Policy.Model, DisplayName: command.Policy.Model,
			SupportedFeatures: []Feature{command.Feature}, ContextLimit: 1, OutputLimit: 1, Enabled: true, Version: 1},
		Connection: ProviderConnection{ID: "legacy", MSPID: command.Policy.MSPID, Name: command.Policy.Provider,
			BaseURL: "https://legacy.example", Adapter: AdapterOpenAICompatible, Network: NetworkRemote,
			Enabled: true, Timeout: time.Minute, RequestLimitBytes: minimumProviderPayloadBytes,
			ResponseLimitBytes: minimumProviderPayloadBytes, Health: HealthHealthy, Version: 1},
		Context: GenerationContext{Fields: fields, AuthorizedCandidateIDs: command.AuthorizedCandidateIDs},
	}, GenerationResult{Text: response.Text, Confidence: &confidence, CandidateIDs: response.CandidateIDs}, now,
		RecommendationIDs{RecommendationID: recommendationID, UsageID: usageID, AuditID: auditID, EventID: eventID, CorrelationID: correlationID})
	if err != nil {
		return Recommendation{}, err
	}
	// The legacy public contract carries the historical provider name rather
	// than the provider-adapter identifier used by durable jobs.
	record.Recommendation.Provider = command.Policy.Provider
	record.Recommendation.Model = command.Policy.Model
	record.Usage.Provider = command.Policy.Provider
	record.Usage.Model = command.Policy.Model
	if err := s.store.Record(ctx, record); err != nil {
		return Recommendation{}, err
	}
	return record.Recommendation, nil
}

func validPolicy(policy Policy, feature Feature) bool {
	if !policy.Enabled || !policy.ProviderDisclosureAccepted ||
		strings.TrimSpace(policy.MSPID) == "" ||
		strings.TrimSpace(policy.Provider) == "" ||
		strings.TrimSpace(policy.Model) == "" ||
		strings.TrimSpace(policy.PromptVersion) == "" {
		return false
	}
	for _, allowed := range policy.AllowedFeatures {
		if allowed == feature &&
			(feature == FeatureSummary || feature == FeatureReplyDraft || feature == FeatureSimilar || feature == FeatureClassification) {
			return true
		}
	}
	return false
}

func minimizedFields(fields []ContextField) []ContextField {
	result := make([]ContextField, 0, len(fields))
	total := 0
	for _, field := range fields {
		if field.Classification != ContextStandard ||
			strings.TrimSpace(field.Name) == "" ||
			strings.TrimSpace(field.Value) == "" ||
			len(result) >= 20 {
			continue
		}
		if total+len(field.Value) > 32_000 {
			break
		}
		result = append(result, field)
		total += len(field.Value)
	}
	return result
}

func candidateSubset(candidateIDs, authorized []string) bool {
	allowed := make(map[string]struct{}, len(authorized))
	for _, value := range authorized {
		allowed[value] = struct{}{}
	}
	for _, value := range candidateIDs {
		if _, ok := allowed[value]; !ok {
			return false
		}
	}
	return true
}
