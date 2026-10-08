package graphintake

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type RuntimeSubscriptionProviderFactoryConfig struct {
	Client       *http.Client
	GraphBaseURL string
	TokenBaseURL string
	Credentials  GraphCredentialResolver
}

type RuntimeSubscriptionProviderFactory struct {
	config    RuntimeSubscriptionProviderFactoryConfig
	graphBase *url.URL
	tokenBase *url.URL
	mu        sync.Mutex
	providers map[string]*HTTPSubscriptionProvider
}

func NewRuntimeSubscriptionProviderFactory(
	config RuntimeSubscriptionProviderFactoryConfig,
) (*RuntimeSubscriptionProviderFactory, error) {
	graphBase, graphErr := parseServiceURL(config.GraphBaseURL)
	tokenBase, tokenErr := parseServiceURL(config.TokenBaseURL)
	if config.Client == nil || config.Credentials == nil ||
		graphErr != nil || tokenErr != nil {
		return nil, ErrInvalidGraphConfiguration
	}
	return &RuntimeSubscriptionProviderFactory{
		config: config, graphBase: graphBase, tokenBase: tokenBase,
		providers: make(map[string]*HTTPSubscriptionProvider),
	}, nil
}

func (f *RuntimeSubscriptionProviderFactory) Provider(
	_ context.Context,
	job SubscriptionJob,
) (SubscriptionProvider, error) {
	ref := strings.TrimSpace(job.CredentialSecretRef)
	if ref == "" {
		return nil, ErrInvalidGraphCredentials
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if provider := f.providers[ref]; provider != nil {
		return provider, nil
	}
	provider := &HTTPSubscriptionProvider{
		client: f.config.Client, graphBase: f.graphBase,
		tokenBase: f.tokenBase, credentials: f.config.Credentials,
		credentialRef: ref,
	}
	f.providers[ref] = provider
	return provider, nil
}

type HTTPSubscriptionProvider struct {
	client        *http.Client
	graphBase     *url.URL
	tokenBase     *url.URL
	credentials   GraphCredentialResolver
	credentialRef string

	tokenMu        sync.Mutex
	accessToken    string
	tokenExpiresAt time.Time
}

func (p *HTTPSubscriptionProvider) Create(
	ctx context.Context,
	request SubscriptionRequest,
) (ProviderSubscription, error) {
	body := map[string]any{
		"changeType":               request.ChangeType,
		"notificationUrl":          request.NotificationURL,
		"lifecycleNotificationUrl": request.LifecycleNotificationURL,
		"resource":                 request.Resource,
		"expirationDateTime":       request.ExpiresAt.UTC().Format(time.RFC3339),
		"clientState":              request.ClientState,
	}
	return p.mutate(
		ctx, http.MethodPost, "/subscriptions", body, http.StatusCreated,
	)
}

func (p *HTTPSubscriptionProvider) Renew(
	ctx context.Context,
	request SubscriptionRequest,
) (ProviderSubscription, error) {
	if strings.TrimSpace(request.ExternalID) == "" {
		return ProviderSubscription{}, ErrInvalidSubscription
	}
	body := map[string]any{
		"expirationDateTime": request.ExpiresAt.UTC().Format(time.RFC3339),
	}
	return p.mutate(
		ctx, http.MethodPatch,
		"/subscriptions/"+url.PathEscape(request.ExternalID),
		body, http.StatusOK,
	)
}

func (p *HTTPSubscriptionProvider) mutate(
	ctx context.Context,
	method string,
	resourcePath string,
	body map[string]any,
	expectedStatus int,
) (ProviderSubscription, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return ProviderSubscription{}, ErrInvalidSubscription
	}
	token, err := p.token(ctx)
	if err != nil {
		return ProviderSubscription{}, err
	}
	requestURL := *p.graphBase
	requestURL.Path = strings.TrimSuffix(requestURL.Path, "/") + resourcePath
	httpRequest, err := http.NewRequestWithContext(
		ctx, method, requestURL.String(), bytes.NewReader(payload),
	)
	if err != nil {
		return ProviderSubscription{}, ErrInvalidGraphConfiguration
	}
	httpRequest.Header.Set("Authorization", "Bearer "+token)
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("Content-Type", "application/json")
	response, err := p.client.Do(httpRequest)
	if err != nil {
		return ProviderSubscription{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != expectedStatus {
		return ProviderSubscription{}, ErrInvalidSubscription
	}
	var result struct {
		ID         string `json:"id"`
		Resource   string `json:"resource"`
		Expiration string `json:"expirationDateTime"`
	}
	if err := json.NewDecoder(
		io.LimitReader(response.Body, 1<<20),
	).Decode(&result); err != nil {
		return ProviderSubscription{}, ErrInvalidSubscription
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, result.Expiration)
	if err != nil {
		return ProviderSubscription{}, ErrInvalidSubscription
	}
	return ProviderSubscription{
		ID: result.ID, Resource: result.Resource,
		ExpiresAt: expiresAt.UTC(),
	}, nil
}

func (p *HTTPSubscriptionProvider) token(
	ctx context.Context,
) (string, error) {
	p.tokenMu.Lock()
	defer p.tokenMu.Unlock()
	if p.accessToken != "" &&
		time.Now().Before(p.tokenExpiresAt.Add(-time.Minute)) {
		return p.accessToken, nil
	}
	protected, err := p.credentials.Resolve(ctx, p.credentialRef)
	if err != nil {
		return "", err
	}
	var credential graphCredentials
	decoder := json.NewDecoder(bytes.NewReader(protected))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&credential); err != nil ||
		strings.TrimSpace(credential.TenantID) == "" ||
		strings.TrimSpace(credential.ClientID) == "" ||
		strings.TrimSpace(credential.ClientSecret) == "" {
		return "", ErrInvalidGraphCredentials
	}
	tokenURL := *p.tokenBase
	if !strings.HasSuffix(tokenURL.Path, "/oauth2/v2.0/token") {
		tokenURL.Path = strings.TrimSuffix(tokenURL.Path, "/") + "/" +
			url.PathEscape(credential.TenantID) + "/oauth2/v2.0/token"
	}
	form := url.Values{
		"client_id": {credential.ClientID}, "client_secret": {credential.ClientSecret},
		"grant_type": {"client_credentials"},
		"scope":      {"https://graph.microsoft.com/.default"},
	}
	request, err := http.NewRequestWithContext(
		ctx, http.MethodPost, tokenURL.String(),
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return "", ErrInvalidGraphConfiguration
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := p.client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", ErrInvalidGraphCredentials
	}
	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(
		io.LimitReader(response.Body, 1<<20),
	).Decode(&result); err != nil ||
		strings.TrimSpace(result.AccessToken) == "" ||
		result.ExpiresIn <= 0 {
		return "", ErrInvalidGraphCredentials
	}
	p.accessToken = result.AccessToken
	p.tokenExpiresAt = time.Now().Add(
		time.Duration(result.ExpiresIn) * time.Second,
	)
	return p.accessToken, nil
}
