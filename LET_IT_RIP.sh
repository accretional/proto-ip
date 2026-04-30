#!/usr/bin/env bash
# LET_IT_RIP.sh — Full end-to-end flow: setup, build, test, run, fuzz.
#
# IDEMPOTENCY CONTRACT:
#   Every sub-script is idempotent. Safe to run from a clean checkout
#   or mid-development. Steps:
#   1. test.sh       — runs setup + build + unit tests + short fuzz
#   2. Smoke test    — start LocalLookup server, query via client,
#                      verify at least one local IP is returned
#   3. Long fuzz     — extended fuzz pass on IPv4/IPv6/CIDR grammars
#
# Run before every push. If it passes, the project is healthy.
set -euo pipefail

cd "$(dirname "$0")"

echo "========================================"
echo "  LET IT RIP — proto-ip full flow"
echo "========================================"
echo ""

bash test.sh
echo ""

echo "=== Smoke test: LocalLookup server + client ==="

PORT=50097
echo "  Starting server on port $PORT..."
bin/server -port "$PORT" &
SERVER_PID=$!

sleep 1
if ! kill -0 "$SERVER_PID" 2>/dev/null; then
    echo "  ERROR: Server failed to start"
    exit 1
fi

cleanup() {
    echo "  Stopping server (PID $SERVER_PID)..."
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
}
trap cleanup EXIT

echo "  Client: ListInterfaces..."
bin/client -addr "localhost:$PORT" interfaces
echo ""

echo "  Client: ListIPs..."
IPS_OUT=$(bin/client -addr "localhost:$PORT" ips)
echo "$IPS_OUT"
echo ""

# Sanity check: at least one IP should come back from the local host.
N=$(echo "$IPS_OUT" | grep -cE '^(IPv4|IPv6)' || true)
if [[ "$N" -lt 1 ]]; then
    echo "  ✗ Expected at least one local IP, got 0"
    exit 1
fi
echo "  ✓ Got $N local IP(s)"

echo ""
echo "=== Long fuzz pass (15s per grammar) ==="
go test -run=NONE -fuzz=FuzzIPv4 -fuzztime=15s ./lang
go test -run=NONE -fuzz=FuzzIPv6 -fuzztime=15s ./lang
go test -run=NONE -fuzz=FuzzCIDR -fuzztime=15s ./lang

echo ""
echo "========================================"
echo "  ALL CHECKS PASSED"
echo "========================================"
