#!/usr/bin/env bash
# Scan the working tree for committed secrets.
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
need_tool gitleaks

section "secrets: gitleaks"
# Scan from the repo root with a relative target so reported paths are relative,
# which keeps the .gitleaks.toml allowlist patterns simple and anchored.
cd "${repo_root}"
gitleaks dir . --config .gitleaks.toml --no-banner
