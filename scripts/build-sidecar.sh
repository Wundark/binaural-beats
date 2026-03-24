#!/usr/bin/env bash
# Build the Go binaural-beats binary as a Tauri sidecar.
# Tauri requires sidecar binaries to be named with the platform target triple.
#
# Usage: ./scripts/build-sidecar.sh [target-triple]
#
# If no target triple is given, the current platform is detected.
# Examples:
#   ./scripts/build-sidecar.sh
#   ./scripts/build-sidecar.sh x86_64-pc-windows-msvc
#   ./scripts/build-sidecar.sh x86_64-apple-darwin
#   ./scripts/build-sidecar.sh aarch64-apple-darwin
#   ./scripts/build-sidecar.sh x86_64-unknown-linux-gnu

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
OUTPUT_DIR="$PROJECT_ROOT/tauri-app/src-tauri/binaries"

mkdir -p "$OUTPUT_DIR"

# Detect current platform if no target given
detect_target() {
    local os arch
    os="$(uname -s)"
    arch="$(uname -m)"

    case "$os" in
        Darwin)
            case "$arch" in
                x86_64) echo "x86_64-apple-darwin" ;;
                arm64)  echo "aarch64-apple-darwin" ;;
                *)      echo "unknown-apple-darwin" ;;
            esac
            ;;
        Linux)
            case "$arch" in
                x86_64)  echo "x86_64-unknown-linux-gnu" ;;
                aarch64) echo "aarch64-unknown-linux-gnu" ;;
                armv7l)  echo "armv7-unknown-linux-gnueabihf" ;;
                *)       echo "unknown-unknown-linux-gnu" ;;
            esac
            ;;
        MINGW*|MSYS*|CYGWIN*)
            echo "x86_64-pc-windows-msvc"
            ;;
        *)
            echo "unknown-unknown-unknown"
            ;;
    esac
}

TARGET="${1:-$(detect_target)}"

# Map target triple to GOOS/GOARCH
case "$TARGET" in
    x86_64-pc-windows-msvc)
        GOOS=windows GOARCH=amd64 EXT=".exe" ;;
    x86_64-apple-darwin)
        GOOS=darwin GOARCH=amd64 EXT="" ;;
    aarch64-apple-darwin)
        GOOS=darwin GOARCH=arm64 EXT="" ;;
    x86_64-unknown-linux-gnu)
        GOOS=linux GOARCH=amd64 EXT="" ;;
    aarch64-unknown-linux-gnu)
        GOOS=linux GOARCH=arm64 EXT="" ;;
    armv7-unknown-linux-gnueabihf)
        GOOS=linux GOARCH=arm EXT="" ;;
    *)
        echo "Error: Unknown target triple: $TARGET"
        exit 1
        ;;
esac

BINARY_NAME="binaural-beats-${TARGET}${EXT}"

echo "Building sidecar for $TARGET..."
echo "  GOOS=$GOOS GOARCH=$GOARCH"
echo "  Output: $OUTPUT_DIR/$BINARY_NAME"

cd "$PROJECT_ROOT"

CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" \
    go build -mod=vendor -ldflags="-s -w" \
    -o "$OUTPUT_DIR/$BINARY_NAME" \
    ./cmd/binaural-beats/

echo "Done: $OUTPUT_DIR/$BINARY_NAME"
