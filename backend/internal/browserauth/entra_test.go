package browserauth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/identity"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sessions"
)

func TestEntraLoginUsesAuthorizationCodePKCEAndCallbackIssuesCookies(t *testing.T) {
	at := time.Date(2026, time.July, 30, 22, 0, 0, 0, time.UTC)
	identityAuth := &fakeIdentityAuthenticator{}
	sessionIssuer := &fakeSessionIssuer{}
	tokenServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			t.Fatalf("token method=%s", request.Method)
		}
		_ = request.ParseForm()
		if request.Form.Get("grant_type") != "authorization_code" ||
			request.Form.Get("code_verifier") == "" {
			t.Fatalf("token form=%v", request.Form)
		}
		_ = json.NewEncoder(writer).Encode(map[string]string{"id_token": "verified-id-token"})
	}))
	defer tokenServer.Close()
	handler, err := NewEntraHandler(Config{
		MSPID: "msp-id", TenantID: "tenant-id", ClientID: "client-id",
		ClientSecret: "client-secret",
		RedirectURL:  "https://rarity.example.test/auth/entra/callback",
		SessionKey:   strings.Repeat("s", 32), SessionTTL: 8 * time.Hour,
	}, identityAuth, sessionIssuer, tokenServer.Client(),
		func() time.Time { return at },
		func(size int) ([]byte, error) { return []byte(strings.Repeat("x", size)), nil },
		http.NotFoundHandler())
	if err != nil {
		t.Fatal(err)
	}
	handler.tokenURL = tokenServer.URL

	login := httptest.NewRequest(http.MethodGet, "https://rarity.example.test/auth/entra/login", nil)
	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, login)
	if loginResponse.Code != http.StatusFound {
		t.Fatalf("login status=%d body=%s", loginResponse.Code, loginResponse.Body.String())
	}
	location, _ := url.Parse(loginResponse.Header().Get("Location"))
	if location.Query().Get("response_type") != "code" ||
		location.Query().Get("code_challenge_method") != "S256" ||
		location.Query().Get("nonce") == "" {
		t.Fatalf("authorization URL=%s", location)
	}
	stateCookie := loginResponse.Result().Cookies()[0]
	callback := httptest.NewRequest(
		http.MethodGet,
		"https://rarity.example.test/auth/entra/callback?code=auth-code&state="+
			url.QueryEscape(location.Query().Get("state")),
		nil,
	)
	callback.AddCookie(stateCookie)
	callbackResponse := httptest.NewRecorder()
	handler.ServeHTTP(callbackResponse, callback)
	if callbackResponse.Code != http.StatusFound ||
		identityAuth.token != "verified-id-token" ||
		identityAuth.nonce != location.Query().Get("nonce") ||
		sessionIssuer.command.TechnicianID != "technician-id" {
		t.Fatalf("callback status=%d identity=%+v session=%+v body=%s",
			callbackResponse.Code, identityAuth, sessionIssuer, callbackResponse.Body.String())
	}
	cookies := callbackResponse.Result().Cookies()
	if !hasCookie(cookies, sessions.SessionCookieName, true) ||
		!hasCookie(cookies, sessions.CSRFCookieName, false) {
		t.Fatalf("callback cookies=%+v", cookies)
	}
}

