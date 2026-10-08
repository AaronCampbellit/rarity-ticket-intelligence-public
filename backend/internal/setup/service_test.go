package setup

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/secrets"
	"golang.org/x/crypto/bcrypt"
)

func TestGlobalAdministratorCanCreateEveryClientResource(t *testing.T) {
	for _, capability := range []string{
		"asset.create",
		"comment.internal.create",
		"comment.public.create",
		"contact.create",
		"contract.create",
		"location.create",
		"service.create",
	} {
		if !slices.Contains(globalAdminCapabilities, capability) {
			t.Errorf("global administrator missing %q", capability)
		}
	}
}

func TestFreshBootstrapGlobalAdministratorCanManageEveryClientResourceLifecycle(t *testing.T) {
	for _, kind := range []string{"location", "contact", "asset", "service", "contract"} {
		for _, action := range []string{"update", "lifecycle"} {
			capability := kind + "." + action
			if !slices.Contains(globalAdminCapabilities, capability) {
				t.Errorf("fresh bootstrap global administrator missing %q", capability)
			}
		}
	}
}

func TestGlobalAdministratorCanReviewDattoReconciliation(t *testing.T) {
	if !slices.Contains(
		globalAdminCapabilities,
		"integration.datto.reconcile",
	) {
		t.Error("global administrator missing Datto reconciliation access")
	}
}

func TestGlobalAdministratorCanGovernClassification(t *testing.T) {
	for _, capability := range []string{"classification.manage", "classification.apply", "classification.report", "classification.ai.manage"} {
		if !slices.Contains(globalAdminCapabilities, capability) {
			t.Errorf("global administrator missing %q", capability)
		}
	}
}

func TestGlobalAdministratorCanUseInternalMentions(t *testing.T) {
	for _, capability := range []string{"mention.create", "mention.read"} {
		if !slices.Contains(globalAdminCapabilities, capability) {
			t.Errorf("global administrator missing %q", capability)
		}
	}
}

func TestGlobalAdministratorCanUseUnifiedCalendar(t *testing.T) {
	for _, capability := range []string{
		"calendar.read",
		"calendar.schedule",
		"calendar.workforce.manage",
		"calendar.commitment.manage",
		"calendar.policy.manage",
		"calendar.ai.recommend",
	} {
		if !slices.Contains(globalAdminCapabilities, capability) {
			t.Errorf("global administrator missing %q", capability)
		}
	}
}

func TestGlobalAdministratorCanManageWorkforceTimeReview(t *testing.T) {
	principal := authorization.Principal{
		ID:           "global-admin",
		Scope:        scope.Principal{MSPID: "msp"},
		Capabilities: authorization.NewCapabilitySet(globalAdminCapabilities...),
	}
	target := scope.Target{MSPID: "msp"}
	for _, capability := range []string{
		"timesheet.read_own",
		"timesheet.review",
		"time_entry.read_scoped",
		"time_entry.update_own",
		"time_entry.amend",
	} {
		if err := authorization.Authorize(principal, capability, target); err != nil {
			t.Errorf("global administrator cannot %q: %v", capability, err)
		}
	}
}

type fakeRepository struct {
	issued       [32]byte
	issuedAt     time.Time
	expiresAt    time.Time
	mutation     BootstrapMutation
	center       CenterConfigurationMutation
	status       CenterStatus
	intake       IntakeStatus
	verification VerificationMutation
}

func (r *fakeRepository) RecordVerification(
	_ context.Context, value VerificationMutation,
) error {
	r.verification = value
	r.status.ConfigurationVersion = value.ExpectedVersion + 1
	result := &VerificationResult{
		Section: value.Section, State: value.State, SafeCode: value.SafeCode,
		CheckedAt: value.CheckedAt, ValidUntil: value.ValidUntil,
		Details: value.Details, Version: value.ExpectedVersion + 1,
	}
	if value.Section == "object_storage" {
		r.status.ObjectStorageStatus.Verification = result
	} else if value.Section == "backups" {
		r.status.BackupStatus.Verification = result
	}
	return nil
}

