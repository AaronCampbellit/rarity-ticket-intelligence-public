package aiassist

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Each test below names a transport-boundary regression: a configuration
// validation omission, a redirect following change, an unpinned DNS dial, or
// a size/status/cancellation error that can expose provider data.
type providerResolver struct{ addresses []net.IPAddr }

func (r providerResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	return r.addresses, nil
}

type stalledProviderResolver struct{}

func (stalledProviderResolver) LookupIPAddr(ctx context.Context, _ string) ([]net.IPAddr, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

type providerRoundTripper func(*http.Request) (*http.Response, error)

func (f providerRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func remoteTransportForTest(resolver providerResolver, roundTripper http.RoundTripper) *providerTransport {
	return &providerTransport{resolver: resolver, roundTripper: roundTripper}
}

func localConnection(baseURL string) ProviderConnection {
	connection := validProviderConnection()
	acknowledged := time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)
	connection.Adapter = AdapterOllama
	connection.Network = NetworkLocal
	connection.BaseURL = baseURL
	connection.Timeout = 15 * time.Minute
	connection.LocalNetworkAcknowledgedAt = &acknowledged
	return connection
}

func TestRemoteTransportRejectsRedirectAndPrivateDialAddress(t *testing.T) {
	transport := NewRemoteTransport(providerResolver{addresses: []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}})
	_, err := transport.Do(context.Background(), validProviderConnection(), HTTPRequest{
		Method: http.MethodPost, Path: "/v1/chat/completions", Body: []byte(`{}`),
	})
	if !errors.Is(err, ErrUnsafeProviderEndpoint) {
		t.Fatalf("private destination error=%v", err)
	}

	redirecting := remoteTransportForTest(providerResolver{addresses: []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}}, providerRoundTripper(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": []string{"https://elsewhere.example.test"}}, Body: io.NopCloser(strings.NewReader("redirect"))}, nil
	}))
	_, err = redirecting.Do(context.Background(), validProviderConnection(), HTTPRequest{Method: http.MethodGet, Path: "/v1/models"})
	if !errors.Is(err, ErrProviderRedirectRefused) {
		t.Fatalf("redirect error=%v", err)
	}
}

func TestRemoteTransportPinsValidatedDNSAddress(t *testing.T) {
	transport := NewRemoteTransport(providerResolver{addresses: []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}}).(*providerTransport)
	var dialAddress string
	transport.dial = func(_ context.Context, _ string, address string) (net.Conn, error) {
		dialAddress = address
		return nil, errors.New("synthetic dial failure")
	}
	_, err := transport.Do(context.Background(), validProviderConnection(), HTTPRequest{Method: http.MethodGet, Path: "/v1/models"})
	if err == nil || dialAddress != "8.8.8.8:443" {
		t.Fatalf("error=%v dial address=%q, want pinned public DNS address", err, dialAddress)
	}
}

func TestRemoteTransportRejectsHTTPAndEscapingPathsBeforeSend(t *testing.T) {
	transport := remoteTransportForTest(providerResolver{addresses: []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}}, providerRoundTripper(func(*http.Request) (*http.Response, error) {
		t.Fatal("unsafe endpoint was sent")
		return nil, nil
	}))
	remoteHTTP := validProviderConnection()
	remoteHTTP.BaseURL = "http://models.example.test"
	if _, err := transport.Do(context.Background(), remoteHTTP, HTTPRequest{Method: http.MethodGet, Path: "/v1/models"}); !errors.Is(err, ErrUnsafeProviderEndpoint) {
		t.Fatalf("remote HTTP error=%v", err)
	}
	for _, path := range []string{"https://attacker.example.test/v1/models", "//attacker.example.test/v1/models", "/v1/../admin", "/%2e%2e/admin", "/v1%2fadmin", "/v1%5cadmin", "/v1\\admin", "/bad%zz", "/v1/models?debug=true"} {
		if _, err := transport.Do(context.Background(), validProviderConnection(), HTTPRequest{Method: http.MethodGet, Path: path}); !errors.Is(err, ErrUnsafeProviderEndpoint) {
			t.Fatalf("path %q error=%v", path, err)
		}
	}
}

func TestRemoteTransportRejectsUnsupportedMethodsBeforeSend(t *testing.T) {
	transport := remoteTransportForTest(providerResolver{addresses: []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}}, providerRoundTripper(func(*http.Request) (*http.Response, error) {
		t.Fatal("unsupported method was sent")
		return nil, nil
	}))
	for _, method := range []string{"", http.MethodPut, http.MethodDelete, http.MethodHead} {
		if _, err := transport.Do(context.Background(), validProviderConnection(), HTTPRequest{Method: method, Path: "/v1/models"}); !errors.Is(err, ErrUnsafeProviderEndpoint) {
			t.Fatalf("method %q error=%v", method, err)
		}
	}
}

