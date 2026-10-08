package compose

import (
	"os"
	"strings"
	"testing"
)

func TestPublicIngressDoesNotExposeMetrics(t *testing.T) {
	for _, name := range []string{"Caddyfile", "Caddyfile.pilot"} {
		content, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		text := string(content)
		if !strings.Contains(text, "@internal path /metrics") ||
			!strings.Contains(text, "respond @internal 404") {
			t.Fatalf("%s must explicitly hide internal metrics", name)
		}
	}
}

func TestPublicIngressRoutesVersionedAPI(t *testing.T) {
	for _, name := range []string{"Caddyfile", "Caddyfile.pilot"} {
		content, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if !strings.Contains(string(content), "/api/v1/*") {
			t.Fatalf("%s must route the public versioned API to rarity-api", name)
		}
	}
}

func TestPublicIngressRoutesBrowserAuthentication(t *testing.T) {
	for _, name := range []string{"Caddyfile", "Caddyfile.pilot"} {
		content, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if !strings.Contains(string(content), "/auth/*") {
			t.Fatalf("%s must route browser authentication to rarity-api", name)
		}
	}
}

func TestAPIHasProviderEgressWithoutPublishingPorts(t *testing.T) {
	content, err := os.ReadFile("compose.yaml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	if !strings.Contains(text, "networks: [rti-private, provider-egress]") ||
		!strings.Contains(text, "provider-egress:") ||
		!strings.Contains(text, "name: rti_demo_provider_egress") {
		t.Fatal("rarity-api must have a dedicated provider egress network")
	}
}

func TestTunnelIngressTrustsHTTPSOnlyOnLoopbackOrigin(t *testing.T) {
	compose, err := os.ReadFile("compose.tunnel.yaml")
	if err != nil {
		t.Fatalf("read tunnel Compose override: %v", err)
	}
	caddy, err := os.ReadFile("Caddyfile.tunnel")
	if err != nil {
		t.Fatalf("read tunnel Caddyfile: %v", err)
	}

	composeText := string(compose)
	if !strings.Contains(
		composeText,
		`127.0.0.1:${RARITY_DEMO_HTTP_PORT:?RARITY_DEMO_HTTP_PORT is required}:80`,
	) {
		t.Fatal("tunnel origin must publish only on host loopback")
	}
	if !strings.Contains(
		composeText,
		"./Caddyfile.tunnel:/etc/caddy/Caddyfile:ro",
	) {
		t.Fatal("tunnel deployment must use the HTTPS-aware Caddy ingress")
	}
	if !strings.Contains(
		string(caddy),
		"header_up X-Forwarded-Proto https",
	) {
		t.Fatal("tunnel ingress must preserve the public HTTPS scheme for the API")
	}
}
