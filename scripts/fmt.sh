#!/usr/bin/env bash
# Format Go code with golangci-lint's bundled formatters (gofmt + gofumpt).
# Usage: fmt.sh [--check]   (--check fails if files are not formatted; no writes)
set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
need_tool golangci-lint

if [ "${1:-}" = "--check" ]; then
	section "fmt: checking formatting"
	if ! golangci-lint fmt --diff; then
		die "formatting issues found — run scripts/fmt.sh to fix"
	fi
else
	section "fmt: applying formatting"
	golangci-lint fmt
fi
