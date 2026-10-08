package aiassist

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"time"
)

var ErrInvalidProviderResponse = errors.New("invalid AI provider response")

var ErrProviderCredentialUnavailable = errors.New("AI provider credential unavailable")

type Usage struct {
	InputUnits, OutputUnits *int64
	CostMinor               *int64
}

type GenerationResult struct {
	Text                     string
	Confidence               *float64
	CandidateIDs             []string
	ClassificationCandidates []ClassificationCandidate
	CalendarCandidates       []CalendarProviderCandidate
	Usage                    Usage
}

// Adapter is deliberately limited to the two provider protocols selected by
// the control plane. It has no administrator-configurable paths, headers,
// templates, or response selectors.
type Adapter interface {
	Type() AdapterType
	Discover(context.Context, ProviderConnection, []byte) ([]DiscoveredModel, error)
	Generate(context.Context, ProviderConnection, ModelProfile, ProviderRequest, []byte) (GenerationResult, error)
}

// PrepareProviderRequest is the sole request encoder for generation. Workers
// use its actual encoded length for context/cost governance; adapters pass the
// same bytes to transport, so escaping and fixed protocol framing cannot make
// a request exceed a preflight estimate.
func PrepareProviderRequest(adapter AdapterType, model ModelProfile, request ProviderRequest) ([]byte, error) {
	prompt, err := boundedPrompt(request)
	if err != nil {
		return nil, err
	}
	if request.MaxOutputUnits <= 0 {
		request.MaxOutputUnits = model.OutputLimit
	}
	if strings.TrimSpace(model.ProviderModelID) == "" || request.MaxOutputUnits <= 0 || request.MaxOutputUnits > model.OutputLimit {
		return nil, ErrInvalidProviderConfiguration
	}
	instruction := recommendationInstruction
	if request.Feature == FeatureClassification {
		instruction = classificationInstruction
	} else if request.Feature == FeatureCalendarRecommendation {
		instruction = calendarRecommendationInstruction
	}
	message := []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}{{Role: "system", Content: instruction}, {Role: "user", Content: prompt}}
	switch adapter {
	case AdapterOllama:
		body := struct {
			Model   string `json:"model"`
			Stream  bool   `json:"stream"`
			Format  string `json:"format"`
			Options struct {
				NumPredict int64 `json:"num_predict"`
			} `json:"options"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}{Model: model.ProviderModelID, Stream: false, Format: "json", Messages: message}
		body.Options.NumPredict = request.MaxOutputUnits
		return json.Marshal(body)
	case AdapterOpenAICompatible:
		body := struct {
			Model          string `json:"model"`
			Stream         bool   `json:"stream"`
			MaxTokens      int64  `json:"max_tokens"`
			ResponseFormat struct {
				Type string `json:"type"`
			} `json:"response_format"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}{Model: model.ProviderModelID, Stream: false, MaxTokens: request.MaxOutputUnits, Messages: message}
		body.ResponseFormat.Type = "json_object"
		return json.Marshal(body)
	default:
		return nil, ErrInvalidProviderConfiguration
	}
}

// AdapterRegistry is constructed once during application composition. Its map
// is never exposed or mutated after construction, making concurrent lookup
// safe without a lock.
type AdapterRegistry struct {
	values map[AdapterType]Adapter
}

var _ ProviderOperations = (*AdapterRegistry)(nil)

func NewAdapterRegistry(adapters ...Adapter) (*AdapterRegistry, error) {
	values := make(map[AdapterType]Adapter, len(adapters))
	for _, adapter := range adapters {
		if nilAdapter(adapter) {
			return nil, ErrInvalidProviderConfiguration
		}
		adapterType := adapter.Type()
		if !validAdapter(adapterType) || values[adapterType] != nil {
			return nil, ErrInvalidProviderConfiguration
		}
		values[adapterType] = adapter
	}
	return &AdapterRegistry{values: values}, nil
}

