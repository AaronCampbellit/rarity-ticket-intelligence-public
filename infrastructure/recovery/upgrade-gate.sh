#!/usr/bin/env bash
set -euo pipefail

if [[ "$#" -ne 2 ]]; then
  echo "usage: upgrade-gate.sh <release-evidence-directory> <expected-revision>" >&2
  exit 2
fi

evidence_dir="$1"
expected_revision="$2"

for artifact in \
  pre-upgrade-backup.json \
  migration-status.json \
  expected-revision.txt \
  rollback-plan.md \
  health-verification.json; do
  if [[ ! -s "${evidence_dir%/}/${artifact}" ]]; then
    echo "Missing required upgrade artifact: $artifact" >&2
    exit 1
  fi
done

recorded_revision="$(tr -d '[:space:]' <"${evidence_dir%/}/expected-revision.txt")"
if [[ "$recorded_revision" != "$expected_revision" ]]; then
  echo "Expected revision evidence does not match the requested release." >&2
  exit 1
fi

jq -e '.status == "passed" and .backup_type != null and .completed_at != null' \
  "${evidence_dir%/}/pre-upgrade-backup.json" >/dev/null
jq -e '.status == "compatible" and .current_schema != null and .target_schema != null' \
  "${evidence_dir%/}/migration-status.json" >/dev/null
jq -e '.status == "passed" and .revision == $revision' \
  --arg revision "$expected_revision" \
  "${evidence_dir%/}/health-verification.json" >/dev/null

for heading in "Rollback trigger" "Rollback procedure" "Data compatibility"; do
  if ! grep -Fxq "## $heading" "${evidence_dir%/}/rollback-plan.md"; then
    echo "Rollback plan must document triggers, procedure, and data compatibility." >&2
    exit 1
  fi
done

echo "Upgrade evidence gate passed for revision $expected_revision"
