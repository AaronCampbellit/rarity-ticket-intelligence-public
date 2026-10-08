package graphintake

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEnvironmentCredentialResolverAllowsOnlyDedicatedGraphReferences(t *testing.T) {
	resolver := NewEnvironmentCredentialResolver(func(name string) (string, bool) {
		if name == "RARITY_GRAPH_CREDENTIAL_SUPPORT" {
			return `{"tenant_id":"tenant","client_id":"client","client_secret":"secret"}`, true
		}
		return "", false
	})
	value, err := resolver.Resolve(
		context.Background(), "env://RARITY_GRAPH_CREDENTIAL_SUPPORT",
	)
	if err != nil || len(value) == 0 {
		t.Fatalf("Resolve() value=%q error=%v", value, err)
	}
	for _, ref := range []string{
		"env://DATABASE_URL",
		"env://RARITY_GRAPH_CREDENTIAL_",
		"file:///tmp/graph-secret",
	} {
		if _, err := resolver.Resolve(context.Background(), ref); !errors.Is(
			err, ErrInvalidGraphCredentials,
		) {
			t.Fatalf("Resolve(%q) error=%v", ref, err)
		}
	}
}

func TestEnvironmentClientStateResolverUsesSeparateReferenceNamespace(t *testing.T) {
	resolver := NewEnvironmentClientStateResolver(func(name string) (string, bool) {
		return name + "-value", true
	})
	value, err := resolver.ResolveClientState(
		context.Background(), "env://RARITY_GRAPH_CLIENT_STATE_SUPPORT",
	)
	if err != nil || value != "RARITY_GRAPH_CLIENT_STATE_SUPPORT-value" {
		t.Fatalf("ResolveClientState() value=%q error=%v", value, err)
	}
	if _, err := resolver.ResolveClientState(
		context.Background(), "env://RARITY_GRAPH_CREDENTIAL_SUPPORT",
	); !errors.Is(err, ErrInvalidNotification) {
		t.Fatalf("credential namespace accepted as client state: %v", err)
	}
}

func TestProviderHTTPClientRefusesRedirectsBeforeForwardingBearerCredentials(t *testing.T) {
	redirectTarget := httptest.NewServer(http.HandlerFunc(func(
		http.ResponseWriter,
		*http.Request,
	) {
		t.Fatal("redirect target was reached")
	}))
	defer redirectTarget.Close()
	redirector := httptest.NewServer(http.RedirectHandler(
		redirectTarget.URL, http.StatusFound,
	))
	defer redirector.Close()

	response, err := NewProviderHTTPClient().Get(redirector.URL)
	if response != nil {
		_ = response.Body.Close()
	}
	if !errors.Is(err, ErrGraphRedirectRefused) {
		t.Fatalf("Get() response=%v error=%v", response, err)
	}
}

func TestRuntimeSourceFactoryBindsConnectionCredentialAndObjectPrefix(t *testing.T) {
	factory, err := NewRuntimeSourceFactory(RuntimeSourceFactoryConfig{
		Client:       http.DefaultClient,
		GraphBaseURL: "https://graph.microsoft.com/v1.0",
		TokenBaseURL: "https://login.microsoftonline.com",
		Credentials: NewEnvironmentCredentialResolver(func(string) (string, bool) {
			return "{}", true
		}),
		MIMEStore: &graphMIMEStore{},
	})
	if err != nil {
		t.Fatalf("NewRuntimeSourceFactory() error = %v", err)
	}
	source, err := factory.Source(context.Background(), ReconciliationJob{
		ConnectionID:        "connection-id",
		CredentialSecretRef: "env://RARITY_GRAPH_CREDENTIAL_SUPPORT",
	})
	if err != nil {
		t.Fatalf("Source() error = %v", err)
	}
	httpSource, ok := source.(*HTTPSource)
	if !ok ||
		httpSource.credentialRef != "env://RARITY_GRAPH_CREDENTIAL_SUPPORT" ||
		httpSource.mimeObjectKeyPrefix != "graph/connection-id" {
		t.Fatalf("factory returned %#v", source)
	}
}