func TestRemoteTransportRejectsSpecialUseAddresses(t *testing.T) {
	for _, rawIP := range []string{"100.64.0.1", "2001:db8::1", "::1"} {
		t.Run(rawIP, func(t *testing.T) {
			transport := remoteTransportForTest(providerResolver{addresses: []net.IPAddr{{IP: net.ParseIP(rawIP)}}}, providerRoundTripper(func(*http.Request) (*http.Response, error) {
				t.Fatal("special-use address was sent")
				return nil, nil
			}))
			if _, err := transport.Do(context.Background(), validProviderConnection(), HTTPRequest{Method: http.MethodGet, Path: "/v1/models"}); !errors.Is(err, ErrUnsafeProviderEndpoint) {
				t.Fatalf("address %s error=%v", rawIP, err)
			}
		})
	}
	transport := remoteTransportForTest(providerResolver{addresses: []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}}, providerRoundTripper(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("ok"))}, nil
	}))
	if _, err := transport.Do(context.Background(), validProviderConnection(), HTTPRequest{Method: http.MethodGet, Path: "/v1/models"}); err != nil {
		t.Fatalf("valid public address error=%v", err)
	}
}

func TestRemoteTransportUsesSharedReachabilityValidator(t *testing.T) {
	allowed := remoteTransportForTest(providerResolver{addresses: []net.IPAddr{{IP: net.ParseIP("64:ff9b::c000:201")}}}, providerRoundTripper(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("ok"))}, nil
	}))
	if _, err := allowed.Do(context.Background(), validProviderConnection(), HTTPRequest{Method: http.MethodGet, Path: "/v1/models"}); err != nil {
		t.Fatalf("globally reachable NAT64 destination error=%v", err)
	}
	blocked := remoteTransportForTest(providerResolver{addresses: []net.IPAddr{{IP: net.ParseIP("100:0:0:1::1")}}}, providerRoundTripper(func(*http.Request) (*http.Response, error) {
		t.Fatal("non-global dummy IPv6 address was sent")
		return nil, nil
	}))
	if _, err := blocked.Do(context.Background(), validProviderConnection(), HTTPRequest{Method: http.MethodGet, Path: "/v1/models"}); !errors.Is(err, ErrUnsafeProviderEndpoint) {
		t.Fatalf("non-global dummy IPv6 error=%v", err)
	}
	mappedCGNAT := remoteTransportForTest(providerResolver{addresses: []net.IPAddr{{IP: net.ParseIP("::ffff:100.64.0.1")}}}, providerRoundTripper(func(*http.Request) (*http.Response, error) {
		t.Fatal("mapped carrier-grade NAT address was sent")
		return nil, nil
	}))
	if _, err := mappedCGNAT.Do(context.Background(), validProviderConnection(), HTTPRequest{Method: http.MethodGet, Path: "/v1/models"}); !errors.Is(err, ErrUnsafeProviderEndpoint) {
		t.Fatalf("mapped carrier-grade NAT error=%v", err)
	}
}

func TestProviderTransportBoundsDNSWithConnectionTimeoutAndCancellation(t *testing.T) {
	connection := validProviderConnection()
	connection.Timeout = time.Second
	outer, cancelOuter := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelOuter()
	started := time.Now()
	_, err := NewRemoteTransport(stalledProviderResolver{}).Do(outer, connection, HTTPRequest{Method: http.MethodGet, Path: "/v1/models"})
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > 1500*time.Millisecond {
		t.Fatalf("connection timeout error=%v elapsed=%s", err, time.Since(started))
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = NewRemoteTransport(stalledProviderResolver{}).Do(canceled, validProviderConnection(), HTTPRequest{Method: http.MethodGet, Path: "/v1/models"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled resolver error=%v", err)
	}
}

func TestPinnedClientPreservesOriginHostAndTLSName(t *testing.T) {
	client := newPinnedHTTPClient("8.8.8.8", "models.example.test", (&net.Dialer{}).DialContext)
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.TLSClientConfig == nil || transport.TLSClientConfig.ServerName != "models.example.test" {
		t.Fatalf("TLS ServerName=%v, want original host", transport.TLSClientConfig)
	}
	seenHost := ""
	requesting := remoteTransportForTest(providerResolver{addresses: []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}}, providerRoundTripper(func(request *http.Request) (*http.Response, error) {
		seenHost = request.Host
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("ok"))}, nil
	}))
	if _, err := requesting.Do(context.Background(), validProviderConnection(), HTTPRequest{Method: http.MethodGet, Path: "/v1/models"}); err != nil || seenHost != "models.example.test" {
		t.Fatalf("Host=%q error=%v", seenHost, err)
	}
}

