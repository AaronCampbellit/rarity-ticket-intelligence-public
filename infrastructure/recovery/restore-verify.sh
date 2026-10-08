#!/usr/bin/env bash
set -euo pipefail

if [[ "$#" -ne 3 ]]; then
  echo "usage: restore-verify.sh <pgbackrest-config> <stanza> <UTC-target-time>" >&2
  exit 2
fi

config_path="$1"
stanza="$2"
target_time="$3"
restore_root="${RARITY_RESTORE_ROOT:?RARITY_RESTORE_ROOT is required}"
verification_port="${RARITY_RESTORE_VERIFY_PORT:-55432}"
run_id="$(date -u +%Y%m%dT%H%M%SZ)"
target="${restore_root%/}/${run_id}"
evidence="${target}/restore-evidence.json"
started=false

case "$restore_root" in
  /var/lib/rarity/restore-verification|/srv/rarity/restore-verification) ;;
  *)
    echo "RARITY_RESTORE_ROOT must be an approved clean-room root." >&2
    exit 1
    ;;
esac

if [[ ! -f "$config_path" ]]; then
  echo "pgBackRest configuration is unavailable." >&2
  exit 1
fi
if ! date -u -d "$target_time" +%s >/dev/null 2>&1; then
  echo "Target time must be a valid UTC date." >&2
  exit 1
fi
if (( $(date -u -d "$target_time" +%s) >= $(date -u +%s) )); then
  echo "Target time must be in the past." >&2
  exit 1
fi

mkdir -p "$restore_root"
chmod 700 "$restore_root"
if [[ -e "$target" ]]; then
  if [[ -n "$(find "$target" -mindepth 1 -maxdepth 1 -print -quit)" ]]; then
    echo "refusing non-empty restore target" >&2
  else
    echo "refusing existing restore target" >&2
  fi
  exit 1
fi
mkdir "$target"
chmod 700 "$target"

stop_postgres() {
  if [[ "$started" == true ]]; then
    pg_ctl -D "$target" -m fast -w stop >/dev/null
  fi
}
trap stop_postgres EXIT

started_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
pgbackrest --config="$config_path" --stanza="$stanza" check
pgbackrest \
  --config="$config_path" \
  --stanza="$stanza" \
  --pg1-path="$target" \
  --type=time \
  --target="$target_time" \
  --target-action=promote \
  restore
pg_controldata "$target" >/dev/null
pg_checksums --check --pgdata="$target" >/dev/null
pg_ctl -D "$target" -o "-p ${verification_port} -c listen_addresses=127.0.0.1" -w start >/dev/null
started=true
psql -X -v ON_ERROR_STOP=1 -h 127.0.0.1 -p "$verification_port" -d postgres \
  -c "SELECT pg_is_in_recovery(), current_setting('server_version_num');" >/dev/null
completed_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

jq -n \
  --arg run_id "$run_id" \
  --arg stanza "$stanza" \
  --arg target_time "$target_time" \
  --arg started_at "$started_at" \
  --arg completed_at "$completed_at" \
  '{
    schema_version: 1,
    run_id: $run_id,
    stanza: $stanza,
    target_time: $target_time,
    started_at: $started_at,
    completed_at: $completed_at,
    checks: {
      repository: "passed",
      restore: "passed",
      control_data: "passed",
      checksums: "passed",
      database_start: "passed",
      sql_probe: "passed"
    }
  }' >"$evidence"
chmod 600 "$evidence"

echo "Restore verification passed; evidence: $evidence"
