package compose

import (
	"os"
	"strings"
	"testing"
)

func TestPilotComposeRequiresTLSMaterialAndPrivateStateServices(t *testing.T) {
	content, err := os.ReadFile("compose.pilot.yaml")
	if err != nil {
		t.Fatalf("read pilot Compose override: %v", err)
	}
	text := string(content)

	for _, required := range []string{
		"RARITY_CONFIG_DIR:?",
		"postgres/server.crt",
		"valkey/server.crt",
		"minio/certs",
		"restart: unless-stopped",
		"logging:",
		"rti_pilot_caddy_data",
		"RARITY_PUBLIC_URL: https://${RARITY_PUBLIC_HOST:?RARITY_PUBLIC_HOST is required}",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("pilot Compose override must include %q", required)
		}
	}

	for _, forbidden := range []string{
		"5432:5432",
		"6379:6379",
		"9000:9000",
		"9001:9001",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("pilot Compose override must not publish state port %q", forbidden)
		}
	}
}

func TestPilotCaddyPublishesHTTPSOnly(t *testing.T) {
	compose, err := os.ReadFile("compose.pilot.yaml")
	if err != nil {
		t.Fatalf("read pilot Compose override: %v", err)
	}
	caddy, err := os.ReadFile("Caddyfile.pilot")
	if err != nil {
		t.Fatalf("read pilot Caddyfile: %v", err)
	}

	if !strings.Contains(string(compose), `"443:443"`) {
		t.Fatal("pilot must publish HTTPS on port 443")
	}
	if strings.Contains(string(compose), `"80:80"`) {
		t.Fatal("pilot must not publish plaintext HTTP")
	}
	if !strings.Contains(string(caddy), "{$RARITY_PUBLIC_HOST}") {
		t.Fatal("pilot Caddyfile must require the configured public host")
	}
}
