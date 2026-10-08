package authorization_test

import (
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

func TestAuthorizeRequiresCapabilityAndMatchingScope(t *testing.T) {
	principal := authorization.Principal{
		Scope:        scope.Principal{MSPID: "msp-a", ClientID: "client-a"},
		Capabilities: authorization.NewCapabilitySet("client.read"),
	}

	if err := authorization.Authorize(principal, "client.read", scope.Target{
		MSPID: "msp-a", ClientID: "client-a",
	}); err != nil {
		t.Fatalf("matching authorization denied: %v", err)
	}
	if err := authorization.Authorize(principal, "client.update", scope.Target{
		MSPID: "msp-a", ClientID: "client-a",
	}); !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("missing capability error = %v, want ErrForbidden", err)
	}
	if err := authorization.Authorize(principal, "client.read", scope.Target{
		MSPID: "msp-a", ClientID: "client-b",
	}); !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("cross-client error = %v, want enumeration-safe ErrNotFound", err)
	}
}

func TestBreakGlassRequiresReasonAndUnexpiredGrant(t *testing.T) {
	now := time.Date(2026, time.July, 29, 13, 0, 0, 0, time.UTC)
	base := authorization.Principal{
		Scope: scope.Principal{MSPID: "msp-a"},
		BreakGlass: &authorization.BreakGlassGrant{
			Capabilities: authorization.NewCapabilitySet("client.update"),
			ExpiresAt:    now.Add(time.Minute),
		},
	}

	if err := authorization.AuthorizeAt(base, "client.update", scope.Target{
		MSPID: "msp-a", ClientID: "client-a",
	}, now); !errors.Is(err, authorization.ErrBreakGlassReasonRequired) {
		t.Fatalf("missing reason error = %v", err)
	}

	base.BreakGlass.Reason = "Restore access during identity outage"
	if err := authorization.AuthorizeAt(base, "client.update", scope.Target{
		MSPID: "msp-a", ClientID: "client-a",
	}, now); err != nil {
		t.Fatalf("valid break-glass grant denied: %v", err)
	}

	base.BreakGlass.ExpiresAt = now
	if err := authorization.AuthorizeAt(base, "client.update", scope.Target{
		MSPID: "msp-a", ClientID: "client-a",
	}, now); !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("expired grant error = %v, want ErrForbidden", err)
	}
}
