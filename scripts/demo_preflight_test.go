package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const testRevision = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestDemoPreflightRejectsMissingRuntimeConfiguration(t *testing.T) {
	repository := newPreflightRepository(t, strings.ReplaceAll(validDemoEnv(testRevision), "S3_BUCKET=rarity-attachments\n", ""))

	output, err := runPreflight(t, repository, testRevision)
	if err == nil {
		t.Fatalf("preflight accepted missing S3_BUCKET:\n%s", output)
	}
	if !strings.Contains(output, "S3_BUCKET") {
		t.Fatalf("preflight did not identify missing S3_BUCKET:\n%s", output)
	}
}

func TestDemoPreflightRejectsMissingBrowserReachablePublicURL(t *testing.T) {
	repository := newPreflightRepository(t, strings.ReplaceAll(
		validDemoEnv(testRevision),
		"RARITY_PUBLIC_URL=https://rarity.example\n",
		"",
	))

	output, err := runPreflight(t, repository, testRevision)
	if err == nil {
		t.Fatalf("preflight accepted missing RARITY_PUBLIC_URL:\n%s", output)
	}
	if !strings.Contains(output, "RARITY_PUBLIC_URL") {
		t.Fatalf("preflight did not identify missing RARITY_PUBLIC_URL:\n%s", output)
	}
}

func TestDemoPreflightRejectsRevisionMismatch(t *testing.T) {
	repository := newPreflightRepository(t, validDemoEnv("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"))

	output, err := runPreflight(t, repository, testRevision)
	if err == nil {
		t.Fatalf("preflight accepted mismatched build revision:\n%s", output)
	}
	if !strings.Contains(output, "RARITY_BUILD_REVISION") {
		t.Fatalf("preflight did not identify revision mismatch:\n%s", output)
	}
}

func newPreflightRepository(t *testing.T, env string) string {
	t.Helper()
	repository := t.TempDir()
	if err := os.Mkdir(filepath.Join(repository, "scripts"), 0o755); err != nil {
		t.Fatalf("create scripts directory: %v", err)
	}
	script, err := os.ReadFile("demo-preflight.sh")
	if err != nil {
		t.Fatalf("read demo preflight: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repository, "scripts", "demo-preflight.sh"), script, 0o755); err != nil {
		t.Fatalf("write demo preflight: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repository, ".gitignore"), []byte(".env\n"), 0o600); err != nil {
		t.Fatalf("write gitignore: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repository, ".env"), []byte(env), 0o600); err != nil {
		t.Fatalf("write demo env: %v", err)
	}
	runGit(t, repository, "init", "-q")
	runGit(t, repository, "config", "user.email", "preflight@example.invalid")
	runGit(t, repository, "config", "user.name", "Preflight Test")
	runGit(t, repository, "add", ".gitignore", "scripts/demo-preflight.sh")
	runGit(t, repository, "commit", "-qm", "test fixture")
	return repository
}

func runPreflight(t *testing.T, repository, revision string) (string, error) {
	t.Helper()
	config := filepath.Join(t.TempDir(), "server-configuration.md")
	body := `| Setting | Value |
| --- | --- |
| SSH host | demo.invalid |
| SSH user | demo |
| Repository path on server | /srv/rti |
| Health endpoint | http://127.0.0.1/healthz |
| Readiness endpoint | http://127.0.0.1/readyz |
| Revision/freshness verification | http://127.0.0.1/v1/system/build |
`
	if err := os.WriteFile(config, []byte(body), 0o600); err != nil {
		t.Fatalf("write server configuration: %v", err)
	}
	command := exec.Command("bash", "scripts/demo-preflight.sh", revision)
	command.Dir = repository
	command.Env = append(os.Environ(), "RTI_DEMO_SERVER_CONFIG="+config)
	output, err := command.CombinedOutput()
	return string(output), err
}

func runGit(t *testing.T, repository string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = repository
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func validDemoEnv(revision string) string {
	return strings.Join([]string{
		"RARITY_ENV=demo",
		"RARITY_HTTP_ADDR=:8080",
		"RARITY_HTTP_PORT=18080",
		"RARITY_DEMO_HTTP_PORT=18081",
		"RARITY_BUILD_REVISION=" + revision,
		"RARITY_PUBLIC_URL=https://rarity.example",
		"RARITY_SESSION_KEY=session-secret",
		"RARITY_SECRET_KEY=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"DATABASE_URL=postgres://rarity:secret@postgres:5432/rarity?sslmode=disable",
		"VALKEY_URL=redis://valkey:6379/0",
		"S3_ENDPOINT=http://minio:9000",
		"S3_BUCKET=rarity-attachments",
		"S3_REGION=us-east-1",
		"S3_ACCESS_KEY_ID=rarity",
		"S3_SECRET_ACCESS_KEY=object-secret",
		"POSTGRES_DB=rarity",
		"POSTGRES_USER=rarity",
		"POSTGRES_PASSWORD=database-secret",
		"MINIO_ROOT_USER=rarity",
		"MINIO_ROOT_PASSWORD=object-secret",
		"",
	}, "\n")
}
