package aiassist

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/webhooks"
)

var (
	ErrUnsafeProviderEndpoint   = errors.New("unsafe AI provider endpoint")
	ErrProviderRedirectRefused  = errors.New("AI provider redirect refused")
	ErrProviderRequestTooLarge  = errors.New("AI provider request exceeds configured limit")
	ErrProviderResponseTooLarge = errors.New("AI provider response exceeds configured limit")
	ErrProviderNonSuccess       = errors.New("AI provider returned non-success status")
	ErrProviderTransport        = errors.New("AI provider transport failed")
)

// ProviderHTTPError intentionally contains status only. Bodies and headers
// can contain provider details or credentials and must never reach job audit,
// retry, or API error paths.
type ProviderHTTPError struct{ StatusCode int }

func (e *ProviderHTTPError) Error() string        { return "AI provider returned non-success status" }
func (e *ProviderHTTPError) Is(target error) bool { return target == ErrProviderNonSuccess }

// ProviderTransportError carries only retry-safe classification, never the
// original dial/read error (which can include endpoint or TLS details).
type ProviderTransportError struct{ Temporary bool }

func (e *ProviderTransportError) Error() string        { return "AI provider transport failed" }
func (e *ProviderTransportError) Is(target error) bool { return target == ErrProviderTransport }

type HTTPRequest struct {
	Method, Path string
	Headers      http.Header
	Body         []byte
}

type HTTPResponse struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

type Transport interface {
	Do(context.Context, ProviderConnection, HTTPRequest) (HTTPResponse, error)
}

// providerTransport resolves and validates the host immediately before each
// request. Its DialContext then uses only that IP, preventing a second DNS
// lookup from changing the destination between validation and connection.
type providerTransport struct {
	resolver     webhooks.Resolver
	local        bool
	dial         func(context.Context, string, string) (net.Conn, error)
	roundTripper http.RoundTripper // test seam; production always uses a pinned dialer.
}

func NewRemoteTransport(resolver webhooks.Resolver) Transport {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	return &providerTransport{resolver: resolver}
}

func NewLocalTransport() Transport {
	return &providerTransport{resolver: net.DefaultResolver, local: true}
}

func (t *providerTransport) Do(ctx context.Context, connection ProviderConnection, request HTTPRequest) (HTTPResponse, error) {
	if t == nil || t.resolver == nil || connection.Network != t.networkMode() || ValidateConnection(connection) != nil ||
		(t.local == false && webhooks.ValidateDestination(connection.BaseURL) != nil) {
		return HTTPResponse{}, ErrUnsafeProviderEndpoint
	}
	if request.Method != http.MethodGet && request.Method != http.MethodPost {
		return HTTPResponse{}, ErrUnsafeProviderEndpoint
	}
	if int64(len(request.Body)) > connection.RequestLimitBytes {
		return HTTPResponse{}, ErrProviderRequestTooLarge
	}
	endpoint, err := providerRequestURL(connection.BaseURL, request.Path)
	if err != nil {
		return HTTPResponse{}, ErrUnsafeProviderEndpoint
	}
	operationContext, cancel := context.WithTimeout(ctx, connection.Timeout)
	defer cancel()
	pinnedIP, err := t.resolvePinnedAddress(operationContext, endpoint.Hostname())
	if err != nil {
		if contextErr := providerContextError(operationContext, err); contextErr != nil {
			return HTTPResponse{}, contextErr
		}
		return HTTPResponse{}, ErrUnsafeProviderEndpoint
	}

	httpRequest, err := http.NewRequestWithContext(operationContext, request.Method, endpoint.String(), bytes.NewReader(request.Body))
	if err != nil {
		return HTTPResponse{}, ErrUnsafeProviderEndpoint
	}
	httpRequest.Host = endpoint.Host
	httpRequest.Header = request.Headers.Clone()
	response, err := t.client(pinnedIP, endpoint.Hostname()).Do(httpRequest)
	if err != nil {
		if errors.Is(err, ErrProviderRedirectRefused) {
			return HTTPResponse{}, ErrProviderRedirectRefused
		}
		if contextErr := providerContextError(operationContext, err); contextErr != nil {
			return HTTPResponse{}, contextErr
		}
		return HTTPResponse{}, providerTransportFailure(err)
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 32<<10))
		return HTTPResponse{StatusCode: response.StatusCode}, &ProviderHTTPError{StatusCode: response.StatusCode}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, connection.ResponseLimitBytes+1))
	if err != nil {
		if contextErr := providerContextError(operationContext, err); contextErr != nil {
			return HTTPResponse{}, contextErr
		}
		return HTTPResponse{}, providerTransportFailure(err)
	}
	if int64(len(body)) > connection.ResponseLimitBytes {
		return HTTPResponse{}, ErrProviderResponseTooLarge
	}
	return HTTPResponse{StatusCode: response.StatusCode, Header: response.Header.Clone(), Body: body}, nil
}

