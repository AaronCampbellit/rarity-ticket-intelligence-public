package httpauth

import (
	"net/http"
	"strings"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
)

type PrincipalResolver func(*http.Request) (authorization.Principal, error)

const ClientHeader = "X-Rarity-Client-ID"

func MultiplexBearer(
	session PrincipalResolver,
	serviceKey PrincipalResolver,
) PrincipalResolver {
	return func(request *http.Request) (authorization.Principal, error) {
		parts := strings.Fields(request.Header.Get("Authorization"))
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") &&
			strings.HasPrefix(parts[1], "rsk_") {
			return serviceKey(request)
		}
		return session(request)
	}
}
