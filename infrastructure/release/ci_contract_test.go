package release

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func readWorkflow(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile("../../.github/workflows/" + name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(body)
}

func requireFragments(t *testing.T, body string, fragments ...string) {
	t.Helper()
	for _, fragment := range fragments {
		if !strings.Contains(body, fragment) {
			t.Errorf("workflow missing %q", fragment)
		}
	}
}

func TestPullRequestCIIsReadOnlyAndCoversPortableAndDatabaseGates(t *testing.T) {
	body := readWorkflow(t, "ci.yml")
	requireFragments(t, body,
		"pull_request:",
		"contents: read",
		"go mod verify",
		"git diff --check",
		"gofmt -l",
		"go test -race ./backend/...",
		"scripts/run-postgres-acceptance.sh",
		"go test ./infrastructure/... -count=1",
		"TEST_DATABASE_URL:",
		"go vet ./backend/... ./tests/...",
		"npm audit --audit-level=high",
		"npx prettier --check",
		"npm test -- --run",
		"npm run build",
		"docker build --file infrastructure/compose/api.Dockerfile",
		"docker build --file infrastructure/compose/frontend.Dockerfile",
		"npx playwright test",
		"node --test scripts/validate-release-acceptance-docs.test.mjs",
		"node scripts/validate-docs.mjs",
		"node scripts/validate-markdown-links.mjs",
		"node scripts/validate-integration-automation-docs.mjs",
		"aquasecurity/trivy-action@",
		"scan-type: fs",
		"scanners: vuln,secret,misconfig,license",
	)
	if strings.Count(body, "persist-credentials: false") != 3 {
		t.Errorf("every PR-reachable checkout must disable persisted credentials")
	}
	for _, forbidden := range []string{
		"pull_request_target:",
		"packages: write",
		"id-token: write",
		"contents: write",
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("pull-request CI contains unsafe permission/event %q", forbidden)
		}
	}
}

func TestBrowserCIRunsDatabaseBackedGoAcceptanceServer(t *testing.T) {
	body := readWorkflow(t, "ci.yml")
	browserStart := strings.Index(body, "\n  browser:\n")
	if browserStart < 0 {
		t.Fatal("CI workflow is missing the browser job")
	}
	browser := body[browserStart:]
	requireFragments(t, browser,
		"services:",
		"image: postgres:17-alpine@sha256:742f40ea20b9ff2ff31db5458d127452988a2164df9e17441e191f3b72252193",
		"POSTGRES_DB: rarity_test",
		"- 5432:5432",
		"TEST_DATABASE_URL: postgres://postgres:postgres@127.0.0.1:5432/rarity_test?sslmode=disable",
		"name: Set up Go",
		"uses: actions/setup-go@924ae3a1cded613372ab5595356fb5720e22ba16",
		"go-version-file: go.mod",
	)
}

func TestPublishBuildsReviewedMainImagesAndCreatesSupplyChainEvidence(t *testing.T) {
	body := readWorkflow(t, "publish.yml")
	requireFragments(t, body,
		"workflow_run:",
		"branches: [main]",
		"github.event.workflow_run.conclusion == 'success'",
		"github.event.workflow_run.event == 'push'",
		"github.event.workflow_run.head_sha",
		"packages: write",
		"id-token: write",
		"ghcr.io",
		"infrastructure/compose/api.Dockerfile",
		"infrastructure/compose/frontend.Dockerfile",
		"context: https://github.com/${{ github.repository }}.git#${{ github.event.workflow_run.head_sha }}",
		"github-token: ${{ secrets.GITHUB_TOKEN }}",
		"push: true",
		"candidate-${{ github.event.workflow_run.head_sha }}",
		"sbom: true",
		"provenance: mode=max",
		"aquasecurity/trivy-action@",
		"cosign sign --yes",
		"cosign verify",
		"--certificate-identity",
		"--certificate-oidc-issuer",
		"docker buildx imagetools create",
	)
	for _, forbidden := range []string{"pull_request_target:", "pull_request:", "\n  push:"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("artifact publication must not run from pull requests: found %q", forbidden)
		}
	}
	scan := strings.Index(body, "Scan the published digest")
	sign := strings.Index(body, "Keyless-sign the immutable digest")
	promote := strings.Index(body, "Promote the verified digest")
	if scan < 0 || sign <= scan || promote <= sign {
		t.Errorf("published digest must be scanned and signed before its trusted tags are promoted")
	}
}

func TestPublishNormalizesOCIRepositoryNamesBeforeScanningAndPromotion(t *testing.T) {
	body := readWorkflow(t, "publish.yml")
	requireFragments(t, body,
		`tr '[:upper:]' '[:lower:]'`,
		"images: ${{ steps.image.outputs.ref }}",
		"image-ref: ${{ steps.image.outputs.ref }}@${{ steps.build.outputs.digest }}",
		"IMAGE: ${{ steps.image.outputs.ref }}",
	)
	if strings.Contains(body, "image-ref: ${{ env.REGISTRY }}/${{ github.repository }}") {
		t.Errorf("Trivy image references must not preserve uppercase GitHub repository characters")
	}
}

func TestAllExternalActionsArePinnedToFullCommitSHAs(t *testing.T) {
	for _, name := range []string{"ci.yml", "publish.yml"} {
		body := readWorkflow(t, name)
		for _, line := range strings.Split(body, "\n") {
			trimmed := strings.TrimSpace(line)
			if !strings.HasPrefix(trimmed, "uses: ") || strings.HasPrefix(trimmed, "uses: ./") {
				continue
			}
			reference := strings.TrimPrefix(trimmed, "uses: ")
			if !regexp.MustCompile(`^[^@\s]+@[0-9a-f]{40}(?:\s+#.*)?$`).MatchString(reference) {
				t.Errorf("%s action is not pinned to a full commit SHA: %s", name, reference)
			}
		}
	}
}

func TestReleaseComposeAndVerifierUseOnlyTrustedDigests(t *testing.T) {
	compose, err := os.ReadFile("../compose/compose.release.yaml")
	if err != nil {
		t.Fatalf("read release Compose override: %v", err)
	}
	requireFragments(t, string(compose),
		"RARITY_API_IMAGE",
		"RARITY_FRONTEND_IMAGE",
		"build: !reset null",
	)
	verifier, err := os.ReadFile("../../scripts/verify-release-artifacts.sh")
	if err != nil {
		t.Fatalf("read release artifact verifier: %v", err)
	}
	requireFragments(t, string(verifier),
		"@sha256:",
		"cosign verify",
		"--certificate-identity",
		"--certificate-oidc-issuer",
		"https://token.actions.githubusercontent.com",
		"{{ json .SBOM }}",
		"{{ json .Provenance.SLSA }}",
		"artifact-provenance.json",
		"40-character-revision",
		"provenance does not identify the requested revision",
	)
}
