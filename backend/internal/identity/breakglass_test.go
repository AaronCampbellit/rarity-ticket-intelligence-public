package identity

import (
	"context"
	"errors"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestBreakGlassAuthenticatorVerifiesPasswordNetworkAndRecordsUse(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("correct horse battery staple"), MinBreakGlassBcryptCost)
	if err != nil {
		t.Fatal(err)
	}
	usedAt := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	repository := &fakeBreakGlassRepository{account: BreakGlassAccount{
		ID: "account-id", MSPID: "msp-id", TechnicianID: "technician-id",
		Username: "recovery-admin", PasswordHash: string(hash), Enabled: true,
		AllowedCIDRs: []string{"10.20.30.0/24"},
	}}
	authenticator := NewBreakGlassAuthenticator("msp-id", repository, func() time.Time {
		return usedAt
	})

	technicianID, err := authenticator.AuthenticateBreakGlass(
		context.Background(),
		" RECOVERY-ADMIN ",
		"correct horse battery staple",
		"10.20.30.44",
	)
	if err != nil || technicianID != "technician-id" {
		t.Fatalf("authentication failed: technician=%q err=%v", technicianID, err)
	}
	if repository.recorded.AccountID != "account-id" ||
		repository.recorded.SourceIP != "10.20.30.44" ||
		!repository.recorded.OccurredAt.Equal(usedAt) {
		t.Fatalf("break-glass use was not recorded: %+v", repository.recorded)
	}
}

func TestBreakGlassAuthenticatorDeniesInvalidAccountStates(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("correct password"), MinBreakGlassBcryptCost)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name      string
		account   BreakGlassAccount
		repoErr   error
		password  string
		sourceIP  string
		recordErr error
	}{
		{name: "missing", repoErr: ErrBreakGlassIdentityNotFound, password: "wrong", sourceIP: "10.0.0.1"},
		{name: "disabled", account: BreakGlassAccount{PasswordHash: string(hash), Enabled: false}, password: "correct password", sourceIP: "10.0.0.1"},
		{name: "wrong password", account: BreakGlassAccount{PasswordHash: string(hash), Enabled: true}, password: "wrong", sourceIP: "10.0.0.1"},
		{name: "outside network", account: BreakGlassAccount{PasswordHash: string(hash), Enabled: true, AllowedCIDRs: []string{"192.0.2.0/24"}}, password: "correct password", sourceIP: "10.0.0.1"},
		{name: "missing network policy", account: BreakGlassAccount{PasswordHash: string(hash), Enabled: true}, password: "correct password", sourceIP: "10.0.0.1"},
		{name: "audit unavailable", account: BreakGlassAccount{PasswordHash: string(hash), Enabled: true}, password: "correct password", sourceIP: "10.0.0.1", recordErr: errors.New("database unavailable")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &fakeBreakGlassRepository{
				account: test.account, findErr: test.repoErr, recordErr: test.recordErr,
			}
			authenticator := NewBreakGlassAuthenticator("msp-id", repository, time.Now)
			if _, err := authenticator.AuthenticateBreakGlass(
				context.Background(), "recovery-admin", test.password, test.sourceIP,
			); !errors.Is(err, ErrBreakGlassDenied) {
				t.Fatalf("expected non-enumerating denial, got %v", err)
			}
		})
	}
}

type fakeBreakGlassRepository struct {
	account   BreakGlassAccount
	findErr   error
	recordErr error
	recorded  BreakGlassUse
}

func (f *fakeBreakGlassRepository) FindBreakGlassAccount(
	context.Context,
	string,
	string,
) (BreakGlassAccount, error) {
	return f.account, f.findErr
}

func (f *fakeBreakGlassRepository) RecordBreakGlassUse(
	_ context.Context,
	use BreakGlassUse,
) error {
	f.recorded = use
	return f.recordErr
}
