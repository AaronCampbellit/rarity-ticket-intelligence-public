package compose

import (
	"os"
	"strings"
	"testing"
)

func TestDockerIgnoreExcludesLocalSecretsAndFrontendDependencies(t *testing.T) {
	content, err := os.ReadFile("../../.dockerignore")
	if err != nil {
		t.Fatalf("read Docker ignore file: %v", err)
	}
	for _, pattern := range []string{".env", "frontend/node_modules", "frontend/dist"} {
		if !strings.Contains(string(content), pattern) {
			t.Fatalf("Docker ignore file must exclude %s", pattern)
		}
	}
}

func TestFrontendDockerfilePinsEveryBaseImageByDigest(t *testing.T) {
	content, err := os.ReadFile("frontend.Dockerfile")
	if err != nil {
		t.Fatalf("read frontend Dockerfile: %v", err)
	}
	for _, line := range strings.Split(string(content), "\n") {
		if strings.HasPrefix(line, "FROM ") &&
			line != "FROM scratch" &&
			!strings.Contains(line, "@sha256:") {
			t.Fatalf("frontend Dockerfile base image is not digest-pinned: %s", line)
		}
	}
}

func TestFrontendImageRunsAsExplicitNonRootUserOnUnprivilegedPort(t *testing.T) {
	dockerfile, err := os.ReadFile("frontend.Dockerfile")
	if err != nil {
		t.Fatalf("read frontend Dockerfile: %v", err)
	}
	for _, directive := range []string{
		"FROM scratch",
		"COPY --from=server /out/frontend-server /frontend-server",
		"COPY --from=build --chown=65532:65532 /src/frontend/dist /usr/share/rarity",
		"USER 65532:65532",
		"EXPOSE 8080",
		`ENTRYPOINT ["/frontend-server"]`,
	} {
		if !strings.Contains(string(dockerfile), directive) {
			t.Fatalf("frontend Dockerfile must contain %q", directive)
		}
	}
}

func TestEdgeCaddyfilesUseUnprivilegedFrontendPort(t *testing.T) {
	for _, name := range []string{"Caddyfile", "Caddyfile.pilot", "Caddyfile.tunnel"} {
		content, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if !strings.Contains(string(content), "reverse_proxy rarity-web:8080") {
			t.Errorf("%s must proxy to rarity-web:8080", name)
		}
		if strings.Contains(string(content), "reverse_proxy rarity-web:80\n") {
			t.Errorf("%s must not proxy to the privileged frontend port", name)
		}
	}
}
