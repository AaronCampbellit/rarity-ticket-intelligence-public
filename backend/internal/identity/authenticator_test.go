package identity

import (
	"context"
	"errors"
	"testing"
)

type fakeVerifier struct {
	claims Claims
	err    error
}

func (v fakeVerifier) Verify(context.Context, string, string) (Claims, error) {
	return v.claims, v.err
}

type fakeIdentityRepository struct {
	found       Technician
	findErr     error
	provisioned ProvisionCommand
	calls       int
}

func (r *fakeIdentityRepository) FindByExternalIdentity(
	context.Context, string, string, string,
) (Technician, error) {
	return r.found, r.findErr
}

func (r *fakeIdentityRepository) ProvisionAtomic(_ context.Context, command ProvisionCommand) (Technician, error) {
	r.calls++
	r.provisioned = command
	return Technician{ID: command.TechnicianID, MSPID: command.MSPID, Email: command.Email}, nil
}

func TestAuthenticateRejectsWrongTenantIssuerOrAudienceBeforeLookup(t *testing.T) {
	base := Claims{
		Issuer: "https://login.example/tenant/v2.0", TenantID: "tenant-id",
		Audience: "rarity-client", Subject: "subject-id", Email: "tech@example.com",
	}
	tests := []struct {
		name   string
		mutate func(*Claims)
	}{
		{name: "tenant", mutate: func(c *Claims) { c.TenantID = "other" }},
		{name: "issuer", mutate: func(c *Claims) { c.Issuer = "https://evil.example" }},
		{name: "audience", mutate: func(c *Claims) { c.Audience = "other-client" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims := base
			tt.mutate(&claims)
			repository := &fakeIdentityRepository{}
			authenticator := NewAuthenticator(testIdentityConfig(), fakeVerifier{claims: claims}, repository, fixedIDs())
			if _, err := authenticator.Authenticate(context.Background(), "token"); !errors.Is(err, ErrUntrustedIdentity) {
				t.Fatalf("Authenticate() error = %v, want ErrUntrustedIdentity", err)
			}
			if repository.calls != 0 {
				t.Fatal("untrusted identity reached provisioning")
			}
		})
	}
}

func TestAuthenticateJITProvisionsFirstTrustedIdentity(t *testing.T) {
	repository := &fakeIdentityRepository{findErr: ErrIdentityNotFound}
	authenticator := NewAuthenticator(testIdentityConfig(), fakeVerifier{claims: Claims{
		Issuer: "https://login.example/tenant/v2.0", TenantID: "tenant-id",
		Audience: "rarity-client", Subject: "subject-id",
		Email: "tech@example.com", DisplayName: "Taylor Tech",
	}}, repository, fixedIDs())

	technician, err := authenticator.Authenticate(context.Background(), "token")
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if technician.ID != "technician-id" || repository.calls != 1 {
		t.Fatalf("unexpected provision result: technician=%+v calls=%d", technician, repository.calls)
	}
	if repository.provisioned.MSPID != "msp-id" ||
		repository.provisioned.ExternalIdentityID != "identity-id" ||
		repository.provisioned.Subject != "subject-id" {
		t.Fatalf("unexpected trusted provisioning command: %+v", repository.provisioned)
	}
}

func TestAuthenticateReturnsExistingLinkedTechnicianWithoutProvisioning(t *testing.T) {
	repository := &fakeIdentityRepository{found: Technician{
		ID: "existing-id", MSPID: "msp-id", Email: "existing@example.com",
	}}
	authenticator := NewAuthenticator(testIdentityConfig(), fakeVerifier{claims: Claims{
		Issuer: "https://login.example/tenant/v2.0", TenantID: "tenant-id",
		Audience: "rarity-client", Subject: "subject-id", Email: "changed@example.com",
	}}, repository, fixedIDs())

	technician, err := authenticator.Authenticate(context.Background(), "token")
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if technician.ID != "existing-id" || repository.calls != 0 {
		t.Fatalf("existing identity was not reused: technician=%+v calls=%d", technician, repository.calls)
	}
}

func testIdentityConfig() Config {
	return Config{
		MSPID: "msp-id", TenantID: "tenant-id",
		Issuer: "https://login.example/tenant/v2.0", Audience: "rarity-client",
		DefaultRoleKey: "service_desk_technician", JITEnabled: true,
	}
}

func fixedIDs() func() string {
	ids := []string{
		"technician-id", "identity-id", "assignment-id",
		"audit-id", "event-id", "correlation-id",
	}
	return func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	}
}
