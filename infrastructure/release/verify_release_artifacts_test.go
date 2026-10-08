package release

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyReleaseArtifactsPreservesCanonicalSigningIdentityWithLowercaseRegistryRepository(t *testing.T) {
	const (
		repository     = "AaronCampbellit/rarity-ticket-intelligence"
		revision       = "0e0a233783e354981b06e7e276b77e552a5cd978"
		apiDigest      = "sha256:457b589e0adb38c8753f1986b8bb325666d704ae5d9eb9d1a041865259ec400d"
		frontendDigest = "sha256:77e6d7c3e58cc2d15006a4453926e42e2c71bd896711dcc15b30324c21486566"
		identity       = "https://github.com/AaronCampbellit/rarity-ticket-intelligence/.github/workflows/publish.yml@refs/heads/main"
		issuer         = "https://token.actions.githubusercontent.com"
		apiImage       = "ghcr.io/aaroncampbellit/rarity-ticket-intelligence-api@" + apiDigest
		frontendImage  = "ghcr.io/aaroncampbellit/rarity-ticket-intelligence-frontend@" + frontendDigest
	)

	tools := t.TempDir()
	writeExecutable(t, filepath.Join(tools, "cosign"), `#!/bin/sh
if [ "$1" != verify ] || [ "$2" != --certificate-identity ] ||
  [ "$3" != 'https://github.com/AaronCampbellit/rarity-ticket-intelligence/.github/workflows/publish.yml@refs/heads/main' ] ||
  [ "$4" != --certificate-oidc-issuer ] ||
  [ "$5" != 'https://token.actions.githubusercontent.com' ]; then
  echo 'unexpected certificate verification arguments' >&2
  exit 1
fi
case "$6" in
  'ghcr.io/aaroncampbellit/rarity-ticket-intelligence-api@sha256:457b589e0adb38c8753f1986b8bb325666d704ae5d9eb9d1a041865259ec400d')
    digest='sha256:457b589e0adb38c8753f1986b8bb325666d704ae5d9eb9d1a041865259ec400d'
    ;;
  'ghcr.io/aaroncampbellit/rarity-ticket-intelligence-frontend@sha256:77e6d7c3e58cc2d15006a4453926e42e2c71bd896711dcc15b30324c21486566')
    digest='sha256:77e6d7c3e58cc2d15006a4453926e42e2c71bd896711dcc15b30324c21486566'
    ;;
  *)
    echo 'unexpected image reference' >&2
    exit 1
    ;;
esac
printf '%s\n' "[{\"critical\":{\"identity\":{\"docker-reference\":\"$6\"},\"image\":{\"docker-manifest-digest\":\"$digest\"}},\"optional\":{\"Issuer\":\"https://token.actions.githubusercontent.com\",\"Subject\":\"https://github.com/AaronCampbellit/rarity-ticket-intelligence/.github/workflows/publish.yml@refs/heads/main\"}}]"
`)
	writeExecutable(t, filepath.Join(tools, "docker"), `#!/bin/sh
if [ "$1" != buildx ] || [ "$2" != imagetools ] || [ "$3" != inspect ] || [ "$5" != --format ]; then
  echo 'unexpected docker arguments' >&2
  exit 1
fi
case "$4" in
  'ghcr.io/aaroncampbellit/rarity-ticket-intelligence-api@sha256:457b589e0adb38c8753f1986b8bb325666d704ae5d9eb9d1a041865259ec400d'|'ghcr.io/aaroncampbellit/rarity-ticket-intelligence-frontend@sha256:77e6d7c3e58cc2d15006a4453926e42e2c71bd896711dcc15b30324c21486566')
    ;;
  *)
    echo 'unexpected image reference' >&2
    exit 1
    ;;
esac
case "$6" in
  *SBOM*)
    printf '%s\n' '{"SPDX":{"SPDXID":"SPDXRef-DOCUMENT","spdxVersion":"SPDX-2.3","packages":[]}}'
    ;;
  *Provenance.SLSA*)
    printf '%s\n' '{"buildType":"https://mobyproject.org/buildkit@v1","buildDefinition":{"externalParameters":{"configSource":{"digest":{"sha1":"0e0a233783e354981b06e7e276b77e552a5cd978"}},"request":{"args":{"build-arg:RARITY_BUILD_REVISION":"0e0a233783e354981b06e7e276b77e552a5cd978","label:org.opencontainers.image.revision":"0e0a233783e354981b06e7e276b77e552a5cd978"}}},"internalParameters":{"github_workflow_sha":"0e0a233783e354981b06e7e276b77e552a5cd978"}}}'
    ;;
  *)
    echo 'unexpected docker format' >&2
    exit 1
    ;;
esac
`)

	evidence := t.TempDir()
	command := exec.Command(
		"bash",
		"../../scripts/verify-release-artifacts.sh",
		repository,
		revision,
		apiImage,
		frontendImage,
		evidence,
	)
	command.Env = append(os.Environ(), "PATH="+tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("verifier failed: %v\n%s", err, output)
	}

	entries, err := os.ReadDir(evidence)
	if err != nil {
		t.Fatal(err)
	}
	wantFiles := map[string]bool{
		"api-signature.json":       true,
		"api-sbom.spdx.json":       true,
		"api-provenance.json":      true,
		"frontend-signature.json":  true,
		"frontend-sbom.spdx.json":  true,
		"frontend-provenance.json": true,
		"artifact-provenance.json": true,
	}
	if len(entries) != len(wantFiles) {
		t.Fatalf("published %d evidence files, want %d: %v", len(entries), len(wantFiles), entryNames(entries))
	}
	for _, entry := range entries {
		if !wantFiles[entry.Name()] {
			t.Fatalf("unexpected evidence file %q", entry.Name())
		}
	}

	manifestBytes, err := os.ReadFile(filepath.Join(evidence, "artifact-provenance.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Revision  string `json:"revision"`
		Artifacts []struct {
			Component         string `json:"component"`
			Image             string `json:"image"`
			Digest            string `json:"digest"`
			SignatureIdentity string `json:"signature_identity"`
		} `json:"artifacts"`
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Revision != revision {
		t.Fatalf("manifest revision = %q, want %q", manifest.Revision, revision)
	}
	if len(manifest.Artifacts) != 2 {
		t.Fatalf("manifest artifacts = %d, want 2", len(manifest.Artifacts))
	}
	wantArtifacts := []struct {
		component string
		image     string
		digest    string
	}{
		{"api", "ghcr.io/aaroncampbellit/rarity-ticket-intelligence-api", apiDigest},
		{"frontend", "ghcr.io/aaroncampbellit/rarity-ticket-intelligence-frontend", frontendDigest},
	}
	for index, want := range wantArtifacts {
		artifact := manifest.Artifacts[index]
		if artifact.Component != want.component || artifact.Image != want.image || artifact.Digest != want.digest || artifact.SignatureIdentity != identity {
			t.Fatalf("manifest artifact %d = %+v, want component=%q image=%q digest=%q signature_identity=%q", index, artifact, want.component, want.image, want.digest, identity)
		}
	}
	assertSignatureRecord(t, filepath.Join(evidence, "api-signature.json"), apiImage, apiDigest)
	assertSignatureRecord(t, filepath.Join(evidence, "frontend-signature.json"), frontendImage, frontendDigest)
}

func TestVerifyReleaseArtifactsPublishesNoPartialEvidenceOnFailure(t *testing.T) {
	tools := t.TempDir()
	writeExecutable(t, filepath.Join(tools, "cosign"), `#!/bin/sh
printf '%s\n' '[{"critical":{"identity":{"docker-reference":"test"},"image":{"docker-manifest-digest":"sha256:test"}}}]'
`)
	writeExecutable(t, filepath.Join(tools, "docker"), `#!/bin/sh
case "$*" in
  *frontend*Provenance.SLSA*)
    printf '%s\n' '{"buildType":"https://mobyproject.org/buildkit@v1","buildDefinition":{"externalParameters":{"configSource":{"digest":{"sha1":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}},"request":{"args":{"build-arg:RARITY_BUILD_REVISION":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","label:org.opencontainers.image.revision":"wrong"}}},"internalParameters":{"github_workflow_sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}}'
    ;;
  *Provenance.SLSA*)
    printf '%s\n' '{"buildType":"https://mobyproject.org/buildkit@v1","buildDefinition":{"externalParameters":{"configSource":{"digest":{"sha1":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}},"request":{"args":{"build-arg:RARITY_BUILD_REVISION":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","label:org.opencontainers.image.revision":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}},"internalParameters":{"github_workflow_sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}}'
    ;;
  *SBOM*)
    printf '%s\n' '{"SPDX":{"SPDXID":"SPDXRef-DOCUMENT","spdxVersion":"SPDX-2.3","packages":[]}}'
    ;;
esac
`)

	evidence := t.TempDir()
	command := exec.Command(
		"bash",
		"../../scripts/verify-release-artifacts.sh",
		"acme/rarity",
		strings.Repeat("a", 40),
		"ghcr.io/acme/rarity-api@sha256:"+strings.Repeat("b", 64),
		"ghcr.io/acme/rarity-frontend@sha256:"+strings.Repeat("c", 64),
		evidence,
	)
	command.Env = append(os.Environ(), "PATH="+tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	if output, err := command.CombinedOutput(); err == nil {
		t.Fatalf("verifier succeeded with mismatched frontend provenance: %s", output)
	} else if !strings.Contains(string(output), "frontend provenance does not identify the requested revision") {
		t.Fatalf("verifier rejected the wrong component or reason: %s", output)
	}

	entries, err := os.ReadDir(evidence)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed verification published partial evidence: %v", entryNames(entries))
	}
}

func TestVerifyReleaseArtifactsRejectsExtraProvenanceDocuments(t *testing.T) {
	tools := t.TempDir()
	writePermissiveVerifierFakes(t, tools)
	evidence := t.TempDir()
	command := verifierCommand(evidence)
	command.Env = append(os.Environ(), "PATH="+tools+string(os.PathListSeparator)+os.Getenv("PATH"), "VERIFIER_TEST_EXTRA_PROVENANCE=1")
	if output, err := command.CombinedOutput(); err == nil {
		t.Fatalf("verifier accepted multiple provenance documents: %s", output)
	}
	assertNoEvidence(t, evidence)
}

func TestVerifyReleaseArtifactsRejectsDigestReferenceWithExtraSeparator(t *testing.T) {
	tools := t.TempDir()
	writePermissiveVerifierFakes(t, tools)
	evidence := t.TempDir()
	command := verifierCommand(evidence)
	command.Args[4] = "ghcr.io/acme/rarity-api@sha256:junk@sha256:" + strings.Repeat("b", 64)
	command.Env = append(os.Environ(), "PATH="+tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	if output, err := command.CombinedOutput(); err == nil {
		t.Fatalf("verifier accepted malformed immutable image reference: %s", output)
	}
	assertNoEvidence(t, evidence)
}

func TestVerifyReleaseArtifactsRejectsReferenceWithRegexWildcardMutation(t *testing.T) {
	tools := t.TempDir()
	writePermissiveVerifierFakes(t, tools)
	evidence := t.TempDir()
	command := verifierCommand(evidence)
	command.Args[4] = "ghcrXio/acme/rarity-api@sha256:" + strings.Repeat("b", 64)
	command.Env = append(os.Environ(), "PATH="+tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	if output, err := command.CombinedOutput(); err == nil {
		t.Fatalf("verifier accepted reference with wildcard mutation: %s", output)
	}
	assertNoEvidence(t, evidence)
}

func TestVerifyReleaseArtifactsPublishesEvidenceAtomically(t *testing.T) {
	tools := t.TempDir()
	writePermissiveVerifierFakes(t, tools)
	writeExecutable(t, filepath.Join(tools, "mv"), `#!/bin/sh
if [ "$1" = -T ]; then
  echo 'forced publication failure' >&2
  exit 1
fi
exec /usr/bin/mv "$@"
`)
	evidence := t.TempDir()
	command := verifierCommand(evidence)
	command.Env = append(os.Environ(), "PATH="+tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	if output, err := command.CombinedOutput(); err == nil {
		t.Fatalf("verifier succeeded despite forced publication failure: %s", output)
	}
	assertNoEvidence(t, evidence)
}

func verifierCommand(evidence string) *exec.Cmd {
	return exec.Command(
		"bash",
		"../../scripts/verify-release-artifacts.sh",
		"acme/rarity",
		strings.Repeat("a", 40),
		"ghcr.io/acme/rarity-api@sha256:"+strings.Repeat("b", 64),
		"ghcr.io/acme/rarity-frontend@sha256:"+strings.Repeat("c", 64),
		evidence,
	)
}

func writePermissiveVerifierFakes(t *testing.T, tools string) {
	t.Helper()
	writeExecutable(t, filepath.Join(tools, "cosign"), `#!/bin/sh
printf '%s\n' '[]'
`)
	writeExecutable(t, filepath.Join(tools, "docker"), `#!/bin/sh
case "$6" in
  *SBOM*)
    printf '%s\n' '{"SPDX":{"SPDXID":"SPDXRef-DOCUMENT","spdxVersion":"SPDX-2.3","packages":[]}}'
    ;;
  *Provenance.SLSA*)
    if [ "${VERIFIER_TEST_EXTRA_PROVENANCE:-}" = 1 ]; then
      printf '%s\n' '{"buildDefinition":{"externalParameters":{"configSource":{"digest":{"sha1":"wrong"}},"request":{"args":{"build-arg:RARITY_BUILD_REVISION":"wrong","label:org.opencontainers.image.revision":"wrong"}}},"internalParameters":{"github_workflow_sha":"wrong"}}}'
    fi
    printf '%s\n' '{"buildDefinition":{"externalParameters":{"configSource":{"digest":{"sha1":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}},"request":{"args":{"build-arg:RARITY_BUILD_REVISION":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","label:org.opencontainers.image.revision":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}},"internalParameters":{"github_workflow_sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}}'
    ;;
esac
`)
}

func assertNoEvidence(t *testing.T, evidence string) {
	t.Helper()
	entries, err := os.ReadDir(evidence)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed verification published partial evidence: %v", entryNames(entries))
	}
}

func assertSignatureRecord(t *testing.T, path, reference, digest string) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var records []struct {
		Critical struct {
			Identity struct {
				DockerReference string `json:"docker-reference"`
			} `json:"identity"`
			Image struct {
				DockerManifestDigest string `json:"docker-manifest-digest"`
			} `json:"image"`
		} `json:"critical"`
	}
	if err := json.Unmarshal(body, &records); err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Critical.Identity.DockerReference != reference || records[0].Critical.Image.DockerManifestDigest != digest {
		t.Fatalf("signature record = %+v, want reference=%q digest=%q", records, reference, digest)
	}
}

func writeExecutable(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
}

func entryNames(entries []os.DirEntry) []string {
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}