func nilAdapter(adapter Adapter) bool {
	if adapter == nil {
		return true
	}
	value := reflect.ValueOf(adapter)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

// Lookup returns an immutable adapter reference. Adapter implementations must
// themselves retain no per-request mutable state.
func (r *AdapterRegistry) Lookup(adapterType AdapterType) (Adapter, bool) {
	if r == nil {
		return nil, false
	}
	adapter, ok := r.values[adapterType]
	return adapter, ok
}

// Test is intentionally a read-only Discover call for management connection
// and authentication validation. Credential-aware management composition uses
// AdapterOperations below; this registry-only bridge remains compatible with
// the original credential-free ProviderOperations contract.
func (r *AdapterRegistry) Test(ctx context.Context, connection ProviderConnection) error {
	_, err := r.Discover(ctx, connection)
	return err
}

func (r *AdapterRegistry) Discover(ctx context.Context, connection ProviderConnection) ([]DiscoveredModel, error) {
	adapter, ok := r.Lookup(connection.Adapter)
	if !ok {
		return nil, ErrInvalidProviderConfiguration
	}
	models, err := adapter.Discover(ctx, connection, nil)
	if err != nil {
		return nil, err
	}
	return append([]DiscoveredModel(nil), models...), nil
}

// CredentialProvider synchronously invokes use exactly once for a configured
// secret, then owns wiping or releasing its buffer before UseCredential
// returns. The callback copies and wipes its private credential buffer after
// provider I/O; adapters and transports must not retain it. Implementations
// must respect ctx cancellation, never invoke use after returning, and never
// expose secret material through errors. AdapterOperations waits for an
// already-started callback, but treats a source return during that callback as
// a contract violation and discards its result.
type CredentialProvider interface {
	UseCredential(context.Context, ProviderConnection, func([]byte) error) error
}

// AdapterOperations binds the immutable adapter registry to the credential
// opening boundary used by management connection tests and model discovery.
// The existing ProviderOperations interface remains intentionally
// credential-free, so ManagementService need not receive secret material.
type AdapterOperations struct {
	registry    *AdapterRegistry
	credentials CredentialProvider
}

var _ ProviderOperations = (*AdapterOperations)(nil)

func NewAdapterOperations(registry *AdapterRegistry, credentials CredentialProvider) *AdapterOperations {
	return &AdapterOperations{registry: registry, credentials: credentials}
}

func (o *AdapterOperations) Test(ctx context.Context, connection ProviderConnection) error {
	// Test-as-Discover is intentional: the fixed read-only model endpoint
	// validates reachability and, where configured, endpoint authentication.
	_, err := o.Discover(ctx, connection)
	return err
}

func (o *AdapterOperations) Discover(ctx context.Context, connection ProviderConnection) ([]DiscoveredModel, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if o == nil || o.registry == nil {
		return nil, ErrInvalidProviderConfiguration
	}
	adapter, ok := o.registry.Lookup(connection.Adapter)
	if !ok {
		return nil, ErrInvalidProviderConfiguration
	}
	if !connection.CredentialConfigured {
		models, err := adapter.Discover(ctx, connection, nil)
		if err != nil {
			return nil, err
		}
		return append([]DiscoveredModel(nil), models...), nil
	}
	if o.credentials == nil {
		return nil, ErrProviderCredentialUnavailable
	}
	state := newCredentialCallbackState()
	err := o.credentials.UseCredential(ctx, connection, func(credential []byte) error {
		if !state.claim() {
			return ErrProviderCredentialUnavailable
		}
		var (
			models      []DiscoveredModel
			callbackErr error
		)
		defer func() { state.complete(models, callbackErr) }()
		if err := ctx.Err(); err != nil {
			callbackErr = err
			return err
		}
		if len(credential) == 0 {
			callbackErr = ErrProviderCredentialUnavailable
			return ErrProviderCredentialUnavailable
		}
		privateCredential := append([]byte(nil), credential...)
		defer wipeCredential(privateCredential)
		value, err := adapter.Discover(ctx, connection, privateCredential)
		if err != nil {
			callbackErr = err
			return adapterOperationError{err: err}
		}
		models = append([]DiscoveredModel(nil), value...)
		return nil
	})
	result := state.closeAndWait()
	if errors.Is(err, context.Canceled) {
		return nil, context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return nil, context.DeadlineExceeded
	}
	if errors.Is(result.err, context.Canceled) {
		return nil, context.Canceled
	}
	if errors.Is(result.err, context.DeadlineExceeded) {
		return nil, context.DeadlineExceeded
	}
	if result.violation || result.calls != 1 {
		return nil, ErrProviderCredentialUnavailable
	}
	if err != nil {
		var operationError adapterOperationError
		if !errors.As(err, &operationError) {
			return nil, ErrProviderCredentialUnavailable
		}
	}
	if result.err != nil {
		return nil, result.err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return append([]DiscoveredModel(nil), result.models...), nil
}

type credentialCallbackState struct {
	mu        sync.Mutex
	ready     *sync.Cond
	calls     int
	active    int
	closed    bool
	violation bool
	models    []DiscoveredModel
	err       error
}

type credentialCallbackResult struct {
	calls     int
	violation bool
	models    []DiscoveredModel
	err       error
}

func newCredentialCallbackState() *credentialCallbackState {
	state := &credentialCallbackState{}
	state.ready = sync.NewCond(&state.mu)
	return state
}

func (s *credentialCallbackState) claim() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.calls != 0 {
		s.violation = true
		return false
	}
	s.calls++
	s.active++
	return true
}

func (s *credentialCallbackState) complete(models []DiscoveredModel, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.models = append([]DiscoveredModel(nil), models...)
	s.err = err
	s.active--
	s.ready.Broadcast()
}

func (s *credentialCallbackState) closeAndWait() credentialCallbackResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	if s.active > 0 {
		s.violation = true
	}
	for s.active > 0 {
		s.ready.Wait()
	}
	return credentialCallbackResult{
		calls: s.calls, violation: s.violation,
		models: append([]DiscoveredModel(nil), s.models...), err: s.err,
	}
}

