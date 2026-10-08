package datto

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type dattoCredentialResolver struct {
	value []byte
}

func (r dattoCredentialResolver) Resolve(
	context.Context,
	string,
) ([]byte, error) {
	return append([]byte(nil), r.value...), nil
}

type dattoPayloadStore struct {
	keys    []string
	payload [][]byte
}

func (s *dattoPayloadStore) Put(
	_ context.Context,
	key string,
	body io.Reader,
	size int64,
	contentType string,
) error {
	value, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	if int64(len(value)) != size || contentType != "application/json" {
		return ErrInvalidSourceResponse
	}
	s.keys = append(s.keys, key)
	s.payload = append(s.payload, value)
	return nil
}

func TestHTTPSourceUsesDocumentedOAuthAndReadOnlyPaginatedDeviceAudit(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(
		response http.ResponseWriter,
		request *http.Request,
	) {
		switch request.URL.Path {
		case "/auth/oauth/token":
			if request.Method != http.MethodPost {
				t.Fatalf("token method = %s", request.Method)
			}
			username, password, ok := request.BasicAuth()
			if !ok || username != "public-client" || password != "public" {
				t.Fatalf("token basic auth = %q %q %v", username, password, ok)
			}
			if err := request.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if request.Form.Get("grant_type") != "password" ||
				request.Form.Get("username") != "api-key" ||
				request.Form.Get("password") != "api-secret" {
				t.Fatalf("token form = %v", request.Form)
			}
			_, _ = response.Write([]byte(`{"access_token":"access-token","expires_in":360000}`))
		case "/api/v2/account/devices":
			if request.Method != http.MethodGet ||
				request.Header.Get("Authorization") != "Bearer access-token" {
				t.Fatalf("device request method=%s auth=%q", request.Method, request.Header.Get("Authorization"))
			}
			if request.URL.Query().Get("max") != "250" {
				t.Fatalf("device query = %v", request.URL.Query())
			}
			_, _ = response.Write([]byte(`{
			  "pageDetails":{"count":1,"totalCount":1,"nextPageUrl":null},
			  "devices":[{
			    "uid":"device-id","siteUid":"site-id","hostname":"WORKSTATION-1",
			    "lastAuditDate":"2026-07-29T20:00:00Z","deleted":false
			  }]
			}`))
		case "/api/v2/audit/device/device-id":
			if request.Method != http.MethodGet {
				t.Fatalf("audit request method=%s", request.Method)
			}
			_, _ = response.Write([]byte(`{
			  "bios":{"serialNumber":"SERIAL-1"},
			  "nics":[{"macAddress":"00:11:22:33:44:55"}]
			}`))
		case "/api/v2/account/alerts/open":
			if request.Method != http.MethodGet ||
				request.URL.Query().Get("max") != "250" {
				t.Fatalf("open alert request = %s %v", request.Method, request.URL.Query())
			}
			_, _ = response.Write([]byte(`{
			  "pageDetails":{"count":1,"totalCount":1,"nextPageUrl":null},
			  "alerts":[{
			    "alertUid":"alert-open","priority":"Critical",
			    "diagnostics":"Gateway unreachable","resolved":false,
			    "timestamp":"2026-07-29T20:01:00Z",
			    "alertContext":{"@class":"OnlineOfflineStatusContext"},
			    "alertSourceInfo":{
			      "deviceUid":"device-id","deviceName":"GATEWAY-1",
			      "siteUid":"site-id","siteName":"Acme"
			    }
			  }]
			}`))
		case "/api/v2/account/alerts/resolved":
			_, _ = response.Write([]byte(`{
			  "pageDetails":{"count":1,"totalCount":1,"nextPageUrl":null},
			  "alerts":[{
			    "alertUid":"alert-cleared","priority":"High",
			    "diagnostics":"Service recovered","resolved":true,
			    "timestamp":"2026-07-29T19:00:00Z",
			    "resolvedOn":"2026-07-29T20:02:00Z",
			    "alertContext":{"@class":"StatusContext"},
			    "alertSourceInfo":{
			      "deviceUid":"device-id","deviceName":"GATEWAY-1",
			      "siteUid":"site-id","siteName":"Acme"
			    }
			  }]
			}`))
		default:
			t.Fatalf("unexpected Datto path %s", request.URL.Path)
		}
	}))
	defer server.Close()

	payloadStore := &dattoPayloadStore{}
	source, err := NewHTTPSource(HTTPSourceConfig{
		Client: server.Client(),
		Credentials: dattoCredentialResolver{value: []byte(`{
		  "api_url":"` + server.URL + `",
		  "api_key":"api-key",
		  "api_secret":"api-secret"
		}`)},
		CredentialRef: "env://RARITY_DATTO_CREDENTIAL_PRIMARY",
		PayloadStore:  payloadStore,
		ObjectPrefix:  "datto/connection-id",
		AllowHTTP:     true,
	})
	if err != nil {
		t.Fatalf("NewHTTPSource() error = %v", err)
	}
	page, err := source.Fetch(
		context.Background(),
		Connection{
			ID:                  "connection-id",
			CredentialSecretRef: "env://RARITY_DATTO_CREDENTIAL_PRIMARY",
		},
		SyncFull,
	)
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if len(page.Assets) != 1 ||
		len(page.Alerts) != 2 ||
		page.Alerts[0].ExternalID != "alert-open" ||
		page.Alerts[0].State != AlertActive ||
		page.Alerts[1].State != AlertCleared ||
		page.Alerts[1].ObservedAt.String() != "2026-07-29 20:02:00 +0000 UTC" ||
		page.Assets[0].ExternalID != "device-id" ||
		page.Assets[0].SerialNumber != "SERIAL-1" ||
		strings.Join(page.Assets[0].MACAddresses, "|") != "00:11:22:33:44:55" ||
		page.Assets[0].SourcePayloadRef == "" ||
		page.NextCursor == "" ||
		len(payloadStore.payload) != 4 ||
		!bytes.Contains(payloadStore.payload[0], []byte(`"device-id"`)) {
		t.Fatalf("unexpected page=%+v payloads=%d", page, len(payloadStore.payload))
	}
}

