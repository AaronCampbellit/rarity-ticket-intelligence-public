package aiassist

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

const (
	ollamaTagsPath = "/api/tags"
	ollamaChatPath = "/api/chat"
)

// OllamaAdapter implements only Ollama's native model-list and chat endpoints.
// It intentionally never forwards credentials because those endpoints are
// normally local and this adapter does not define an authorization contract.
type OllamaAdapter struct {
	transport Transport
}

func NewOllamaAdapter(transport Transport) *OllamaAdapter {
	return &OllamaAdapter{transport: transport}
}

func (*OllamaAdapter) Type() AdapterType { return AdapterOllama }

func (a *OllamaAdapter) Discover(ctx context.Context, connection ProviderConnection, _ []byte) ([]DiscoveredModel, error) {
	if a == nil || a.transport == nil || connection.Adapter != AdapterOllama {
		return nil, ErrInvalidProviderConfiguration
	}
	response, err := a.transport.Do(ctx, connection, HTTPRequest{
		Method: http.MethodGet, Path: ollamaTagsPath, Headers: adapterHeaders(nil, false),
	})
	if err != nil {
		return nil, err
	}
	return decodeDiscoveredModels(response.Body, "models", "name")
}

func (a *OllamaAdapter) Generate(
	ctx context.Context,
	connection ProviderConnection,
	model ModelProfile,
	request ProviderRequest,
	_ []byte,
) (GenerationResult, error) {
	if a == nil || a.transport == nil || connection.Adapter != AdapterOllama ||
		strings.TrimSpace(model.ProviderModelID) == "" {
		return GenerationResult{}, ErrInvalidProviderConfiguration
	}
	if request.MaxOutputUnits <= 0 {
		request.MaxOutputUnits = model.OutputLimit
	}
	body, err := PrepareProviderRequest(AdapterOllama, model, request)
	if err != nil {
		return GenerationResult{}, ErrInvalidProviderConfiguration
	}
	response, err := a.transport.Do(ctx, connection, HTTPRequest{
		Method: http.MethodPost, Path: ollamaChatPath, Headers: adapterHeaders(nil, false), Body: body,
	})
	if err != nil {
		return GenerationResult{}, err
	}
	var payload struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		PromptEvalCount *int64 `json:"prompt_eval_count"`
		EvalCount       *int64 `json:"eval_count"`
	}
	if json.Unmarshal(response.Body, &payload) != nil || payload.Message.Content == "" || invalidUsage(payload.PromptEvalCount, payload.EvalCount) {
		return GenerationResult{}, ErrInvalidProviderResponse
	}
	var result GenerationResult
	if request.Feature == FeatureClassification {
		result, err = parseClassificationGeneration(payload.Message.Content, request.AuthorizedCandidateIDs)
	} else if request.Feature == FeatureCalendarRecommendation {
		result, err = parseCalendarRecommendationGeneration(payload.Message.Content, request.AuthorizedCandidateIDs)
	} else {
		result, err = parseStructuredRecommendation(payload.Message.Content)
	}
	if err != nil {
		return GenerationResult{}, err
	}
	result.Usage = Usage{InputUnits: payload.PromptEvalCount, OutputUnits: payload.EvalCount}
	if result.Usage.OutputUnits != nil && *result.Usage.OutputUnits > request.MaxOutputUnits {
		return GenerationResult{}, ErrInvalidProviderResponse
	}
	return result, nil
}
