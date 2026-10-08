package sessions

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/httpauth"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type resolverAuthenticator struct {
	authenticated Authenticated
	token         string
}

func (a *resolverAuthenticator) Authenticate(_ context.Context, token string) (Authenticated, error) {
	a.token = token
	return a.authenticated, nil
}

type resolverLoader struct {
	clientID string
}

func (l *resolverLoader) LoadPrincipal(
	_ context.Context,
	authenticated Authenticated,
	clientID string,
) (authorization.Principal, error) {
	l.clientID = clientID
	return authorization.Principal{
		ID:           authenticated.TechnicianID,
		Scope:        scope.Principal{MSPID: authenticated.MSPID, ClientID: clientID},
		Capabilities: authorization.NewCapabilitySet("prospect.create"),
	}, nil
}

func TestBearerPrincipalResolverLoadsOnlyMSPGlobalContext(t *testing.T) {
	authenticator := &resolverAuthenticator{authenticated: Authenticated{
		MSPID: "msp-id", TechnicianID: "technician-id",
	}}
	loader := &resolverLoader{}
	resolve := BearerPrincipalResolver(authenticator, loader)
	request := httptest.NewRequest("POST", "/api/v1/prospects", nil)
	request.Header.Set("Authorization", "Bearer opaque-session")

	principal, err := resolve(request)
	if err != nil {
		t.Fatalf("resolve() error = %v", err)
	}
	if authenticator.token != "opaque-session" || loader.clientID != "" ||
		principal.Scope.ClientID != "" || principal.ID != "technician-id" {
		t.Fatalf("resolver widened session scope: principal=%+v client=%q", principal, loader.clientID)
	}
}

func TestBearerPrincipalResolverLoadsValidatedSelectedClientContext(t *testing.T) {
	authenticator := &resolverAuthenticator{authenticated: Authenticated{
		MSPID: "msp-id", TechnicianID: "technician-id",
	}}
	loader := &resolverLoader{}
	resolve := BearerPrincipalResolver(authenticator, loader)
	request := httptest.NewRequest("POST", "/api/v1/projects", nil)
	request.Header.Set("Authorization", "Bearer opaque-session")
	request.Header.Set(httpauth.ClientHeader, " client-id ")

	principal, err := resolve(request)
	if err != nil {
		t.Fatalf("resolve() error = %v", err)
	}
	if loader.clientID != "client-id" || principal.Scope.ClientID != "client-id" {
		t.Fatalf("selected Client was not loaded: principal=%+v client=%q", principal, loader.clientID)
	}
}
