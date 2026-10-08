package sessions

import (
	"context"
	"net/http"
	"strings"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/httpauth"
)

type Authenticator interface {
	Authenticate(context.Context, string) (Authenticated, error)
}

type PrincipalLoader interface {
	LoadPrincipal(context.Context, Authenticated, string) (authorization.Principal, error)
}

func BearerPrincipalResolver(
	authenticator Authenticator,
	loader PrincipalLoader,
) func(*http.Request) (authorization.Principal, error) {
	return func(request *http.Request) (authorization.Principal, error) {
		if request == nil || authenticator == nil || loader == nil {
			return authorization.Principal{}, ErrInvalidSession
		}
		parts := strings.Fields(request.Header.Get("Authorization"))
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			return authorization.Principal{}, ErrInvalidSession
		}
		authenticated, err := authenticator.Authenticate(request.Context(), parts[1])
		if err != nil {
			return authorization.Principal{}, ErrInvalidSession
		}
		clientID := strings.TrimSpace(request.Header.Get(httpauth.ClientHeader))
		principal, err := loader.LoadPrincipal(request.Context(), authenticated, clientID)
		if err != nil {
			return authorization.Principal{}, ErrInvalidSession
		}
		return principal, nil
	}
}
