package identity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"golang.org/x/crypto/bcrypt"
)

type breakGlassManagementRepositoryStub struct {
	created  BreakGlassAccountMutation
	disabled BreakGlassAccountMutation
	reset    BreakGlassAccountMutation
	networks BreakGlassAccountMutation
	list     []ManagedBreakGlassAccount
	err      error
}

func (r *breakGlassManagementRepositoryStub) CreateBreakGlassAccountAtomic(_ context.Context, value BreakGlassAccountMutation) error {
	r.created = value
	return r.err
}
func (r *breakGlassManagementRepositoryStub) ListBreakGlassAccounts(context.Context, string) ([]ManagedBreakGlassAccount, error) {
	return r.list, r.err
}
func (r *breakGlassManagementRepositoryStub) DisableBreakGlassAccountAtomic(_ context.Context, value BreakGlassAccountMutation) error {
	r.disabled = value
	return r.err
}
func (r *breakGlassManagementRepositoryStub) ResetBreakGlassPasswordAtomic(_ context.Context, value BreakGlassAccountMutation) error {
	r.reset = value
	return r.err
}
func (r *breakGlassManagementRepositoryStub) UpdateBreakGlassNetworksAtomic(_ context.Context, value BreakGlassAccountMutation) error {
	r.networks = value
	return r.err
}

func breakGlassManager() authorization.Principal {
	return authorization.Principal{
		ID: "technician-1", Scope: scope.Principal{MSPID: "msp-1"},
		Capabilities: authorization.NewCapabilitySet("organization.manage"),
	}
}

func TestBreakGlassManagementCreateHashesPasswordAndCanonicalizesCIDRs(t *testing.T) {
	repository := &breakGlassManagementRepositoryStub{}
	ids := []string{"account-1", "correlation-1", "audit-1", "event-1"}
	service := NewBreakGlassManagementService(repository, func() time.Time {
		return time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	}, func() string {
		value := ids[0]
		ids = ids[1:]
		return value
	})
	account, err := service.Create(context.Background(), CreateBreakGlassAccountCommand{
		Principal: breakGlassManager(), Username: " Recovery.Admin ",
		Password:     "a sufficiently long recovery password",
		AllowedCIDRs: []string{"10.0.0.4/24"}, Reason: "establish outage access", Source: "api",
	})
	if err != nil {
		t.Fatal(err)
	}
	if account.Username != "recovery.admin" || account.AllowedCIDRs[0] != "10.0.0.0/24" {
		t.Fatalf("unexpected account: %#v", account)
	}
	if repository.created.PasswordHash == "" || repository.created.PasswordHash == "a sufficiently long recovery password" {
		t.Fatal("password was not replaced by a hash")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(repository.created.PasswordHash), []byte("a sufficiently long recovery password")); err != nil {
		t.Fatalf("hash does not verify: %v", err)
	}
	cost, _ := bcrypt.Cost([]byte(repository.created.PasswordHash))
	if cost < MinBreakGlassBcryptCost {
		t.Fatalf("bcrypt cost = %d", cost)
	}
	if repository.created.Audit.Action != "security.local_admin.created" ||
		repository.created.Event.EventType != "security.local_admin.created" ||
		repository.created.Audit.SubjectType != "local_administrator" {
		t.Fatal("security facts were not created")
	}
}

