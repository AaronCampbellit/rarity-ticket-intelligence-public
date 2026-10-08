#!/usr/bin/env bash
set -euo pipefail

revision="${1:-}"
if [[ -z "$revision" ]]; then
  revision=$(git rev-parse --verify HEAD 2>/dev/null || true)
fi

if [[ -z "$revision" ]]; then
  echo "A deployable Git revision is required." >&2
  exit 1
fi

if [[ -n "$(git status --porcelain)" ]]; then
  echo "Refusing demo deployment from a dirty working tree." >&2
  exit 1
fi

if [[ ! -f .env ]]; then
  echo "Missing .env; generate or inject non-production configuration first." >&2
  exit 1
fi

required_env=(
  RARITY_ENV
  RARITY_HTTP_ADDR
  RARITY_HTTP_PORT
  RARITY_DEMO_HTTP_PORT
  RARITY_BUILD_REVISION
  RARITY_PUBLIC_URL
  RARITY_SESSION_KEY
  RARITY_SECRET_KEY
  DATABASE_URL
  VALKEY_URL
  S3_ENDPOINT
  S3_BUCKET
  S3_REGION
  S3_ACCESS_KEY_ID
  S3_SECRET_ACCESS_KEY
  POSTGRES_DB
  POSTGRES_USER
  POSTGRES_PASSWORD
  MINIO_ROOT_USER
  MINIO_ROOT_PASSWORD
)
for setting in "${required_env[@]}"; do
  count=$(grep -c "^${setting}=" .env || true)
  value=$(sed -n "s/^${setting}=//p" .env)
  if [[ "$count" -ne 1 || -z "${value//[[:space:]]/}" ]]; then
    echo "Demo configuration requires exactly one non-empty $setting value." >&2
    exit 1
  fi
done

configured_revision=$(sed -n 's/^RARITY_BUILD_REVISION=//p' .env)
if [[ "$configured_revision" != "$revision" ]]; then
  echo "RARITY_BUILD_REVISION must match the deployable revision $revision." >&2
  exit 1
fi

skill_config="${RTI_DEMO_SERVER_CONFIG:-/Users/aaroncampbell/.codex/skills/rti-demo-server/references/server-configuration.md}"
if [[ ! -f "$skill_config" ]]; then
  echo "RTI demo-server configuration is unavailable: $skill_config" >&2
  exit 1
fi

for setting in "SSH host" "SSH user" "Repository path on server" "Health endpoint" "Readiness endpoint" "Revision/freshness verification"; do
  line=$(grep -F "| $setting |" "$skill_config" || true)
  if [[ -z "$line" || "$line" == *"Not configured"* ]]; then
    echo "RTI demo-server configuration is incomplete: $setting" >&2
    exit 1
  fi
done

printf 'Demo preflight passed for revision %s\n' "$revision"
