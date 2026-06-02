#!/usr/bin/env bash
# setup.sh — Idempotent project setup.
#
# IDEMPOTENCY CONTRACT:
#   Checks before acting. Will:
#   - Verify Go 1.26.x is installed (does NOT install)
#   - Install protoc via brew if missing
#   - Install protoc-gen-go and protoc-gen-go-grpc if missing
#   - Generate proto stubs if proto sources have changed or stubs are missing
#   - Run go mod tidy
set -euo pipefail

cd "$(dirname "$0")"

echo "=== setup.sh ==="

REQUIRED_GO_MINOR="1.26"
GO_VERSION=$(go version 2>/dev/null | grep -oE 'go[0-9]+\.[0-9]+' | head -1)
if [[ -z "$GO_VERSION" ]]; then
    echo "ERROR: Go is not installed. Install Go ${REQUIRED_GO_MINOR}.x first."
    exit 1
fi
if [[ "$GO_VERSION" != "go${REQUIRED_GO_MINOR}" ]]; then
    echo "ERROR: Go ${REQUIRED_GO_MINOR}.x required, found $GO_VERSION"
    exit 1
fi
echo "  Go version OK: $(go version)"

if ! command -v protoc &>/dev/null; then
    echo "  Installing protoc via brew..."
    brew install protobuf
else
    echo "  protoc OK: $(protoc --version)"
fi

GOBIN=$(go env GOBIN)
if [[ -z "$GOBIN" ]]; then
    GOBIN=$(go env GOPATH)/bin
fi

if [[ ! -x "$GOBIN/protoc-gen-go" ]]; then
    echo "  Installing protoc-gen-go..."
    go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
else
    echo "  protoc-gen-go OK"
fi

if [[ ! -x "$GOBIN/protoc-gen-go-grpc" ]]; then
    echo "  Installing protoc-gen-go-grpc..."
    go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
else
    echo "  protoc-gen-go-grpc OK"
fi

export PATH="$GOBIN:$PATH"

PROTO_DIR="proto/ippb"
PROTO_FILES=("$PROTO_DIR"/ipv4.proto "$PROTO_DIR"/ipv6.proto "$PROTO_DIR"/ip.proto "$PROTO_DIR"/subnet.proto "$PROTO_DIR"/cidr.proto "$PROTO_DIR"/local_lookup.proto "$PROTO_DIR"/rdap.proto "$PROTO_DIR"/geo.proto)

# Detect whether any .proto is newer than its .pb.go (or stubs missing).
NEED_REGEN=false
for src in "${PROTO_FILES[@]}"; do
    base="${src%.proto}"
    pb="${base}.pb.go"
    if [[ ! -f "$pb" ]] || [[ "$src" -nt "$pb" ]]; then
        NEED_REGEN=true
        break
    fi
done
if [[ ! -f "$PROTO_DIR/local_lookup_grpc.pb.go" ]]; then
    NEED_REGEN=true
fi
if [[ ! -f "$PROTO_DIR/rdap_grpc.pb.go" ]]; then
    NEED_REGEN=true
fi
if [[ ! -f "$PROTO_DIR/geo_grpc.pb.go" ]]; then
    NEED_REGEN=true
fi

if $NEED_REGEN; then
    echo "  Generating protobuf stubs..."
    protoc \
        -I . \
        --go_out=. --go_opt=paths=source_relative \
        --go-grpc_out=. --go-grpc_opt=paths=source_relative \
        "${PROTO_FILES[@]}"
    echo "  Proto stubs generated."
else
    echo "  Proto stubs up to date"
fi

echo "  Running go mod tidy..."
go mod tidy
echo "  go mod tidy done"

# --- GeoLookup data sources -------------------------------------------------
# All downloadable databases for the GeoLookup service are declared in one
# manifest and fetched by a single idempotent loop. To add a file-based source,
# add one GEO_SOURCES row. Every download is best-effort: a failure only WARNS,
# and the remaining sources still run. Files land in a gitignored cache.
GEO_DATA_DIR="data/geoip"
mkdir -p "$GEO_DATA_DIR"

# Manifest rows are "name|url|dest|freshness|postprocess":
#   url/dest  may contain {YYYY-MM}, expanded to the target month.
#   freshness "monthly" -> skip if the current OR previous month's dest exists,
#                          and on download try the current month then fall back
#                          to the previous (current month may not be published
#                          yet early in the month).
#             "<N>d"    -> skip if dest exists and is newer than N days.
#   postprocess "gunzip" downloads to dest.gz then gunzips; "none" downloads
#               straight to dest.
GEO_SOURCES=(
    "DB-IP City Lite (CC BY 4.0)|https://download.db-ip.com/free/dbip-city-lite-{YYYY-MM}.mmdb.gz|dbip-city-lite-{YYYY-MM}.mmdb|monthly|gunzip"
    "RIPE IPmap (RIPE NCC ToS)|https://ftp.ripe.net/ripe/ipmap/geolocations-latest|ipmap-geolocations-latest.csv.bz2|7d|none"
    "iptoasn IPv4 (PDDL)|https://iptoasn.com/data/ip2asn-v4.tsv.gz|ip2asn-v4.tsv|7d|gunzip"
    "iptoasn IPv6 (PDDL)|https://iptoasn.com/data/ip2asn-v6.tsv.gz|ip2asn-v6.tsv|7d|gunzip"
    "bgp.tools anycast IPv4|https://raw.githubusercontent.com/bgptools/anycast-prefixes/master/anycatch-v4-prefixes.txt|anycast-v4-prefixes.txt|7d|none"
    "bgp.tools anycast IPv6|https://raw.githubusercontent.com/bgptools/anycast-prefixes/master/anycatch-v6-prefixes.txt|anycast-v6-prefixes.txt|7d|none"
)

