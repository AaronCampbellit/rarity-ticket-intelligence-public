package sessions

import (
	"crypto/subtle"
	"net/http"
	"net/url"
	"strings"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/httpauth"
)

const (
	SessionCookieName            = "__Host-rarity_session"
	CSRFCookieName               = "__Host-rarity_csrf"
	DevelopmentSessionCookieName = "rarity_session"
	DevelopmentCSRFCookieName    = "rarity_csrf"
	CSRFHeaderName               = "X-Rarity-CSRF"
)

func CookieOrBearerPrincipalResolver(
	authenticator Authenticator,
	loader PrincipalLoader,
) func(*http.Request) (authorization.Principal, error) {
	bearer := BearerPrincipalResolver(authenticator, loader)
	return func(request *http.Request) (authorization.Principal, error) {
		if request == nil || authenticator == nil || loader == nil {
			return authorization.Principal{}, ErrInvalidSession
		}
		cookie, err := browserCookie(
			request, SessionCookieName, DevelopmentSessionCookieName,
		)
		if err != nil {
			return bearer(request)
		}
		authenticated, err := authenticator.Authenticate(request.Context(), cookie.Value)
		if err != nil {
			return authorization.Principal{}, ErrInvalidSession
		}
		clientID := strings.TrimSpace(request.Header.Get(httpauth.ClientHeader))
		principal, err := loader.LoadPrincipal(request.Context(), authenticated, clientID)
		if err != nil {
			return authorization.Principal{}, ErrInvalidSession
		}
		return principal, nil
	}
}

func CSRFMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request == nil || next == nil {
			http.Error(writer, "invalid request", http.StatusBadRequest)
			return
		}
		if !requiresCSRF(request) {
			next.ServeHTTP(writer, request)
			return
		}
		csrfCookie, err := browserCookie(
			request, CSRFCookieName, DevelopmentCSRFCookieName,
		)
		header := request.Header.Get(CSRFHeaderName)
		if err != nil || csrfCookie.Value == "" || header == "" ||
			len(csrfCookie.Value) != len(header) ||
			subtle.ConstantTimeCompare([]byte(csrfCookie.Value), []byte(header)) != 1 ||
			!sameOrigin(request) {
			writer.Header().Set("Content-Type", "application/json")
			writer.WriteHeader(http.StatusForbidden)
			_, _ = writer.Write([]byte(`{"error":{"code":"csrf_failed","message":"request origin or CSRF token is invalid"}}`))
			return
		}
		next.ServeHTTP(writer, request)
	})
}

func BrowserSecurityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set(
			"Content-Security-Policy",
			"default-src 'self'; base-uri 'none'; frame-ancestors 'none'; "+
				"form-action 'self'; img-src 'self' data:; object-src 'none'; "+
				"script-src 'self'; style-src 'self'; connect-src 'self'",
		)
		writer.Header().Set("Referrer-Policy", "no-referrer")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		writer.Header().Set("X-Frame-Options", "DENY")
		writer.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if request.TLS != nil {
			writer.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(writer, request)
	})
}

func requiresCSRF(request *http.Request) bool {
	switch request.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	_, err := browserCookie(
		request, SessionCookieName, DevelopmentSessionCookieName,
	)
	return err == nil
}

func browserCookie(request *http.Request, names ...string) (*http.Cookie, error) {
	var lastErr error
	for _, name := range names {
		cookie, err := request.Cookie(name)
		if err == nil {
			return cookie, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func sameOrigin(request *http.Request) bool {
	origin := strings.TrimSpace(request.Header.Get("Origin"))
	if origin == "" {
		return false
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.User != nil || parsed.Path != "" && parsed.Path != "/" ||
		parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	return strings.EqualFold(parsed.Host, request.Host) &&
		(parsed.Scheme == "https" || parsed.Scheme == "http")
}
