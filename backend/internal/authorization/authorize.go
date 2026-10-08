// Package authorization combines capability grants with the trusted tenant
// scope boundary. Domain-specific workflow checks remain in application
// services.
package authorization

import (
	"errors"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

var (
	ErrForbidden                = errors.New("forbidden")
	ErrBreakGlassReasonRequired = errors.New("break-glass reason required")
)

type CapabilitySet map[string]struct{}

func NewCapabilitySet(capabilities ...string) CapabilitySet {
	set := make(CapabilitySet, len(capabilities))
	for _, capability := range capabilities {
		if normalized := strings.TrimSpace(capability); normalized != "" {
			set[normalized] = struct{}{}
		}
	}
	return set
}

func (s CapabilitySet) Has(capability string) bool {
	_, ok := s[capability]
	return ok
}

type BreakGlassGrant struct {
	Capabilities CapabilitySet
	Reason       string
	ExpiresAt    time.Time
}

type Principal struct {
	ID           string
	Scope        scope.Principal
	Capabilities CapabilitySet
	DataScopes   CapabilitySet
	BreakGlass   *BreakGlassGrant
}

func (p Principal) AllowsData(dataScope string) bool {
	return p.DataScopes.Has(dataScope)
}

func Authorize(principal Principal, capability string, target scope.Target) error {
	return AuthorizeAt(principal, capability, target, time.Now())
}

func AuthorizeAt(principal Principal, capability string, target scope.Target, now time.Time) error {
	if err := scope.Authorize(principal.Scope, target); err != nil {
		return err
	}
	if principal.Capabilities.Has(capability) {
		return nil
	}
	if principal.BreakGlass == nil ||
		!principal.BreakGlass.Capabilities.Has(capability) ||
		!principal.BreakGlass.ExpiresAt.After(now) {
		return ErrForbidden
	}
	if strings.TrimSpace(principal.BreakGlass.Reason) == "" {
		return ErrBreakGlassReasonRequired
	}
	return nil
}
