// Package releasegate validates revision-bound production-readiness evidence.
package releasegate

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
)

type Status string

const (
	StatusPassed  Status = "passed"
	StatusPending Status = "pending"
	StatusFailed  Status = "failed"
)

var requiredGateIDs = []string{
	"security_scan",
	"dependency_scan",
	"client_isolation",
	"accessibility_automated",
	"accessibility_manual",
	"backup",
	"restore_dr",
	"ha_failover",
	"upgrade_rollback",
	"capacity",
	"observability_alerting",
	"migration",
	"artifact_provenance",
	"runbook_review",
}

type GateEvidence struct {
	ID         string    `json:"id"`
	Status     Status    `json:"status"`
	Revision   string    `json:"revision"`
	Artifact   string    `json:"artifact"`
	ObservedAt time.Time `json:"observed_at"`
}

type Evidence struct {
	Release     string             `json:"release"`
	Revision    string             `json:"revision"`
	Environment string             `json:"environment"`
	Gates       []GateEvidence     `json:"gates"`
	Artifacts   []ArtifactEvidence `json:"artifacts"`
}

type ArtifactEvidence struct {
	Component         string `json:"component"`
	Image             string `json:"image"`
	Digest            string `json:"digest"`
	SignatureIdentity string `json:"signature_identity"`
	SignatureBundle   string `json:"signature_bundle"`
	SBOM              string `json:"sbom"`
	Provenance        string `json:"provenance"`
}

type Result struct {
	Passed            bool     `json:"passed"`
	Release           string   `json:"release"`
	Revision          string   `json:"revision"`
	Environment       string   `json:"environment"`
	AcceptedGates     []string `json:"accepted_gates"`
	AcceptedArtifacts []string `json:"accepted_artifacts"`
}

func RequiredGateIDs() []string {
	return append([]string(nil), requiredGateIDs...)
}

func Evaluate(evidence Evidence, expectedRevision string) (Result, error) {
	result := Result{
		Release: evidence.Release, Revision: evidence.Revision,
		Environment: evidence.Environment,
	}
	var problems []string
	if strings.TrimSpace(evidence.Release) == "" {
		problems = append(problems, "release is required")
	}
	if strings.TrimSpace(expectedRevision) == "" ||
		evidence.Revision != expectedRevision {
		problems = append(problems, "release revision does not match expected revision")
	}
	if strings.TrimSpace(evidence.Environment) == "" {
		problems = append(problems, "environment is required")
	}

	byID := make(map[string]GateEvidence, len(evidence.Gates))
	for _, gate := range evidence.Gates {
		if _, exists := byID[gate.ID]; exists {
			problems = append(problems, fmt.Sprintf("%s evidence is duplicated", gate.ID))
			continue
		}
		byID[gate.ID] = gate
	}
	for _, id := range requiredGateIDs {
		gate, exists := byID[id]
		if !exists {
			problems = append(problems, fmt.Sprintf("%s evidence is missing", id))
			continue
		}
		if gate.Status != StatusPassed {
			problems = append(problems, fmt.Sprintf("%s must pass", id))
		}
		if gate.Revision != expectedRevision {
			problems = append(problems, fmt.Sprintf("%s revision does not match", id))
		}
		if !safeArtifactPath(gate.Artifact) {
			problems = append(problems, fmt.Sprintf("%s artifact must be a safe relative path", id))
		}
		if gate.ObservedAt.IsZero() {
			problems = append(problems, fmt.Sprintf("%s observed_at is required", id))
		}
		if gate.Status == StatusPassed && gate.Revision == expectedRevision &&
			safeArtifactPath(gate.Artifact) && !gate.ObservedAt.IsZero() {
			result.AcceptedGates = append(result.AcceptedGates, id)
		}
	}
	validateArtifacts(evidence.Artifacts, &result, &problems)
	sort.Strings(problems)
	if len(problems) > 0 {
		return result, errors.New(strings.Join(problems, "; "))
	}
	result.Passed = true
	return result, nil
}

