package aiassist

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type providerFixture struct {
	credential []byte
	transport  *adapterTransport
}

type adapterTransport struct {
	requests  []HTTPRequest
	responses []HTTPResponse
	errs      []error
}

func (t *adapterTransport) Do(_ context.Context, _ ProviderConnection, request HTTPRequest) (HTTPResponse, error) {
	t.requests = append(t.requests, HTTPRequest{
		Method: request.Method, Path: request.Path, Headers: request.Headers.Clone(), Body: append([]byte(nil), request.Body...),
	})
	if len(t.errs) > 0 {
		err := t.errs[0]
		t.errs = t.errs[1:]
		return HTTPResponse{}, err
	}
	if len(t.responses) == 0 {
		return HTTPResponse{}, errors.New("unexpected provider request")
	}
	response := t.responses[0]
	t.responses = t.responses[1:]
	return response, nil
}

func providerRequestFixture() ProviderRequest {
	return ProviderRequest{
		Feature: FeatureSummary, Provider: "provider", Model: "model", PromptVersion: "v1",
		MSPID: "msp-id", ClientID: "client-id", WorkRecordID: "work-id",
		Fields: []ContextField{
			{Name: "title", Value: "VPN unavailable", Classification: ContextStandard},
			{Name: "notes", Value: "Technician needs a concise update.", Classification: ContextStandard},
		},
		AuthorizedCandidateIDs: []string{"candidate-1"},
	}
}

func adapterConnection(adapter AdapterType) ProviderConnection {
	return ProviderConnection{
		ID: "connection-id", MSPID: "msp-id", Name: "Provider", Adapter: adapter,
		Network: NetworkLocal, BaseURL: "http://127.0.0.1:11434", Timeout: defaultLocalTimeout,
		RequestLimitBytes: defaultRequestLimitBytes, ResponseLimitBytes: defaultResponseLimitBytes,
		LocalNetworkAcknowledgedAt: testTimePtr(), Health: HealthHealthy, Version: 1,
	}
}

func adapterModel(adapter AdapterType) ModelProfile {
	return ModelProfile{
		ID: "model-id", MSPID: "msp-id", ConnectionID: "connection-id", ProviderModelID: "model-id",
		DisplayName: "Model", SupportedFeatures: []Feature{FeatureSummary}, ContextLimit: 32_768,
		OutputLimit: 4_096, Enabled: true, Version: 1,
	}
}

func TestPrepareProviderRequestCountsEscapedBytesBeforeTransport(t *testing.T) {
	request := providerRequestFixture()
	request.Fields = []ContextField{{Name: "title", Value: "<\\\"\u0001", Classification: ContextStandard}}
	request.MaxOutputUnits = 4
	model := adapterModel(AdapterOpenAICompatible)
	encoded, err := PrepareProviderRequest(AdapterOpenAICompatible, model, request)
	if err != nil || len(encoded) <= len(request.Fields[0].Value) {
		t.Fatalf("PrepareProviderRequest() bytes=%d err=%v", len(encoded), err)
	}
}

func runAdapterContract(
	t *testing.T,
	adapter Adapter,
	connection ProviderConnection,
	model ModelProfile,
	fixture *providerFixture,
) {
	t.Helper()
	models, err := adapter.Discover(context.Background(), connection, fixture.credential)
	if err != nil || len(models) == 0 || models[0].ProviderModelID == "" {
		t.Fatalf("Discover() models=%+v error=%v", models, err)
	}
	result, err := adapter.Generate(context.Background(), connection, model, providerRequestFixture(), fixture.credential)
	if err != nil || strings.TrimSpace(result.Text) == "" {
		t.Fatalf("Generate() result=%+v error=%v", result, err)
	}
}

func TestAdapterRegistryIsImmutableAndProvidesManagementOperations(t *testing.T) {
	transport := &adapterTransport{responses: []HTTPResponse{{Body: []byte(`{"models":[{"name":"llama3:8b"}]}`)}}}
	ollama := NewOllamaAdapter(transport)
	registry, err := NewAdapterRegistry(ollama)
	if err != nil {
		t.Fatalf("NewAdapterRegistry() error=%v", err)
	}
	lookup, ok := registry.Lookup(AdapterOllama)
	if !ok || lookup != ollama {
		t.Fatalf("Lookup() adapter=%T ok=%t", lookup, ok)
	}
	if _, ok := registry.Lookup(AdapterOpenAICompatible); ok {
		t.Fatal("Lookup() returned absent adapter")
	}
	if err := registry.Test(context.Background(), adapterConnection(AdapterOllama)); err != nil {
		t.Fatalf("Test() error=%v", err)
	}
	if len(transport.requests) != 1 || transport.requests[0].Path != "/api/tags" {
		t.Fatalf("Test() requests=%+v", transport.requests)
	}
}

func TestAdapterRegistryRejectsNilDuplicateAndUnsupportedAdapters(t *testing.T) {
	ollama := NewOllamaAdapter(&adapterTransport{})
	for name, adapters := range map[string][]Adapter{
		"nil":         {nil},
		"typed nil":   {(*OllamaAdapter)(nil)},
		"duplicate":   {ollama, ollama},
		"unsupported": {adapterStub{typeValue: AdapterType("custom")}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewAdapterRegistry(adapters...); !errors.Is(err, ErrInvalidProviderConfiguration) {
				t.Fatalf("NewAdapterRegistry() error=%v", err)
			}
		})
	}
}

type adapterStub struct{ typeValue AdapterType }

func (a adapterStub) Type() AdapterType { return a.typeValue }
func (adapterStub) Discover(context.Context, ProviderConnection, []byte) ([]DiscoveredModel, error) {
	return nil, nil
}
func (adapterStub) Generate(context.Context, ProviderConnection, ModelProfile, ProviderRequest, []byte) (GenerationResult, error) {
	return GenerationResult{}, nil
}

func testTimePtr() *time.Time {
	value := time.Date(2026, time.July, 29, 0, 0, 0, 0, time.UTC)
	return &value
}
