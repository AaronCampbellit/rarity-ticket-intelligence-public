package aiassist

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

type credentialSourceStub struct {
	credential []byte
	err        error
	calls      int
	mspID      string
	connection string
}

type credentialSourceFunc func(context.Context, ProviderConnection, func([]byte) error) error

func (f credentialSourceFunc) UseCredential(ctx context.Context, connection ProviderConnection, use func([]byte) error) error {
	return f(ctx, connection, use)
}

type credentialProbeAdapter struct {
	entered  chan struct{}
	release  chan struct{}
	once     sync.Once
	mu       sync.Mutex
	calls    int
	observed []byte
	err      error
}

func (*credentialProbeAdapter) Type() AdapterType { return AdapterOpenAICompatible }

func (a *credentialProbeAdapter) Discover(_ context.Context, _ ProviderConnection, credential []byte) ([]DiscoveredModel, error) {
	a.mu.Lock()
	a.calls++
	a.mu.Unlock()
	a.once.Do(func() { close(a.entered) })
	<-a.release
	a.mu.Lock()
	a.observed = append([]byte(nil), credential...)
	a.mu.Unlock()
	if a.err != nil {
		return nil, a.err
	}
	return []DiscoveredModel{{ProviderModelID: "model-a", DisplayName: "model-a", ContextLimit: 4096}}, nil
}

func (*credentialProbeAdapter) Generate(context.Context, ProviderConnection, ModelProfile, ProviderRequest, []byte) (GenerationResult, error) {
	return GenerationResult{}, nil
}

func (a *credentialProbeAdapter) snapshot() (int, []byte) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.calls, append([]byte(nil), a.observed...)
}

func (s *credentialSourceStub) UseCredential(_ context.Context, connection ProviderConnection, use func([]byte) error) error {
	s.calls++
	s.mspID, s.connection = connection.MSPID, connection.ID
	if s.err != nil {
		return s.err
	}
	return use(s.credential)
}

func TestAdapterOperationsUsesConfiguredCredentialOnlyWithinSourceCallback(t *testing.T) {
	transport := &adapterTransport{responses: []HTTPResponse{
		{Body: []byte(`{"data":[{"id":"compatible-model"}]}`)},
		{Body: []byte(`{"data":[{"id":"compatible-model"}]}`)},
	}}
	registry, err := NewAdapterRegistry(NewOpenAICompatibleAdapter(transport))
	if err != nil {
		t.Fatalf("NewAdapterRegistry() error=%v", err)
	}
	source := &credentialSourceStub{credential: []byte("management-secret")}
	operations := NewAdapterOperations(registry, source)
	connection := adapterConnection(AdapterOpenAICompatible)
	connection.CredentialConfigured = true
	if err := operations.Test(context.Background(), connection); err != nil {
		t.Fatalf("Test() error=%v", err)
	}
	if _, err := operations.Discover(context.Background(), connection); err != nil {
		t.Fatalf("Discover() error=%v", err)
	}
	if source.calls != 2 || source.mspID != "msp-id" || source.connection != "connection-id" ||
		len(transport.requests) != 2 || transport.requests[0].Headers.Get("Authorization") != "Bearer management-secret" ||
		transport.requests[1].Headers.Get("Authorization") != "Bearer management-secret" {
		t.Fatalf("source=%+v requests=%+v", source, transport.requests)
	}
	encoded, err := json.Marshal(operations)
	if err != nil || strings.Contains(string(encoded), "management-secret") {
		t.Fatalf("operations serialized credential: json=%s error=%v", encoded, err)
	}
}