var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
var canonicalRepositoryPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$`)

const (
	workflowIdentityPrefix = "https://github.com/"
	workflowIdentitySuffix = "/.github/workflows/publish.yml@refs/heads/main"
)

func validateArtifacts(artifacts []ArtifactEvidence, result *Result, problems *[]string) {
	byComponent := make(map[string]ArtifactEvidence, len(artifacts))
	for _, artifact := range artifacts {
		if _, exists := byComponent[artifact.Component]; exists {
			*problems = append(*problems, fmt.Sprintf("%s artifact is duplicated", artifact.Component))
			continue
		}
		byComponent[artifact.Component] = artifact
	}
	var repository string
	for _, component := range []string{"api", "frontend"} {
		artifact, exists := byComponent[component]
		if !exists {
			*problems = append(*problems, fmt.Sprintf("%s artifact is missing", component))
			continue
		}
		if !digestPattern.MatchString(artifact.Digest) {
			*problems = append(*problems, fmt.Sprintf("%s artifact digest must be immutable sha256", component))
		}
		suffix := "-" + component
		if !strings.HasPrefix(artifact.Image, "ghcr.io/") ||
			!strings.HasSuffix(artifact.Image, suffix) {
			*problems = append(*problems, fmt.Sprintf("%s artifact image must be its GHCR component repository", component))
		} else {
			candidate := strings.TrimSuffix(strings.TrimPrefix(artifact.Image, "ghcr.io/"), suffix)
			if candidate == "" || !strings.Contains(candidate, "/") {
				*problems = append(*problems, fmt.Sprintf("%s artifact image repository is invalid", component))
			} else if candidate != strings.ToLower(candidate) {
				*problems = append(*problems, fmt.Sprintf("%s artifact image repository must be lowercase", component))
			} else if repository == "" {
				repository = candidate
			} else if repository != candidate {
				*problems = append(*problems, "published artifacts do not share one repository identity")
			}
		}
		for label, value := range map[string]string{
			"signature_bundle": artifact.SignatureBundle,
			"sbom":             artifact.SBOM,
			"provenance":       artifact.Provenance,
		} {
			if !safeArtifactPath(value) {
				*problems = append(*problems, fmt.Sprintf("%s artifact %s must be a safe relative path", component, label))
			}
		}
	}
	if repository != "" {
		var sharedIdentity string
		for _, component := range []string{"api", "frontend"} {
			artifact, exists := byComponent[component]
			if !exists {
				continue
			}
			identityRepository, ok := canonicalWorkflowIdentityRepository(artifact.SignatureIdentity)
			if !ok || strings.ToLower(identityRepository) != repository {
				*problems = append(*problems, fmt.Sprintf("%s artifact signature identity does not match trusted workflow", component))
				continue
			}
			if sharedIdentity == "" {
				sharedIdentity = artifact.SignatureIdentity
			} else if artifact.SignatureIdentity != sharedIdentity {
				*problems = append(*problems, "published artifacts do not share one canonical workflow identity")
			}
		}
	}
	if len(*problems) == 0 {
		result.AcceptedArtifacts = []string{"api", "frontend"}
	}
}

func canonicalWorkflowIdentityRepository(identity string) (string, bool) {
	if !strings.HasPrefix(identity, workflowIdentityPrefix) || !strings.HasSuffix(identity, workflowIdentitySuffix) {
		return "", false
	}
	repository := strings.TrimSuffix(strings.TrimPrefix(identity, workflowIdentityPrefix), workflowIdentitySuffix)
	return repository, canonicalRepositoryPattern.MatchString(repository)
}

func safeArtifactPath(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || strings.Contains(trimmed, "\\") ||
		strings.HasPrefix(trimmed, "/") {
		return false
	}
	cleaned := path.Clean(trimmed)
	return cleaned != "." && cleaned != ".." &&
		!strings.HasPrefix(cleaned, "../")
}