# month_offset N -> YYYY-MM for N months ago, handling BSD (darwin) and GNU date.
month_offset() {
    local n="$1"
    if date -v-"${n}"m +%Y-%m >/dev/null 2>&1; then
        date -v-"${n}"m +%Y-%m          # BSD/macOS
    else
        date -d "${n} months ago" +%Y-%m # GNU/Linux
    fi
}

# expand_month TEMPLATE OFFSET -> TEMPLATE with {YYYY-MM} set to that month.
expand_month() {
    echo "${1//\{YYYY-MM\}/$(month_offset "$2")}"
}

# download_to URL DEST POSTPROCESS -> 0 on success (DEST then exists), 1 on
# failure (any partial file is removed).
download_to() {
    local url="$1" dest="$2" post="$3"
    if [[ "$post" == "gunzip" ]]; then
        if curl -fsSL --max-time 120 -o "$dest.gz" "$url"; then
            gunzip -f "$dest.gz" && return 0
        fi
        rm -f "$dest.gz"
        return 1
    fi
    if curl -fsSL --max-time 120 -o "$dest" "$url"; then
        return 0
    fi
    rm -f "$dest"
    return 1
}

fetch_geo_source() {
    local name="$1" url="$2" dest_tmpl="$3" freshness="$4" post="$5"

    if [[ "$freshness" == "monthly" ]]; then
        local cur prev
        cur="$GEO_DATA_DIR/$(expand_month "$dest_tmpl" 0)"
        prev="$GEO_DATA_DIR/$(expand_month "$dest_tmpl" 1)"
        if [[ -f "$cur" || -f "$prev" ]]; then
            echo "  $name present (skipping download)"
            return 0
        fi
        echo "  Downloading ${name}…"
        local off
        for off in 0 1; do
            if download_to "$(expand_month "$url" "$off")" \
                           "$GEO_DATA_DIR/$(expand_month "$dest_tmpl" "$off")" "$post"; then
                echo "  $name downloaded to $GEO_DATA_DIR"
                return 0
            fi
        done
        echo "  WARNING: $name download failed; that source will be disabled."
        return 0
    fi

    # freshness "<N>d": re-fetch when missing or older than N days.
    local days="${freshness%d}"
    if [[ -n "$(find "$GEO_DATA_DIR" -name "$dest_tmpl" -mtime -"$days" 2>/dev/null)" ]]; then
        echo "  $name present and fresh (skipping download)"
        return 0
    fi
    echo "  Downloading ${name}…"
    if download_to "$url" "$GEO_DATA_DIR/$dest_tmpl" "$post"; then
        echo "  $name downloaded to $GEO_DATA_DIR"
    else
        echo "  WARNING: $name download failed; that source will be disabled."
    fi
    return 0
}

for entry in "${GEO_SOURCES[@]}"; do
    IFS='|' read -r s_name s_url s_dest s_fresh s_post <<< "$entry"
    fetch_geo_source "$s_name" "$s_url" "$s_dest" "$s_fresh" "$s_post"
done

# --- IP2Location LITE DB9 (OPTIONAL, opt-in) — CC BY-SA 4.0 -----------------
# Kept separate from the manifest because it is credentialed: it needs a free
# IP2Location LITE token and ships as a ZIP. Enable by exporting
# IP2LOCATION_TOKEN before running setup. We fetch the MMDB build (one file
# covering IPv4+IPv6, mmap'd at runtime — negligible memory), not the CSV.
# Not redistributed (the gitignored cache holds it), so the CC BY-SA
# share-alike term imposes no extra burden; attribution is still required and
# is carried in the GeoSourceResult.
fetch_ip2location() {  # file_code dest_name zip_member
    local code="$1" dest="$2" member="$3"
    if [[ -n "$(find "$GEO_DATA_DIR" -name "$dest" -mtime -25 2>/dev/null)" ]]; then
        echo "  IP2Location LITE $code present and fresh (skipping download)"
        return 0
    fi
    echo "  Downloading IP2Location LITE ${code}…"
    local zip="$GEO_DATA_DIR/${dest}.zip"
    # IP2Location returns HTTP 200 with a text error body for a bad token or an
    # exceeded quota, so unzip failure (below) is the real success signal.
    if curl -fsSL --max-time 180 -o "$zip" \
        "https://www.ip2location.com/download/?token=${IP2LOCATION_TOKEN}&file=${code}"; then
        if unzip -o -j "$zip" "$member" -d "$GEO_DATA_DIR" >/dev/null 2>&1; then
            mv -f "$GEO_DATA_DIR/$member" "$GEO_DATA_DIR/$dest"
            rm -f "$zip"
            echo "  IP2Location LITE $code downloaded to $GEO_DATA_DIR"
        else
            rm -f "$zip"
            echo "  WARNING: IP2Location $code not a valid ZIP (bad token or quota?); skipping."
        fi
    else
        rm -f "$zip"
        echo "  WARNING: IP2Location $code download failed; skipping."
    fi
}

if [[ -n "${IP2LOCATION_TOKEN:-}" ]]; then
    fetch_ip2location DB9LITEMMDB ip2location-lite-db9.mmdb 'IP2LOCATION-LITE-DB9.MMDB'
else
    echo "  IP2Location LITE (optional): export IP2LOCATION_TOKEN to enable this source"
fi

echo "=== setup.sh complete ==="
