package webhooks

import (
	"context"
	"errors"
	"strings"
)

var ErrInboundSecretUnavailable = errors.New("inbound webhook secret unavailable")

type EnvironmentInboundSecretResolver struct {
	lookup func(string) (string, bool)
}

func NewEnvironmentInboundSecretResolver(
	lookup func(string) (string, bool),
) *EnvironmentInboundSecretResolver {
	return &EnvironmentInboundSecretResolver{lookup: lookup}
}

func (r *EnvironmentInboundSecretResolver) Resolve(
	_ context.Context,
	ref string,
) ([]byte, error) {
	const prefix = "env://"
	name := strings.TrimPrefix(ref, prefix)
	if r == nil || r.lookup == nil || name == ref ||
		!strings.HasPrefix(name, "RARITY_WEBHOOK_SECRET_") {
		return nil, ErrInboundSecretUnavailable
	}
	value, ok := r.lookup(name)
	if !ok || strings.TrimSpace(value) == "" {
		return nil, ErrInboundSecretUnavailable
	}
	return []byte(value), nil
}