func TestEntraCallbackRejectsConfigurationChangedAfterLogin(t *testing.T) {
	tokenRequests := 0
	tokenServer := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		_ *http.Request,
	) {
		tokenRequests++
		_ = json.NewEncoder(writer).Encode(map[string]string{
			"id_token": "verified-id-token",
		})
	}))
	defer tokenServer.Close()
	newHandler := func(clientID string) *Handler {
		handler, err := NewEntraHandler(Config{
			MSPID: "msp-id", TenantID: "tenant-id", ClientID: clientID,
			ClientSecret: "client-secret-" + clientID,
			RedirectURL:  "https://rarity.example.test/auth/entra/callback",
			SessionKey:   strings.Repeat("s", 32), SessionTTL: 8 * time.Hour,
		}, &fakeIdentityAuthenticator{}, &fakeSessionIssuer{},
			tokenServer.Client(), time.Now,
			func(size int) ([]byte, error) {
				return []byte(strings.Repeat("x", size)), nil
			},
			http.NotFoundHandler())
		if err != nil {
			t.Fatal(err)
		}
		handler.tokenURL = tokenServer.URL
		return handler
	}

	loginResponse := httptest.NewRecorder()
	newHandler("client-before").ServeHTTP(
		loginResponse,
		httptest.NewRequest(
			http.MethodGet,
			"https://rarity.example.test/auth/entra/login",
			nil,
		),
	)
	authorizationURL, _ := url.Parse(loginResponse.Header().Get("Location"))
	callback := httptest.NewRequest(
		http.MethodGet,
		"https://rarity.example.test/auth/entra/callback?code=auth-code&state="+
			url.QueryEscape(authorizationURL.Query().Get("state")),
		nil,
	)
	callback.AddCookie(loginResponse.Result().Cookies()[0])
	callbackResponse := httptest.NewRecorder()

	newHandler("client-after").ServeHTTP(callbackResponse, callback)

	if callbackResponse.Code != http.StatusFound ||
		callbackResponse.Header().Get("Location") != "/#/login?error=entra" ||
		tokenRequests != 0 {
		t.Fatalf(
			"status=%d location=%q token_requests=%d",
			callbackResponse.Code,
			callbackResponse.Header().Get("Location"),
			tokenRequests,
		)
	}
}

func TestLogoutRevokesServerSessionAndClearsCookies(t *testing.T) {
	manager := &fakeSessionIssuer{}
	handler, err := NewEntraHandler(Config{
		MSPID: "msp-id", TenantID: "tenant-id", ClientID: "client-id",
		ClientSecret: "client-secret",
		RedirectURL:  "https://rarity.example.test/auth/entra/callback",
		SessionKey:   strings.Repeat("s", 32), SessionTTL: 8 * time.Hour,
	}, &fakeIdentityAuthenticator{}, manager, nil, time.Now, nil, http.NotFoundHandler())
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "https://rarity.example.test/auth/logout", nil)
	request.AddCookie(&http.Cookie{Name: sessions.SessionCookieName, Value: "opaque-session"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || manager.revoked != "opaque-session" {
		t.Fatalf("logout status=%d manager=%+v", response.Code, manager)
	}
}

func TestEntraCallbackFailuresReturnToLogin(t *testing.T) {
	at := time.Date(2026, time.August, 3, 20, 0, 0, 0, time.UTC)
	tests := []struct {
		name           string
		identityErr    error
		issueErr       error
		failCSRFRandom bool
		wantLocation   string
	}{
		{
			name:         "identity rejection",
			identityErr:  errors.New("identity rejected"),
			wantLocation: "/#/login?error=entra",
		},
		{
			name:         "session issuance unavailable",
			issueErr:     errors.New("session store unavailable"),
			wantLocation: "/#/login?error=unavailable",
		},
		{
			name:           "CSRF generation unavailable",
			failCSRFRandom: true,
			wantLocation:   "/#/login?error=unavailable",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			identityAuth := &fakeIdentityAuthenticator{err: test.identityErr}
			sessionIssuer := &fakeSessionIssuer{issueErr: test.issueErr}
			tokenServer := httptest.NewServer(http.HandlerFunc(func(
				writer http.ResponseWriter,
				_ *http.Request,
			) {
				_ = json.NewEncoder(writer).Encode(map[string]string{
					"id_token": "verified-id-token",
				})
			}))
			defer tokenServer.Close()
			randomCalls := 0
			handler, err := NewEntraHandler(Config{
				MSPID: "msp-id", TenantID: "tenant-id", ClientID: "client-id",
				ClientSecret: "client-secret",
				RedirectURL:  "https://rarity.example.test/auth/entra/callback",
				SessionKey:   strings.Repeat("s", 32), SessionTTL: 8 * time.Hour,
			}, identityAuth, sessionIssuer, tokenServer.Client(),
				func() time.Time { return at },
				func(size int) ([]byte, error) {
					randomCalls++
					if test.failCSRFRandom && randomCalls == 4 {
						return nil, errors.New("random unavailable")
					}
					return []byte(strings.Repeat("x", size)), nil
				},
				http.NotFoundHandler())
			if err != nil {
				t.Fatal(err)
			}
			handler.tokenURL = tokenServer.URL

			login := httptest.NewRequest(
				http.MethodGet,
				"https://rarity.example.test/auth/entra/login",
				nil,
			)
			loginResponse := httptest.NewRecorder()
			handler.ServeHTTP(loginResponse, login)
			location, _ := url.Parse(loginResponse.Header().Get("Location"))
			callback := httptest.NewRequest(
				http.MethodGet,
				"https://rarity.example.test/auth/entra/callback?code=auth-code&state="+
					url.QueryEscape(location.Query().Get("state")),
				nil,
			)
			callback.AddCookie(loginResponse.Result().Cookies()[0])
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, callback)

			if response.Code != http.StatusFound ||
				response.Header().Get("Location") != test.wantLocation ||
				response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf(
					"status=%d location=%q cache=%q body=%q",
					response.Code,
					response.Header().Get("Location"),
					response.Header().Get("Cache-Control"),
					response.Body.String(),
				)
			}
		})
	}
}