func TestAdapterOperationsSkipsCredentialSourceWhenNotConfigured(t *testing.T) {
	transport := &adapterTransport{responses: []HTTPResponse{{Body: []byte(`{"data":[{"id":"compatible-model"}]}`)}}}
	registry, err := NewAdapterRegistry(NewOpenAICompatibleAdapter(transport))
	if err != nil {
		t.Fatalf("NewAdapterRegistry() error=%v", err)
	}
	source := &credentialSourceStub{credential: []byte("must-not-be-read")}
	connection := adapterConnection(AdapterOpenAICompatible)
	if _, err := NewAdapterOperations(registry, source).Discover(context.Background(), connection); err != nil {
		t.Fatalf("Discover() error=%v", err)
	}
	if source.calls != 0 || transport.requests[0].Headers.Get("Authorization") != "" {
		t.Fatalf("source=%+v request=%+v", source, transport.requests[0])
	}
}

func TestAdapterOperationsDoesNotAuthorizeOllamaWhenCredentialConfigured(t *testing.T) {
	transport := &adapterTransport{responses: []HTTPResponse{{Body: []byte(`{"models":[{"name":"llama3:8b"}]}`)}}}
	registry, err := NewAdapterRegistry(NewOllamaAdapter(transport))
	if err != nil {
		t.Fatalf("NewAdapterRegistry() error=%v", err)
	}
	source := &credentialSourceStub{credential: []byte("ollama-secret")}
	connection := adapterConnection(AdapterOllama)
	connection.CredentialConfigured = true
	if _, err := NewAdapterOperations(registry, source).Discover(context.Background(), connection); err != nil {
		t.Fatalf("Discover() error=%v", err)
	}
	if source.calls != 1 || transport.requests[0].Headers.Get("Authorization") != "" {
		t.Fatalf("source=%+v request=%+v", source, transport.requests[0])
	}
}

func TestAdapterOperationsReturnsSafeErrorWhenCredentialSourceFails(t *testing.T) {
	registry, err := NewAdapterRegistry(NewOpenAICompatibleAdapter(&adapterTransport{}))
	if err != nil {
		t.Fatalf("NewAdapterRegistry() error=%v", err)
	}
	source := &credentialSourceStub{err: errors.New("credential storage: provider-secret")}
	connection := adapterConnection(AdapterOpenAICompatible)
	connection.CredentialConfigured = true
	_, got := NewAdapterOperations(registry, source).Discover(context.Background(), connection)
	if !errors.Is(got, ErrProviderCredentialUnavailable) || strings.Contains(got.Error(), "provider-secret") {
		t.Fatalf("Discover() error=%v", got)
	}
}

func TestAdapterOperationsRejectsConfiguredSourceThatDoesNotInvokeCallback(t *testing.T) {
	registry, err := NewAdapterRegistry(NewOpenAICompatibleAdapter(&adapterTransport{}))
	if err != nil {
		t.Fatalf("NewAdapterRegistry() error=%v", err)
	}
	source := credentialSourceFunc(func(context.Context, ProviderConnection, func([]byte) error) error { return nil })
	connection := adapterConnection(AdapterOpenAICompatible)
	connection.CredentialConfigured = true
	models, err := NewAdapterOperations(registry, source).Discover(context.Background(), connection)
	if !errors.Is(err, ErrProviderCredentialUnavailable) || models != nil {
		t.Fatalf("Discover() models=%+v error=%v", models, err)
	}
}

func TestAdapterOperationsRejectsConfiguredSourceThatInvokesCallbackTwice(t *testing.T) {
	transport := &adapterTransport{responses: []HTTPResponse{{Body: []byte(`{"data":[{"id":"model-a"}]}`)}}}
	registry, err := NewAdapterRegistry(NewOpenAICompatibleAdapter(transport))
	if err != nil {
		t.Fatalf("NewAdapterRegistry() error=%v", err)
	}
	source := credentialSourceFunc(func(_ context.Context, _ ProviderConnection, use func([]byte) error) error {
		if err := use([]byte("source-secret")); err != nil {
			return err
		}
		return use([]byte("source-secret"))
	})
	connection := adapterConnection(AdapterOpenAICompatible)
	connection.CredentialConfigured = true
	models, err := NewAdapterOperations(registry, source).Discover(context.Background(), connection)
	if !errors.Is(err, ErrProviderCredentialUnavailable) || models != nil || len(transport.requests) != 1 ||
		strings.Contains(err.Error(), "source-secret") {
		t.Fatalf("Discover() models=%+v error=%v requests=%+v", models, err, transport.requests)
	}
}

