package releasegate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateFilesBindsRetainedArtifactManifestToRelease(t *testing.T) {
	root := t.TempDir()
	evidence := completeEvidence("abc123")
	for _, gate := range evidence.Gates {
		if gate.ID != "artifact_provenance" {
			writeEvidenceFile(t, root, gate.Artifact, []byte(`{"passed":true}`))
		}
	}
	for _, artifact := range evidence.Artifacts {
		writeEvidenceFile(t, root, artifact.SignatureBundle, []byte(`{"verified":true}`))
		writeEvidenceFile(t, root, artifact.SBOM, []byte(`{
			"SPDX":{"SPDXID":"SPDXRef-DOCUMENT","spdxVersion":"SPDX-2.3","packages":[]}
		}`))
		writeEvidenceFile(t, root, artifact.Provenance, []byte(`{
			"buildDefinition":{
				"buildType":"https://github.com/moby/buildkit/blob/master/docs/attestations/slsa-definitions.md",
				"resolvedDependencies":[{"uri":"https://github.com/AaronCampbellit/rarity-ticket-intelligence.git#abc123","digest":{"sha1":"abc123"}}],
				"externalParameters":{"configSource":{"digest":{"sha1":"abc123"}},"request":{"args":{"build-arg:RARITY_BUILD_REVISION":"abc123","label:org.opencontainers.image.revision":"abc123"}}},
				"internalParameters":{"github_workflow_sha":"abc123"}
			}
		}`))
	}
	manifest := ArtifactManifest{Revision: evidence.Revision, Artifacts: evidence.Artifacts}
	body, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	provenance := gateByID(t, evidence, "artifact_provenance").Artifact
	writeEvidenceFile(t, root, provenance, body)

	if err := ValidateFiles(evidence, filepath.Join(root, "release-evidence.json")); err != nil {
		t.Fatalf("ValidateFiles() error = %v", err)
	}
}

func TestValidateFilesRejectsUnboundBuildAttestations(t *testing.T) {
	root := t.TempDir()
	evidence := completeEvidence("abc123")
	for _, gate := range evidence.Gates {
		writeEvidenceFile(t, root, gate.Artifact, []byte(`{"passed":true}`))
	}
	manifest, err := json.Marshal(ArtifactManifest{Revision: evidence.Revision, Artifacts: evidence.Artifacts})
	if err != nil {
		t.Fatal(err)
	}
	writeEvidenceFile(t, root, gateByID(t, evidence, "artifact_provenance").Artifact, manifest)
	for _, artifact := range evidence.Artifacts {
		writeEvidenceFile(t, root, artifact.SignatureBundle, []byte(`{"verified":true}`))
		writeEvidenceFile(t, root, artifact.SBOM, []byte(`{"SPDX":{"SPDXID":"wrong"}}`))
		writeEvidenceFile(t, root, artifact.Provenance, []byte(`{
			"buildType":"https://mobyproject.org/buildkit@v1",
			"materials":[],
			"metadata":{"vcs":{"revision":"other"}}
		}`))
	}

	err = ValidateFiles(evidence, filepath.Join(root, "release-evidence.json"))
	if err == nil ||
		!strings.Contains(err.Error(), "SBOM extraction") ||
		!strings.Contains(err.Error(), "provenance extraction") {
		t.Fatalf("ValidateFiles() error = %v", err)
	}
}

func TestValidProvenanceSupportsLegacyAndRejectsIncompleteCurrentBuildKitPredicates(t *testing.T) {
	root := t.TempDir()
	write := func(name, body string) string {
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}

	if !validProvenance(write("legacy.json", `{
		"buildType":"https://mobyproject.org/buildkit@v1",
		"materials":[],
		"metadata":{"vcs":{"revision":"abc123"}}
	}`), "abc123") {
		t.Fatal("validProvenance() rejected legitimate legacy BuildKit evidence")
	}
	for name, body := range map[string]string{
		"missing-dependencies.json": `{
			"buildDefinition":{"buildType":"https://github.com/moby/buildkit/blob/master/docs/attestations/slsa-definitions.md"}
		}`,
		"mismatched-label.json": `{
			"buildDefinition":{
				"buildType":"https://github.com/moby/buildkit/blob/master/docs/attestations/slsa-definitions.md",
				"resolvedDependencies":[{"uri":"https://github.com/example/repository.git#abc123","digest":{"sha1":"abc123"}}],
				"externalParameters":{"configSource":{"digest":{"sha1":"abc123"}},"request":{"args":{"build-arg:RARITY_BUILD_REVISION":"abc123","label:org.opencontainers.image.revision":"other"}}},
				"internalParameters":{"github_workflow_sha":"abc123"}
			}
		}`,
		"malformed.json": `{`,
	} {
		if validProvenance(write(name, body), "abc123") {
			t.Fatalf("validProvenance() accepted %s", name)
		}
	}
}

func TestValidateFilesRejectsManifestMismatchAndMissingEvidence(t *testing.T) {
	root := t.TempDir()
	evidence := completeEvidence("abc123")
	manifest := ArtifactManifest{
		Revision:  "other",
		Artifacts: append([]ArtifactEvidence(nil), evidence.Artifacts...),
	}
	manifest.Artifacts[0].Digest = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	body, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	writeEvidenceFile(t, root, gateByID(t, evidence, "artifact_provenance").Artifact, body)

	err = ValidateFiles(evidence, filepath.Join(root, "release-evidence.json"))
	if err == nil ||
		!strings.Contains(err.Error(), "revision") ||
		!strings.Contains(err.Error(), "does not match release artifacts") ||
		!strings.Contains(err.Error(), "retained file") {
		t.Fatalf("ValidateFiles() error = %v", err)
	}
}

func gateByID(t *testing.T, evidence Evidence, id string) GateEvidence {
	t.Helper()
	for _, gate := range evidence.Gates {
		if gate.ID == id {
			return gate
		}
	}
	t.Fatalf("missing gate %s", id)
	return GateEvidence{}
}

func writeEvidenceFile(t *testing.T, root, name string, body []byte) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
}
