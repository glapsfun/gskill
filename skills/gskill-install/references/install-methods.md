# Install methods

Each method gives the same `gskill` binary. Use only these. PyPI, `uvx`, and `pipx` are not
supported channels.

Every method needs Linux or macOS on amd64 or arm64.

## Install script (default)

- **Needs**: `curl` or `wget`, `tar`, and `sha256sum` or `shasum`.
- **Install the latest release**:

  ```sh
  curl -sSfL https://raw.githubusercontent.com/glapsfun/gskill/main/scripts/install.sh | sh
  ```

- **Install an exact version**: set `VERSION` to the release tag, with the `v`:

  ```sh
  curl -sSfL https://raw.githubusercontent.com/glapsfun/gskill/main/scripts/install.sh | VERSION=v0.12.0 sh
  ```

- **Choose the directory**: set `INSTALL_DIR`. Without it, the script uses `/usr/local/bin`
  when that is writable (or when running as root), else `$HOME/.local/bin`. To avoid `sudo`,
  pick the user directory:

  ```sh
  curl -sSfL https://raw.githubusercontent.com/glapsfun/gskill/main/scripts/install.sh | INSTALL_DIR="$HOME/.local/bin" sh
  ```

- **Verifies**: the archive's SHA-256 against the release's `checksums.txt`, before installing.
  It stops on a mismatch.
- **Where it lands**: the script prints `install: installed gskill to <dir>/gskill`. Use that
  path for the rest of the session, for example `"$HOME/.local/bin/gskill" version`.
- **Network**: `api.github.com` (to find the latest release), `github.com`, and
  `objects.githubusercontent.com`.
- **Upgrade owner**: `gskill self upgrade` replaces this binary in place.

## Homebrew

- **Needs**: `brew`.
- **Install**:

  ```sh
  brew install glapsfun/tap/gskill
  ```

- **Exact version**: not available. Homebrew installs the current release. If the user needs a
  specific older version, use the install script instead and say why.
- **Verifies**: Homebrew checks the download.
- **Upgrade owner**: Homebrew. `brew upgrade glapsfun/tap/gskill`.

## Go

- **Needs**: a Go toolchain.
- **Install**:

  ```sh
  go install github.com/glapsfun/gskill/cmd/gskill@latest
  ```

- **Exact version**: replace `@latest` with the tag, for example `@v0.12.0`.
- **Where it lands**: `$(go env GOPATH)/bin/gskill`.
- **Verifies**: Go module checksums.
- **Upgrade owner**: Go. Run `go install` again with the new version.

## npm

- **Needs**: Node.js 20 or newer (`node --version`).
- **Install**:

  ```sh
  npm install --global @glapsfun/gskill
  ```

- **Exact version**: `npm install --global @glapsfun/gskill@0.12.0` (no `v`).
- **Run without installing**: `npx @glapsfun/gskill version`.
- **Verifies**: npm checks the package.
- **Upgrade owner**: npm.

## Container image

- **Needs**: Docker, Podman, or another container runtime. Linux images only.
- **Run**:

  ```sh
  docker run --rm ghcr.io/glapsfun/gskill:latest version
  ```

- **Exact version**: use the tag without `v`, for example `ghcr.io/glapsfun/gskill:0.12.0`.
- **Use it on a project**: mount the project and run as the current user:

  ```sh
  docker run --rm --user "$(id -u):$(id -g)" -e HOME=/tmp -v "$PWD:/workspace" -w /workspace ghcr.io/glapsfun/gskill:latest list
  ```

- **Note**: this is not an install on the host. `gskill` stays unavailable as a command.
  Offer it when the user does not want to install anything.
- **Upgrade owner**: pull a newer tag.

## Upgrade GSKILL

`gskill --json --dry-run self upgrade` reports `channel`, the way this binary was installed.
It changes nothing.

| `channel` | What to do |
| --- | --- |
| `standalone` | After approval, `gskill --yes self upgrade` (add `--to X.Y.Z` for an exact version). It verifies the release signature and checksum, then replaces the binary in place. |
| `homebrew` | Show `next_command` (`brew upgrade glapsfun/tap/gskill`) and run it after approval. |
| `npm-global`, `npm-project`, `npx`, `npm` | Show `next_command` and run it after approval. |
| `go` | Show `next_command` (`go install ...@<version>`) and run it after approval. |
| `container` | Pull the newer image tag; nothing on the host changes. |
| `development` | A build from source. `self upgrade` refuses with exit 1. Explain; installing a release means choosing a method above, after approval. |
| `unknown` | GSKILL could not tell which tool owns `executable`, so `next_command` lists every tool's command. Show the path and ask the user which tool installed it. |

For the package-managed channels and `unknown`, `self upgrade` prints the command and exits
with code 8 ("an update is available; nothing was changed"). Never install a second copy through another
channel to get a newer version. `self upgrade` never downgrades.

## Choosing

| Situation | Method |
| --- | --- |
| No preference | Install script |
| The user's tools are managed by Homebrew | Homebrew |
| The user wants npm or Go to own it | npm or Go |
| An exact older version is required | Install script with `VERSION`, or npm or Go |
| Nothing may be installed on the host | Container image |