func TestLocalAdministratorCreationUsesDistinctTechnician(t *testing.T) {
	repository := &breakGlassManagementRepositoryStub{}
	ids := []string{
		"technician-2", "assignment-2", "account-2",
		"correlation-2", "audit-2", "event-2",
	}
	service := NewBreakGlassManagementService(
		repository, time.Now,
		func() string {
			value := ids[0]
			ids = ids[1:]
			return value
		},
	)
	account, err := service.Create(
		context.Background(),
		CreateBreakGlassAccountCommand{
			Principal: breakGlassManager(),
			Email:     "second-admin@example.test", DisplayName: "Second Admin",
			Username:     "second-admin",
			Password:     "a sufficiently long local password",
			AllowedCIDRs: []string{"192.168.86.0/24"},
			Reason:       "add installation administrator", Source: "api",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if account.TechnicianID != "technician-2" ||
		repository.created.AssignmentID != "assignment-2" ||
		repository.created.Email != "second-admin@example.test" ||
		repository.created.DisplayName != "Second Admin" {
		t.Fatalf("local administrator did not receive a distinct technician: %+v %+v", account, repository.created)
	}
}

func TestBreakGlassManagementRejectsMissingCIDRAndWeakPassword(t *testing.T) {
	service := NewBreakGlassManagementService(&breakGlassManagementRepositoryStub{}, time.Now, func() string { return "id" })
	_, err := service.Create(context.Background(), CreateBreakGlassAccountCommand{
		Principal: breakGlassManager(), Username: "recovery", Password: "short",
		Reason: "test", Source: "api",
	})
	if !errors.Is(err, ErrInvalidBreakGlassAccount) {
		t.Fatalf("err = %v", err)
	}
}

func TestBreakGlassManagementRequiresOrganizationCapability(t *testing.T) {
	service := NewBreakGlassManagementService(&breakGlassManagementRepositoryStub{}, time.Now, func() string { return "id" })
	principal := breakGlassManager()
	principal.Capabilities = nil
	_, err := service.List(context.Background(), principal)
	if !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("err = %v", err)
	}
}

func TestBreakGlassManagementDisableCreatesAuditedMutation(t *testing.T) {
	repository := &breakGlassManagementRepositoryStub{}
	service := NewBreakGlassManagementService(repository, time.Now, func() string { return "fact-id" })
	account, err := service.Disable(context.Background(), DisableBreakGlassAccountCommand{
		Principal: breakGlassManager(), AccountID: "account-1", ExpectedVersion: 4,
		Reason: "credential retired", Source: "api",
	})
	if err != nil {
		t.Fatal(err)
	}
	if account.Enabled || account.Version != 5 || repository.disabled.Audit.SubjectVersion != 5 {
		t.Fatalf("unexpected disable: %#v %#v", account, repository.disabled)
	}
}

func TestLocalAdministratorPasswordResetHashesPasswordAndAuditsVersion(t *testing.T) {
	repository := &breakGlassManagementRepositoryStub{}
	service := NewBreakGlassManagementService(repository, time.Now, func() string { return "fact-id" })
	account, err := service.ResetPassword(context.Background(), ResetBreakGlassPasswordCommand{
		Principal: breakGlassManager(), AccountID: "account-1", ExpectedVersion: 4,
		Password: "a different sufficiently long password", Reason: "scheduled credential rotation", Source: "api",
	})
	if err != nil {
		t.Fatal(err)
	}
	if account.Version != 5 || repository.reset.PasswordHash == "" ||
		repository.reset.PasswordHash == "a different sufficiently long password" ||
		repository.reset.Audit.Action != "security.local_admin.password_reset" {
		t.Fatalf("unexpected reset: account=%+v mutation=%+v", account, repository.reset)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(repository.reset.PasswordHash), []byte("a different sufficiently long password")); err != nil {
		t.Fatalf("reset hash does not verify: %v", err)
	}
}

func TestLocalAdministratorNetworkUpdateCanonicalizesCIDRsAndAuditsVersion(t *testing.T) {
	repository := &breakGlassManagementRepositoryStub{}
	service := NewBreakGlassManagementService(repository, time.Now, func() string { return "fact-id" })
	account, err := service.UpdateNetworks(context.Background(), UpdateBreakGlassNetworksCommand{
		Principal: breakGlassManager(), AccountID: "account-1", ExpectedVersion: 2,
		AllowedCIDRs: []string{"192.168.86.44/24", "10.0.0.1/8"}, Reason: "expand administration network", Source: "api",
	})
	if err != nil {
		t.Fatal(err)
	}
	if account.Version != 3 || len(account.AllowedCIDRs) != 2 ||
		account.AllowedCIDRs[0] != "192.168.86.0/24" || account.AllowedCIDRs[1] != "10.0.0.0/8" ||
		repository.networks.Audit.Action != "security.local_admin.networks_updated" {
		t.Fatalf("unexpected network update: account=%+v mutation=%+v", account, repository.networks)
	}
}
