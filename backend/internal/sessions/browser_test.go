package sessions

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
)

func TestCookieResolverAuthenticatesSessionWithoutBrowserBearer(t *testing.T) {
	authenticator := &fakeSessionAuthenticator{authenticated: Authenticated{
		SessionID: "session-id", MSPID: "msp-id", TechnicianID: "tech-id",
	}}
	loader := &fakePrincipalLoader{principal: authorization.Principal{ID: "tech-id"}}
	request := httptest.NewRequest(http.MethodGet, "https://rarity.example.test/api/v1/work-records", nil)
	request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "opaque-cookie"})
	request.Header.Set("X-Rarity-Client-ID", "client-id")

	principal, err := CookieOrBearerPrincipalResolver(authenticator, loader)(request)
	if err != nil || principal.ID != "tech-id" ||
		authenticator.token != "opaque-cookie" || loader.clientID != "client-id" {
		t.Fatalf("resolver principal=%+v error=%v auth=%+v loader=%+v",
			principal, err, authenticator, loader)
	}
}

func TestCSRFMiddlewareRequiresMatchingTokenAndOriginForCookieMutation(t *testing.T) {
	handler := CSRFMiddleware(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))
	valid := httptest.NewRequest(http.MethodPost, "https://rarity.example.test/api/v1/work-records", nil)
	valid.Host = "rarity.example.test"
	valid.Header.Set("Origin", "https://rarity.example.test")
	valid.Header.Set(CSRFHeaderName, "csrf-token")
	valid.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "session"})
	valid.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: "csrf-token"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, valid)
	if response.Code != http.StatusNoContent {
		t.Fatalf("valid status=%d body=%s", response.Code, response.Body.String())
	}

	invalid := valid.Clone(context.Background())
	invalid.Header.Set("Origin", "https://evil.example.test")
	invalidResponse := httptest.NewRecorder()
	handler.ServeHTTP(invalidResponse, invalid)
	if invalidResponse.Code != http.StatusForbidden {
		t.Fatalf("invalid status=%d body=%s", invalidResponse.Code, invalidResponse.Body.String())
	}
}

func TestBrowserSecurityMiddlewareSetsRestrictiveHeaders(t *testing.T) {
	handler := BrowserSecurityMiddleware(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodGet, "https://rarity.example.test/", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Header().Get("Content-Security-Policy") == "" ||
		response.Header().Get("X-Frame-Options") != "DENY" ||
		response.Header().Get("Strict-Transport-Security") == "" {
		t.Fatalf("security headers=%v", response.Header())
	}
}

type fakeSessionAuthenticator struct {
	authenticated Authenticated
	token         string
}

func (a *fakeSessionAuthenticator) Authenticate(_ context.Context, token string) (Authenticated, error) {
	a.token = token
	return a.authenticated, nil
}

type fakePrincipalLoader struct {
	principal authorization.Principal
	clientID  string
}

func (l *fakePrincipalLoader) LoadPrincipal(
	_ context.Context,
	_ Authenticated,
	clientID string,
) (authorization.Principal, error) {
	l.clientID = clientID
	return l.principal, nil
}
