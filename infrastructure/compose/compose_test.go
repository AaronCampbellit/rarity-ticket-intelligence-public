package compose

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestDemoComposePublishesCaddyOnVMNetworkByDefault(t *testing.T) {
	content, err := os.ReadFile("compose.demo.yaml")
	if err != nil {
		t.Fatal(err)
	}
	binding := "${RARITY_DEMO_BIND_ADDRESS:-0.0.0.0}:${RARITY_DEMO_HTTP_PORT:?RARITY_DEMO_HTTP_PORT is required}:80"
	if !strings.Contains(string(content), binding) {
		t.Fatalf("demo caddy binding must default to the VM network: %s", binding)
	}
}

type composeService struct {
	Image      string         `json:"image"`
	Ports      []any          `json:"ports"`
	Build      map[string]any `json:"build"`
	Restart    string         `json:"restart"`
	Profiles   []string       `json:"profiles"`
	Networks   map[string]any `json:"networks"`
	Entrypoint []string       `json:"entrypoint"`
}

func TestComposePackagesIsolatedOperatorCLI(t *testing.T) {
	cfg := loadComposeWithProfile(t, "operator", "compose.yaml")
	admin, ok := cfg.Services["rarity-admin"]
	if !ok {
		t.Fatal("expected rarity-admin service")
	}
	if len(admin.Profiles) != 1 || admin.Profiles[0] != "operator" {
		t.Fatalf("rarity-admin profiles = %v", admin.Profiles)
	}
	if len(admin.Ports) != 0 || len(admin.Networks) != 1 {
		t.Fatalf("rarity-admin must have no ports and only the private network: %+v", admin)
	}
	if _, ok := admin.Networks["rti-private"]; !ok {
		t.Fatalf("rarity-admin networks = %v", admin.Networks)
	}
	if len(admin.Entrypoint) != 1 || admin.Entrypoint[0] != "/rarity-admin" ||
		admin.Restart != "no" {
		t.Fatalf("rarity-admin execution contract = %+v", admin)
	}
}

type composeConfig struct {
	Services map[string]composeService `json:"services"`
}

func loadCompose(t *testing.T, files ...string) composeConfig {
	t.Helper()
	return loadComposeWithProfile(t, "", files...)
}

func loadComposeWithProfile(t *testing.T, profile string, files ...string) composeConfig {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("Docker is required for rendered Compose verification")
	}
	args := []string{"compose", "--env-file", "../../.env"}
	if profile != "" {
		args = append(args, "--profile", profile)
	}
	for _, file := range files {
		args = append(args, "-f", file)
	}
	args = append(args, "config", "--format", "json")
	output, err := exec.Command("docker", args...).Output()
	if err != nil {
		t.Fatalf("render compose config: %v", err)
	}
	var cfg composeConfig
	if err := json.Unmarshal(output, &cfg); err != nil {
		t.Fatalf("decode compose config: %v", err)
	}
	return cfg
}

func TestDemoComposeExposesOnlyCaddy(t *testing.T) {
	cfg := loadCompose(t, "compose.yaml", "compose.demo.yaml")
	if _, ok := cfg.Services["caddy"]; !ok {
		t.Fatal("expected caddy service")
	}
	for _, name := range []string{"postgres", "valkey", "minio"} {
		if len(cfg.Services[name].Ports) != 0 {
			t.Fatalf("%s must remain private", name)
		}
	}
	if len(cfg.Services["caddy"].Ports) != 1 {
		t.Fatal("expected exactly one published caddy port")
	}
}

func TestDemoComposeServicesRecoverAfterTransientDependencyOutages(t *testing.T) {
	cfg := loadCompose(t, "compose.yaml", "compose.demo.yaml")
	for name, service := range cfg.Services {
		if service.Restart != "unless-stopped" {
			t.Errorf("%s restart policy = %q, want %q", name, service.Restart, "unless-stopped")
		}
	}
}

func TestWebBuildReceivesRevision(t *testing.T) {
	cfg := loadCompose(t, "compose.yaml")
	web, ok := cfg.Services["rarity-web"]
	if !ok {
		t.Fatal("expected rarity-web service")
	}
	args, ok := web.Build["args"].(map[string]any)
	if !ok || args["RARITY_BUILD_REVISION"] == "" {
		t.Fatal("rarity-web build must receive RARITY_BUILD_REVISION")
	}
}

func TestAPIReceivesRequestedBuildRevision(t *testing.T) {
	content, err := os.ReadFile("compose.yaml")
	if err != nil {
		t.Fatal(err)
	}
	apiBlock, ok := strings.CutPrefix(string(content), "name: rti\n\nservices:\n  rarity-api:\n")
	if !ok {
		t.Fatal("rarity-api service block is missing")
	}
	apiBlock, _, ok = strings.Cut(apiBlock, "\n  rarity-admin:")
	if !ok {
		t.Fatal("rarity-admin boundary is missing")
	}
	required := "RARITY_BUILD_REVISION: ${RARITY_BUILD_REVISION:?RARITY_BUILD_REVISION is required}"
	if !strings.Contains(apiBlock, required) {
		t.Fatal("rarity-api must receive the requested RARITY_BUILD_REVISION")
	}
}

func TestComposeUsesPinnedRuntimeImages(t *testing.T) {
	cfg := loadCompose(t, "compose.yaml")
	for _, name := range []string{"postgres", "valkey", "minio", "caddy"} {
		image := cfg.Services[name].Image
		if image == "" || !strings.Contains(image, "@sha256:") {
			t.Fatalf("%s must use a digest-pinned image, got %q", name, image)
		}
	}
}

func TestReleaseComposeUsesVerifiedDigestsWithoutLocalApplicationBuilds(t *testing.T) {
	digest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	t.Setenv("RARITY_API_IMAGE", "ghcr.io/example/rarity-api@"+digest)
	t.Setenv("RARITY_FRONTEND_IMAGE", "ghcr.io/example/rarity-frontend@"+digest)
	cfg := loadComposeWithProfile(t, "operator", "compose.yaml", "compose.release.yaml")
	for _, name := range []string{"rarity-api", "rarity-admin", "rarity-web"} {
		service := cfg.Services[name]
		if !strings.Contains(service.Image, "@sha256:") || len(service.Build) != 0 {
			t.Fatalf("%s must use a digest without a local build: %+v", name, service)
		}
	}
}