func wipeCredential(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

type adapterOperationError struct{ err error }

func (e adapterOperationError) Error() string { return "adapter operation failed" }

const maximumDiscoveredModels = 1000

func decodeDiscoveredModels(body []byte, collection, identityField string) ([]DiscoveredModel, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, ErrInvalidProviderResponse
	}
	found := false
	models := make([]DiscoveredModel, 0)
	seen := make(map[string]struct{})
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return nil, ErrInvalidProviderResponse
		}
		name, ok := key.(string)
		if !ok {
			return nil, ErrInvalidProviderResponse
		}
		if name != collection {
			var ignored json.RawMessage
			if decoder.Decode(&ignored) != nil {
				return nil, ErrInvalidProviderResponse
			}
			continue
		}
		if found {
			return nil, ErrInvalidProviderResponse
		}
		found = true
		arrayStart, err := decoder.Token()
		if err != nil || arrayStart != json.Delim('[') {
			return nil, ErrInvalidProviderResponse
		}
		for decoder.More() {
			if len(models) >= maximumDiscoveredModels {
				return nil, ErrInvalidProviderResponse
			}
			var item struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			}
			if decoder.Decode(&item) != nil {
				return nil, ErrInvalidProviderResponse
			}
			identity := item.ID
			if identityField == "name" {
				identity = item.Name
			}
			identity = strings.TrimSpace(identity)
			if identity == "" || len(identity) > maximumPromptCandidateID {
				return nil, ErrInvalidProviderResponse
			}
			if _, duplicate := seen[identity]; duplicate {
				return nil, ErrInvalidProviderResponse
			}
			seen[identity] = struct{}{}
			models = append(models, DiscoveredModel{
				ProviderModelID: identity, DisplayName: identity, ContextLimit: defaultDiscoveredContextLimit,
			})
		}
		arrayEnd, err := decoder.Token()
		if err != nil || arrayEnd != json.Delim(']') {
			return nil, ErrInvalidProviderResponse
		}
	}
	objectEnd, err := decoder.Token()
	if err != nil || objectEnd != json.Delim('}') || !found || len(models) == 0 || ensureJSONEOF(decoder) != nil {
		return nil, ErrInvalidProviderResponse
	}
	return models, nil
}

const (
	maximumPromptFields       = 20
	maximumPromptFieldName    = 256
	maximumPromptFieldValue   = 32_000
	maximumPromptCandidates   = 20
	maximumPromptCandidateID  = 256
	maximumRecommendationText = 32_000
)

type recommendationPrompt struct {
	Feature                Feature       `json:"feature"`
	Fields                 []promptField `json:"fields"`
	AuthorizedCandidateIDs []string      `json:"authorized_candidate_ids"`
}

type promptField struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

const recommendationInstruction = "Return exactly one JSON object with only text, confidence, and candidate_ids. text must be a concise technician recommendation. confidence, when known, must be a number from 0 through 1. candidate_ids, when present, must contain only identifiers from authorized_candidate_ids. Do not include markdown or any explanation outside the JSON object."
const classificationInstruction = "Return exactly one JSON object with only suggestions. suggestions must be an array of objects with only tag_id, confidence, and rationale. Each tag_id must be one of authorized_candidate_ids. confidence must be a number from 0 through 1. rationale must be concise. Do not include markdown or any explanation outside the JSON object."
const calendarRecommendationInstruction = "Return exactly one JSON object with only candidates. candidates must be an array of objects with only technician_id, starts_at, ends_at, timezone, allocation, explanation, and tradeoffs. technician_id must be one of authorized_candidate_ids. starts_at and ends_at must be RFC3339 timestamps with ends_at after starts_at. timezone must be an IANA timezone. allocation, when present, has only technician_id and planned_minutes. explanation and tradeoffs must be concise. Do not include proposal IDs, apply instructions, markdown, or text outside the JSON object."

