package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/releasegate"
)

func TestRunEmitsPassedResultForCompleteEvidence(t *testing.T) {
	revision := "abc123"
	gates := make([]releasegate.GateEvidence, 0, len(releasegate.RequiredGateIDs()))
	for _, id := range releasegate.RequiredGateIDs() {
		gates = append(gates, releasegate.GateEvidence{
			ID: id, Status: releasegate.StatusPassed, Revision: revision,
			Artifact: "evidence/" + id + ".json", ObservedAt: time.Now().UTC(),
		})
	}
	evidence := releasegate.Evidence{
		Release: "v1.0.0-rc.1", Revision: revision,
		Environment: "pilot-acceptance", Gates: gates,
		Artifacts: []releasegate.ArtifactEvidence{
			{
				Component: "api", Image: "ghcr.io/rarity-ticket-intelligence/rarity-api",
				Digest:            "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				SignatureIdentity: "https://github.com/rarity-ticket-intelligence/rarity/.github/workflows/publish.yml@refs/heads/main",
				SignatureBundle:   "evidence/api-signature.json",
				SBOM:              "evidence/api-sbom.json", Provenance: "evidence/api-provenance.json",
			},
			{
				Component: "frontend", Image: "ghcr.io/rarity-ticket-intelligence/rarity-frontend",
				Digest:            "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
				SignatureIdentity: "https://github.com/rarity-ticket-intelligence/rarity/.github/workflows/publish.yml@refs/heads/main",
				SignatureBundle:   "evidence/frontend-signature.json",
				SBOM:              "evidence/frontend-sbom.json", Provenance: "evidence/frontend-provenance.json",
			},
		},
	}
	data, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	for _, gate := range evidence.Gates {
		if err := writeMainEvidence(root, gate.Artifact, []byte(`{"passed":true}`)); err != nil {
			t.Fatal(err)
		}
	}
	for _, artifact := range evidence.Artifacts {
		if err := writeMainEvidence(root, artifact.SignatureBundle, []byte(`{"verified":true}`)); err != nil {
			t.Fatal(err)
		}
		if err := writeMainEvidence(root, artifact.SBOM, []byte(`{
			"SPDX":{"SPDXID":"SPDXRef-DOCUMENT","spdxVersion":"SPDX-2.3","packages":[]}
		}`)); err != nil {
			t.Fatal(err)
		}
		if err := writeMainEvidence(root, artifact.Provenance, []byte(`{
			"buildType":"https://mobyproject.org/buildkit@v1",
			"materials":[],
			"metadata":{"https://mobyproject.org/buildkit@v1#metadata":{"vcs":{"revision":"abc123"}}}
		}`)); err != nil {
			t.Fatal(err)
		}
	}
	provenance, err := json.Marshal(releasegate.ArtifactManifest{Revision: revision, Artifacts: evidence.Artifacts})
	if err != nil {
		t.Fatal(err)
	}
	for _, gate := range evidence.Gates {
		if gate.ID == "artifact_provenance" {
			if err := writeMainEvidence(root, gate.Artifact, provenance); err != nil {
				t.Fatal(err)
			}
		}
	}
	path := filepath.Join(root, "release-evidence.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer

	code := run([]string{"-evidence", path, "-revision", revision}, &stdout, &stderr)

	if code != 0 || !strings.Contains(stdout.String(), `"passed": true`) {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func writeMainEvidence(root, name string, body []byte) error {
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o600)
}
