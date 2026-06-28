#!/usr/bin/env bash
# Install pinned dev tools into ./bin and activate pre-commit hooks.
set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

section "bootstrap: installing pinned tools into ${repo_root}/bin"
mkdir -p "${repo_root}/bin"

install_tool() {
	local bin="$1" module="$2" version
	version="$(tool_version "$3")"
	log_info "installing ${bin}@${version}"
	GOBIN="${repo_root}/bin" go install "${module}@${version}"
}

install_tool golangci-lint github.com/golangci/golangci-lint/v2/cmd/golangci-lint GOLANGCI_LINT
install_tool govulncheck   golang.org/x/vuln/cmd/govulncheck                       GOVULNCHECK
install_tool gitleaks      github.com/zricethezav/gitleaks/v8                       GITLEAKS
install_tool actionlint    github.com/rhysd/actionlint/cmd/actionlint              ACTIONLINT

# zizmor is a PyPI tool (not go-installable); scripts/audit-workflows.sh runs it via
# `uvx zizmor==$(tool_version ZIZMOR)`. We only need `uv` present, not a global install.
if ! have uv; then
	log_warn "uv not found — install it (https://docs.astral.sh/uv) so scripts/audit-workflows.sh can run zizmor via uvx"
fi

if ! have pre-commit; then
	log_warn "pre-commit not found — install it (https://pre-commit.com) then re-run to enable git hooks"
elif [ ! -f "${repo_root}/.pre-commit-config.yaml" ]; then
	log_warn "no .pre-commit-config.yaml yet — skipping hook install (re-run after it exists)"
else
	section "bootstrap: installing git hooks"
	pre-commit install --install-hooks
	pre-commit install --hook-type pre-push
fi

log_info "bootstrap complete"
