package identity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/secrets"
)

type entraSettingsRepositoryStub struct {
	settings  EntraSettings
	candidate EntraCandidate
	saved     EntraSettingsMutation
	promoted  EntraSettingsMutation
	disabled  EntraSettingsMutation
	err       error
}

func (r *entraSettingsRepositoryStub) GetEntraSettings(context.Context, string) (EntraSettings, error) {
	return r.settings, r.err
}
func (r *entraSettingsRepositoryStub) SaveEntraCandidate(_ context.Context, mutation EntraSettingsMutation) (EntraSettings, error) {
	r.saved = mutation
	return EntraSettings{
		State: "verification_required", Version: mutation.ExpectedVersion + 1,
		TenantID: mutation.TenantID, ClientID: mutation.ClientID,
		RedirectURL: mutation.RedirectURL, CredentialConfigured: true,
	}, r.err
}
func (r *entraSettingsRepositoryStub) GetEntraCandidate(context.Context, string, int64) (EntraCandidate, error) {
	return r.candidate, r.err
}
func (r *entraSettingsRepositoryStub) PromoteEntraCandidate(_ context.Context, mutation EntraSettingsMutation) (EntraSettings, error) {
	r.promoted = mutation
	return EntraSettings{
		State: "connected", Version: mutation.ExpectedVersion + 1,
		TenantID: mutation.TenantID, ClientID: mutation.ClientID,
		RedirectURL: mutation.RedirectURL, CredentialConfigured: true,
	}, r.err
}
func (r *entraSettingsRepositoryStub) DisableEntra(_ context.Context, mutation EntraSettingsMutation) (EntraSettings, error) {
	r.disabled = mutation
	return EntraSettings{State: "not_connected", Version: mutation.ExpectedVersion + 1}, r.err
}

type entraSecretProviderStub struct{ sealed secrets.SealedValue }

func (p *entraSecretProviderStub) Seal(_ context.Context, purpose string, plaintext []byte) (secrets.SealedValue, error) {
	if purpose != "installation.entra.client_secret" || string(plaintext) != "write-only-secret" {
		return secrets.SealedValue{}, errors.New("unexpected secret input")
	}
	return p.sealed, nil
}
func (*entraSecretProviderStub) Open(context.Context, string, secrets.SealedValue) ([]byte, error) {
	return nil, errors.New("not used")
}

type entraDiscoveryStub struct {
	document EntraDiscoveryDocument
	err      error
}

func (d entraDiscoveryStub) Discover(context.Context, string) (EntraDiscoveryDocument, error) {
	return d.document, d.err
}

func entraManager() authorization.Principal {
	return authorization.Principal{
		ID: "technician-1", Scope: scope.Principal{MSPID: "msp-1"},
		Capabilities: authorization.NewCapabilitySet("organization.manage"),
	}
}

func TestEntraSettingsConfigureSealsCandidateWithoutReturningSecret(t *testing.T) {
	repository := &entraSettingsRepositoryStub{}
	provider := &entraSecretProviderStub{sealed: secrets.SealedValue{
		Version: 1, Nonce: []byte("nonce"), Ciphertext: []byte("ciphertext"),
	}}
	service := NewEntraSettingsService(repository, provider, nil, time.Now, func() string { return "fact-id" })
	settings, err := service.Configure(context.Background(), ConfigureEntraCommand{
		Principal: entraManager(), ExpectedVersion: 3,
		TenantID: "tenant-id", ClientID: "client-id",
		ClientSecret: "write-only-secret",
		RedirectURL:  "https://rarity.example/auth/callback",
		Reason:       "connect workforce SSO", Source: "browser",
	})
	if err != nil {
		t.Fatal(err)
	}
	if settings.State != "verification_required" || settings.Version != 4 ||
		repository.saved.Secret.Ciphertext == nil ||
		repository.saved.Audit.Action != "identity.entra.configuration.staged" {
		t.Fatalf("settings=%+v mutation=%+v", settings, repository.saved)
	}
}

