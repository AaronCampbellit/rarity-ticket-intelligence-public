package webhooks

import (
	"context"
	"errors"
	"testing"
)

func TestEnvironmentInboundSecretResolverAllowsOnlyDedicatedReferences(t *testing.T) {
	resolver := NewEnvironmentInboundSecretResolver(func(name string) (string, bool) {
		if name == "RARITY_WEBHOOK_SECRET_MONITOR" {
			return "secret-value", true
		}
		return "", false
	})
	value, err := resolver.Resolve(
		context.Background(), "env://RARITY_WEBHOOK_SECRET_MONITOR",
	)
	if err != nil || string(value) != "secret-value" {
		t.Fatalf("Resolve() value=%q error=%v", value, err)
	}
	for _, ref := range []string{
		"env://DATABASE_URL",
		"env://RARITY_WEBHOOK_SECRET_MISSING",
		"secret-value",
	} {
		if _, err := resolver.Resolve(context.Background(), ref); !errors.Is(
			err, ErrInboundSecretUnavailable,
		) {
			t.Fatalf("Resolve(%q) error=%v", ref, err)
		}
	}
}
