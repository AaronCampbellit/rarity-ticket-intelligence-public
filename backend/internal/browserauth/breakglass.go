package browserauth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sessions"
)

var ErrBreakGlassDenied = errors.New("break-glass authentication denied")

type BreakGlassConfig struct {
	MSPID      string
	SessionTTL time.Duration
	Secure     bool
}

type BreakGlassAuthenticator interface {
	AuthenticateBreakGlass(
		context.Context,
		string,
		string,
		string,
	) (technicianID string, err error)
}

type BrowserSessionRegistry interface {
	SessionManager
	Rotate(context.Context, string) (sessions.Issued, error)
	ListActive(context.Context, string) ([]sessions.Record, error)
	RevokeSession(context.Context, string, string) error
}

type BreakGlassHandler struct {
	config        BreakGlassConfig
	accounts      BreakGlassAuthenticator
	sessions      BrowserSessionRegistry
	random        func(int) ([]byte, error)
	next          http.Handler
	sessionCookie string
	csrfCookie    string
}

func NewBreakGlassHandler(
	config BreakGlassConfig,
	accounts BreakGlassAuthenticator,
	sessionManager BrowserSessionRegistry,
	random func(int) ([]byte, error),
	next http.Handler,
) (*BreakGlassHandler, error) {
	if config.MSPID == "" || config.SessionTTL <= 0 || accounts == nil ||
		sessionManager == nil || next == nil {
		return nil, errors.New("invalid break-glass authentication configuration")
	}
	if random == nil {
		random = breakGlassRandom
	}
	handler := &BreakGlassHandler{
		config: config, accounts: accounts, sessions: sessionManager,
		random: random, next: next,
		sessionCookie: sessions.DevelopmentSessionCookieName,
		csrfCookie:    sessions.DevelopmentCSRFCookieName,
	}
	if config.Secure {
		handler.sessionCookie = sessions.SessionCookieName
		handler.csrfCookie = sessions.CSRFCookieName
	}
	return handler, nil
}

func (h *BreakGlassHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.URL.Path == "/auth/session/refresh" {
		h.refreshSession(writer, request)
		return
	}
	if request.URL.Path == "/api/v1/sessions" ||
		strings.HasPrefix(request.URL.Path, "/api/v1/sessions/") {
		h.sessionAPI(writer, request)
		return
	}
	if request.URL.Path == "/auth/logout" {
		h.logout(writer, request)
		return
	}
	if request.URL.Path != "/auth/local/login" &&
		request.URL.Path != "/auth/break-glass/login" {
		h.next.ServeHTTP(writer, request)
		return
	}
	if request.URL.Path == "/auth/break-glass/login" {
		writer.Header().Set("Deprecation", "true")
		writer.Header().Set("Sunset", "Mon, 02 Nov 2026 00:00:00 GMT")
		writer.Header().Set("Link", `</auth/local/login>; rel="successor-version"`)
	}
	writer.Header().Set("Cache-Control", "no-store")
	if request.Method != http.MethodPost {
		writer.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, 16<<10)
	if err := request.ParseForm(); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(writer, "request too large", http.StatusRequestEntityTooLarge)
			return
		}
		h.deny(writer)
		return
	}
	technicianID, err := h.accounts.AuthenticateBreakGlass(
		request.Context(),
		strings.TrimSpace(request.Form.Get("username")),
		request.Form.Get("password"),
		remoteIP(request.RemoteAddr),
	)
	if err != nil {
		h.deny(writer)
		return
	}
	issued, err := h.sessions.Issue(request.Context(), sessions.IssueCommand{
		MSPID: h.config.MSPID, TechnicianID: technicianID,
		TTL: h.config.SessionTTL, IdleTimeout: 10 * time.Minute,
		UserAgent: request.UserAgent(), RemoteIP: remoteIP(request.RemoteAddr),
	})
	if err != nil {
		http.Error(writer, "authentication unavailable", http.StatusServiceUnavailable)
		return
	}
	csrfBytes, err := h.random(32)
	if err != nil {
		http.Error(writer, "authentication unavailable", http.StatusServiceUnavailable)
		return
	}
	maxAge := int(h.config.SessionTTL.Seconds())
	http.SetCookie(writer, &http.Cookie{
		Name: h.sessionCookie, Value: issued.Token, Path: "/",
		Secure: h.config.Secure, HttpOnly: true,
		SameSite: http.SameSiteLaxMode, MaxAge: maxAge,
	})
	http.SetCookie(writer, &http.Cookie{
		Name:  h.csrfCookie,
		Value: base64.RawURLEncoding.EncodeToString(csrfBytes),
		Path:  "/", Secure: h.config.Secure, HttpOnly: false,
		SameSite: http.SameSiteStrictMode, MaxAge: maxAge,
	})
	http.Redirect(writer, request, "/", http.StatusSeeOther)
}

