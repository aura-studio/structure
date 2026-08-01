#!/usr/bin/env bash
# Runs the test suite with coverage and fails when statement coverage drops
# below the project gate.
set -euo pipefail

THRESHOLD="${COVERAGE_THRESHOLD:-95}"
cd "$(dirname "$0")/.."

go test -covermode=atomic -coverpkg=./... -coverprofile=cover.out ./...
go tool cover -func=cover.out | tail -n 1

total="$(go tool cover -func=cover.out | awk '/^total:/ { gsub(/%/, "", $3); print $3 }')"
if [ -z "${total}" ]; then
  echo "coverage: could not read total from cover.out" >&2
  exit 1
fi

awk -v total="${total}" -v threshold="${THRESHOLD}" 'BEGIN {
  if (total + 0 < threshold + 0) {
    printf "coverage %.1f%% is below the %.1f%% gate\n", total, threshold > "/dev/stderr"
    exit 1
  }
  printf "coverage %.1f%% meets the %.1f%% gate\n", total, threshold
}'
