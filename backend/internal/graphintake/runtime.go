package graphintake

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
)

const graphCredentialEnvironmentPrefix = "RARITY_GRAPH_CREDENTIAL_"
const graphClientStateEnvironmentPrefix = "RARITY_GRAPH_CLIENT_STATE_"

var ErrGraphRedirectRefused = errors.New("Graph redirect refused")

func NewProviderHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
		CheckRedirect: func(
			_ *http.Request,
			_ []*http.Request,
		) error {
			return ErrGraphRedirectRefused
		},
	}
}

type EnvironmentCredentialResolver struct {
	lookup func(string) (string, bool)
}

type EnvironmentClientStateResolver struct {
	lookup func(string) (string, bool)
}

func NewEnvironmentClientStateResolver(
	lookup func(string) (string, bool),
) *EnvironmentClientStateResolver {
	return &EnvironmentClientStateResolver{lookup: lookup}
}

func (r *EnvironmentClientStateResolver) ResolveClientState(
	_ context.Context,
	ref string,
) (string, error) {
	if r == nil || r.lookup == nil ||
		!strings.HasPrefix(ref, "env://"+graphClientStateEnvironmentPrefix) {
		return "", ErrInvalidNotification
	}
	name := strings.TrimPrefix(ref, "env://")
	suffix := strings.TrimPrefix(name, graphClientStateEnvironmentPrefix)
	if suffix == "" ||
		strings.Trim(suffix, "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_") != "" {
		return "", ErrInvalidNotification
	}
	value, ok := r.lookup(name)
	if !ok || strings.TrimSpace(value) == "" {
		return "", ErrInvalidNotification
	}
	return value, nil
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
		!strings.HasPrefix(ref, "env://"+graphCredentialEnvironmentPrefix) {
		return nil, ErrInvalidGraphCredentials
	}
	name := strings.TrimPrefix(ref, "env://")
	suffix := strings.TrimPrefix(name, graphCredentialEnvironmentPrefix)
	if suffix == "" || strings.Trim(suffix, "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_") != "" {
		return nil, ErrInvalidGraphCredentials
	}
	value, ok := r.lookup(name)
	if !ok || strings.TrimSpace(value) == "" {
		return nil, ErrInvalidGraphCredentials
	}
	return []byte(value), nil
}

type RuntimeSourceFactoryConfig struct {
	Client       *http.Client
	GraphBaseURL string
	TokenBaseURL string
	Credentials  GraphCredentialResolver
	MIMEStore    MIMEObjectStore
}

type RuntimeSourceFactory struct {
	config RuntimeSourceFactoryConfig
}

var _ SourceFactory = (*RuntimeSourceFactory)(nil)

func NewRuntimeSourceFactory(
	config RuntimeSourceFactoryConfig,
) (*RuntimeSourceFactory, error) {
	if config.Client == nil || config.Credentials == nil ||
		config.MIMEStore == nil {
		return nil, ErrInvalidGraphConfiguration
	}
	if _, err := parseServiceURL(config.GraphBaseURL); err != nil {
		return nil, ErrInvalidGraphConfiguration
	}
	if _, err := parseServiceURL(config.TokenBaseURL); err != nil {
		return nil, ErrInvalidGraphConfiguration
	}
	return &RuntimeSourceFactory{config: config}, nil
}

func (f *RuntimeSourceFactory) Source(
	_ context.Context,
	job ReconciliationJob,
) (GraphSource, error) {
	return NewHTTPSource(HTTPSourceConfig{
		Client: f.config.Client, GraphBaseURL: f.config.GraphBaseURL,
		TokenBaseURL:  f.config.TokenBaseURL,
		CredentialRef: job.CredentialSecretRef,
		Credentials:   f.config.Credentials, MIMEStore: f.config.MIMEStore,
		MIMEObjectKeyPrefix: "graph/" + strings.TrimSpace(job.ConnectionID),
	})
}