func (h *BreakGlassHandler) refreshSession(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	if request.Method != http.MethodPost {
		writer.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	cookie, err := request.Cookie(h.sessionCookie)
	if err != nil {
		http.Error(writer, "authentication required", http.StatusUnauthorized)
		return
	}
	rotated, err := h.sessions.Rotate(request.Context(), cookie.Value)
	if err != nil {
		http.Error(writer, "authentication required", http.StatusUnauthorized)
		return
	}
	if rotated.Token != "" {
		http.SetCookie(writer, &http.Cookie{
			Name: h.sessionCookie, Value: rotated.Token, Path: "/",
			Secure: h.config.Secure, HttpOnly: true,
			SameSite: http.SameSiteLaxMode, Expires: rotated.Record.ExpiresAt,
		})
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (h *BreakGlassHandler) sessionAPI(
	writer http.ResponseWriter,
	request *http.Request,
) {
	cookie, err := request.Cookie(h.sessionCookie)
	if err != nil {
		http.Error(writer, "authentication required", http.StatusUnauthorized)
		return
	}
	writer.Header().Set("Cache-Control", "no-store")
	if request.URL.Path == "/api/v1/sessions" {
		if request.Method != http.MethodGet {
			writer.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		found, err := h.sessions.ListActive(request.Context(), cookie.Value)
		if err != nil {
			http.Error(writer, "authentication required", http.StatusUnauthorized)
			return
		}
		currentHash := sessions.HashToken(cookie.Value)
		type sessionView struct {
			ID                string    `json:"id"`
			CreatedAt         time.Time `json:"created_at"`
			LastSeenAt        time.Time `json:"last_seen_at"`
			ExpiresAt         time.Time `json:"expires_at"`
			NetworkPrefix     string    `json:"network_prefix,omitempty"`
			DeviceFingerprint string    `json:"device_fingerprint,omitempty"`
			Current           bool      `json:"current"`
		}
		response := make([]sessionView, 0, len(found))
		for _, record := range found {
			fingerprint := ""
			if record.UserAgentHash != ([32]byte{}) {
				fingerprint = hex.EncodeToString(record.UserAgentHash[:6])
			}
			response = append(response, sessionView{
				ID: record.ID, CreatedAt: record.CreatedAt,
				LastSeenAt: record.LastSeenAt, ExpiresAt: record.ExpiresAt,
				NetworkPrefix:     record.IPPrefix,
				DeviceFingerprint: fingerprint,
				Current:           record.TokenHash == currentHash,
			})
		}
		writer.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(writer).Encode(response); err != nil {
			return
		}
		return
	}
	if request.Method != http.MethodDelete {
		writer.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	sessionID := strings.TrimPrefix(request.URL.Path, "/api/v1/sessions/")
	if sessionID == "" ||
		h.sessions.RevokeSession(request.Context(), cookie.Value, sessionID) != nil {
		http.Error(writer, "session not found", http.StatusNotFound)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (h *BreakGlassHandler) logout(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writer.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if cookie, err := request.Cookie(h.sessionCookie); err == nil {
		if authenticated, authErr := h.sessions.Authenticate(
			request.Context(), cookie.Value,
		); authErr == nil {
			_ = h.sessions.Revoke(
				request.Context(), cookie.Value, authenticated.TechnicianID,
			)
		}
	}
	for _, name := range []string{h.sessionCookie, h.csrfCookie} {
		http.SetCookie(writer, &http.Cookie{
			Name: name, Value: "", Path: "/", MaxAge: -1,
			Secure: h.config.Secure, HttpOnly: name == h.sessionCookie,
			SameSite: http.SameSiteStrictMode,
		})
	}
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(http.StatusNoContent)
}

func (h *BreakGlassHandler) deny(writer http.ResponseWriter) {
	http.Error(writer, "authentication failed", http.StatusUnauthorized)
}

func remoteIP(address string) string {
	host, _, err := net.SplitHostPort(address)
	if err == nil {
		return host
	}
	return strings.TrimSpace(address)
}

func breakGlassRandom(size int) ([]byte, error) {
	value := make([]byte, size)
	_, err := rand.Read(value)
	return value, err
}
