package recovery

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func readFile(t *testing.T, name string) string {
	t.Helper()
	content, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(content)
}

func TestPgBackRestContractUsesEncryptedRemoteRepository(t *testing.T) {
	text := readFile(t, "pgbackrest.conf.tmpl")
	for _, required := range []string{
		"repo1-type=s3",
		"repo1-cipher-type=aes-256-cbc",
		"repo1-retention-full=13",
		"repo1-retention-archive-type=full",
		"archive-async=y",
		"spool-path=",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("pgBackRest contract must include %q", required)
		}
	}
}

func TestBackupScheduleMatchesAcceptedCadence(t *testing.T) {
	text := readFile(t, "backup-schedule.yaml")
	for _, required := range []string{
		"type: full",
		"calendar: Sun *-*-* 02:00:00",
		"type: diff",
		"calendar: Mon..Sat *-*-* 02:00:00",
		"type: incr",
		"calendar: \"*-*-* 00,06,12,18:00:00\"",
		"restore-verification",
		"calendar: monthly",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("backup schedule must include %q", required)
		}
	}
}

func TestBackupRunnerIsLockedCheckedAndEvidenceProducing(t *testing.T) {
	text := readFile(t, "backup.sh")
	for _, required := range []string{
		"full|diff|incr",
		"flock -n",
		"pgbackrest",
		"--output=json",
		"backup-evidence-",
		"chmod 600",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("backup runner must include %q", required)
		}
	}
}

func TestRestoreVerificationIsCleanRoomAndEvidenceProducing(t *testing.T) {
	text := readFile(t, "restore-verify.sh")
	for _, required := range []string{
		"RARITY_RESTORE_ROOT",
		"refusing non-empty restore target",
		"--type=time",
		"--target-action=promote",
		"pg_checksums",
		"restore-evidence.json",
		"chmod 600",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("restore verification must include %q", required)
		}
	}
	if strings.Contains(text, "--delta") {
		t.Fatal("clean-room restore must not use destructive delta restore")
	}
}

func TestUpgradeGateRequiresBackupCompatibilityAndRollbackEvidence(t *testing.T) {
	text := readFile(t, "upgrade-gate.sh")
	for _, required := range []string{
		"pre-upgrade-backup",
		"migration-status",
		"expected-revision",
		"rollback-plan",
		"health-verification",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("upgrade gate must include %q", required)
		}
	}
}

func TestUpgradeGateAcceptsCompleteMatchingEvidence(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq is required to execute the upgrade gate")
	}
	evidence := t.TempDir()
	files := map[string]string{
		"pre-upgrade-backup.json": `{
			"status": "passed",
			"backup_type": "full",
			"completed_at": "2026-07-29T12:00:00Z"
		}`,
		"migration-status.json": `{
			"status": "compatible",
			"current_schema": "20",
			"target_schema": "21"
		}`,
		"expected-revision.txt": "revision-123\n",
		"rollback-plan.md": `# Release rollback
## Rollback trigger
Failed readiness.
## Rollback procedure
Restore the compatible image.
## Data compatibility
Migration is backward compatible.
`,
		"health-verification.json": `{
			"status": "passed",
			"revision": "revision-123"
		}`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(evidence, name), []byte(content), 0o600); err != nil {
			t.Fatalf("write evidence: %v", err)
		}
	}

	command := exec.Command("bash", "upgrade-gate.sh", evidence, "revision-123")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("upgrade gate failed: %v\n%s", err, output)
	}
}
