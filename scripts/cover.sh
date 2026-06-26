#!/usr/bin/env bash
# Enforce a minimum total coverage from coverage.out. Floor: $COVERAGE_MIN (default 0).
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

profile="${repo_root}/coverage.out"
min="${COVERAGE_MIN:-0}"

[ -f "$profile" ] || die "no coverage profile at ${profile} — run scripts/test.sh first"

section "cover: enforcing minimum ${min}%"
total="$(go tool cover -func="$profile" | awk '/^total:/ {gsub(/%/,"",$3); print $3}')"
log_info "total coverage: ${total}%"

# Float comparison via awk; exit non-zero if below the floor.
awk -v t="$total" -v m="$min" 'BEGIN { exit (t+0 >= m+0) ? 0 : 1 }' \
	|| die "coverage ${total}% is below minimum ${min}%"
