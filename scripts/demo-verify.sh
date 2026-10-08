#!/usr/bin/env bash
set -euo pipefail

if [[ "$#" -ne 2 ]]; then
  echo "Usage: $0 <base-url> <expected-revision>" >&2
  exit 2
fi

base_url="${1%/}"
expected_revision="$2"

health=$(curl -fsS "$base_url/healthz")
if [[ "$health" != '{"status":"ok"}' ]]; then
  echo "Unexpected health response: $health" >&2
  exit 1
fi

ready=$(curl -fsS "$base_url/readyz")
if [[ "$ready" != *'"status":"ready"'* || "$ready" != *"\"revision\":\"$expected_revision\""* ]]; then
  echo "Readiness did not report revision $expected_revision." >&2
  exit 1
fi

build=$(curl -fsS "$base_url/v1/system/build")
if [[ "$build" != "{\"revision\":\"$expected_revision\"}" ]]; then
  echo "Build endpoint did not report expected revision $expected_revision." >&2
  exit 1
fi

root=$(curl -fsS "$base_url/")
if [[ "$root" != *"$expected_revision"* ]]; then
  echo "Rendered shell did not include expected revision $expected_revision." >&2
  exit 1
fi

printf 'Demo verification passed for revision %s\n' "$expected_revision"