func TestAdapterOperationsRejectsEmptyConfiguredCredentialBeforeProviderCall(t *testing.T) {
	transport := &adapterTransport{}
	registry, err := NewAdapterRegistry(NewOpenAICompatibleAdapter(transport))
	if err != nil {
		t.Fatalf("NewAdapterRegistry() error=%v", err)
	}
	source := credentialSourceFunc(func(_ context.Context, _ ProviderConnection, use func([]byte) error) error {
		return use([]byte{})
	})
	connection := adapterConnection(AdapterOpenAICompatible)
	connection.CredentialConfigured = true
	_, got := NewAdapterOperations(registry, source).Discover(context.Background(), connection)
	if !errors.Is(got, ErrProviderCredentialUnavailable) || len(transport.requests) != 0 {
		t.Fatalf("Discover() error=%v requests=%+v", got, transport.requests)
	}
}

func TestAdapterOperationsTreatsNonEmptyCredentialAsOpaqueBytes(t *testing.T) {
	transport := &adapterTransport{responses: []HTTPResponse{{Body: []byte(`{"data":[{"id":"model-a"}]}`)}}}
	registry, err := NewAdapterRegistry(NewOpenAICompatibleAdapter(transport))
	if err != nil {
		t.Fatalf("NewAdapterRegistry() error=%v", err)
	}
	source := credentialSourceFunc(func(_ context.Context, _ ProviderConnection, use func([]byte) error) error {
		return use([]byte(" "))
	})
	connection := adapterConnection(AdapterOpenAICompatible)
	connection.CredentialConfigured = true
	models, err := NewAdapterOperations(registry, source).Discover(context.Background(), connection)
	if err != nil || len(models) != 1 || transport.requests[0].Headers.Get("Authorization") != "Bearer  " {
		t.Fatalf("Discover() models=%+v error=%v request=%+v", models, err, transport.requests)
	}
}

func TestAdapterOperationsPreservesSafeAdapterFailureFromCredentialCallback(t *testing.T) {
	transport := &adapterTransport{errs: []error{ErrProviderNonSuccess}}
	registry, err := NewAdapterRegistry(NewOpenAICompatibleAdapter(transport))
	if err != nil {
		t.Fatalf("NewAdapterRegistry() error=%v", err)
	}
	source := credentialSourceFunc(func(_ context.Context, _ ProviderConnection, use func([]byte) error) error {
		return use([]byte("source-secret"))
	})
	connection := adapterConnection(AdapterOpenAICompatible)
	connection.CredentialConfigured = true
	_, got := NewAdapterOperations(registry, source).Discover(context.Background(), connection)
	if !errors.Is(got, ErrProviderNonSuccess) || strings.Contains(got.Error(), "source-secret") {
		t.Fatalf("Discover() error=%v", got)
	}
}

