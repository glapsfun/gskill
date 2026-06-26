#!/usr/bin/env bash
# Run Go tests. Default: race + shuffle + coverage profile to coverage.out.
# Usage: test.sh [--short]   (--short skips race/coverage for the fast pre-commit hook)
set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

if [ "${1:-}" = "--short" ]; then
	section "test: short"
	go test -shuffle=on ./...
else
	section "test: race + coverage"
	go test -race -shuffle=on -coverprofile="${repo_root}/coverage.out" -covermode=atomic ./...
fi
