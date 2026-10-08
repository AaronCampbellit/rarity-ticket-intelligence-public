package identity

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/secrets"
)

var (
	ErrInvalidEntraSettings    = errors.New("invalid Entra settings")
	ErrEntraVerificationFailed = errors.New("Entra verification failed")
	ErrEntraSettingsConflict   = errors.New("Entra settings version conflict")
)

var entraTenantPattern = regexp.MustCompile(`^[A-Za-z0-9.-]{1,253}$`)

type EntraSettings struct {
	State                string `json:"state"`
	Version              int64  `json:"version"`
	TenantID             string `json:"tenant_id,omitempty"`
	ClientID             string `json:"client_id,omitempty"`
	RedirectURL          string `json:"redirect_url,omitempty"`
	CredentialConfigured bool   `json:"credential_configured"`
}

type EntraCandidate struct {
	Version     int64
	TenantID    string
	ClientID    string
	RedirectURL string
	Secret      secrets.SealedValue
}

type EntraDiscoveryDocument struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	JWKSURI               string `json:"jwks_uri"`
}

type EntraDiscovery interface {
	Discover(context.Context, string) (EntraDiscoveryDocument, error)
}

type EntraSettingsMutation struct {
	MSPID           string
	ExpectedVersion int64
	TenantID        string
	ClientID        string
	RedirectURL     string
	Secret          secrets.SealedValue
	Audit           mutation.AuditRecord
	Event           mutation.EventRecord
}

type EntraSettingsRepository interface {
	GetEntraSettings(context.Context, string) (EntraSettings, error)
	SaveEntraCandidate(context.Context, EntraSettingsMutation) (EntraSettings, error)
	GetEntraCandidate(context.Context, string, int64) (EntraCandidate, error)
	PromoteEntraCandidate(context.Context, EntraSettingsMutation) (EntraSettings, error)
	DisableEntra(context.Context, EntraSettingsMutation) (EntraSettings, error)
}

type ConfigureEntraCommand struct {
	Principal       authorization.Principal
	ExpectedVersion int64
	TenantID        string
	ClientID        string
	ClientSecret    string
	RedirectURL     string
	Reason          string
	Source          string
}

type VerifyEntraCommand struct {
	Principal       authorization.Principal
	ExpectedVersion int64
	Reason          string
	Source          string
}

type DisableEntraCommand = VerifyEntraCommand

type EntraSettingsService struct {
	repository EntraSettingsRepository
	secrets    secrets.Provider
	discovery  EntraDiscovery
	now        func() time.Time
	newID      func() string
}

func NewEntraSettingsService(
	repository EntraSettingsRepository,
	secretProvider secrets.Provider,
	discovery EntraDiscovery,
	now func() time.Time,
	newID func() string,
) *EntraSettingsService {
	if now == nil {
		now = time.Now
	}
	return &EntraSettingsService{
		repository: repository, secrets: secretProvider, discovery: discovery,
		now: now, newID: newID,
	}
}

func (s *EntraSettingsService) Get(ctx context.Context, principal authorization.Principal) (EntraSettings, error) {
	if err := authorizeEntraSettings(principal); err != nil {
		return EntraSettings{}, err
	}
	if s.repository == nil {
		return EntraSettings{}, ErrInvalidEntraSettings
	}
	return s.repository.GetEntraSettings(ctx, principal.Scope.MSPID)
}

func (s *EntraSettingsService) Configure(ctx context.Context, command ConfigureEntraCommand) (EntraSettings, error) {
	if err := authorizeEntraSettings(command.Principal); err != nil {
		return EntraSettings{}, err
	}
	tenantID := strings.TrimSpace(command.TenantID)
	clientID := strings.TrimSpace(command.ClientID)
	redirectURL := strings.TrimSpace(command.RedirectURL)
	reason, source := strings.TrimSpace(command.Reason), strings.TrimSpace(command.Source)
	if command.ExpectedVersion < 1 || !validEntraTenant(tenantID) ||
		clientID == "" || len(clientID) > 255 ||
		len(command.ClientSecret) < 8 || len(command.ClientSecret) > 4096 ||
		!validEntraRedirect(redirectURL) || reason == "" || source == "" ||
		s.repository == nil || s.secrets == nil || s.newID == nil {
		return EntraSettings{}, ErrInvalidEntraSettings
	}
	sealed, err := s.secrets.Seal(ctx, "installation.entra.client_secret", []byte(command.ClientSecret))
	if err != nil {
		return EntraSettings{}, ErrInvalidEntraSettings
	}
	accepted := s.settingsMutation(
		command.Principal, command.ExpectedVersion,
		"identity.entra.configuration.staged", reason, source,
	)
	accepted.TenantID, accepted.ClientID = tenantID, clientID
	accepted.RedirectURL, accepted.Secret = redirectURL, sealed
	return s.repository.SaveEntraCandidate(ctx, accepted)
}

