#!/bin/sh

set -eu

KEYS_ROOT=".dev"
KEYS_DIR="$KEYS_ROOT/keys"
LOCK_DIR="$KEYS_ROOT/.keys.lock"
PRIVATE_KEY_FILE="$KEYS_DIR/private.pem"
PUBLIC_KEY_FILE="$KEYS_DIR/public.pem"

if ! command -v openssl >/dev/null 2>&1; then
    echo "error: openssl is not installed or not in PATH" >&2
    exit 1
fi

umask 077
mkdir -p "$KEYS_ROOT"

if ! mkdir "$LOCK_DIR"; then
    echo "error: JWT key generation is already running" >&2
    exit 1
fi

TMP_DIR=""

cleanup() {
    if [ -n "$TMP_DIR" ] && [ -d "$TMP_DIR" ]; then
        rm -rf "$TMP_DIR"
    fi
    rmdir "$LOCK_DIR"
}

trap cleanup EXIT
trap 'exit 1' HUP INT TERM

if [ -e "$KEYS_DIR" ] || [ -L "$KEYS_DIR" ]; then
    echo "error: JWT key directory already exists" >&2
    echo "remove it manually if you want to regenerate the key pair" >&2
    exit 1
fi

TMP_DIR="$(mktemp -d "$KEYS_ROOT/.keys.tmp.XXXXXX")"

TMP_PRIVATE_KEY_FILE="$TMP_DIR/private.pem"
TMP_PUBLIC_KEY_FILE="$TMP_DIR/public.pem"

openssl genpkey \
    -algorithm ED25519 \
    -out "$TMP_PRIVATE_KEY_FILE"

openssl pkey \
    -in "$TMP_PRIVATE_KEY_FILE" \
    -pubout \
    -out "$TMP_PUBLIC_KEY_FILE"

chmod 600 "$TMP_PRIVATE_KEY_FILE"

if [ -e "$KEYS_DIR" ] || [ -L "$KEYS_DIR" ]; then
    echo "error: JWT key directory appeared during generation; refusing to overwrite" >&2
    exit 1
fi

mv "$TMP_DIR" "$KEYS_DIR"
TMP_DIR=""

echo "JWT key pair generated:"
echo "    private: $PRIVATE_KEY_FILE"
echo "    public: $PUBLIC_KEY_FILE"
