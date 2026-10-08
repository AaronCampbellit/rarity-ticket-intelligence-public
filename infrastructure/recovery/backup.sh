#!/usr/bin/env bash
set -euo pipefail

if [[ "$#" -ne 4 ]]; then
  echo "usage: backup.sh <pgbackrest-config> <stanza> <full|diff|incr> <evidence-directory>" >&2
  exit 2
fi

config_path="$1"
stanza="$2"
backup_type="$3"
evidence_dir="$4"

case "$backup_type" in
  full|diff|incr) ;;
  *)
    echo "Backup type must be full, diff, or incr." >&2
    exit 2
    ;;
esac
if [[ ! -f "$config_path" ]]; then
  echo "pgBackRest configuration is unavailable." >&2
  exit 1
fi

mkdir -p "$evidence_dir"
chmod 700 "$evidence_dir"
exec 9>"${evidence_dir%/}/backup.lock"
if ! flock -n 9; then
  echo "Another backup job is already running." >&2
  exit 1
fi

run_id="$(date -u +%Y%m%dT%H%M%SZ)"
started_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
evidence="${evidence_dir%/}/backup-evidence-${run_id}.json"
temporary="${evidence}.tmp"
trap 'rm -f "$temporary"' EXIT

pgbackrest --config="$config_path" --stanza="$stanza" check
pgbackrest --config="$config_path" --stanza="$stanza" --type="$backup_type" backup
repository_info="$(pgbackrest --config="$config_path" --stanza="$stanza" --output=json info)"
completed_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

jq -n \
  --arg run_id "$run_id" \
  --arg stanza "$stanza" \
  --arg backup_type "$backup_type" \
  --arg started_at "$started_at" \
  --arg completed_at "$completed_at" \
  --argjson repository "$repository_info" \
  '{
    schema_version: 1,
    run_id: $run_id,
    stanza: $stanza,
    backup_type: $backup_type,
    started_at: $started_at,
    completed_at: $completed_at,
    status: "passed",
    repository: $repository
  }' >"$temporary"
chmod 600 "$temporary"
mv "$temporary" "$evidence"
trap - EXIT

echo "Backup passed; evidence: $evidence"
