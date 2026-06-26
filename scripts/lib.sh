#!/usr/bin/env bash
# Shared helpers sourced by every script in scripts/. Not executable on its own.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export repo_root

# Pinned tools install here; prefer them over any system install.
export PATH="${repo_root}/bin:${PATH}"
export GOBIN="${repo_root}/bin"

_color() { if [ -t 2 ]; then printf '%s' "$1"; fi; }
_reset() { _color $'\033[0m'; }

log_info()  { printf '%s[info]%s %s\n'  "$(_color $'\033[34m')" "$(_reset)" "$*" >&2; }
log_warn()  { printf '%s[warn]%s %s\n'  "$(_color $'\033[33m')" "$(_reset)" "$*" >&2; }
log_error() { printf '%s[err ]%s %s\n'  "$(_color $'\033[31m')" "$(_reset)" "$*" >&2; }
die()       { log_error "$*"; exit 1; }
section()   { printf '\n%s== %s ==%s\n' "$(_color $'\033[1m')" "$*" "$(_reset)" >&2; }

have() { command -v "$1" >/dev/null 2>&1; }

# Read a pinned version from .config/tool-versions.
tool_version() {
	local name="$1" file="${repo_root}/.config/tool-versions" line
	line="$(grep -E "^${name}=" "$file" || true)"
	[ -n "$line" ] || die "no pinned version for ${name} in ${file}"
	printf '%s' "${line#*=}"
}

# Fail with guidance if a pinned tool is missing.
need_tool() {
	have "$1" || die "missing tool '$1' — run scripts/bootstrap.sh"
}