func TestHTTPSourceRejectsOffOriginPaginationBeforeSendingBearerToken(t *testing.T) {
	source, err := NewHTTPSource(HTTPSourceConfig{
		Client: http.DefaultClient,
		Credentials: dattoCredentialResolver{value: []byte(`{
		  "api_url":"https://merlot-api.centrastage.net",
		  "api_key":"api-key",
		  "api_secret":"api-secret"
		}`)},
		CredentialRef: "env://RARITY_DATTO_CREDENTIAL_PRIMARY",
		PayloadStore:  &dattoPayloadStore{},
		ObjectPrefix:  "datto/connection-id",
	})
	if err != nil {
		t.Fatalf("NewHTTPSource() error = %v", err)
	}
	if err := source.validateProviderURL(
		"https://attacker.example/api/v2/account/devices?page=2",
	); err != ErrInvalidProviderURL {
		t.Fatalf("validateProviderURL() error=%v", err)
	}
}

func TestHTTPSourceRetriesOneProviderRateLimitWithoutChangingMethod(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(
		response http.ResponseWriter,
		request *http.Request,
	) {
		calls++
		if request.Method != http.MethodGet {
			t.Fatalf("rate-limit retry method=%s", request.Method)
		}
		if calls == 1 {
			response.Header().Set("Retry-After", "0")
			response.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = response.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	source := &HTTPSource{
		client: server.Client(), apiBase: base,
		accessToken: "token", tokenExpiresAt: time.Now().Add(time.Hour),
		now: time.Now,
	}

	payload, _, err := source.authorizedJSON(
		context.Background(), server.URL+"/api/v2/test",
	)

	if err != nil || calls != 2 || string(payload) != `{"ok":true}` {
		t.Fatalf(
			"authorizedJSON() calls=%d payload=%s error=%v",
			calls, payload, err,
		)
	}
}
