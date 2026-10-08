package aiassist

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

const (
	openAICompatibleModelsPath = "/v1/models"
	openAICompatibleChatPath   = "/v1/chat/completions"

	// Neither list endpoint declares model context limits. This conservative
	// starting value is explicit adapter policy, not provider response data.
	defaultDiscoveredContextLimit int64 = 4_096
)

// OpenAICompatibleAdapter implements the narrow OpenAI-compatible chat API.
// Credentials are optional so self-hosted compatible endpoints remain usable.
type OpenAICompatibleAdapter struct {
	transport Transport
}

func NewOpenAICompatibleAdapter(transport Transport) *OpenAICompatibleAdapter {
	return &OpenAICompatibleAdapter{transport: transport}
}

func (*OpenAICompatibleAdapter) Type() AdapterType { return AdapterOpenAICompatible }

func (a *OpenAICompatibleAdapter) Discover(ctx context.Context, connection ProviderConnection, credential []byte) ([]DiscoveredModel, error) {
	if a == nil || a.transport == nil || connection.Adapter != AdapterOpenAICompatible {
		return nil, ErrInvalidProviderConfiguration
	}
	response, err := a.transport.Do(ctx, connection, HTTPRequest{
		Method: http.MethodGet, Path: openAICompatibleModelsPath, Headers: adapterHeaders(credential, true),
	})
	if err != nil {
		return nil, err
	}
	return decodeDiscoveredModels(response.Body, "data", "id")
}

func (a *OpenAICompatibleAdapter) Generate(
	ctx context.Context,
	connection ProviderConnection,
	model ModelProfile,
	request ProviderRequest,
	credential []byte,
) (GenerationResult, error) {
	if a == nil || a.transport == nil || connection.Adapter != AdapterOpenAICompatible ||
		strings.TrimSpace(model.ProviderModelID) == "" {
		return GenerationResult{}, ErrInvalidProviderConfiguration
	}
	if request.MaxOutputUnits <= 0 {
		request.MaxOutputUnits = model.OutputLimit
	}
	body, err := PrepareProviderRequest(AdapterOpenAICompatible, model, request)
	if err != nil {
		return GenerationResult{}, ErrInvalidProviderConfiguration
	}
	response, err := a.transport.Do(ctx, connection, HTTPRequest{
		Method: http.MethodPost, Path: openAICompatibleChatPath, Headers: adapterHeaders(credential, true), Body: body,
	})
	if err != nil {
		return GenerationResult{}, err
	}
	var payload struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     *int64 `json:"prompt_tokens"`
			CompletionTokens *int64 `json:"completion_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(response.Body, &payload) != nil || len(payload.Choices) != 1 || payload.Choices[0].Message.Content == "" ||
		invalidUsage(payload.Usage.PromptTokens, payload.Usage.CompletionTokens) {
		return GenerationResult{}, ErrInvalidProviderResponse
	}
	var result GenerationResult
	if request.Feature == FeatureClassification {
		result, err = parseClassificationGeneration(payload.Choices[0].Message.Content, request.AuthorizedCandidateIDs)
	} else if request.Feature == FeatureCalendarRecommendation {
		result, err = parseCalendarRecommendationGeneration(payload.Choices[0].Message.Content, request.AuthorizedCandidateIDs)
	} else {
		result, err = parseStructuredRecommendation(payload.Choices[0].Message.Content)
	}
	if err != nil {
		return GenerationResult{}, err
	}
	result.Usage = Usage{InputUnits: payload.Usage.PromptTokens, OutputUnits: payload.Usage.CompletionTokens}
	if result.Usage.OutputUnits != nil && *result.Usage.OutputUnits > request.MaxOutputUnits {
		return GenerationResult{}, ErrInvalidProviderResponse
	}
	return result, nil
}

func invalidUsage(values ...*int64) bool {
	for _, value := range values {
		if value != nil && *value < 0 {
			return true
		}
	}
	return false
}
