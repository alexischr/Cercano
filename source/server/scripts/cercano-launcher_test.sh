#!/usr/bin/env bash
# Exercise only build_install: never run the launcher's process management.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$SCRIPT_DIR/../../.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
SERVER_DIR="$TMP/server"
mkdir -p "$SERVER_DIR/scripts"
printf '#!/bin/sh\nexit 0\n' > "$SERVER_DIR/scripts/codesign-if-available.sh"
chmod +x "$SERVER_DIR/scripts/codesign-if-available.sh"
# Extract the actual function, avoiding mkdir, rebuilds, kill, and exec at top level.
sed -n '/^build_install() {/,/^}/p' "$SCRIPT_DIR/cercano-launcher.sh" > "$TMP/build_install.sh"
source "$TMP/build_install.sh"
go() {
    [[ "$1" == build && "$2" == -ldflags && "$3" == "-X main.version=$EXPECTED" && "$4" == -o ]] || return 1
    touch "$5"
}
unset VERSION
EXPECTED="$(git -C "$REPO" describe --tags --always --dirty | sed 's/^v//')"
build_install "$TMP/cli" "$REPO/source/clients/cli" .
[[ -f "$TMP/cli" ]]
VERSION="v9.87.654-rc.12"
EXPECTED="$VERSION"
build_install "$TMP/server-bin" "$REPO/source/server" ./cmd/cercano/
unset VERSION
REPO="$TMP/not-a-repo"
EXPECTED=dev
build_install "$TMP/fallback" "$TMP" .
echo PASS