func TestEntraCallbackWithoutValidStateReturnsToLogin(t *testing.T) {
	handler, err := NewEntraHandler(Config{
		MSPID: "msp-id", TenantID: "tenant-id", ClientID: "client-id",
		ClientSecret: "client-secret",
		RedirectURL:  "https://rarity.example.test/auth/entra/callback",
		SessionKey:   strings.Repeat("s", 32), SessionTTL: 8 * time.Hour,
	}, &fakeIdentityAuthenticator{}, &fakeSessionIssuer{}, nil, time.Now, nil,
		http.NotFoundHandler())
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{
		"https://rarity.example.test/auth/entra/callback",
		"https://rarity.example.test/auth/entra/callback?error=access_denied&error_description=do-not-reflect",
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(
			response,
			httptest.NewRequest(http.MethodGet, target, nil),
		)
		if response.Code != http.StatusFound ||
			response.Header().Get("Location") != "/#/login?error=entra" ||
			strings.Contains(response.Body.String(), "do-not-reflect") {
			t.Fatalf(
				"target=%q status=%d location=%q body=%q",
				target,
				response.Code,
				response.Header().Get("Location"),
				response.Body.String(),
			)
		}
	}
}

type fakeIdentityAuthenticator struct {
	token string
	nonce string
	err   error
}

func (a *fakeIdentityAuthenticator) Authenticate(
	_ context.Context,
	token string,
	nonce ...string,
) (identity.Technician, error) {
	a.token = token
	if len(nonce) > 0 {
		a.nonce = nonce[0]
	}
	if a.err != nil {
		return identity.Technician{}, a.err
	}
	return identity.Technician{ID: "technician-id", MSPID: "msp-id"}, nil
}

type fakeSessionIssuer struct {
	command          sessions.IssueCommand
	revoked          string
	activeSessions   []sessions.Record
	revokedSessionID string
	rotatedFrom      string
	rotated          sessions.Issued
	issueErr         error
}

func (s *fakeSessionIssuer) Rotate(_ context.Context, token string) (sessions.Issued, error) {
	s.rotatedFrom = token
	return s.rotated, nil
}

func (s *fakeSessionIssuer) Issue(
	_ context.Context,
	command sessions.IssueCommand,
) (sessions.Issued, error) {
	s.command = command
	if s.issueErr != nil {
		return sessions.Issued{}, s.issueErr
	}
	return sessions.Issued{Token: "opaque-session"}, nil
}

func (*fakeSessionIssuer) Authenticate(
	context.Context,
	string,
) (sessions.Authenticated, error) {
	return sessions.Authenticated{TechnicianID: "technician-id"}, nil
}

func (s *fakeSessionIssuer) Revoke(_ context.Context, token, _ string) error {
	s.revoked = token
	return nil
}

func (s *fakeSessionIssuer) ListActive(
	context.Context,
	string,
) ([]sessions.Record, error) {
	return s.activeSessions, nil
}

func (s *fakeSessionIssuer) RevokeSession(
	_ context.Context,
	_ string,
	sessionID string,
) error {
	s.revokedSessionID = sessionID
	return nil
}

func hasCookie(cookies []*http.Cookie, name string, httpOnly bool) bool {
	for _, cookie := range cookies {
		if cookie.Name == name && cookie.HttpOnly == httpOnly &&
			cookie.Secure && cookie.Path == "/" {
			return true
		}
	}
	return false
}
