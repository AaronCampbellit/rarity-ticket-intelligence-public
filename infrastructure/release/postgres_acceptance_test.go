package release

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPostgresAcceptanceRunsPackageIsolationCommandFromRepositoryRoot(t *testing.T) {
	temporaryDirectory := t.TempDir()
	capturePath := filepath.Join(temporaryDirectory, "invocation")
	fakeGoPath := filepath.Join(temporaryDirectory, "go")
	fakeGo := `#!/usr/bin/env bash
set -euo pipefail
{
  pwd
  printf '%s\n' "$@"
} >"$POSTGRES_ACCEPTANCE_CAPTURE"
`
	if err := os.WriteFile(fakeGoPath, []byte(fakeGo), 0o700); err != nil {
		t.Fatalf("write fake go: %v", err)
	}

	command := exec.Command("bash", "../../scripts/run-postgres-acceptance.sh")
	for _, variable := range os.Environ() {
		if strings.HasPrefix(variable, "PATH=") ||
			strings.HasPrefix(variable, "POSTGRES_ACCEPTANCE_CAPTURE=") {
			continue
		}
		command.Env = append(command.Env, variable)
	}
	command.Env = append(command.Env,
		"PATH="+temporaryDirectory+string(os.PathListSeparator)+os.Getenv("PATH"),
		"POSTGRES_ACCEPTANCE_CAPTURE="+capturePath,
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("run PostgreSQL acceptance wrapper: %v\n%s", err, output)
	}
	capture, err := os.ReadFile(capturePath)
	if err != nil {
		t.Fatalf("read captured go invocation: %v", err)
	}
	repositoryRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	want := strings.Join([]string{
		repositoryRoot,
		"run",
		"./infrastructure/cmd/postgres-acceptance",
		"./backend/...",
		"./tests/...",
		"",
	}, "\n")
	if string(capture) != want {
		t.Fatalf("captured invocation:\n%s\nwant:\n%s", capture, want)
	}
}
