package aiassist

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestOpenAICompatibleAdapterContractUsesFixedEndpointsAndOptionalCredential(t *testing.T) {
	transport := &adapterTransport{responses: []HTTPResponse{
		{Body: []byte(`{"data":[{"id":"gpt-compatible","owned_by":"local"}]}`)},
		{Body: []byte(`{"choices":[{"message":{"content":"{\"text\":\"Ask the user to reconnect.\",\"confidence\":0.6,\"candidate_ids\":[\"candidate-1\"]}"}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`)},
	}}
	fixture := &providerFixture{credential: []byte("compatible-token"), transport: transport}
	runAdapterContract(t, NewOpenAICompatibleAdapter(transport), adapterConnection(AdapterOpenAICompatible), adapterModel(AdapterOpenAICompatible), fixture)
	if len(transport.requests) != 2 || transport.requests[0].Method != "GET" || transport.requests[0].Path != "/v1/models" ||
		transport.requests[1].Method != "POST" || transport.requests[1].Path != "/v1/chat/completions" ||
		transport.requests[0].Headers.Get("Authorization") != "Bearer compatible-token" || transport.requests[1].Headers.Get("Authorization") != "Bearer compatible-token" {
		t.Fatalf("requests=%+v", transport.requests)
	}
	var request struct {
		Stream bool `json:"stream"`
	}
	if err := json.Unmarshal(transport.requests[1].Body, &request); err != nil || request.Stream {
		t.Fatalf("chat request=%s error=%v", transport.requests[1].Body, err)
	}

	noCredential := &adapterTransport{responses: []HTTPResponse{{Body: []byte(`{"data":[{"id":"gpt-compatible"}]}`)}}}
	if _, err := NewOpenAICompatibleAdapter(noCredential).Discover(context.Background(), adapterConnection(AdapterOpenAICompatible), nil); err != nil || noCredential.requests[0].Headers.Get("Authorization") != "" {
		t.Fatalf("optional credential error=%v request=%+v", err, noCredential.requests)
	}
}

func TestOpenAICompatibleAdapterRejectsMultipleChoicesAndInvalidCandidateStructure(t *testing.T) {
	for name, body := range map[string]string{
		"multiple choices":    `{"choices":[{"message":{"content":"{\"text\":\"first\"}"}},{"message":{"content":"{\"text\":\"second\"}"}}]}`,
		"duplicate candidate": `{"choices":[{"message":{"content":"{\"text\":\"answer\",\"candidate_ids\":[\"candidate-1\",\"candidate-1\"]}"}}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			transport := &adapterTransport{responses: []HTTPResponse{{Body: []byte(body)}}}
			_, err := NewOpenAICompatibleAdapter(transport).Generate(context.Background(), adapterConnection(AdapterOpenAICompatible), adapterModel(AdapterOpenAICompatible), providerRequestFixture(), nil)
			if !errors.Is(err, ErrInvalidProviderResponse) {
				t.Fatalf("Generate() error=%v", err)
			}
		})
	}
}

func TestOpenAICompatibleAdapterRejectsMalformedOutputWithoutProviderLeakage(t *testing.T) {
	secret := "provider secret response"
	transport := &adapterTransport{responses: []HTTPResponse{{Body: []byte(`{"choices":[{"message":{"content":"` + secret + `"}}]}`)}}}
	_, err := NewOpenAICompatibleAdapter(transport).Generate(context.Background(), adapterConnection(AdapterOpenAICompatible), adapterModel(AdapterOpenAICompatible), providerRequestFixture(), []byte("credential-secret"))
	if !errors.Is(err, ErrInvalidProviderResponse) || strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "credential-secret") {
		t.Fatalf("Generate() error=%v", err)
	}
}
