package identity

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"
)

type entraDiscoveryHTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

type HTTPEntraDiscovery struct {
	client entraDiscoveryHTTPClient
}

func NewHTTPEntraDiscovery(client entraDiscoveryHTTPClient) *HTTPEntraDiscovery {
	if client == nil {
		client = &http.Client{
			Timeout: 15 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return ErrEntraVerificationFailed
			},
		}
	}
	return &HTTPEntraDiscovery{client: client}
}

func (d *HTTPEntraDiscovery) Discover(ctx context.Context, endpoint string) (EntraDiscoveryDocument, error) {
	if d == nil || d.client == nil {
		return EntraDiscoveryDocument{}, ErrEntraVerificationFailed
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return EntraDiscoveryDocument{}, ErrEntraVerificationFailed
	}
	request.Header.Set("Accept", "application/json")
	response, err := d.client.Do(request)
	if err != nil {
		return EntraDiscoveryDocument{}, ErrEntraVerificationFailed
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 32<<10))
		return EntraDiscoveryDocument{}, ErrEntraVerificationFailed
	}
	var document EntraDiscoveryDocument
	decoder := json.NewDecoder(io.LimitReader(response.Body, (1<<20)+1))
	if decoder.Decode(&document) != nil {
		return EntraDiscoveryDocument{}, ErrEntraVerificationFailed
	}
	return document, nil
}
