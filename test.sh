#!/usr/bin/env bash
# test.sh — Run setup, build, then ALL tests (unit, fuzz seeds, e2e).
#
# IDEMPOTENCY CONTRACT:
#   Calls build.sh first (which calls setup.sh).
#   Tests are stateless reads of host state. Safe to repeat.
set -euo pipefail

cd "$(dirname "$0")"

bash build.sh

echo "=== test.sh ==="

echo "  Running all unit/integration tests..."
go test -v -count=1 ./...

echo "  Running short fuzz pass on each fuzz target..."
# Each Fuzz* test will run for 5s as a smoke pass. Real fuzzing runs in
# LET_IT_RIP.sh with a longer budget.
go test -run=NONE -fuzz=FuzzIPv4 -fuzztime=3s ./lang
go test -run=NONE -fuzz=FuzzIPv6 -fuzztime=3s ./lang
go test -run=NONE -fuzz=FuzzCIDR -fuzztime=3s ./lang

echo "=== test.sh complete ==="
