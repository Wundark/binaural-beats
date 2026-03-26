#!/usr/bin/env bash
# Build the Go binaural-beats engine as a C shared library for Android.
#
# Prerequisites:
#   - Android NDK installed (set ANDROID_NDK_HOME or NDK will be auto-detected)
#   - Go 1.20+
#
# Usage:
#   ./scripts/build-android-lib.sh
#
# Output:
#   tauri-app/src-tauri/gen/android/app/src/main/jniLibs/arm64-v8a/libbinaural.so
#   tauri-app/src-tauri/gen/android/app/src/main/jniLibs/armeabi-v7a/libbinaural.so

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# Auto-detect NDK
if [ -z "${ANDROID_NDK_HOME:-}" ]; then
    if [ -d "${ANDROID_HOME:-}/ndk" ]; then
        ANDROID_NDK_HOME="$(ls -d "$ANDROID_HOME"/ndk/*/ 2>/dev/null | sort -V | tail -1)"
        ANDROID_NDK_HOME="${ANDROID_NDK_HOME%/}"
    fi
fi

if [ -z "${ANDROID_NDK_HOME:-}" ]; then
    echo "Error: ANDROID_NDK_HOME not set and could not be auto-detected."
    echo "Set ANDROID_NDK_HOME to your Android NDK installation path."
    exit 1
fi

TOOLCHAIN="$ANDROID_NDK_HOME/toolchains/llvm/prebuilt/linux-x86_64"
if [ ! -d "$TOOLCHAIN" ]; then
    # Try macOS path
    TOOLCHAIN="$ANDROID_NDK_HOME/toolchains/llvm/prebuilt/darwin-x86_64"
fi
if [ ! -d "$TOOLCHAIN" ]; then
    echo "Error: Could not find NDK toolchain at $ANDROID_NDK_HOME/toolchains/llvm/prebuilt/"
    exit 1
fi

MIN_SDK=24

build_arch() {
    local goarch="$1"
    local cc="$2"
    local jni_dir="$3"

    local output_dir="$PROJECT_ROOT/tauri-app/src-tauri/gen/android/app/src/main/jniLibs/$jni_dir"
    mkdir -p "$output_dir"

    echo "Building libbinaural.so for $jni_dir (GOARCH=$goarch)..."

    local sysroot="$TOOLCHAIN/sysroot"
    local cc_path="$TOOLCHAIN/bin/$cc"

    cd "$PROJECT_ROOT"
    CGO_ENABLED=1 \
    GOOS=android \
    GOARCH="$goarch" \
    CC="$cc_path" \
    CXX="$cc_path++" \
    CGO_CFLAGS="--sysroot=$sysroot -D__ANDROID_API__=$MIN_SDK" \
    CGO_CXXFLAGS="--sysroot=$sysroot -D__ANDROID_API__=$MIN_SDK" \
    CGO_LDFLAGS="--sysroot=$sysroot -llog -landroid" \
    go build -buildmode=c-shared -mod=vendor \
        -ldflags="-s -w" \
        -o "$output_dir/libbinaural.so" \
        ./cmd/binaural-beats-lib/

    # Remove the generated header (Rust uses its own bindings)
    rm -f "$output_dir/libbinaural.h"

    echo "  -> $output_dir/libbinaural.so"
}

build_arch "arm64" "aarch64-linux-android${MIN_SDK}-clang" "arm64-v8a"
build_arch "arm"   "armv7a-linux-androideabi${MIN_SDK}-clang" "armeabi-v7a"

echo "Done. Android shared libraries built."
