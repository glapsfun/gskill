#!/bin/sh
# gskill installer — downloads the right release archive for your OS/architecture,
# verifies it against the published checksums, and installs the `gskill` binary.
#
#   curl -sSfL https://raw.githubusercontent.com/glapsfun/gskill/main/scripts/install.sh | sh
#
# Environment overrides:
#   VERSION       release tag to install (e.g. v0.4.0). Default: latest stable release.
#   INSTALL_DIR   target bin directory. Default: /usr/local/bin, else $HOME/.local/bin.
#
# gskill ships for Linux and macOS (amd64/arm64) only. Windows is not supported.

set -eu

OWNER="glapsfun"
REPO="gskill"
BIN="gskill"

err() { echo "install: $*" >&2; exit 1; }
have() { command -v "$1" >/dev/null 2>&1; }

# --- pick a downloader -------------------------------------------------------
if have curl; then
  dl() { curl -sSfL "$1" -o "$2"; }
  dl_stdout() { curl -sSfL "$1"; }
elif have wget; then
  dl() { wget -qO "$2" "$1"; }
  dl_stdout() { wget -qO - "$1"; }
else
  err "need curl or wget"
fi

# --- detect platform ---------------------------------------------------------
os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  linux) os=linux ;;
  darwin) os=darwin ;;
  *) err "unsupported OS '$os' — gskill supports Linux and macOS only (Windows is not supported)" ;;
esac

arch=$(uname -m)
case "$arch" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) err "unsupported architecture '$arch' — gskill supports amd64 and arm64 only" ;;
esac

# --- resolve version ---------------------------------------------------------
version="${VERSION:-}"
if [ -z "$version" ]; then
  version=$(dl_stdout "https://api.github.com/repos/${OWNER}/${REPO}/releases/latest" \
    | grep '"tag_name"' | head -n1 | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')
  [ -n "$version" ] || err "could not determine the latest release (is there a published release yet?)"
fi
ver_no_v=${version#v}

archive="${BIN}_${ver_no_v}_${os}_${arch}.tar.gz"
base="https://github.com/${OWNER}/${REPO}/releases/download/${version}"

# --- download archive + checksums --------------------------------------------
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
echo "install: downloading ${archive} (${version})"
dl "${base}/${archive}" "${tmp}/${archive}" || err "download failed: ${base}/${archive}"
dl "${base}/checksums.txt" "${tmp}/checksums.txt" || err "could not fetch checksums.txt"

# --- verify checksum (before installing) -------------------------------------
expected=$(grep " ${archive}\$" "${tmp}/checksums.txt" | awk '{print $1}')
[ -n "$expected" ] || err "no checksum entry for ${archive}"
if have sha256sum; then
  actual=$(sha256sum "${tmp}/${archive}" | awk '{print $1}')
elif have shasum; then
  actual=$(shasum -a 256 "${tmp}/${archive}" | awk '{print $1}')
else
  err "need sha256sum or shasum to verify the download"
fi
[ "$expected" = "$actual" ] || err "checksum mismatch for ${archive} (expected ${expected}, got ${actual})"
echo "install: checksum verified"

# --- extract + install -------------------------------------------------------
tar -xzf "${tmp}/${archive}" -C "$tmp" "$BIN" || err "could not extract ${BIN}"

dir="${INSTALL_DIR:-}"
if [ -z "$dir" ]; then
  if [ -w /usr/local/bin ] || [ "$(id -u)" = "0" ]; then
    dir=/usr/local/bin
  else
    dir="${HOME}/.local/bin"
  fi
fi
mkdir -p "$dir"
install -m 0755 "${tmp}/${BIN}" "${dir}/${BIN}" 2>/dev/null \
  || { mv "${tmp}/${BIN}" "${dir}/${BIN}" && chmod 0755 "${dir}/${BIN}"; }

echo "install: installed ${BIN} to ${dir}/${BIN}"
case ":${PATH}:" in
  *":${dir}:"*) ;;
  *) echo "install: add ${dir} to your PATH to run '${BIN}'" ;;
esac
"${dir}/${BIN}" version || true
