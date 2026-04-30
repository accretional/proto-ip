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
PROTO_FILES=("$PROTO_DIR"/ipv4.proto "$PROTO_DIR"/ipv6.proto "$PROTO_DIR"/ip.proto "$PROTO_DIR"/subnet.proto "$PROTO_DIR"/cidr.proto "$PROTO_DIR"/local_lookup.proto)

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

echo "=== setup.sh complete ==="
