#!/usr/bin/env bash
# The single definition-of-done gate. Runs every check in order; stops at the
# first failure. Exit 0 means the work is correct. Used by the agent, pre-push,
# and CI.
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

cd "$repo_root"

section "verify: go mod tidy check"
go mod tidy
if ! git diff --quiet -- go.mod go.sum; then
	die "go.mod/go.sum not tidy — commit the result of 'go mod tidy'"
fi

"${repo_root}/scripts/fmt.sh" --check
"${repo_root}/scripts/lint.sh"
"${repo_root}/scripts/test.sh"
"${repo_root}/scripts/cover.sh"
"${repo_root}/scripts/vuln.sh"
"${repo_root}/scripts/secrets.sh"

section "verify: PASSED"
log_info "all checks passed — safe to call this done"
