#!/usr/bin/env bash
set -euo pipefail

if [[ -e .env ]]; then
  echo '.env already exists; refusing to overwrite it.' >&2
  exit 1
fi

umask 077

postgres_password=$(openssl rand -hex 32)
minio_password=$(openssl rand -hex 32)
session_key=$(openssl rand -hex 32)
secret_key=$(openssl rand -hex 32)

cat >.env <<EOF
RARITY_ENV=local
RARITY_HTTP_ADDR=:8080
RARITY_HTTP_PORT=18080
RARITY_DEMO_BIND_ADDRESS=0.0.0.0
RARITY_DEMO_HTTP_PORT=18081
RARITY_BUILD_REVISION=local-dev
RARITY_PUBLIC_URL=http://127.0.0.1:18080
RARITY_SESSION_KEY=${session_key}
RARITY_SECRET_KEY=${secret_key}
DATABASE_URL=postgres://rarity:${postgres_password}@postgres:5432/rarity?sslmode=disable
VALKEY_URL=redis://valkey:6379/0
S3_ENDPOINT=http://minio:9000
S3_BUCKET=rarity-attachments
S3_REGION=us-east-1
S3_ACCESS_KEY_ID=rarity-local
S3_SECRET_ACCESS_KEY=${minio_password}
POSTGRES_DB=rarity
POSTGRES_USER=rarity
POSTGRES_PASSWORD=${postgres_password}
MINIO_ROOT_USER=rarity-local
MINIO_ROOT_PASSWORD=${minio_password}
EOF

chmod 600 .env
echo 'Created local-only .env with generated secrets.'
