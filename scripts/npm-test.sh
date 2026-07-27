#!/usr/bin/env bash
# npm distribution channel tests (npm/). Part of the verify.sh gate: Node >= 20
# is a hard requirement, not a soft skip — a missing runtime fails the gate.
set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

cd "$repo_root"

section "npm-test: node --test npm/test"

have node || die "node not found — install Node.js >= 20 (https://nodejs.org); required by the npm distribution channel tests"

major="$(node -p 'process.versions.node.split(".")[0]')"
[ "$major" -ge 20 ] || die "node $(node --version) is too old — the npm distribution channel requires Node.js >= 20"

# Glob expanded by the shell: node's own --test glob support needs Node >= 21,
# but Node 20 is the supported floor.
node --test npm/test/*.test.mjs

log_info "npm tests passed"
