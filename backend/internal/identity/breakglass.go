package identity

import (
	"context"
	"errors"
	"net"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const MinBreakGlassBcryptCost = 12

var (
	ErrBreakGlassDenied           = errors.New("break-glass authentication denied")
	ErrBreakGlassIdentityNotFound = errors.New("break-glass identity not found")
)

type BreakGlassAccount struct {
	ID           string
	MSPID        string
	TechnicianID string
	Username     string
	PasswordHash string
	Enabled      bool
	AllowedCIDRs []string
}

type BreakGlassUse struct {
	AccountID    string
	MSPID        string
	TechnicianID string
	SourceIP     string
	OccurredAt   time.Time
}

type BreakGlassRepository interface {
	FindBreakGlassAccount(context.Context, string, string) (BreakGlassAccount, error)
	RecordBreakGlassUse(context.Context, BreakGlassUse) error
}

type BreakGlassAuthenticator struct {
	mspID      string
	repository BreakGlassRepository
	now        func() time.Time
	dummyHash  []byte
}

func NewBreakGlassAuthenticator(
	mspID string,
	repository BreakGlassRepository,
	now func() time.Time,
) *BreakGlassAuthenticator {
	if now == nil {
		now = time.Now
	}
	dummyHash, _ := bcrypt.GenerateFromPassword(
		[]byte("rarity-break-glass-dummy-password"),
		MinBreakGlassBcryptCost,
	)
	return &BreakGlassAuthenticator{
		mspID: mspID, repository: repository, now: now, dummyHash: dummyHash,
	}
}

func (a *BreakGlassAuthenticator) AuthenticateBreakGlass(
	ctx context.Context,
	username string,
	password string,
	sourceIP string,
) (string, error) {
	if a.mspID == "" || a.repository == nil {
		return "", ErrBreakGlassDenied
	}
	account, err := a.repository.FindBreakGlassAccount(
		ctx, a.mspID, strings.ToLower(strings.TrimSpace(username)),
	)
	if err != nil {
		_ = bcrypt.CompareHashAndPassword(a.dummyHash, []byte(password))
		return "", ErrBreakGlassDenied
	}
	cost, costErr := bcrypt.Cost([]byte(account.PasswordHash))
	passwordErr := bcrypt.CompareHashAndPassword(
		[]byte(account.PasswordHash), []byte(password),
	)
	if !account.Enabled || costErr != nil || cost < MinBreakGlassBcryptCost ||
		passwordErr != nil || !ipAllowed(sourceIP, account.AllowedCIDRs) {
		return "", ErrBreakGlassDenied
	}
	use := BreakGlassUse{
		AccountID: account.ID, MSPID: account.MSPID,
		TechnicianID: account.TechnicianID, SourceIP: sourceIP,
		OccurredAt: a.now().UTC(),
	}
	if err := a.repository.RecordBreakGlassUse(ctx, use); err != nil {
		return "", ErrBreakGlassDenied
	}
	return account.TechnicianID, nil
}

func ipAllowed(sourceIP string, allowedCIDRs []string) bool {
	if len(allowedCIDRs) == 0 {
		return false
	}
	ip := net.ParseIP(sourceIP)
	if ip == nil {
		return false
	}
	for _, value := range allowedCIDRs {
		_, network, err := net.ParseCIDR(value)
		if err == nil && network.Contains(ip) {
			return true
		}
	}
	return false
}
