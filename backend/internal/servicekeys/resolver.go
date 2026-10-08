package servicekeys

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

func BearerPrincipalResolver(
	authenticator Authenticator,
) func(*http.Request) (authorization.Principal, error) {
	return func(request *http.Request) (authorization.Principal, error) {
		if request == nil || authenticator == nil {
			return authorization.Principal{}, ErrInvalidKey
		}
		parts := strings.Fields(request.Header.Get("Authorization"))
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			return authorization.Principal{}, ErrInvalidKey
		}
		authenticated, err := authenticator.Authenticate(request.Context(), parts[1])
		if err != nil {
			return authorization.Principal{}, ErrInvalidKey
		}
		selectedClient := strings.TrimSpace(request.Header.Get(httpauth.ClientHeader))
		if selectedClient != "" && selectedClient != authenticated.ClientID {
			return authorization.Principal{}, ErrInvalidKey
		}
		return authenticated.Principal(), nil
	}
}
