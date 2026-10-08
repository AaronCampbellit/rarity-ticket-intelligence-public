package datto

import (
	"context"
	"net/http"
	"strings"
	"time"
)

const dattoCredentialEnvironmentPrefix = "RARITY_DATTO_CREDENTIAL_"

func NewProviderHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
		CheckRedirect: func(
			*http.Request,
			[]*http.Request,
		) error {
			return ErrProviderRedirectRefused
		},
	}
}

type EnvironmentCredentialResolver struct {
	lookup func(string) (string, bool)
}

func NewEnvironmentCredentialResolver(
	lookup func(string) (string, bool),
) *EnvironmentCredentialResolver {
	return &EnvironmentCredentialResolver{lookup: lookup}
}

func (r *EnvironmentCredentialResolver) Resolve(
	_ context.Context,
	ref string,
) ([]byte, error) {
	if r == nil || r.lookup == nil ||
		!strings.HasPrefix(ref, "env://"+dattoCredentialEnvironmentPrefix) {
		return nil, ErrInvalidSourceCredentials
	}
	name := strings.TrimPrefix(ref, "env://")
	suffix := strings.TrimPrefix(name, dattoCredentialEnvironmentPrefix)
	if suffix == "" ||
		strings.Trim(suffix, "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_") != "" {
		return nil, ErrInvalidSourceCredentials
	}
	value, ok := r.lookup(name)
	if !ok || strings.TrimSpace(value) == "" {
		return nil, ErrInvalidSourceCredentials
	}
	return []byte(value), nil
}

type SourceFactory interface {
	Source(context.Context, Connection) (SyncSource, error)
}

type RuntimeSourceFactoryConfig struct {
	Client       *http.Client
	Credentials  CredentialResolver
	PayloadStore PayloadObjectStore
}

type RuntimeSourceFactory struct {
	config RuntimeSourceFactoryConfig
}

func NewRuntimeSourceFactory(
	config RuntimeSourceFactoryConfig,
) (*RuntimeSourceFactory, error) {
	if config.Client == nil || config.Credentials == nil ||
		config.PayloadStore == nil {
		return nil, ErrInvalidSourceConfiguration
	}
	return &RuntimeSourceFactory{config: config}, nil
}

func (f *RuntimeSourceFactory) Source(
	_ context.Context,
	connection Connection,
) (SyncSource, error) {
	return NewHTTPSource(HTTPSourceConfig{
		Client: f.config.Client, Credentials: f.config.Credentials,
		CredentialRef: connection.CredentialSecretRef,
		PayloadStore:  f.config.PayloadStore,
		ObjectPrefix:  "datto/" + strings.TrimSpace(connection.ID),
	})
}
