package browserauth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/identity"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sessions"
)

const oidcCookieName = "__Host-rarity_oidc"

type Config struct {
	MSPID        string
	TenantID     string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	SessionKey   string
	SessionTTL   time.Duration
}

type IdentityAuthenticator interface {
	Authenticate(context.Context, string, ...string) (identity.Technician, error)
}

type SessionManager interface {
	Issue(context.Context, sessions.IssueCommand) (sessions.Issued, error)
	Authenticate(context.Context, string) (sessions.Authenticated, error)
	Revoke(context.Context, string, string) error
}

type Handler struct {
	config            Config
	configurationTag  string
	identity          IdentityAuthenticator
	sessions          SessionManager
	client            *http.Client
	now               func() time.Time
	random            func(int) ([]byte, error)
	next              http.Handler
	authorizeURL      string
	tokenURL          string
	secure            bool
	sessionCookieName string
	csrfCookieName    string
	oidcCookieName    string
}

func NewEntraHandler(
	config Config,
	authenticator IdentityAuthenticator,
	sessionIssuer SessionManager,
	client *http.Client,
	now func() time.Time,
	random func(int) ([]byte, error),
	next http.Handler,
) (*Handler, error) {
	redirect, err := url.Parse(config.RedirectURL)
	if err != nil || !redirect.IsAbs() || redirect.Host == "" ||
		config.MSPID == "" || config.TenantID == "" || config.ClientID == "" ||
		config.ClientSecret == "" || len(config.SessionKey) < 32 ||
		config.SessionTTL <= 0 || authenticator == nil || sessionIssuer == nil ||
		next == nil {
		return nil, errors.New("invalid Entra browser authentication configuration")
	}
	if client == nil {
		client = safeHTTPClient()
	}
	if now == nil {
		now = time.Now
	}
	if random == nil {
		random = randomBytes
	}
	tenant := url.PathEscape(config.TenantID)
	handler := &Handler{
		config: config, identity: authenticator, sessions: sessionIssuer,
		client: client, now: now, random: random, next: next,
		configurationTag:  configurationTag(config),
		authorizeURL:      "https://login.microsoftonline.com/" + tenant + "/oauth2/v2.0/authorize",
		tokenURL:          "https://login.microsoftonline.com/" + tenant + "/oauth2/v2.0/token",
		secure:            redirect.Scheme == "https",
		sessionCookieName: sessions.DevelopmentSessionCookieName,
		csrfCookieName:    sessions.DevelopmentCSRFCookieName,
		oidcCookieName:    "rarity_oidc",
	}
	if handler.secure {
		handler.sessionCookieName = sessions.SessionCookieName
		handler.csrfCookieName = sessions.CSRFCookieName
		handler.oidcCookieName = oidcCookieName
	}
	return handler, nil
}

func (h *Handler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	switch request.URL.Path {
	case "/auth/entra/login":
		h.login(writer, request)
	case "/auth/entra/callback":
		h.callback(writer, request)
	case "/auth/logout":
		h.logout(writer, request)
	default:
		h.next.ServeHTTP(writer, request)
	}
}

func (h *Handler) logout(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writer.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	cookie, err := request.Cookie(h.sessionCookieName)
	if err == nil {
		if authenticated, authErr := h.sessions.Authenticate(
			request.Context(), cookie.Value,
		); authErr == nil {
			_ = h.sessions.Revoke(
				request.Context(), cookie.Value, authenticated.TechnicianID,
			)
		}
	}
	for _, name := range []string{h.sessionCookieName, h.csrfCookieName} {
		http.SetCookie(writer, &http.Cookie{
			Name: name, Value: "", Path: "/", MaxAge: -1,
			Secure: h.secure, HttpOnly: name == h.sessionCookieName,
			SameSite: http.SameSiteStrictMode,
		})
	}
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(http.StatusNoContent)
}

type loginState struct {
	State            string `json:"state"`
	Nonce            string `json:"nonce"`
	CodeVerifier     string `json:"code_verifier"`
	ConfigurationTag string `json:"configuration_tag"`
	ExpiresAt        int64  `json:"expires_at"`
}

