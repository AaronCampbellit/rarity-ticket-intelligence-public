package browserauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sessions"
)

func TestLocalAdminLoginIssuesSessionAndSecureCookies(t *testing.T) {
	accounts := &fakeBreakGlassAuthenticator{
		technicianID: "technician-id",
	}
	sessionManager := &fakeSessionIssuer{}
	handler, err := NewBreakGlassHandler(BreakGlassConfig{
		MSPID: "msp-id", SessionTTL: 30 * time.Minute, Secure: true,
	}, accounts, sessionManager, func(int) ([]byte, error) {
		return []byte("01234567890123456789012345678901"), nil
	}, http.NotFoundHandler())
	if err != nil {
		t.Fatal(err)
	}

	form := url.Values{"username": {"recovery-admin"}, "password": {"correct horse battery staple"}}
	request := httptest.NewRequest(http.MethodPost, "https://rarity.example.test/auth/local/login", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.RemoteAddr = "10.20.30.44:52000"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusSeeOther {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if accounts.username != "recovery-admin" || accounts.password != "correct horse battery staple" ||
		accounts.sourceIP != "10.20.30.44" {
		t.Fatalf("authentication evidence not preserved: %+v", accounts)
	}
	if sessionManager.command.MSPID != "msp-id" ||
		sessionManager.command.TechnicianID != "technician-id" ||
		sessionManager.command.TTL != 30*time.Minute {
		t.Fatalf("unexpected session command: %+v", sessionManager.command)
	}
	cookies := response.Result().Cookies()
	if !hasCookie(cookies, sessions.SessionCookieName, true) ||
		!hasCookie(cookies, sessions.CSRFCookieName, false) {
		t.Fatalf("secure session cookies missing: %+v", cookies)
	}
	if location := response.Header().Get("Location"); location != "/" {
		t.Fatalf("unexpected redirect %q", location)
	}
}

func TestBreakGlassLoginAliasIsDeprecated(t *testing.T) {
	handler, err := NewBreakGlassHandler(BreakGlassConfig{
		MSPID: "msp-id", SessionTTL: 30 * time.Minute,
	}, &fakeBreakGlassAuthenticator{err: ErrBreakGlassDenied}, &fakeSessionIssuer{}, nil, http.NotFoundHandler())
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(
		http.MethodPost, "/auth/break-glass/login",
		strings.NewReader("username=admin&password=incorrect"),
	)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Header().Get("Deprecation") != "true" ||
		!strings.Contains(response.Header().Get("Link"), "/auth/local/login") {
		t.Fatalf("compatibility headers missing: %v", response.Header())
	}
}

func TestBreakGlassLoginFailsWithoutLeakingAccountState(t *testing.T) {
	accounts := &fakeBreakGlassAuthenticator{err: ErrBreakGlassDenied}
	handler, err := NewBreakGlassHandler(BreakGlassConfig{
		MSPID: "msp-id", SessionTTL: 30 * time.Minute,
	}, accounts, &fakeSessionIssuer{}, nil, http.NotFoundHandler())
	if err != nil {
		t.Fatal(err)
	}

	for _, form := range []url.Values{
		{"username": {"missing"}, "password": {"wrong"}},
		{"username": {"disabled"}, "password": {"wrong"}},
	} {
		request := httptest.NewRequest(http.MethodPost, "/auth/break-glass/login", strings.NewReader(form.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized ||
			strings.TrimSpace(response.Body.String()) != "authentication failed" {
			t.Fatalf("response leaked account state: status=%d body=%q", response.Code, response.Body.String())
		}
	}
}

func TestBreakGlassLoginRejectsOversizedCredentialBody(t *testing.T) {
	handler, err := NewBreakGlassHandler(BreakGlassConfig{
		MSPID: "msp-id", SessionTTL: 30 * time.Minute,
	}, &fakeBreakGlassAuthenticator{}, &fakeSessionIssuer{}, nil, http.NotFoundHandler())
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(
		http.MethodPost,
		"/auth/break-glass/login",
		strings.NewReader("username=recovery-admin&password="+strings.Repeat("x", 20<<10)),
	)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized login status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestBreakGlassHandlerRevokesLocalSessionOnLogout(t *testing.T) {
	manager := &fakeSessionIssuer{}
	handler, err := NewBreakGlassHandler(BreakGlassConfig{
		MSPID: "msp-id", SessionTTL: 30 * time.Minute,
	}, &fakeBreakGlassAuthenticator{}, manager, nil, http.NotFoundHandler())
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	request.AddCookie(&http.Cookie{
		Name: sessions.DevelopmentSessionCookieName, Value: "opaque-session",
	})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent || manager.revoked != "opaque-session" {
		t.Fatalf("logout did not revoke local session: status=%d revoked=%q", response.Code, manager.revoked)
	}
}

func TestBreakGlassHandlerRefreshesRotatedSessionCookie(t *testing.T) {
	manager := &fakeSessionIssuer{rotated: sessions.Issued{
		Token:  "replacement-token",
		Record: sessions.Record{ExpiresAt: time.Now().Add(time.Hour)},
	}}
	handler, err := NewBreakGlassHandler(BreakGlassConfig{
		MSPID: "msp-id", SessionTTL: 30 * time.Minute, Secure: true,
	}, &fakeBreakGlassAuthenticator{}, manager, nil, http.NotFoundHandler())
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "https://rarity.example.test/auth/session/refresh", nil)
	request.AddCookie(&http.Cookie{Name: sessions.SessionCookieName, Value: "old-token"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || manager.rotatedFrom != "old-token" {
		t.Fatalf("status=%d rotated_from=%q", response.Code, manager.rotatedFrom)
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Value != "replacement-token" ||
		!cookies[0].Secure || !cookies[0].HttpOnly {
		t.Fatalf("replacement cookie=%+v", cookies)
	}
}

func TestBreakGlassHandlerListsAndRevokesOwnSessionsWithoutSecrets(t *testing.T) {
	manager := &fakeSessionIssuer{activeSessions: []sessions.Record{{
		ID:         "session-id",
		CreatedAt:  time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC),
		LastSeenAt: time.Date(2026, 7, 30, 11, 0, 0, 0, time.UTC),
		ExpiresAt:  time.Date(2026, 7, 30, 18, 0, 0, 0, time.UTC),
		IPPrefix:   "192.0.2.0/24",
		TokenHash:  sessions.HashToken("opaque-session"),
	}}}
	handler, err := NewBreakGlassHandler(BreakGlassConfig{
		MSPID: "msp-id", SessionTTL: 30 * time.Minute,
	}, &fakeBreakGlassAuthenticator{}, manager, nil, http.NotFoundHandler())
	if err != nil {
		t.Fatal(err)
	}

	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil)
	listRequest.AddCookie(&http.Cookie{
		Name: sessions.DevelopmentSessionCookieName, Value: "opaque-session",
	})
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK ||
		!strings.Contains(listResponse.Body.String(), `"id":"session-id"`) ||
		!strings.Contains(listResponse.Body.String(), `"current":true`) ||
		strings.Contains(listResponse.Body.String(), "token") {
		t.Fatalf("unsafe session list: status=%d body=%s", listResponse.Code, listResponse.Body.String())
	}

	deleteRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/sessions/session-id", nil)
	deleteRequest.AddCookie(&http.Cookie{
		Name: sessions.DevelopmentSessionCookieName, Value: "opaque-session",
	})
	deleteResponse := httptest.NewRecorder()
	handler.ServeHTTP(deleteResponse, deleteRequest)
	if deleteResponse.Code != http.StatusNoContent ||
		manager.revokedSessionID != "session-id" {
		t.Fatalf("session revoke failed: status=%d id=%q", deleteResponse.Code, manager.revokedSessionID)
	}
}

type fakeBreakGlassAuthenticator struct {
	technicianID string
	err          error
	username     string
	password     string
	sourceIP     string
}

func (f *fakeBreakGlassAuthenticator) AuthenticateBreakGlass(
	_ context.Context,
	username string,
	password string,
	sourceIP string,
) (string, error) {
	f.username, f.password, f.sourceIP = username, password, sourceIP
	return f.technicianID, f.err
}
