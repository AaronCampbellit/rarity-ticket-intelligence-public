#!/usr/bin/env bash
set -euo pipefail

for target in format test verify compose-config local-env; do
  make -n "$target" >/dev/null
done

test ! -e .env
