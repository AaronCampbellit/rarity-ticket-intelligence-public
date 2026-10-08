package compose

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestAPIDockerfileCopiesGoModAndGoSumBeforeDependencyDownload(t *testing.T) {
	content, err := os.ReadFile("api.Dockerfile")
	if err != nil {
		t.Fatalf("read api Dockerfile: %v", err)
	}
	if !strings.Contains(string(content), "COPY go.mod go.sum ./") {
		t.Fatal("api Dockerfile must copy go.mod and go.sum before go mod download")
	}
	for _, line := range strings.Split(string(content), "\n") {
		if strings.HasPrefix(line, "FROM ") &&
			line != "FROM scratch" &&
			!strings.Contains(line, "@sha256:") {
			t.Fatalf("api Dockerfile base image is not digest-pinned: %s", line)
		}
	}
}

func TestAPIRuntimeIsScratchWithTrustDataAndNumericNonRootUser(t *testing.T) {
	content, err := os.ReadFile("api.Dockerfile")
	if err != nil {
		t.Fatalf("read api Dockerfile: %v", err)
	}
	for _, directive := range []string{
		"FROM scratch",
		"RUN apk add --no-cache tzdata",
		"COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt",
		"COPY --from=build /usr/share/zoneinfo /usr/share/zoneinfo",
		"USER 65532:65532",
	} {
		if !strings.Contains(string(content), directive) {
			t.Fatalf("api Dockerfile must contain %q", directive)
		}
	}
}

func TestAPIImageIncludesOperatorCLI(t *testing.T) {
	content, err := os.ReadFile("api.Dockerfile")
	if err != nil {
		t.Fatalf("read api Dockerfile: %v", err)
	}
	value := string(content)
	if !strings.Contains(value, "./backend/cmd/rarity-admin") ||
		!strings.Contains(value, "COPY --from=build /out/rarity-admin /rarity-admin") {
		t.Fatal("api image must build and package the operator CLI")
	}
}

func TestAPIBuilderSupportsModuleGoVersion(t *testing.T) {
	module, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	dockerfile, err := os.ReadFile("api.Dockerfile")
	if err != nil {
		t.Fatalf("read api Dockerfile: %v", err)
	}

	moduleVersion := regexp.MustCompile(`(?m)^go ([0-9]+)\.([0-9]+)`).FindStringSubmatch(string(module))
	builderVersion := regexp.MustCompile(`(?m)^FROM golang:([0-9]+)\.([0-9]+)`).FindStringSubmatch(string(dockerfile))
	if len(moduleVersion) != 3 || len(builderVersion) != 3 {
		t.Fatal("go.mod and api Dockerfile must declare major.minor Go versions")
	}

	moduleMajor, _ := strconv.Atoi(moduleVersion[1])
	moduleMinor, _ := strconv.Atoi(moduleVersion[2])
	builderMajor, _ := strconv.Atoi(builderVersion[1])
	builderMinor, _ := strconv.Atoi(builderVersion[2])
	if builderMajor < moduleMajor || (builderMajor == moduleMajor && builderMinor < moduleMinor) {
		t.Fatalf("api builder Go %d.%d does not support module Go %d.%d", builderMajor, builderMinor, moduleMajor, moduleMinor)
	}
}
