package servicekeys

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/httpauth"
)

type stubAuthenticator struct {
	token  string
	result Authenticated
	err    error
}

func (s *stubAuthenticator) Authenticate(_ context.Context, token string) (Authenticated, error) {
	s.token = token
	return s.result, s.err
}

func TestBearerPrincipalResolverBuildsTrustedIntegrationPrincipal(t *testing.T) {
	authenticator := &stubAuthenticator{result: Authenticated{
		KeyID: "key-id", MSPID: "msp-id", ClientID: "client-id",
		Capabilities: []string{"work_record.create"}, DataScopes: []string{"work_records"},
	}}
	resolve := BearerPrincipalResolver(authenticator)
	request := httptest.NewRequest(http.MethodPost, "/v1/work-records", nil)
	request.Header.Set("Authorization", "Bearer rsk_prefix_secret")

	principal, err := resolve(request)
	if err != nil {
		t.Fatalf("resolve() error = %v", err)
	}
	if authenticator.token != "rsk_prefix_secret" || principal.ID != "key-id" {
		t.Fatalf("unexpected authentication result: token=%q principal=%+v", authenticator.token, principal)
	}
	if !principal.Capabilities.Has("work_record.create") ||
		!principal.AllowsData("work_records") ||
		principal.AllowsData("attachments") {
		t.Fatalf("resolver widened service-key grants: %+v", principal)
	}
}

func TestBearerPrincipalResolverRejectsMissingMalformedAndInvalidCredentials(t *testing.T) {
	invalid := &stubAuthenticator{err: ErrInvalidKey}
	resolve := BearerPrincipalResolver(invalid)
	for _, header := range []string{"", "Basic abc", "Bearer", "Bearer one two"} {
		request := httptest.NewRequest(http.MethodGet, "/v1/work-records", nil)
		request.Header.Set("Authorization", header)
		if _, err := resolve(request); !errors.Is(err, ErrInvalidKey) {
			t.Fatalf("header %q error = %v", header, err)
		}
	}

	request := httptest.NewRequest(http.MethodGet, "/v1/work-records", nil)
	request.Header.Set("Authorization", "Bearer rsk_prefix_invalid")
	if _, err := resolve(request); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("invalid credential error = %v", err)
	}
}

func TestBearerPrincipalResolverRejectsClientHeaderScopeOverride(t *testing.T) {
	resolve := BearerPrincipalResolver(&stubAuthenticator{result: Authenticated{
		KeyID: "key-id", MSPID: "msp-id", ClientID: "client-id",
	}})
	request := httptest.NewRequest("GET", "/", nil)
	request.Header.Set("Authorization", "Bearer rsk_prefix_secret")
	request.Header.Set(httpauth.ClientHeader, "other-client")

	if _, err := resolve(request); err == nil {
		t.Fatal("service key accepted a conflicting selected Client")
	}
}
