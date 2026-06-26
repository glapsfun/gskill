#!/usr/bin/env bash
# Scan called code paths for known vulnerabilities.
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
need_tool govulncheck

section "vuln: govulncheck"
govulncheck ./...