func TestLocalTransportPermitsAcknowledgedLoopbackAndRefusesRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/models", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"models":[]}`))
	}))
	defer server.Close()

	transport := NewLocalTransport()
	response, err := transport.Do(context.Background(), localConnection(server.URL), HTTPRequest{Method: http.MethodGet, Path: "/models"})
	if err != nil || response.StatusCode != http.StatusOK || string(response.Body) != `{"models":[]}` {
		t.Fatalf("loopback response=%+v error=%v", response, err)
	}
	_, err = transport.Do(context.Background(), localConnection(server.URL), HTTPRequest{Method: http.MethodGet, Path: "/redirect"})
	if !errors.Is(err, ErrProviderRedirectRefused) {
		t.Fatalf("redirect error=%v", err)
	}
}

func TestProviderTransportBoundsRequestAndResponseWithoutSendingOversizeRequest(t *testing.T) {
	called := false
	transport := remoteTransportForTest(providerResolver{addresses: []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}}, providerRoundTripper(func(*http.Request) (*http.Response, error) {
		called = true
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(make([]byte, defaultResponseLimitBytes+1)))}, nil
	}))
	connection := validProviderConnection()
	connection.RequestLimitBytes = 1024
	if _, err := transport.Do(context.Background(), connection, HTTPRequest{Method: http.MethodPost, Path: "/v1/chat/completions", Body: make([]byte, 1025)}); !errors.Is(err, ErrProviderRequestTooLarge) || called {
		t.Fatalf("oversize request error=%v called=%t", err, called)
	}
	connection.RequestLimitBytes = defaultRequestLimitBytes
	_, err := transport.Do(context.Background(), connection, HTTPRequest{Method: http.MethodGet, Path: "/v1/models"})
	if !errors.Is(err, ErrProviderResponseTooLarge) {
		t.Fatalf("oversize response error=%v", err)
	}
}

func TestProviderTransportKeepsNonSuccessBodyOutOfErrors(t *testing.T) {
	secret := "provider response secret"
	transport := remoteTransportForTest(providerResolver{addresses: []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}}, providerRoundTripper(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusUnauthorized, Header: http.Header{"X-Diagnostic": []string{secret}}, Body: io.NopCloser(strings.NewReader(secret))}, nil
	}))
	response, err := transport.Do(context.Background(), validProviderConnection(), HTTPRequest{Method: http.MethodGet, Path: "/v1/models", Headers: http.Header{"Authorization": []string{"Bearer client-secret"}}})
	if !errors.Is(err, ErrProviderNonSuccess) || response.StatusCode != http.StatusUnauthorized || len(response.Body) != 0 || len(response.Header) != 0 || strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "client-secret") {
		t.Fatalf("response=%+v error=%v leaked sensitive provider data", response, err)
	}
}

func TestProviderTransportHonorsCancellationAndConfiguredTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	transport := NewLocalTransport()
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := transport.Do(canceled, localConnection(server.URL), HTTPRequest{Method: http.MethodGet, Path: "/models"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error=%v", err)
	}
	connection := localConnection(server.URL)
	connection.Timeout = time.Second
	started := time.Now()
	_, err = transport.Do(context.Background(), connection, HTTPRequest{Method: http.MethodGet, Path: "/models"})
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > 3*time.Second {
		t.Fatalf("timeout error=%v elapsed=%s", err, time.Since(started))
	}
}

func TestModeTransportSelectsStoredNetworkMode(t *testing.T) {
	remoteCalled, localCalled := false, false
	remote := transportFunc(func(context.Context, ProviderConnection, HTTPRequest) (HTTPResponse, error) {
		remoteCalled = true
		return HTTPResponse{}, nil
	})
	local := transportFunc(func(context.Context, ProviderConnection, HTTPRequest) (HTTPResponse, error) {
		localCalled = true
		return HTTPResponse{}, nil
	})
	connection := validProviderConnection()
	if _, err := NewModeTransport(remote, local).Do(context.Background(), connection, HTTPRequest{}); err != nil || !remoteCalled || localCalled {
		t.Fatalf("remote=%t local=%t error=%v", remoteCalled, localCalled, err)
	}
}

type transportFunc func(context.Context, ProviderConnection, HTTPRequest) (HTTPResponse, error)

func (f transportFunc) Do(ctx context.Context, connection ProviderConnection, request HTTPRequest) (HTTPResponse, error) {
	return f(ctx, connection, request)
}