func TestEntraSettingsFailedVerificationDoesNotPromoteCandidate(t *testing.T) {
	repository := &entraSettingsRepositoryStub{candidate: EntraCandidate{
		Version: 4, TenantID: "tenant-id", ClientID: "client-id",
		RedirectURL: "https://rarity.example/auth/callback",
	}}
	service := NewEntraSettingsService(repository, nil, entraDiscoveryStub{err: errors.New("offline")}, time.Now, func() string { return "fact-id" })
	_, err := service.Verify(context.Background(), VerifyEntraCommand{
		Principal: entraManager(), ExpectedVersion: 4,
		Reason: "verify workforce SSO", Source: "browser",
	})
	if !errors.Is(err, ErrEntraVerificationFailed) || repository.promoted.ExpectedVersion != 0 {
		t.Fatalf("err=%v promoted=%+v", err, repository.promoted)
	}
}

func TestEntraSettingsVerificationRequiresExactTenantDiscovery(t *testing.T) {
	repository := &entraSettingsRepositoryStub{candidate: EntraCandidate{
		Version: 4, TenantID: "tenant-id", ClientID: "client-id",
		RedirectURL: "https://rarity.example/auth/callback",
		Secret:      secrets.SealedValue{Version: 1, Nonce: []byte("nonce"), Ciphertext: []byte("ciphertext")},
	}}
	discovery := entraDiscoveryStub{document: EntraDiscoveryDocument{
		Issuer:                "https://login.microsoftonline.com/other-tenant/v2.0",
		AuthorizationEndpoint: "https://login.microsoftonline.com/tenant-id/oauth2/v2.0/authorize",
		TokenEndpoint:         "https://login.microsoftonline.com/tenant-id/oauth2/v2.0/token",
		JWKSURI:               "https://login.microsoftonline.com/tenant-id/discovery/v2.0/keys",
	}}
	service := NewEntraSettingsService(repository, nil, discovery, time.Now, func() string { return "fact-id" })
	_, err := service.Verify(context.Background(), VerifyEntraCommand{
		Principal: entraManager(), ExpectedVersion: 4,
		Reason: "verify workforce SSO", Source: "browser",
	})
	if !errors.Is(err, ErrEntraVerificationFailed) || repository.promoted.ExpectedVersion != 0 {
		t.Fatalf("err=%v promoted=%+v", err, repository.promoted)
	}
}

func TestEntraSettingsVerificationPromotesExactCandidateAndDisableKeepsLocalAuthIndependent(t *testing.T) {
	repository := &entraSettingsRepositoryStub{candidate: EntraCandidate{
		Version: 4, TenantID: "tenant-id", ClientID: "client-id",
		RedirectURL: "https://rarity.example/auth/callback",
		Secret:      secrets.SealedValue{Version: 1, Nonce: []byte("nonce"), Ciphertext: []byte("ciphertext")},
	}}
	discovery := entraDiscoveryStub{document: EntraDiscoveryDocument{
		Issuer:                "https://login.microsoftonline.com/tenant-id/v2.0",
		AuthorizationEndpoint: "https://login.microsoftonline.com/tenant-id/oauth2/v2.0/authorize",
		TokenEndpoint:         "https://login.microsoftonline.com/tenant-id/oauth2/v2.0/token",
		JWKSURI:               "https://login.microsoftonline.com/tenant-id/discovery/v2.0/keys",
	}}
	service := NewEntraSettingsService(repository, nil, discovery, time.Now, func() string { return "fact-id" })
	connected, err := service.Verify(context.Background(), VerifyEntraCommand{
		Principal: entraManager(), ExpectedVersion: 4,
		Reason: "verify workforce SSO", Source: "browser",
	})
	if err != nil || connected.State != "connected" || repository.promoted.Secret.Version != 1 {
		t.Fatalf("connected=%+v err=%v promoted=%+v", connected, err, repository.promoted)
	}
	disabled, err := service.Disable(context.Background(), DisableEntraCommand{
		Principal: entraManager(), ExpectedVersion: connected.Version,
		Reason: "return to local-only identity", Source: "browser",
	})
	if err != nil || disabled.State != "not_connected" ||
		repository.disabled.Audit.Action != "identity.entra.configuration.disabled" {
		t.Fatalf("disabled=%+v err=%v mutation=%+v", disabled, err, repository.disabled)
	}
}
