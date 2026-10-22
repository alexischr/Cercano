#!/usr/bin/env bash
# Builds cercano-cli and the cercano agent server into test/e2e/.bin/ as
# siblings, which is the layout the CLI's auto-launch looks for.
set -euo pipefail

here="$(cd "$(dirname "$0")/.." && pwd)"
root="$(cd "$here/../.." && pwd)"
out="$here/.bin"
mkdir -p "$out"

(cd "$root/source/server" && go build -o "$out/cercano" ./cmd/cercano/)
(cd "$root/source/clients/cli" && go build -ldflags "-X main.version=e2e" -o "$out/cercano-cli" .)

echo "built $out/cercano and $out/cercano-cli"
