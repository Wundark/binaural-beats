#!/usr/bin/env bash
# Align and sign a release APK so it can be installed.
#
# Uses the keystore in ANDROID_KEYSTORE_BASE64 (with ANDROID_KEYSTORE_PASSWORD
# and ANDROID_KEY_ALIAS) when set. Otherwise signs with a throwaway key, which
# is fine for test builds but means the APK cannot update an install signed
# with a different key.
#
# Usage:
#   ./scripts/sign-apk.sh <unsigned.apk> <output.apk>

set -euo pipefail

if [ $# -ne 2 ]; then
    echo "Usage: $0 <unsigned.apk> <output.apk>"
    exit 1
fi

UNSIGNED="$1"
OUTPUT="$2"

BUILD_TOOLS="$(ls -d "${ANDROID_HOME:?ANDROID_HOME not set}"/build-tools/*/ | sort -V | tail -1)"
BUILD_TOOLS="${BUILD_TOOLS%/}"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

if [ -n "${ANDROID_KEYSTORE_BASE64:-}" ]; then
    echo "Signing with provided keystore"
    echo "$ANDROID_KEYSTORE_BASE64" | base64 -d > "$WORK/release.jks"
    STORE_PASS="${ANDROID_KEYSTORE_PASSWORD:?ANDROID_KEYSTORE_PASSWORD not set}"
    ALIAS="${ANDROID_KEY_ALIAS:?ANDROID_KEY_ALIAS not set}"
else
    echo "No keystore provided, signing with a throwaway key"
    STORE_PASS="android"
    ALIAS="throwaway"
    keytool -genkeypair -keystore "$WORK/release.jks" \
        -storepass "$STORE_PASS" -keypass "$STORE_PASS" -alias "$ALIAS" \
        -keyalg RSA -keysize 2048 -validity 365 -dname "CN=Binaural Beats Test Build"
fi

"$BUILD_TOOLS/zipalign" -f -p 4 "$UNSIGNED" "$WORK/aligned.apk"
"$BUILD_TOOLS/apksigner" sign \
    --ks "$WORK/release.jks" --ks-pass "pass:$STORE_PASS" --ks-key-alias "$ALIAS" \
    --out "$OUTPUT" "$WORK/aligned.apk"
"$BUILD_TOOLS/apksigner" verify "$OUTPUT"

echo "Signed APK: $OUTPUT"