func TestAdapterOperationsDoesNotOpenCredentialSourceForCanceledContext(t *testing.T) {
	registry, err := NewAdapterRegistry(NewOpenAICompatibleAdapter(&adapterTransport{}))
	if err != nil {
		t.Fatalf("NewAdapterRegistry() error=%v", err)
	}
	called := false
	source := credentialSourceFunc(func(context.Context, ProviderConnection, func([]byte) error) error {
		called = true
		return nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	connection := adapterConnection(AdapterOpenAICompatible)
	connection.CredentialConfigured = true
	_, got := NewAdapterOperations(registry, source).Discover(ctx, connection)
	if !errors.Is(got, context.Canceled) || called {
		t.Fatalf("Discover() error=%v sourceCalled=%t", got, called)
	}
}

func TestAdapterOperationsPropagatesCredentialSourceCancellation(t *testing.T) {
	registry, err := NewAdapterRegistry(NewOpenAICompatibleAdapter(&adapterTransport{}))
	if err != nil {
		t.Fatalf("NewAdapterRegistry() error=%v", err)
	}
	source := credentialSourceFunc(func(context.Context, ProviderConnection, func([]byte) error) error {
		return context.DeadlineExceeded
	})
	connection := adapterConnection(AdapterOpenAICompatible)
	connection.CredentialConfigured = true
	_, got := NewAdapterOperations(registry, source).Discover(context.Background(), connection)
	if !errors.Is(got, context.DeadlineExceeded) {
		t.Fatalf("Discover() error=%v", got)
	}
}

func TestAdapterOperationsDoesNotUseCredentialAfterContextCancelsBeforeCallback(t *testing.T) {
	transport := &adapterTransport{}
	registry, err := NewAdapterRegistry(NewOpenAICompatibleAdapter(transport))
	if err != nil {
		t.Fatalf("NewAdapterRegistry() error=%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	source := credentialSourceFunc(func(_ context.Context, _ ProviderConnection, use func([]byte) error) error {
		cancel()
		return use([]byte("source-secret"))
	})
	connection := adapterConnection(AdapterOpenAICompatible)
	connection.CredentialConfigured = true
	_, got := NewAdapterOperations(registry, source).Discover(ctx, connection)
	if !errors.Is(got, context.Canceled) || len(transport.requests) != 0 || strings.Contains(got.Error(), "source-secret") {
		t.Fatalf("Discover() error=%v requests=%+v", got, transport.requests)
	}
}

func TestAdapterOperationsRejectsCallbackAfterCredentialSourceReturns(t *testing.T) {
	transport := &adapterTransport{}
	registry, err := NewAdapterRegistry(NewOpenAICompatibleAdapter(transport))
	if err != nil {
		t.Fatalf("NewAdapterRegistry() error=%v", err)
	}
	release := make(chan struct{})
	finished := make(chan struct{})
	source := credentialSourceFunc(func(_ context.Context, _ ProviderConnection, use func([]byte) error) error {
		go func() {
			defer close(finished)
			<-release
			_ = use([]byte("source-secret"))
		}()
		return nil
	})
	connection := adapterConnection(AdapterOpenAICompatible)
	connection.CredentialConfigured = true
	_, got := NewAdapterOperations(registry, source).Discover(context.Background(), connection)
	if !errors.Is(got, ErrProviderCredentialUnavailable) {
		t.Fatalf("Discover() error=%v", got)
	}
	close(release)
	<-finished
	if len(transport.requests) != 0 {
		t.Fatalf("late credential callback reached provider: requests=%+v", transport.requests)
	}
}

func TestAdapterOperationsWaitsForAsyncCallbackAndUsesCredentialCopy(t *testing.T) {
	adapter := &credentialProbeAdapter{entered: make(chan struct{}), release: make(chan struct{})}
	registry, err := NewAdapterRegistry(adapter)
	if err != nil {
		t.Fatalf("NewAdapterRegistry() error=%v", err)
	}
	original := []byte("source-secret")
	sourceReturned := make(chan struct{})
	source := credentialSourceFunc(func(_ context.Context, _ ProviderConnection, use func([]byte) error) error {
		go func() { _ = use(original) }()
		<-adapter.entered
		for index := range original {
			original[index] = 0
		}
		close(sourceReturned)
		return nil
	})
	connection := adapterConnection(AdapterOpenAICompatible)
	connection.CredentialConfigured = true
	type outcome struct {
		models []DiscoveredModel
		err    error
	}
	completed := make(chan outcome, 1)
	go func() {
		models, err := NewAdapterOperations(registry, source).Discover(context.Background(), connection)
		completed <- outcome{models: models, err: err}
	}()
	<-sourceReturned
	close(adapter.release)
	result := <-completed
	calls, observed := adapter.snapshot()
	if !errors.Is(result.err, ErrProviderCredentialUnavailable) || result.models != nil || calls != 1 || string(observed) != "source-secret" {
		t.Fatalf("Discover() models=%+v error=%v calls=%d observed=%q", result.models, result.err, calls, observed)
	}
}

func TestAdapterOperationsRecordsIgnoredEmptyCredentialCallbackError(t *testing.T) {
	transport := &adapterTransport{}
	registry, err := NewAdapterRegistry(NewOpenAICompatibleAdapter(transport))
	if err != nil {
		t.Fatalf("NewAdapterRegistry() error=%v", err)
	}
	source := credentialSourceFunc(func(_ context.Context, _ ProviderConnection, use func([]byte) error) error {
		_ = use(nil)
		return nil
	})
	connection := adapterConnection(AdapterOpenAICompatible)
	connection.CredentialConfigured = true
	_, got := NewAdapterOperations(registry, source).Discover(context.Background(), connection)
	if !errors.Is(got, ErrProviderCredentialUnavailable) || len(transport.requests) != 0 {
		t.Fatalf("Discover() error=%v requests=%+v", got, transport.requests)
	}
}

func TestAdapterOperationsRecordsIgnoredAdapterCallbackError(t *testing.T) {
	transport := &adapterTransport{errs: []error{ErrProviderNonSuccess}}
	registry, err := NewAdapterRegistry(NewOpenAICompatibleAdapter(transport))
	if err != nil {
		t.Fatalf("NewAdapterRegistry() error=%v", err)
	}
	source := credentialSourceFunc(func(_ context.Context, _ ProviderConnection, use func([]byte) error) error {
		_ = use([]byte("source-secret"))
		return nil
	})
	connection := adapterConnection(AdapterOpenAICompatible)
	connection.CredentialConfigured = true
	_, got := NewAdapterOperations(registry, source).Discover(context.Background(), connection)
	if !errors.Is(got, ErrProviderNonSuccess) || strings.Contains(got.Error(), "source-secret") {
		t.Fatalf("Discover() error=%v", got)
	}
}

func TestAdapterOperationsRejectsOverlappingCredentialCallbacksBeforeSecondAdapterCall(t *testing.T) {
	adapter := &credentialProbeAdapter{entered: make(chan struct{}), release: make(chan struct{})}
	registry, err := NewAdapterRegistry(adapter)
	if err != nil {
		t.Fatalf("NewAdapterRegistry() error=%v", err)
	}
	secondDone := make(chan struct{})
	source := credentialSourceFunc(func(_ context.Context, _ ProviderConnection, use func([]byte) error) error {
		go func() { _ = use([]byte("first-secret")) }()
		<-adapter.entered
		go func() {
			_ = use([]byte("second-secret"))
			close(secondDone)
		}()
		<-secondDone
		return nil
	})
	connection := adapterConnection(AdapterOpenAICompatible)
	connection.CredentialConfigured = true
	completed := make(chan error, 1)
	go func() {
		_, err := NewAdapterOperations(registry, source).Discover(context.Background(), connection)
		completed <- err
	}()
	close(adapter.release)
	got := <-completed
	calls, _ := adapter.snapshot()
	if !errors.Is(got, ErrProviderCredentialUnavailable) || calls != 1 {
		t.Fatalf("Discover() error=%v calls=%d", got, calls)
	}
}

func TestAdaptersBoundDiscoveryBeforeBuildingProfiles(t *testing.T) {
	for name, fixture := range map[string]struct {
		adapter    Adapter
		connection ProviderConnection
		body       func(int) []byte
	}{
		"ollama": {
			connection: adapterConnection(AdapterOllama),
			body:       func(count int) []byte { return discoveryFixture("models", "name", count) },
		},
		"openai compatible": {
			connection: adapterConnection(AdapterOpenAICompatible),
			body:       func(count int) []byte { return discoveryFixture("data", "id", count) },
		},
	} {
		t.Run(name, func(t *testing.T) {
			fixture.adapter = newDiscoveryAdapter(name, &adapterTransport{responses: []HTTPResponse{{Body: fixture.body(maximumDiscoveredModels)}}})
			models, err := fixture.adapter.Discover(context.Background(), fixture.connection, nil)
			if err != nil || len(models) != maximumDiscoveredModels {
				t.Fatalf("max Discover() models=%d error=%v", len(models), err)
			}
			fixture.adapter = newDiscoveryAdapter(name, &adapterTransport{responses: []HTTPResponse{{Body: fixture.body(maximumDiscoveredModels + 1)}}})
			if _, err := fixture.adapter.Discover(context.Background(), fixture.connection, nil); !errors.Is(err, ErrInvalidProviderResponse) {
				t.Fatalf("oversized Discover() error=%v", err)
			}
		})
	}
}

func TestAdaptersRejectMalformedDiscoveryWithoutProviderBodyLeakage(t *testing.T) {
	secret := "provider discovery secret"
	for name, fixture := range map[string]struct {
		adapter    Adapter
		connection ProviderConnection
		body       []byte
	}{
		"ollama": {
			connection: adapterConnection(AdapterOllama),
			body:       []byte(`{"models":[{"name":"model-a"}` + secret),
		},
		"openai compatible": {
			connection: adapterConnection(AdapterOpenAICompatible),
			body:       []byte(`{"data":[{"id":"model-a"}` + secret),
		},
	} {
		t.Run(name, func(t *testing.T) {
			transport := &adapterTransport{responses: []HTTPResponse{{Body: fixture.body}}}
			fixture.adapter = newDiscoveryAdapter(name, transport)
			_, err := fixture.adapter.Discover(context.Background(), fixture.connection, nil)
			if !errors.Is(err, ErrInvalidProviderResponse) || strings.Contains(err.Error(), secret) {
				t.Fatalf("Discover() error=%v", err)
			}
		})
	}
}

func TestAdapterRegistryLookupIsSafeUnderConcurrentReads(t *testing.T) {
	registry, err := NewAdapterRegistry(NewOllamaAdapter(&adapterTransport{}), NewOpenAICompatibleAdapter(&adapterTransport{}))
	if err != nil {
		t.Fatalf("NewAdapterRegistry() error=%v", err)
	}
	var group sync.WaitGroup
	for index := 0; index < 64; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for repeat := 0; repeat < 500; repeat++ {
				if adapter, ok := registry.Lookup(AdapterOllama); !ok || adapter.Type() != AdapterOllama {
					t.Errorf("Ollama lookup adapter=%T ok=%t", adapter, ok)
					return
				}
			}
		}()
	}
	group.Wait()
}

