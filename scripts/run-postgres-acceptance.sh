#!/usr/bin/env bash
set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repository_root"
exec go run ./infrastructure/cmd/postgres-acceptance ./backend/... ./tests/...
