package authn

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/id"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/identity"
	"golang.org/x/crypto/bcrypt"
)

func TestLocalAdministratorAuthenticationIntegration(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	mspID := os.Getenv("TEST_MSP_ID")
	username := os.Getenv("TEST_LOCAL_ADMIN_USERNAME")
	password := os.Getenv("TEST_LOCAL_ADMIN_PASSWORD")
	sourceIP := os.Getenv("TEST_LOCAL_ADMIN_SOURCE_IP")
	if databaseURL == "" || mspID == "" || username == "" ||
		password == "" || sourceIP == "" {
		t.Skip("local administrator integration environment is required")
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	store := NewBreakGlassStore(pool, id.New)
	account, err := store.FindBreakGlassAccount(
		context.Background(), mspID, username,
	)
	if err != nil {
		t.Fatalf("lookup local administrator: %v", err)
	}
	if !account.Enabled {
		t.Fatal("local administrator is disabled")
	}
	if cost, err := bcrypt.Cost([]byte(account.PasswordHash)); err != nil ||
		cost < identity.MinBreakGlassBcryptCost {
		t.Fatalf("local administrator bcrypt policy: cost=%d err=%v", cost, err)
	}
	if err := bcrypt.CompareHashAndPassword(
		[]byte(account.PasswordHash), []byte(password),
	); err != nil {
		t.Fatal("local administrator password does not match")
	}
	allowed := false
	for _, value := range account.AllowedCIDRs {
		_, network, err := net.ParseCIDR(value)
		if err == nil && network.Contains(net.ParseIP(sourceIP)) {
			allowed = true
		}
	}
	if !allowed {
		t.Fatalf("source IP %s is outside the stored network policy", sourceIP)
	}
	authenticator := identity.NewBreakGlassAuthenticator(mspID, store, time.Now)
	if _, err := authenticator.AuthenticateBreakGlass(
		context.Background(), username, password, sourceIP,
	); err != nil {
		t.Fatalf("local administrator authentication: %v", err)
	}
}
