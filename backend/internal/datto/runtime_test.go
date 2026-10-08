package datto

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEnvironmentCredentialResolverAllowsOnlyDedicatedDattoReferences(t *testing.T) {
	resolver := NewEnvironmentCredentialResolver(func(name string) (string, bool) {
		if name == "RARITY_DATTO_CREDENTIAL_PRIMARY" {
			return `{"api_url":"https://merlot-api.centrastage.net","api_key":"key","api_secret":"secret"}`, true
		}
		return "", false
	})
	value, err := resolver.Resolve(
		context.Background(), "env://RARITY_DATTO_CREDENTIAL_PRIMARY",
	)
	if err != nil || len(value) == 0 {
		t.Fatalf("Resolve() value=%q error=%v", value, err)
	}
	for _, ref := range []string{
		"env://DATABASE_URL",
		"env://RARITY_DATTO_CREDENTIAL_",
		"file:///tmp/datto-secret",
	} {
		if _, err := resolver.Resolve(context.Background(), ref); !errors.Is(
			err, ErrInvalidSourceCredentials,
		) {
			t.Fatalf("Resolve(%q) error=%v", ref, err)
		}
	}
}

func TestProviderHTTPClientRefusesRedirects(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(
		http.ResponseWriter,
		*http.Request,
	) {
		t.Fatal("redirect target was reached")
	}))
	defer target.Close()
	redirector := httptest.NewServer(http.RedirectHandler(
		target.URL, http.StatusFound,
	))
	defer redirector.Close()

	response, err := NewProviderHTTPClient().Get(redirector.URL)
	if response != nil {
		_ = response.Body.Close()
	}
	if !errors.Is(err, ErrProviderRedirectRefused) {
		t.Fatalf("Get() response=%v error=%v", response, err)
	}
}

func TestRuntimeSourceFactoryBindsConnectionWithoutAddingWriteCapability(t *testing.T) {
	factory, err := NewRuntimeSourceFactory(RuntimeSourceFactoryConfig{
		Client: http.DefaultClient,
		Credentials: NewEnvironmentCredentialResolver(func(string) (string, bool) {
			return "{}", true
		}),
		PayloadStore: &dattoPayloadStore{},
	})
	if err != nil {
		t.Fatalf("NewRuntimeSourceFactory() error = %v", err)
	}
	source, err := factory.Source(context.Background(), Connection{
		ID:                  "connection-id",
		CredentialSecretRef: "env://RARITY_DATTO_CREDENTIAL_PRIMARY",
	})
	if err != nil {
		t.Fatalf("Source() error = %v", err)
	}
	httpSource, ok := source.(*HTTPSource)
	if !ok ||
		httpSource.credentialRef != "env://RARITY_DATTO_CREDENTIAL_PRIMARY" ||
		httpSource.objectPrefix != "datto/connection-id" {
		t.Fatalf("factory returned %#v", source)
	}
}