func (s *EntraSettingsService) Verify(ctx context.Context, command VerifyEntraCommand) (EntraSettings, error) {
	if err := authorizeEntraSettings(command.Principal); err != nil {
		return EntraSettings{}, err
	}
	reason, source := strings.TrimSpace(command.Reason), strings.TrimSpace(command.Source)
	if command.ExpectedVersion < 1 || reason == "" || source == "" ||
		s.repository == nil || s.discovery == nil || s.newID == nil {
		return EntraSettings{}, ErrInvalidEntraSettings
	}
	candidate, err := s.repository.GetEntraCandidate(ctx, command.Principal.Scope.MSPID, command.ExpectedVersion)
	if err != nil {
		return EntraSettings{}, err
	}
	discoveryURL := "https://login.microsoftonline.com/" + url.PathEscape(candidate.TenantID) +
		"/v2.0/.well-known/openid-configuration"
	document, err := s.discovery.Discover(ctx, discoveryURL)
	if err != nil || !validEntraDiscovery(candidate.TenantID, document) {
		return EntraSettings{}, ErrEntraVerificationFailed
	}
	accepted := s.settingsMutation(
		command.Principal, command.ExpectedVersion,
		"identity.entra.configuration.connected", reason, source,
	)
	accepted.TenantID, accepted.ClientID = candidate.TenantID, candidate.ClientID
	accepted.RedirectURL, accepted.Secret = candidate.RedirectURL, candidate.Secret
	return s.repository.PromoteEntraCandidate(ctx, accepted)
}

func (s *EntraSettingsService) Disable(ctx context.Context, command DisableEntraCommand) (EntraSettings, error) {
	if err := authorizeEntraSettings(command.Principal); err != nil {
		return EntraSettings{}, err
	}
	reason, source := strings.TrimSpace(command.Reason), strings.TrimSpace(command.Source)
	if command.ExpectedVersion < 1 || reason == "" || source == "" ||
		s.repository == nil || s.newID == nil {
		return EntraSettings{}, ErrInvalidEntraSettings
	}
	accepted := s.settingsMutation(
		command.Principal, command.ExpectedVersion,
		"identity.entra.configuration.disabled", reason, source,
	)
	return s.repository.DisableEntra(ctx, accepted)
}

func authorizeEntraSettings(principal authorization.Principal) error {
	return authorization.Authorize(principal, "organization.manage", scope.Target{MSPID: principal.Scope.MSPID})
}

func (s *EntraSettingsService) settingsMutation(
	principal authorization.Principal,
	expectedVersion int64,
	action, reason, source string,
) EntraSettingsMutation {
	now, correlationID := s.now().UTC(), s.newID()
	return EntraSettingsMutation{
		MSPID: principal.Scope.MSPID, ExpectedVersion: expectedVersion,
		Audit: mutation.AuditRecord{
			ID: s.newID(), OccurredAt: now, MSPID: principal.Scope.MSPID,
			ActorType: "technician", ActorID: principal.ID, Action: action,
			SubjectType: "entra_configuration", SubjectID: principal.Scope.MSPID,
			SubjectVersion: expectedVersion + 1, Source: source, Reason: reason,
			CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: s.newID(), EventType: action, SchemaVersion: 1,
			OccurredAt: now, MSPID: principal.Scope.MSPID,
			ActorType: "technician", ActorID: principal.ID,
			SubjectType: "entra_configuration", SubjectID: principal.Scope.MSPID,
			SubjectVersion: expectedVersion + 1, Source: source,
			CorrelationID: correlationID,
		},
	}
}

func validEntraTenant(value string) bool {
	return entraTenantPattern.MatchString(value) && value != "." && value != ".."
}

func validEntraRedirect(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	return parsed.Scheme == "https" ||
		(parsed.Scheme == "http" &&
			(parsed.Hostname() == "localhost" || parsed.Hostname() == "127.0.0.1"))
}

func validEntraDiscovery(tenantID string, document EntraDiscoveryDocument) bool {
	expectedIssuer := "https://login.microsoftonline.com/" + tenantID + "/v2.0"
	if document.Issuer != expectedIssuer {
		return false
	}
	for _, value := range []string{
		document.AuthorizationEndpoint, document.TokenEndpoint, document.JWKSURI,
	} {
		parsed, err := url.Parse(value)
		if err != nil || parsed.Scheme != "https" ||
			!strings.EqualFold(parsed.Hostname(), "login.microsoftonline.com") ||
			parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return false
		}
	}
	return true
}
