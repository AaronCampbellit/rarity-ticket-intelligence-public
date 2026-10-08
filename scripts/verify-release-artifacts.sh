#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 5 ]]; then
  echo "usage: $0 <canonical-owner/repository> <40-character-revision> <api-image@sha256:digest> <frontend-image@sha256:digest> <existing-evidence-directory>" >&2
  exit 2
fi

repository=$1
revision=$2
api_image=$3
frontend_image=$4
evidence_directory=$5

if [[ ! "$repository" =~ ^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$ ]]; then
  echo "repository must be a canonical owner/repository identity using ASCII letters, digits, periods, underscores, or hyphens" >&2
  exit 2
fi
if [[ ! "$revision" =~ ^[0-9a-f]{40}$ ]]; then
  echo "revision must be the exact 40-character lowercase Git revision" >&2
  exit 2
fi
if [[ ! -d "$evidence_directory" ]]; then
  echo "evidence directory must already exist" >&2
  exit 2
fi
if [[ -L "$evidence_directory" ]]; then
  echo "evidence directory must not be a symbolic link" >&2
  exit 2
fi
if find "$evidence_directory" -mindepth 1 -maxdepth 1 -print -quit | grep -q .; then
  echo "evidence directory must be empty" >&2
  exit 2
fi
for dependency in cosign docker jq; do
  if ! command -v "$dependency" >/dev/null 2>&1; then
    echo "$dependency is required" >&2
    exit 2
  fi
done

registry_repository=$(printf '%s' "$repository" | tr '[:upper:]' '[:lower:]')
identity="https://github.com/${repository}/.github/workflows/publish.yml@refs/heads/main"
issuer="https://token.actions.githubusercontent.com"
umask 077
evidence_parent=$(dirname "$evidence_directory")
staging_directory=$(mktemp -d "${evidence_parent}/.verify-release.XXXXXX")
cleanup() {
  find "$staging_directory" -mindepth 1 -delete 2>/dev/null || true
  rmdir "$staging_directory" 2>/dev/null || true
}
trap cleanup EXIT

verify_component() {
  local component=$1
  local reference=$2
  local expected_prefix="ghcr.io/${registry_repository}-${component}@sha256:"
  local digest

  if [[ "$reference" != "$expected_prefix"* ]]; then
    echo "$component image must be its immutable GHCR sha256 digest reference" >&2
    return 2
  fi
  digest=${reference#"$expected_prefix"}
  if [[ ${#digest} -ne 64 ]] || [[ ! "$digest" =~ ^[0-9a-f]{64}$ ]]; then
    echo "$component image must be its immutable GHCR sha256 digest reference" >&2
    return 2
  fi

  cosign verify \
    --certificate-identity "$identity" \
    --certificate-oidc-issuer "$issuer" \
    "$reference" >"${staging_directory}/${component}-signature.json"

  docker buildx imagetools inspect \
    "$reference" \
    --format '{{ json .SBOM }}' \
    >"${staging_directory}/${component}-sbom.spdx.json"

  docker buildx imagetools inspect \
    "$reference" \
    --format '{{ json .Provenance.SLSA }}' \
    >"${staging_directory}/${component}-provenance.json"
  if ! jq --exit-status --slurp --arg revision "$revision" '
    length == 1 and
    (.[0] |
      type == "object" and
      .buildDefinition.externalParameters.configSource.digest.sha1 == $revision and
      .buildDefinition.externalParameters.request.args["build-arg:RARITY_BUILD_REVISION"] == $revision and
      .buildDefinition.externalParameters.request.args["label:org.opencontainers.image.revision"] == $revision and
      .buildDefinition.internalParameters.github_workflow_sha == $revision
    )
  ' "${staging_directory}/${component}-provenance.json" >/dev/null; then
    echo "$component provenance does not identify the requested revision" >&2
    return 1
  fi

}

verify_component api "$api_image"
verify_component frontend "$frontend_image"

api_digest=${api_image##*@}
frontend_digest=${frontend_image##*@}
manifest="${staging_directory}/artifact-provenance.json"
temporary_manifest="${manifest}.tmp.$$"
printf '%s\n' \
  '{' \
  "  \"revision\": \"${revision}\"," \
  '  "artifacts": [' \
  '    {' \
  '      "component": "api",' \
  "      \"image\": \"ghcr.io/${registry_repository}-api\"," \
  "      \"digest\": \"${api_digest}\"," \
  "      \"signature_identity\": \"${identity}\"," \
  '      "signature_bundle": "api-signature.json",' \
  '      "sbom": "api-sbom.spdx.json",' \
  '      "provenance": "api-provenance.json"' \
  '    },' \
  '    {' \
  '      "component": "frontend",' \
  "      \"image\": \"ghcr.io/${registry_repository}-frontend\"," \
  "      \"digest\": \"${frontend_digest}\"," \
  "      \"signature_identity\": \"${identity}\"," \
  '      "signature_bundle": "frontend-signature.json",' \
  '      "sbom": "frontend-sbom.spdx.json",' \
  '      "provenance": "frontend-provenance.json"' \
  '    }' \
  '  ]' \
  '}' >"$temporary_manifest"
mv "$temporary_manifest" "$manifest"

mv -T "$staging_directory" "$evidence_directory"
trap - EXIT

printf 'Verified release artifacts for %s\n' "$repository"