func (r *fakeRepository) IssueToken(_ context.Context, hash [32]byte, issuedAt, expiresAt time.Time) error {
	r.issued, r.issuedAt, r.expiresAt = hash, issuedAt, expiresAt
	return nil
}
func (r *fakeRepository) Bootstrap(_ context.Context, mutation BootstrapMutation) error {
	r.mutation = mutation
	return nil
}
func (*fakeRepository) Status(context.Context) (bool, bool, error) {
	return false, false, nil
}
func (r *fakeRepository) CenterStatus(context.Context, string) (CenterStatus, error) {
	return r.status, nil
}
func (r *fakeRepository) IntakeStatus(context.Context, string) (IntakeStatus, error) {
	return r.intake, nil
}

func TestCenterStatusUsesAuthoritativeIntakeAndRuntimeConfiguration(t *testing.T) {
	repository := &fakeRepository{
		status: CenterStatus{ConfigurationVersion: 3},
		intake: IntakeStatus{GraphConfigured: true},
	}
	service := NewService(
		repository, time.Now, nil,
		WithSetupRuntimeConfiguration(SetupRuntimeConfiguration{
			PublicURL:  "https://rarity.example",
			S3Endpoint: "https://minio.example",
			S3Bucket:   "rarity-attachments", S3Region: "us-east-1",
			S3AccessKeyConfigured: true, S3CredentialConfigured: true,
		}),
	)
	status, err := service.CenterStatus(context.Background(), authorization.Principal{
		ID: "admin", Scope: scope.Principal{MSPID: "msp"},
		Capabilities: authorization.NewCapabilitySet("organization.read"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !status.IntakeStatus.GraphConfigured ||
		status.ObjectStorageStatus.Bucket != "rarity-attachments" ||
		status.ObjectStorageStatus.CredentialConfigured != true {
		t.Fatalf("unexpected status: %+v", status)
	}
	if status.Sections[1].State != ReadinessVerified ||
		status.Sections[2].State != ReadinessReadyToVerify ||
		status.Sections[3].State != ReadinessActionRequired {
		t.Fatalf("unexpected readiness sections: %+v", status.Sections)
	}
}
func (r *fakeRepository) UpdateCenterConfiguration(
	_ context.Context,
	mutation CenterConfigurationMutation,
) (CenterStatus, error) {
	r.center = mutation
	return CenterStatus{ConfigurationVersion: mutation.ExpectedVersion + 1}, nil
}

func TestIssueTokenStoresOnlyHashWithFifteenMinuteExpiry(t *testing.T) {
	now := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	repository := &fakeRepository{}
	token, expiresAt, err := NewService(repository, func() time.Time { return now }, nil).
		IssueToken(context.Background())
	if err != nil || token == "" {
		t.Fatalf("IssueToken() token=%q err=%v", token, err)
	}
	if repository.issuedAt != now || repository.expiresAt != now.Add(15*time.Minute) ||
		expiresAt != repository.expiresAt || strings.Contains(string(repository.issued[:]), token) {
		t.Fatalf("unexpected token evidence: %+v", repository)
	}
}

func TestBootstrapHashesRecoveryPasswordAndClearsPlaintext(t *testing.T) {
	now := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	repository := &fakeRepository{}
	next := 0
	provider, _ := secrets.NewLocalProvider(bytes.Repeat([]byte{1}, 32), nil)
	service := NewService(repository, func() time.Time { return now }, func() string {
		next++
		return "00000000-0000-4000-8000-0000000000" + string(rune('0'+next))
	}, WithExpectedMSPID("00000000-0000-4000-8000-000000000001"),
		WithSecretProvider(provider))
	value := validConfiguration()
	if err := service.Bootstrap(context.Background(), "single-use-token", value); err != nil {
		t.Fatalf("Bootstrap() error=%v", err)
	}
	if repository.mutation.PasswordHash == "" ||
		repository.mutation.PasswordHash == value.RecoveryPassword ||
		repository.mutation.Configuration.RecoveryPassword != "" ||
		repository.mutation.Configuration.EntraClientSecret != "" ||
		len(repository.mutation.EntraSecret.Ciphertext) == 0 {
		t.Fatalf("credentials were not protected: %+v", repository.mutation)
	}
	cost, err := bcrypt.Cost([]byte(repository.mutation.PasswordHash))
	if err != nil || cost < 12 {
		t.Fatalf("bootstrap password bcrypt cost=%d err=%v", cost, err)
	}
}

func TestBootstrapAcceptsNoEntraConfiguration(t *testing.T) {
	repository := &fakeRepository{}
	provider, _ := secrets.NewLocalProvider(bytes.Repeat([]byte{1}, 32), nil)
	value := validConfiguration()
	value.EntraTenantID = ""
	value.EntraClientID = ""
	value.EntraClientSecret = ""
	value.EntraRedirectURL = ""
	value.AdminEntraSubject = ""
	err := NewService(
		repository, time.Now, func() string { return "00000000-0000-4000-8000-000000000009" },
		WithExpectedMSPID(value.OrganizationID), WithSecretProvider(provider),
	).Bootstrap(context.Background(), "token", value)
	if err != nil {
		t.Fatalf("Bootstrap() error=%v", err)
	}
	if repository.mutation.EntraSecret.Version != 0 ||
		len(repository.mutation.EntraSecret.Nonce) != 0 ||
		len(repository.mutation.EntraSecret.Ciphertext) != 0 {
		t.Fatalf("unexpected Entra secret: %+v", repository.mutation.EntraSecret)
	}
}

func TestBootstrapRejectsPartialEntraConfiguration(t *testing.T) {
	value := validConfiguration()
	value.EntraClientSecret = ""
	provider, _ := secrets.NewLocalProvider(bytes.Repeat([]byte{1}, 32), nil)
	err := NewService(
		&fakeRepository{}, time.Now, func() string { return "id" },
		WithExpectedMSPID(value.OrganizationID), WithSecretProvider(provider),
	).Bootstrap(context.Background(), "token", value)
	if err != ErrInvalidBootstrap {
		t.Fatalf("Bootstrap() error=%v", err)
	}
}

func TestBootstrapRejectsInsecureRedirectAndInvalidRecoveryNetwork(t *testing.T) {
	for _, mutate := range []func(*Configuration){
		func(value *Configuration) { value.EntraRedirectURL = "http://rarity.example/callback" },
		func(value *Configuration) { value.RecoveryAllowedCIDR = []string{"not-a-network"} },
	} {
		value := validConfiguration()
		mutate(&value)
		provider, _ := secrets.NewLocalProvider(bytes.Repeat([]byte{1}, 32), nil)
		if err := NewService(
			&fakeRepository{}, time.Now, func() string { return "id" },
			WithExpectedMSPID(value.OrganizationID), WithSecretProvider(provider),
		).
			Bootstrap(context.Background(), "token", value); err != ErrInvalidBootstrap {
			t.Fatalf("Bootstrap() error=%v", err)
		}
	}
}

func TestBootstrapRejectsMSPIDOutsideDeploymentAuthority(t *testing.T) {
	value := validConfiguration()
	provider, _ := secrets.NewLocalProvider(bytes.Repeat([]byte{1}, 32), nil)
	err := NewService(
		&fakeRepository{}, time.Now, func() string { return "id" },
		WithExpectedMSPID("00000000-0000-4000-8000-000000000099"),
		WithSecretProvider(provider),
	).Bootstrap(context.Background(), "token", value)
	if err != ErrInvalidBootstrap {
		t.Fatalf("Bootstrap() error=%v", err)
	}
}

func TestCenterStatusRequiresOrganizationRead(t *testing.T) {
	_, err := NewService(&fakeRepository{}, time.Now, nil).CenterStatus(
		context.Background(),
		authorization.Principal{
			ID:    "technician",
			Scope: scope.Principal{MSPID: "00000000-0000-4000-8000-000000000001"},
		},
	)
	if err == nil {
		t.Fatal("expected authorization failure")
	}
}

func TestCenterConfigurationUpdateRequiresManageAndBuildsAuditedMutation(t *testing.T) {
	at := time.Date(2026, 8, 4, 19, 0, 0, 0, time.UTC)
	repository := &fakeRepository{}
	service := NewService(
		repository,
		func() time.Time { return at },
		func() string { return "mutation-id" },
	)
	principal := authorization.Principal{
		ID:           "admin-id",
		Scope:        scope.Principal{MSPID: "msp-id"},
		Capabilities: authorization.NewCapabilitySet("organization.manage"),
	}
	updated, err := service.UpdateCenterConfiguration(
		context.Background(),
		UpdateCenterConfigurationCommand{
			Principal:       principal,
			ExpectedVersion: 3,
			Intake: map[string]any{
				"graph_mailbox": "configured",
			},
			ObjectStorage: map[string]any{
				"bucket_ref": "env://RARITY_S3_BUCKET",
			},
			Backups: map[string]any{
				"repository_ref": "env://PGBACKREST_REPO",
			},
			Reason: "Record deployment references",
			Source: "browser",
		},
	)
	if err != nil || updated.ConfigurationVersion != 4 {
		t.Fatalf("updated=%+v err=%v", updated, err)
	}
	if repository.center.ExpectedVersion != 3 ||
		repository.center.Audit.Action != "installation.setup.configuration.updated" ||
		repository.center.Audit.ActorID != "admin-id" ||
		repository.center.Audit.Reason != "Record deployment references" ||
		repository.center.Event.EventType != "installation.setup.configuration.updated" {
		t.Fatalf("mutation=%+v", repository.center)
	}

	principal.Capabilities = authorization.NewCapabilitySet("organization.read")
	_, err = service.UpdateCenterConfiguration(
		context.Background(),
		UpdateCenterConfigurationCommand{
			Principal: principal, ExpectedVersion: 4,
			Intake: map[string]any{}, ObjectStorage: map[string]any{},
			Backups: map[string]any{}, Reason: "Unauthorized", Source: "browser",
		},
	)
	if err == nil {
		t.Fatal("organization.read unexpectedly updated setup configuration")
	}
}

func TestCenterConfigurationRejectsInlineSecrets(t *testing.T) {
	service := NewService(
		&fakeRepository{},
		time.Now,
		func() string { return "mutation-id" },
	)
	_, err := service.UpdateCenterConfiguration(
		context.Background(),
		UpdateCenterConfigurationCommand{
			Principal: authorization.Principal{
				ID:    "admin-id",
				Scope: scope.Principal{MSPID: "msp-id"},
				Capabilities: authorization.NewCapabilitySet(
					"organization.manage",
				),
			},
			ExpectedVersion: 1,
			Intake:          map[string]any{},
			ObjectStorage: map[string]any{
				"access_key": "plaintext-credential",
			},
			Backups: map[string]any{},
			Reason:  "Unsafe configuration",
			Source:  "browser",
		},
	)
	if !errors.Is(err, ErrInvalidCenterConfiguration) {
		t.Fatalf("UpdateCenterConfiguration() error=%v", err)
	}
}

func TestIdentityCenterSectionTreatsAbsentEntraAsOptional(t *testing.T) {
	section := identitySection("not_connected")
	if section.State != "configured" ||
		!strings.Contains(section.Summary, "optional") ||
		section.RemediationHref != "#/recovery-access" {
		t.Fatalf("unexpected identity section: %+v", section)
	}
}

func validConfiguration() Configuration {
	return Configuration{
		OrganizationID:   "00000000-0000-4000-8000-000000000001",
		OrganizationName: "Rarity MSP", OrganizationDisplay: "RARITY",
		EntraTenantID: "tenant", EntraClientID: "client",
		EntraClientSecret: "synthetic-client-secret",
		EntraRedirectURL:  "https://rarity.example/auth/entra/callback",
		AdminEmail:        "admin@example.com", AdminDisplayName: "Admin",
		AdminEntraSubject: "entra-subject", RecoveryUsername: "recovery",
		RecoveryPassword:    "correct horse battery staple",
		RecoveryAllowedCIDR: []string{"10.0.0.0/8"},
		Intake:              map[string]any{}, ObjectStorage: map[string]any{}, Backups: map[string]any{},
	}
}
