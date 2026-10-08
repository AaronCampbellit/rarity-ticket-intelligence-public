package scope_test

import (
	"errors"
	"testing"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

func TestAuthorizeRejectsCrossMSPAndCrossClientObjects(t *testing.T) {
	principal := scope.Principal{MSPID: "msp-a", ClientID: "client-a"}

	tests := []struct {
		name   string
		target scope.Target
	}{
		{name: "other MSP", target: scope.Target{MSPID: "msp-b", ClientID: "client-a"}},
		{name: "other client", target: scope.Target{MSPID: "msp-a", ClientID: "client-b"}},
		{name: "MSP global from client principal", target: scope.Target{MSPID: "msp-a"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := scope.Authorize(principal, tt.target); !errors.Is(err, scope.ErrNotFound) {
				t.Fatalf("Authorize() error = %v, want enumeration-safe ErrNotFound", err)
			}
		})
	}
}

func TestAuthorizeAllowsMatchingClientAndMSPGlobalPrincipals(t *testing.T) {
	if err := scope.Authorize(
		scope.Principal{MSPID: "msp-a", ClientID: "client-a"},
		scope.Target{MSPID: "msp-a", ClientID: "client-a"},
	); err != nil {
		t.Fatalf("matching client scope denied: %v", err)
	}

	if err := scope.Authorize(
		scope.Principal{MSPID: "msp-a"},
		scope.Target{MSPID: "msp-a"},
	); err != nil {
		t.Fatalf("matching MSP-global scope denied: %v", err)
	}
}

func TestAuthorizeRejectsIncompleteTrustedScope(t *testing.T) {
	if err := scope.Authorize(scope.Principal{}, scope.Target{MSPID: "msp-a"}); !errors.Is(err, scope.ErrInvalid) {
		t.Fatalf("Authorize() error = %v, want ErrInvalid", err)
	}
}
