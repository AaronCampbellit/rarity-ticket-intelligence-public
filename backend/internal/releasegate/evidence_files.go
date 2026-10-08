package releasegate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

type ArtifactManifest struct {
	Revision  string             `json:"revision"`
	Artifacts []ArtifactEvidence `json:"artifacts"`
}

// ValidateFiles binds the JSON release decision to retained evidence on disk.
// evidencePath is the release manifest's path; every artifact is resolved
// relative to its directory after Evaluate has accepted the safe relative paths.
func ValidateFiles(evidence Evidence, evidencePath string) error {
	root := filepath.Dir(evidencePath)
	var problems []string
	var provenancePath string
	for _, gate := range evidence.Gates {
		if !safeArtifactPath(gate.Artifact) {
			continue
		}
		path := filepath.Join(root, filepath.FromSlash(gate.Artifact))
		if !regularFile(path) {
			problems = append(problems, fmt.Sprintf("%s retained file is missing or not regular", gate.ID))
		}
		if gate.ID == "artifact_provenance" {
			provenancePath = path
		}
	}
	for _, artifact := range evidence.Artifacts {
		for label, name := range map[string]string{
			"signature":  artifact.SignatureBundle,
			"sbom":       artifact.SBOM,
			"provenance": artifact.Provenance,
		} {
			if safeArtifactPath(name) &&
				!regularFile(filepath.Join(root, filepath.FromSlash(name))) {
				problems = append(problems, fmt.Sprintf("%s %s retained file is missing or not regular", artifact.Component, label))
			}
		}
		if safeArtifactPath(artifact.SBOM) {
			path := filepath.Join(root, filepath.FromSlash(artifact.SBOM))
			if regularFile(path) && !validSBOM(path) {
				problems = append(problems, fmt.Sprintf("%s SBOM extraction is invalid", artifact.Component))
			}
		}
		if safeArtifactPath(artifact.Provenance) {
			path := filepath.Join(root, filepath.FromSlash(artifact.Provenance))
			if regularFile(path) && !validProvenance(path, evidence.Revision) {
				problems = append(problems, fmt.Sprintf("%s provenance extraction or revision is invalid", artifact.Component))
			}
		}
	}

	if provenancePath != "" && regularFile(provenancePath) {
		manifest, err := readArtifactManifest(provenancePath)
		if err != nil {
			problems = append(problems, "artifact provenance manifest is invalid")
		} else {
			if manifest.Revision != evidence.Revision {
				problems = append(problems, "artifact provenance revision does not match release revision")
			}
			if !sameArtifacts(manifest.Artifacts, evidence.Artifacts) {
				problems = append(problems, "artifact provenance manifest does not match release artifacts")
			}
		}
	}
	sort.Strings(problems)
	if len(problems) != 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

func validSBOM(path string) bool {
	value, err := readJSONValue(path, 32<<20)
	if err != nil {
		return false
	}
	return containsObject(value, func(object map[string]any) bool {
		spdx, ok := object["SPDX"].(map[string]any)
		if !ok {
			return false
		}
		version, versionOK := spdx["spdxVersion"].(string)
		return spdx["SPDXID"] == "SPDXRef-DOCUMENT" && versionOK && version != ""
	})
}

func validProvenance(path, revision string) bool {
	value, err := readJSONValue(path, 32<<20)
	if err != nil {
		return false
	}
	object, ok := value.(map[string]any)
	return ok && (validCurrentBuildKitProvenance(object, revision) || validLegacyBuildKitProvenance(object, revision))
}

func validCurrentBuildKitProvenance(object map[string]any, revision string) bool {
	buildDefinition, ok := object["buildDefinition"].(map[string]any)
	if !ok || buildDefinition["buildType"] != "https://github.com/moby/buildkit/blob/master/docs/attestations/slsa-definitions.md" {
		return false
	}
	dependencies, ok := buildDefinition["resolvedDependencies"].([]any)
	if !ok || len(dependencies) == 0 {
		return false
	}
	external, ok := buildDefinition["externalParameters"].(map[string]any)
	if !ok || nestedString(external, "configSource", "digest", "sha1") != revision ||
		nestedString(external, "request", "args", "build-arg:RARITY_BUILD_REVISION") != revision ||
		nestedString(external, "request", "args", "label:org.opencontainers.image.revision") != revision ||
		nestedString(buildDefinition, "internalParameters", "github_workflow_sha") != revision {
		return false
	}
	return true
}

func validLegacyBuildKitProvenance(object map[string]any, revision string) bool {
	if object["buildType"] != "https://mobyproject.org/buildkit@v1" {
		return false
	}
	if _, ok := object["materials"].([]any); !ok {
		return false
	}
	return containsRevision(object, revision)
}

func nestedString(object map[string]any, path ...string) string {
	value := any(object)
	for _, key := range path {
		current, ok := value.(map[string]any)
		if !ok {
			return ""
		}
		value = current[key]
	}
	result, _ := value.(string)
	return result
}

func containsObject(value any, matches func(map[string]any) bool) bool {
	switch typed := value.(type) {
	case map[string]any:
		if matches(typed) {
			return true
		}
		for _, child := range typed {
			if containsObject(child, matches) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if containsObject(child, matches) {
				return true
			}
		}
	}
	return false
}

func readJSONValue(path string, limit int64) (any, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, limit))
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("expected exactly one JSON value")
	}
	return value, nil
}

func containsRevision(value any, revision string) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if key == "revision" && child == revision {
				return true
			}
			if containsRevision(child, revision) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if containsRevision(child, revision) {
				return true
			}
		}
	}
	return false
}

func regularFile(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular()
}

func readArtifactManifest(path string) (ArtifactManifest, error) {
	file, err := os.Open(path)
	if err != nil {
		return ArtifactManifest{}, err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 1<<20))
	decoder.DisallowUnknownFields()
	var manifest ArtifactManifest
	if err := decoder.Decode(&manifest); err != nil {
		return ArtifactManifest{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return ArtifactManifest{}, errors.New("expected exactly one JSON value")
	}
	return manifest, nil
}

func sameArtifacts(left, right []ArtifactEvidence) bool {
	toMap := func(values []ArtifactEvidence) map[string]ArtifactEvidence {
		result := make(map[string]ArtifactEvidence, len(values))
		for _, value := range values {
			result[value.Component] = value
		}
		return result
	}
	return reflect.DeepEqual(toMap(left), toMap(right))
}
