#!/bin/sh

set -eu

SCRIPT_DIR="$(CDPATH= cd "$(dirname "$0")" && pwd)"
SCRIPT_PATH="$SCRIPT_DIR/generate-jwt-keys.sh"
TEST_DIR="$(mktemp -d)"

trap 'rm -rf "$TEST_DIR"' EXIT

mkdir "$TEST_DIR/success" "$TEST_DIR/failure" "$TEST_DIR/fake-bin"

(
    cd "$TEST_DIR/success"
    "$SCRIPT_PATH" >/dev/null

    openssl pkey -in .dev/keys/private.pem -pubout -out derived-public.pem
    cmp .dev/keys/public.pem derived-public.pem
    cp .dev/keys/private.pem original-private.pem

    if "$SCRIPT_PATH" >/dev/null 2>&1; then
        echo "error: repeated generation overwrote existing keys" >&2
        exit 1
    fi
    cmp .dev/keys/private.pem original-private.pem
)

printf '#!/bin/sh\nexit 1\n' > "$TEST_DIR/fake-bin/mv"
chmod +x "$TEST_DIR/fake-bin/mv"

(
    cd "$TEST_DIR/failure"
    if PATH="$TEST_DIR/fake-bin:$PATH" "$SCRIPT_PATH" >/dev/null 2>&1; then
        echo "error: failed publication reported success" >&2
        exit 1
    fi
    if [ -e .dev/keys ] || [ -e .dev/.keys.lock ]; then
        echo "error: failed publication left keys or a lock behind" >&2
        exit 1
    fi

    mkdir .dev/.keys.lock
    if "$SCRIPT_PATH" >/dev/null 2>&1; then
        echo "error: another generation bypassed the lock" >&2
        exit 1
    fi
    if [ -e .dev/keys ]; then
        echo "error: generation published keys despite the lock" >&2
        exit 1
    fi
)

echo "JWT key generation checks passed"
