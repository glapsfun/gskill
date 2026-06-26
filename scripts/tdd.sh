#!/usr/bin/env bash
# Inner TDD loop: re-run tests whenever a .go file changes.
# Uses entr if available, otherwise falls back to a polling loop.
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

run_tests() { go test -shuffle=on ./... || true; }

if have entr; then
	section "tdd: watching with entr (Ctrl-C to stop)"
	find "$repo_root" -name '*.go' -not -path '*/bin/*' | entr -c "${repo_root}/scripts/test.sh" --short
else
	log_warn "entr not found — using 2s polling loop (install entr for instant feedback)"
	section "tdd: polling every 2s (Ctrl-C to stop)"
	last=""
	while true; do
		now="$(find "$repo_root" -name '*.go' -not -path '*/bin/*' -exec stat -f '%m %N' {} + 2>/dev/null | sort | md5)"
		if [ "$now" != "$last" ]; then
			clear || true
			run_tests
			last="$now"
		fi
		sleep 2
	done
fi