func providerTransportFailure(err error) error {
	var networkError net.Error
	return &ProviderTransportError{Temporary: errors.As(err, &networkError) && (networkError.Timeout() || networkError.Temporary())}
}

func (t *providerTransport) networkMode() NetworkMode {
	if t.local {
		return NetworkLocal
	}
	return NetworkRemote
}

func (t *providerTransport) resolvePinnedAddress(ctx context.Context, host string) (string, error) {
	addresses, err := t.resolver.LookupIPAddr(ctx, host)
	if err != nil || len(addresses) == 0 {
		return "", err
	}
	for _, address := range addresses {
		if (t.local && !isLocalProviderIP(address.IP)) || (!t.local && !isPublicProviderIP(address.IP)) {
			return "", ErrUnsafeProviderEndpoint
		}
	}
	return addresses[0].IP.String(), nil
}

func (t *providerTransport) client(pinnedIP, serverName string) *http.Client {
	if t.roundTripper != nil {
		return &http.Client{Transport: t.roundTripper, CheckRedirect: refuseProviderRedirect}
	}
	dial := t.dial
	if dial == nil {
		dial = (&net.Dialer{}).DialContext
	}
	return newPinnedHTTPClient(pinnedIP, serverName, dial)
}

func newPinnedHTTPClient(pinnedIP, serverName string, dial func(context.Context, string, string) (net.Conn, error)) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, ServerName: serverName}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		_, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, ErrUnsafeProviderEndpoint
		}
		return dial(ctx, network, net.JoinHostPort(pinnedIP, port))
	}
	return &http.Client{Transport: transport, CheckRedirect: refuseProviderRedirect}
}

func refuseProviderRedirect(*http.Request, []*http.Request) error { return ErrProviderRedirectRefused }

func providerRequestURL(rawBaseURL, path string) (*url.URL, error) {
	normalizedPath, err := normalizedProviderPath(path)
	if err != nil {
		return nil, err
	}
	endpoint, err := url.Parse(rawBaseURL)
	if err != nil {
		return nil, err
	}
	endpoint.Path = normalizedPath
	endpoint.RawPath = ""
	endpoint.RawQuery = ""
	endpoint.Fragment = ""
	return endpoint, nil
}

func validProviderPath(path string) bool {
	_, err := normalizedProviderPath(path)
	return err == nil
}

func normalizedProviderPath(path string) (string, error) {
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.ContainsAny(path, "?#\\") {
		return "", ErrUnsafeProviderEndpoint
	}
	lowerPath := strings.ToLower(path)
	if strings.Contains(lowerPath, "%2f") || strings.Contains(lowerPath, "%5c") {
		return "", ErrUnsafeProviderEndpoint
	}
	decoded, err := url.PathUnescape(path)
	if err != nil || !strings.HasPrefix(decoded, "/") || strings.HasPrefix(decoded, "//") || strings.Contains(decoded, "\\") {
		return "", ErrUnsafeProviderEndpoint
	}
	for _, segment := range strings.Split(decoded, "/") {
		if segment == "." || segment == ".." {
			return "", ErrUnsafeProviderEndpoint
		}
	}
	return decoded, nil
}

func providerContextError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return nil
}

func isPublicProviderIP(ip net.IP) bool {
	return webhooks.IsPublicIP(ip)
}

func isLocalProviderIP(ip net.IP) bool {
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast())
}

type modeTransport struct {
	remote Transport
	local  Transport
}

func NewModeTransport(remote Transport, local Transport) Transport {
	return modeTransport{remote: remote, local: local}
}

func (t modeTransport) Do(ctx context.Context, connection ProviderConnection, request HTTPRequest) (HTTPResponse, error) {
	if ValidateConnection(connection) != nil {
		return HTTPResponse{}, ErrUnsafeProviderEndpoint
	}
	switch connection.Network {
	case NetworkRemote:
		if t.remote != nil {
			return t.remote.Do(ctx, connection, request)
		}
	case NetworkLocal:
		if t.local != nil {
			return t.local.Do(ctx, connection, request)
		}
	}
	return HTTPResponse{}, ErrUnsafeProviderEndpoint
}
