package aiassist

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestOllamaAdapterContractUsesOnlyNativeEndpointsWithoutAuthorization(t *testing.T) {
	transport := &adapterTransport{responses: []HTTPResponse{
		{Body: []byte(`{"models":[{"name":"llama3:8b","details":{"family":"llama"}}]}`)},
		{Body: []byte(`{"message":{"content":"{\"text\":\"Restart the VPN client.\",\"confidence\":0.8,\"candidate_ids\":[\"candidate-1\"]}"},"prompt_eval_count":12,"eval_count":8}`)},
	}}
	fixture := &providerFixture{credential: []byte("must-not-send"), transport: transport}
	runAdapterContract(t, NewOllamaAdapter(transport), adapterConnection(AdapterOllama), adapterModel(AdapterOllama), fixture)
	if len(transport.requests) != 2 || transport.requests[0].Method != "GET" || transport.requests[0].Path != "/api/tags" ||
		transport.requests[1].Method != "POST" || transport.requests[1].Path != "/api/chat" ||
		transport.requests[0].Headers.Get("Authorization") != "" || transport.requests[1].Headers.Get("Authorization") != "" {
		t.Fatalf("requests=%+v", transport.requests)
	}
	var request struct {
		Stream bool `json:"stream"`
	}
	if err := json.Unmarshal(transport.requests[1].Body, &request); err != nil || request.Stream {
		t.Fatalf("chat request=%s error=%v", transport.requests[1].Body, err)
	}
}

func TestOllamaAdapterRejectsMalformedStructuredOutputWithoutLeakingProviderData(t *testing.T) {
	secret := "provider secret response"
	transport := &adapterTransport{responses: []HTTPResponse{{Body: []byte(`{"message":{"content":"not JSON` + secret + `"}}`)}}}
	_, err := NewOllamaAdapter(transport).Generate(context.Background(), adapterConnection(AdapterOllama), adapterModel(AdapterOllama), providerRequestFixture(), nil)
	if !errors.Is(err, ErrInvalidProviderResponse) || strings.Contains(err.Error(), secret) {
		t.Fatalf("Generate() error=%v", err)
	}
}

func TestOllamaAdapterRejectsTransportOversizeAndProviderErrorWithoutLeakage(t *testing.T) {
	secret := "provider secret response"
	for name, err := range map[string]error{
		"oversize": ErrProviderResponseTooLarge,
		"failure":  ErrProviderNonSuccess,
	} {
		t.Run(name, func(t *testing.T) {
			transport := &adapterTransport{errs: []error{err}}
			_, got := NewOllamaAdapter(transport).Discover(context.Background(), adapterConnection(AdapterOllama), []byte(secret))
			if !errors.Is(got, err) || strings.Contains(got.Error(), secret) {
				t.Fatalf("Discover() error=%v", got)
			}
		})
	}
}
