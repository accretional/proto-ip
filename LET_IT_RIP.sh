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
echo "=== Smoke test: RDAPLookup server + client ==="

RDAP_PORT=50098
echo "  Starting rdap-server on port $RDAP_PORT..."
bin/rdap-server -port "$RDAP_PORT" &
RDAP_PID=$!

# Give the server time to fetch IANA bootstrap.
sleep 3
if ! kill -0 "$RDAP_PID" 2>/dev/null; then
    echo "  ERROR: rdap-server failed to start"
    exit 1
fi

rdap_cleanup() {
    echo "  Stopping rdap-server (PID $RDAP_PID)..."
    kill "$RDAP_PID" 2>/dev/null || true
    wait "$RDAP_PID" 2>/dev/null || true
}
trap rdap_cleanup EXIT

echo "  RDAP lookup: 8.8.8.8 (Google DNS, IPv4)..."
bin/rdap-client -addr "localhost:$RDAP_PORT" ip 8.8.8.8
echo ""

echo "  RDAP lookup: 2001:4860:4860::8888 (Google DNS, IPv6)..."
bin/rdap-client -addr "localhost:$RDAP_PORT" ip 2001:4860:4860::8888
echo ""

echo "  RDAP lookup: 1.1.1.0/24 (Cloudflare, CIDR)..."
bin/rdap-client -addr "localhost:$RDAP_PORT" cidr 1.1.1.0/24
echo ""

echo "  RDAP lookup: AS15169 (Google, ASN)..."
bin/rdap-client -addr "localhost:$RDAP_PORT" asn 15169
echo ""

echo "  RDAP lookup: AS13335 (Cloudflare, ASN)..."
bin/rdap-client -addr "localhost:$RDAP_PORT" asn 13335
echo ""

echo "  ✓ RDAP smoke tests passed"

echo ""
echo "=== Smoke test: GeoLookup server + client ==="

GEO_PORT=50099
echo "  Starting geo-server on port $GEO_PORT..."
bin/geo-server -port "$GEO_PORT" -data-dir data/geoip &
GEO_PID=$!

# Give the server time to open the DB-IP DB and fetch the RDAP bootstrap.
sleep 4
if ! kill -0 "$GEO_PID" 2>/dev/null; then
    echo "  ERROR: geo-server failed to start"
    exit 1
fi

# Replace the trap with one that cleans up ALL servers started so far.
geo_cleanup() {
    echo "  Stopping servers..."
    kill "$GEO_PID" "$RDAP_PID" "$SERVER_PID" 2>/dev/null || true
    wait "$GEO_PID" "$RDAP_PID" "$SERVER_PID" 2>/dev/null || true
}
trap geo_cleanup EXIT

echo "  Geo lookup: 8.8.8.8 (Google DNS, expect coordinates from DB-IP)..."
GEO_OUT=$(bin/geo-client -addr "localhost:$GEO_PORT" ip 8.8.8.8)
echo "$GEO_OUT"
if ! echo "$GEO_OUT" | grep -qE 'coordinates: +-?[0-9]'; then
    echo "  ✗ Expected lat/lon coordinates for 8.8.8.8"
    exit 1
fi
echo "  ✓ Got coordinates for 8.8.8.8"
# Reliable AS-level "network spine": 8.8.8.8 should report an origin ASN and an
# RPKI verdict. Best-effort — the RPKI dump / RDAP may be absent offline.
if echo "$GEO_OUT" | grep -qiE '^asn: *AS[0-9]'; then
    echo "  ✓ origin ASN reported (network spine)"
fi
if echo "$GEO_OUT" | grep -qiE 'rpki_status: *(valid|invalid|not_found)'; then
    echo "  ✓ RPKI status evaluated (network spine)"
fi
echo ""

echo "  Geo lookup: 1.1.1.1 (Cloudflare anycast; expect anycast flag + ASN)..."
ONE_OUT=$(bin/geo-client -addr "localhost:$GEO_PORT" ip 1.1.1.1)
echo "$ONE_OUT"
if echo "$ONE_OUT" | grep -q 'source: *ipmap'; then
    echo "  ✓ RIPE IPmap (measured infrastructure) source contributed"
else
    echo "  (1.1.1.1 not in this IPmap dump revision — best-effort, OK)"
fi
# When the anycast list + iptoasn are present, 1.1.1.1 should be flagged anycast
# with low confidence and carry an origin ASN. Best-effort (lists may be absent).
if echo "$ONE_OUT" | grep -qE 'anycast: *true'; then
    echo "  ✓ 1.1.1.1 flagged anycast (confidence forced low)"
fi
if echo "$ONE_OUT" | grep -qiE 'asn: *AS[0-9]'; then
    echo "  ✓ origin ASN reported (iptoasn / BGP)"
fi
# 1.1.1.1 is announced by AS13335 under a valid ROA, so a healthy run with the
# RPKI dump present shows rpki_status: valid. Best-effort.
if echo "$ONE_OUT" | grep -qiE 'rpki_status: *valid'; then
    echo "  ✓ 1.1.1.1 RPKI-valid (AS13335)"
fi
echo ""

# Geofeed coverage is operator-published and changes over time; this lookup is
# best-effort and never fails the run if the feed is withdrawn. The Pfcloud /39
# publishes an RFC 8805 geofeed via the RIPE `geofeed:` whois attribute, so a
# healthy run shows BOTH a dbip_lite (coordinates) and an authoritative geofeed
# (country/region) source merged into `best`.
echo "  Geo lookup: 2a05:b0c6:a200::1 (RIPE geofeed publisher, best-effort)..."
GEOFEED_OUT=$(bin/geo-client -addr "localhost:$GEO_PORT" ip 2a05:b0c6:a200::1 || true)
echo "$GEOFEED_OUT"
if echo "$GEOFEED_OUT" | grep -q 'source: *geofeed'; then
    echo "  ✓ Authoritative geofeed source contributed"
else
    echo "  (no geofeed advertised for this prefix right now — best-effort, OK)"
fi
echo ""

echo "  ✓ GeoLookup smoke tests passed"

echo ""
echo "=== Long fuzz pass (15s per grammar) ==="
go test -run=NONE -fuzz=FuzzIPv4 -fuzztime=15s ./lang
go test -run=NONE -fuzz=FuzzIPv6 -fuzztime=15s ./lang
go test -run=NONE -fuzz=FuzzCIDR -fuzztime=15s ./lang

echo ""
echo "========================================"
echo "  ALL CHECKS PASSED"
echo "========================================"