const (
	maximumClassificationSuggestions = 20
	maximumClassificationRationale   = 2048
)

// ParseClassificationOutput accepts precisely the typed schema used for tag
// classification. It intentionally rejects unknown fields and duplicate or
// unauthorized IDs, preventing a provider response from broadening its own
// authority.
func ParseClassificationOutput(body []byte, authorizedIDs []string) ([]ClassificationCandidate, error) {
	var response struct {
		Suggestions []ClassificationCandidate `json:"suggestions"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil || ensureJSONEOF(decoder) != nil || len(response.Suggestions) > maximumClassificationSuggestions {
		return nil, ErrInvalidAIOutput
	}
	allowed := make(map[string]struct{}, len(authorizedIDs))
	for _, id := range authorizedIDs {
		if strings.TrimSpace(id) == "" {
			return nil, ErrInvalidAIOutput
		}
		allowed[id] = struct{}{}
	}
	seen := make(map[string]struct{}, len(response.Suggestions))
	for _, candidate := range response.Suggestions {
		if strings.TrimSpace(candidate.TagID) == "" || candidate.Confidence < 0 || candidate.Confidence > 1 ||
			strings.TrimSpace(candidate.Rationale) == "" || len(candidate.Rationale) > maximumClassificationRationale {
			return nil, ErrInvalidAIOutput
		}
		if _, ok := allowed[candidate.TagID]; !ok {
			return nil, ErrInvalidAIOutput
		}
		if _, duplicate := seen[candidate.TagID]; duplicate {
			return nil, ErrInvalidAIOutput
		}
		seen[candidate.TagID] = struct{}{}
	}
	result := make([]ClassificationCandidate, len(response.Suggestions))
	copy(result, response.Suggestions)
	return result, nil
}

func parseClassificationGeneration(content string, authorizedIDs []string) (GenerationResult, error) {
	candidates, err := ParseClassificationOutput([]byte(content), authorizedIDs)
	if err != nil {
		return GenerationResult{}, ErrInvalidProviderResponse
	}
	ids := make([]string, 0, len(candidates))
	maxConfidence := 0.0
	for _, candidate := range candidates {
		ids = append(ids, candidate.TagID)
		if candidate.Confidence > maxConfidence {
			maxConfidence = candidate.Confidence
		}
	}
	return GenerationResult{Text: content, Confidence: &maxConfidence, CandidateIDs: ids, ClassificationCandidates: candidates}, nil
}

const (
	maximumCalendarCandidates  = 10
	maximumCalendarExplanation = 2048
	maximumCalendarTradeoffs   = 10
	maximumCalendarTradeoff    = 512
)

// ParseCalendarRecommendationOutput rejects any provider attempt to broaden
// technician authority or smuggle mutation instructions into the response.
func ParseCalendarRecommendationOutput(body []byte, authorizedIDs []string) ([]CalendarProviderCandidate, error) {
	var response struct {
		Candidates []CalendarProviderCandidate `json:"candidates"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil || ensureJSONEOF(decoder) != nil || len(response.Candidates) > maximumCalendarCandidates {
		return nil, ErrInvalidAIOutput
	}
	allowed := make(map[string]struct{}, len(authorizedIDs))
	for _, id := range authorizedIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			return nil, ErrInvalidAIOutput
		}
		allowed[id] = struct{}{}
	}
	for _, candidate := range response.Candidates {
		if _, ok := allowed[candidate.TechnicianID]; !ok || candidate.StartsAt == nil || candidate.EndsAt == nil ||
			!candidate.EndsAt.After(*candidate.StartsAt) || strings.TrimSpace(candidate.Timezone) == "" ||
			strings.TrimSpace(candidate.Explanation) == "" || len(candidate.Explanation) > maximumCalendarExplanation ||
			len(candidate.Tradeoffs) > maximumCalendarTradeoffs {
			return nil, ErrInvalidAIOutput
		}
		if _, err := time.LoadLocation(candidate.Timezone); err != nil {
			return nil, ErrInvalidAIOutput
		}
		for _, tradeoff := range candidate.Tradeoffs {
			if strings.TrimSpace(tradeoff) == "" || len(tradeoff) > maximumCalendarTradeoff {
				return nil, ErrInvalidAIOutput
			}
		}
		if candidate.Allocation != nil && (candidate.Allocation.TechnicianID != candidate.TechnicianID || candidate.Allocation.PlannedMinutes <= 0) {
			return nil, ErrInvalidAIOutput
		}
	}
	result := make([]CalendarProviderCandidate, len(response.Candidates))
	copy(result, response.Candidates)
	return result, nil
}

