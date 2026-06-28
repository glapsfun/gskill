#!/usr/bin/env bash
# Audit GitHub Actions workflows: actionlint (correctness) + zizmor (security,
# incl. hash-pin enforcement). Same invocation runs in CI's workflow-audit job, so a
# green local run and a green CI run mean the same thing. Docs: docs/ci-cd.md.
set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

cd "$repo_root"

need_tool actionlint
section "audit: actionlint"
actionlint

# zizmor is a PyPI tool run via uvx at the version pinned in .config/tool-versions.
need_tool uv
zizmor_version="$(tool_version ZIZMOR)"
section "audit: zizmor ${zizmor_version}"
uvx "zizmor==${zizmor_version#v}" --config .github/zizmor.yml .github/workflows

section "audit: PASSED"