func TestAdaptersGenerateFixedStructuredWireRequestWithoutCallerOverrides(t *testing.T) {
	for name, fixture := range map[string]struct {
		adapter    Adapter
		connection ProviderConnection
		body       []byte
	}{
		"ollama": {
			connection: adapterConnection(AdapterOllama),
			body:       []byte(`{"message":{"content":"{\"text\":\"answer\"}"}}`),
		},
		"openai compatible": {
			connection: adapterConnection(AdapterOpenAICompatible),
			body:       []byte(`{"choices":[{"message":{"content":"{\"text\":\"answer\"}"}}]}`),
		},
	} {
		t.Run(name, func(t *testing.T) {
			transport := &adapterTransport{responses: []HTTPResponse{{Body: fixture.body}}}
			fixture.adapter = newDiscoveryAdapter(name, transport)
			request := providerRequestFixture()
			request.Model = "caller-controlled-model"
			model := adapterModel(fixture.connection.Adapter)
			if _, err := fixture.adapter.Generate(context.Background(), fixture.connection, model, request, []byte("credential")); err != nil {
				t.Fatalf("Generate() error=%v", err)
			}
			var wire struct {
				Model          string `json:"model"`
				Stream         bool   `json:"stream"`
				Format         string `json:"format"`
				ResponseFormat struct {
					Type string `json:"type"`
				} `json:"response_format"`
				Messages []struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"messages"`
			}
			if err := json.Unmarshal(transport.requests[0].Body, &wire); err != nil {
				t.Fatalf("unmarshal wire request: %v", err)
			}
			if wire.Model != "model-id" || wire.Stream || len(wire.Messages) != 2 ||
				wire.Messages[0].Role != "system" || wire.Messages[0].Content != recommendationInstruction ||
				wire.Messages[1].Role != "user" || strings.Contains(string(transport.requests[0].Body), "caller-controlled-model") {
				t.Fatalf("wire=%s", transport.requests[0].Body)
			}
			if name == "ollama" && wire.Format != "json" {
				t.Fatalf("Ollama format=%q request=%s", wire.Format, transport.requests[0].Body)
			}
			if name == "openai compatible" && wire.ResponseFormat.Type != "json_object" {
				t.Fatalf("OpenAI response_format=%+v request=%s", wire.ResponseFormat, transport.requests[0].Body)
			}
			var prompt struct {
				Feature                Feature       `json:"feature"`
				Fields                 []promptField `json:"fields"`
				AuthorizedCandidateIDs []string      `json:"authorized_candidate_ids"`
			}
			if err := json.Unmarshal([]byte(wire.Messages[1].Content), &prompt); err != nil || prompt.Feature != FeatureSummary ||
				len(prompt.Fields) != 2 || prompt.Fields[0].Name != "title" || prompt.Fields[0].Value != "VPN unavailable" ||
				len(prompt.AuthorizedCandidateIDs) != 1 || prompt.AuthorizedCandidateIDs[0] != "candidate-1" {
				t.Fatalf("prompt=%+v error=%v", prompt, err)
			}
		})
	}
}

func TestAdapterRejectsOversizedPromptInputsBeforeProviderRequest(t *testing.T) {
	for name, request := range map[string]ProviderRequest{
		"too many fields": func() ProviderRequest {
			value := providerRequestFixture()
			for index := 0; index < maximumPromptFields-1; index++ {
				value.Fields = append(value.Fields, ContextField{Name: fmt.Sprintf("field-%d", index), Value: "value", Classification: ContextStandard})
			}
			return value
		}(),
		"too many candidates": func() ProviderRequest {
			value := providerRequestFixture()
			value.AuthorizedCandidateIDs = make([]string, maximumPromptCandidates+1)
			for index := range value.AuthorizedCandidateIDs {
				value.AuthorizedCandidateIDs[index] = fmt.Sprintf("candidate-%d", index)
			}
			return value
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			transport := &adapterTransport{}
			_, err := NewOpenAICompatibleAdapter(transport).Generate(context.Background(), adapterConnection(AdapterOpenAICompatible), adapterModel(AdapterOpenAICompatible), request, nil)
			if err == nil || len(transport.requests) != 0 {
				t.Fatalf("Generate() error=%v requests=%+v", err, transport.requests)
			}
		})
	}
}

func newDiscoveryAdapter(name string, transport Transport) Adapter {
	if name == "ollama" {
		return NewOllamaAdapter(transport)
	}
	return NewOpenAICompatibleAdapter(transport)
}

func discoveryFixture(collection, field string, count int) []byte {
	var body strings.Builder
	body.WriteString(`{"`)
	body.WriteString(collection)
	body.WriteString(`":[`)
	for index := 0; index < count; index++ {
		if index > 0 {
			body.WriteByte(',')
		}
		fmt.Fprintf(&body, `{"%s":"model-%d"}`, field, index)
	}
	body.WriteString(`]}`)
	return []byte(body.String())
}
