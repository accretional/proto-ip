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

# --- DB-IP City Lite database (CC BY 4.0) -----------------------------------
# Downloaded into a gitignored cache for the GeoLookup service. Idempotent:
# skips if a current- or previous-month file already exists. A download
# failure only WARNS — the geofeed source still works offline.
GEO_DATA_DIR="data/geoip"
mkdir -p "$GEO_DATA_DIR"

# month_offset N -> YYYY-MM for N months ago, handling BSD (darwin) and GNU date.
month_offset() {
    local n="$1"
    if date -v-"${n}"m +%Y-%m >/dev/null 2>&1; then
        date -v-"${n}"m +%Y-%m          # BSD/macOS
    else
        date -d "${n} months ago" +%Y-%m # GNU/Linux
    fi
}

THIS_MONTH=$(month_offset 0)
LAST_MONTH=$(month_offset 1)

if [[ -f "$GEO_DATA_DIR/dbip-city-lite-${THIS_MONTH}.mmdb" || \
      -f "$GEO_DATA_DIR/dbip-city-lite-${LAST_MONTH}.mmdb" ]]; then
    echo "  DB-IP City Lite DB present (skipping download)"
else
    echo "  Downloading DB-IP City Lite database…"
    fetched=false
    for M in "$THIS_MONTH" "$LAST_MONTH"; do
        URL="https://download.db-ip.com/free/dbip-city-lite-${M}.mmdb.gz"
        GZ="$GEO_DATA_DIR/dbip-city-lite-${M}.mmdb.gz"
        if curl -fsSL -o "$GZ" "$URL"; then
            gunzip -f "$GZ"
            echo "  DB-IP City Lite ${M} downloaded to $GEO_DATA_DIR"
            fetched=true
            break
        fi
        rm -f "$GZ"
    done
    if ! $fetched; then
        echo "  WARNING: DB-IP download failed; GeoLookup will run geofeed-only."
    fi
fi

echo "=== setup.sh complete ==="
