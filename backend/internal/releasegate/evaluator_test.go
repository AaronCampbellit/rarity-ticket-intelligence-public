package releasegate

import (
	"strings"
	"testing"
	"time"
)

func TestEvaluateAcceptsCompleteRevisionBoundEvidence(t *testing.T) {
	evidence := completeEvidence("abc123")

	result, err := Evaluate(evidence, "abc123")

	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if !result.Passed || len(result.AcceptedGates) != len(RequiredGateIDs()) {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestEvaluateRejectsMissingAndStaleGates(t *testing.T) {
	evidence := completeEvidence("abc123")
	evidence.Gates = evidence.Gates[1:]
	evidence.Gates[0].Revision = "older"

	result, err := Evaluate(evidence, "abc123")

	if err == nil {
		t.Fatal("Evaluate() unexpectedly passed")
	}
	if result.Passed {
		t.Fatalf("failed evidence reported passed: %+v", result)
	}
	message := err.Error()
	if !strings.Contains(message, "security_scan") ||
		!strings.Contains(message, "revision") {
		t.Fatalf("error does not identify missing and stale evidence: %v", err)
	}
}

func TestEvaluateRejectsPendingOrUntraceableEvidence(t *testing.T) {
	evidence := completeEvidence("abc123")
	evidence.Gates[0].Status = StatusPending
	evidence.Gates[1].Artifact = ""

	_, err := Evaluate(evidence, "abc123")

	if err == nil ||
		!strings.Contains(err.Error(), "must pass") ||
		!strings.Contains(err.Error(), "artifact") {
		t.Fatalf("Evaluate() error = %v", err)
	}
}

func TestEvaluateRejectsMissingOrUnboundPublishedArtifacts(t *testing.T) {
	evidence := completeEvidence("abc123")
	evidence.Artifacts = evidence.Artifacts[:1]
	evidence.Artifacts[0].Digest = "latest"

	_, err := Evaluate(evidence, "abc123")

	if err == nil ||
		!strings.Contains(err.Error(), "frontend artifact is missing") ||
		!strings.Contains(err.Error(), "api artifact digest") {
		t.Fatalf("Evaluate() error = %v", err)
	}
}

func TestEvaluateRejectsUnboundOrDivergentCanonicalWorkflowIdentities(t *testing.T) {
	for name, mutate := range map[string]func(*Evidence){
		"wrong repository": func(evidence *Evidence) {
			evidence.Artifacts[0].SignatureIdentity = "https://github.com/Other/rarity/.github/workflows/publish.yml@refs/heads/main"
		},
		"wrong workflow": func(evidence *Evidence) {
			evidence.Artifacts[0].SignatureIdentity = "https://github.com/Rarity-Ticket-Intelligence/rarity/.github/workflows/other.yml@refs/heads/main"
		},
		"wrong ref": func(evidence *Evidence) {
			evidence.Artifacts[0].SignatureIdentity = "https://github.com/Rarity-Ticket-Intelligence/rarity/.github/workflows/publish.yml@refs/tags/v1"
		},
		"divergent identities": func(evidence *Evidence) {
			evidence.Artifacts[1].SignatureIdentity = "https://github.com/rarity-ticket-intelligence/rarity/.github/workflows/publish.yml@refs/heads/main"
		},
	} {
		t.Run(name, func(t *testing.T) {
			evidence := completeEvidence("abc123")
			mutate(&evidence)
			if _, err := Evaluate(evidence, "abc123"); err == nil {
				t.Fatal("Evaluate() unexpectedly accepted unbound workflow identity")
			}
		})
	}
}

func completeEvidence(revision string) Evidence {
	gates := make([]GateEvidence, 0, len(RequiredGateIDs()))
	for _, id := range RequiredGateIDs() {
		gates = append(gates, GateEvidence{
			ID: id, Status: StatusPassed, Revision: revision,
			Artifact:   "evidence/" + id + ".json",
			ObservedAt: time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC),
		})
	}
	return Evidence{
		Release: "v1.0.0-rc.1", Revision: revision,
		Environment: "pilot-acceptance", Gates: gates,
		Artifacts: []ArtifactEvidence{
			{
				Component: "api", Image: "ghcr.io/rarity-ticket-intelligence/rarity-api",
				Digest:            "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				SignatureIdentity: "https://github.com/Rarity-Ticket-Intelligence/rarity/.github/workflows/publish.yml@refs/heads/main",
				SignatureBundle:   "evidence/release/api-signature.json",
				SBOM:              "evidence/release/api-sbom.spdx.json",
				Provenance:        "evidence/release/api-provenance.json",
			},
			{
				Component: "frontend", Image: "ghcr.io/rarity-ticket-intelligence/rarity-frontend",
				Digest:            "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
				SignatureIdentity: "https://github.com/Rarity-Ticket-Intelligence/rarity/.github/workflows/publish.yml@refs/heads/main",
				SignatureBundle:   "evidence/release/frontend-signature.json",
				SBOM:              "evidence/release/frontend-sbom.spdx.json",
				Provenance:        "evidence/release/frontend-provenance.json",
			},
		},
	}
}