func (h *Handler) login(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writer.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	state, err := h.randomValue(32)
	if err != nil {
		http.Error(writer, "authentication unavailable", http.StatusServiceUnavailable)
		return
	}
	nonce, err := h.randomValue(32)
	if err != nil {
		http.Error(writer, "authentication unavailable", http.StatusServiceUnavailable)
		return
	}
	verifier, err := h.randomValue(48)
	if err != nil {
		http.Error(writer, "authentication unavailable", http.StatusServiceUnavailable)
		return
	}
	transient := loginState{
		State: state, Nonce: nonce, CodeVerifier: verifier,
		ConfigurationTag: h.configurationTag,
		ExpiresAt:        h.now().Add(10 * time.Minute).Unix(),
	}
	value, err := h.signState(transient)
	if err != nil {
		http.Error(writer, "authentication unavailable", http.StatusServiceUnavailable)
		return
	}
	http.SetCookie(writer, &http.Cookie{
		Name: h.oidcCookieName, Value: value, Path: "/", MaxAge: 600,
		Secure: h.secure, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
	challenge := sha256.Sum256([]byte(verifier))
	query := url.Values{
		"client_id":             {h.config.ClientID},
		"response_type":         {"code"},
		"redirect_uri":          {h.config.RedirectURL},
		"response_mode":         {"query"},
		"scope":                 {"openid profile email"},
		"state":                 {state},
		"nonce":                 {nonce},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(challenge[:])},
		"code_challenge_method": {"S256"},
	}
	writer.Header().Set("Cache-Control", "no-store")
	http.Redirect(writer, request, h.authorizeURL+"?"+query.Encode(), http.StatusFound)
}

func (h *Handler) callback(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writer.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	writer.Header().Set("Cache-Control", "no-store")
	cookie, err := request.Cookie(h.oidcCookieName)
	if err != nil || request.URL.Query().Get("error") != "" {
		h.redirectCallbackFailure(writer, request, "entra")
		return
	}
	state, err := h.verifyState(cookie.Value)
	code := request.URL.Query().Get("code")
	if err != nil || code == "" ||
		!hmac.Equal(
			[]byte(state.ConfigurationTag),
			[]byte(h.configurationTag),
		) ||
		!hmac.Equal([]byte(state.State), []byte(request.URL.Query().Get("state"))) ||
		state.ExpiresAt <= h.now().Unix() {
		h.redirectCallbackFailure(writer, request, "entra")
		return
	}
	idToken, err := h.exchange(request.Context(), code, state.CodeVerifier)
	if err != nil {
		h.redirectCallbackFailure(writer, request, "entra")
		return
	}
	technician, err := h.identity.Authenticate(request.Context(), idToken, state.Nonce)
	if err != nil {
		h.redirectCallbackFailure(writer, request, "entra")
		return
	}
	issued, err := h.sessions.Issue(request.Context(), sessions.IssueCommand{
		MSPID: h.config.MSPID, TechnicianID: technician.ID,
		TTL: h.config.SessionTTL, IdleTimeout: 30 * time.Minute,
		UserAgent: request.UserAgent(), RemoteIP: remoteIP(request.RemoteAddr),
	})
	if err != nil {
		h.redirectCallbackFailure(writer, request, "unavailable")
		return
	}
	csrf, err := h.randomValue(32)
	if err != nil {
		h.redirectCallbackFailure(writer, request, "unavailable")
		return
	}
	http.SetCookie(writer, &http.Cookie{
		Name: h.sessionCookieName, Value: issued.Token, Path: "/",
		Secure: h.secure, HttpOnly: true, SameSite: http.SameSiteLaxMode,
		MaxAge: int(h.config.SessionTTL.Seconds()),
	})
	http.SetCookie(writer, &http.Cookie{
		Name: h.csrfCookieName, Value: csrf, Path: "/",
		Secure: h.secure, HttpOnly: false, SameSite: http.SameSiteStrictMode,
		MaxAge: int(h.config.SessionTTL.Seconds()),
	})
	http.SetCookie(writer, &http.Cookie{
		Name: h.oidcCookieName, Value: "", Path: "/", MaxAge: -1,
		Secure: h.secure, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(writer, request, "/", http.StatusFound)
}

func configurationTag(config Config) string {
	mac := hmac.New(sha256.New, []byte(config.SessionKey))
	for _, value := range []string{
		config.MSPID,
		config.TenantID,
		config.ClientID,
		config.ClientSecret,
		config.RedirectURL,
	} {
		_, _ = mac.Write([]byte(value))
		_, _ = mac.Write([]byte{0})
	}
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (h *Handler) redirectCallbackFailure(
	writer http.ResponseWriter,
	request *http.Request,
	code string,
) {
	http.SetCookie(writer, &http.Cookie{
		Name: h.oidcCookieName, Value: "", Path: "/", MaxAge: -1,
		Secure: h.secure, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(writer, request, "/#/login?error="+code, http.StatusFound)
}

func (h *Handler) exchange(ctx context.Context, code, verifier string) (string, error) {
	form := url.Values{
		"client_id":     {h.config.ClientID},
		"client_secret": {h.config.ClientSecret},
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {h.config.RedirectURL},
		"code_verifier": {verifier},
	}
	request, err := http.NewRequestWithContext(
		ctx, http.MethodPost, h.tokenURL, strings.NewReader(form.Encode()),
	)
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := h.client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", errors.New("token exchange failed")
	}
	var result struct {
		IDToken string `json:"id_token"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, (1<<20)+1)).Decode(&result); err != nil ||
		result.IDToken == "" {
		return "", errors.New("invalid token response")
	}
	return result.IDToken, nil
}

func (h *Handler) signState(state loginState) (string, error) {
	data, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(data)
	mac := hmac.New(sha256.New, []byte(h.config.SessionKey))
	_, _ = mac.Write([]byte(encoded))
	return encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (h *Handler) verifyState(value string) (loginState, error) {
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return loginState{}, errors.New("invalid state")
	}
	mac := hmac.New(sha256.New, []byte(h.config.SessionKey))
	_, _ = mac.Write([]byte(parts[0]))
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(signature, mac.Sum(nil)) {
		return loginState{}, errors.New("invalid state")
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return loginState{}, err
	}
	var state loginState
	if json.Unmarshal(data, &state) != nil || state.State == "" ||
		state.Nonce == "" || state.CodeVerifier == "" {
		return loginState{}, errors.New("invalid state")
	}
	return state, nil
}

func (h *Handler) randomValue(size int) (string, error) {
	value, err := h.random(size)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func randomBytes(size int) ([]byte, error) {
	value := make([]byte, size)
	_, err := rand.Read(value)
	return value, err
}

func safeHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}