func parseCalendarRecommendationGeneration(content string, authorizedIDs []string) (GenerationResult, error) {
	candidates, err := ParseCalendarRecommendationOutput([]byte(content), authorizedIDs)
	if err != nil {
		return GenerationResult{}, ErrInvalidProviderResponse
	}
	return GenerationResult{Text: content, CalendarCandidates: candidates}, nil
}

func boundedPrompt(request ProviderRequest) (string, error) {
	if request.Feature != FeatureSummary && request.Feature != FeatureReplyDraft && request.Feature != FeatureSimilar && request.Feature != FeatureClassification && request.Feature != FeatureCalendarRecommendation {
		return "", ErrInvalidProviderConfiguration
	}
	fields := make([]promptField, 0, len(request.Fields))
	total := 0
	for _, field := range request.Fields {
		if field.Classification != ContextStandard || strings.TrimSpace(field.Name) == "" || strings.TrimSpace(field.Value) == "" ||
			len(field.Name) > maximumPromptFieldName || len(field.Value) > maximumPromptFieldValue || len(fields) >= maximumPromptFields {
			return "", ErrInvalidProviderConfiguration
		}
		total += len(field.Value)
		if total > maximumPromptFieldValue {
			return "", ErrInvalidProviderConfiguration
		}
		fields = append(fields, promptField{Name: field.Name, Value: field.Value})
	}
	if len(fields) == 0 {
		return "", ErrInvalidProviderConfiguration
	}
	candidates, err := boundedCandidateIDs(request.AuthorizedCandidateIDs)
	if err != nil {
		return "", err
	}
	prompt, err := json.Marshal(recommendationPrompt{
		Feature: request.Feature, Fields: fields, AuthorizedCandidateIDs: candidates,
	})
	if err != nil {
		return "", ErrInvalidProviderConfiguration
	}
	return string(prompt), nil
}

type structuredRecommendation struct {
	Text         string          `json:"text"`
	Confidence   *float64        `json:"confidence,omitempty"`
	CandidateIDs json.RawMessage `json:"candidate_ids,omitempty"`
}

func parseStructuredRecommendation(content string) (GenerationResult, error) {
	decoder := json.NewDecoder(strings.NewReader(content))
	decoder.DisallowUnknownFields()
	var response structuredRecommendation
	if err := decoder.Decode(&response); err != nil {
		return GenerationResult{}, ErrInvalidProviderResponse
	}
	if err := ensureJSONEOF(decoder); err != nil || strings.TrimSpace(response.Text) == "" || len(response.Text) > maximumRecommendationText ||
		(response.Confidence != nil && (*response.Confidence < 0 || *response.Confidence > 1)) {
		return GenerationResult{}, ErrInvalidProviderResponse
	}
	candidates, err := parseCandidateIDs(response.CandidateIDs)
	if err != nil {
		return GenerationResult{}, ErrInvalidProviderResponse
	}
	return GenerationResult{Text: response.Text, Confidence: response.Confidence, CandidateIDs: candidates}, nil
}

func parseCandidateIDs(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, err
	}
	return boundedCandidateIDs(values)
}

func boundedCandidateIDs(values []string) ([]string, error) {
	if len(values) > maximumPromptCandidates {
		return nil, ErrInvalidProviderResponse
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" || len(value) > maximumPromptCandidateID {
			return nil, ErrInvalidProviderResponse
		}
		if _, duplicate := seen[value]; duplicate {
			return nil, ErrInvalidProviderResponse
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return ErrInvalidProviderResponse
	}
	return nil
}

func adapterHeaders(credential []byte, sendAuthorization bool) http.Header {
	headers := make(http.Header)
	headers.Set("Content-Type", "application/json")
	if sendAuthorization && len(credential) > 0 {
		headers.Set("Authorization", "Bearer "+string(credential))
	}
	return headers
}
