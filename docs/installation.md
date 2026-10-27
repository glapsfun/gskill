# Install GSKILL

GSKILL is a single `gskill` binary for Linux and macOS, on amd64 and arm64. Pick one install
method below; each one gives you the same command. Windows is not supported.

<!-- install-methods:start -->
### Install script

Downloads the right archive for your platform and verifies its checksum.

```sh
curl -sSfL https://raw.githubusercontent.com/glapsfun/gskill/main/scripts/install.sh | sh
```

### Homebrew

macOS and Linux.

```sh
brew install glapsfun/tap/gskill
```

### Go

Requires a Go toolchain.

```sh
go install github.com/glapsfun/gskill/cmd/gskill@latest
```

### npm

Requires Node.js 20 or newer.

```sh
npm install --global @glapsfun/gskill
```

### npx

Run without installing. Requires Node.js 20 or newer.

```sh
npx @glapsfun/gskill version
```

```sh
npx @glapsfun/gskill add github.com/glapsfun/gskill/examples/skills/hello-gskill --agent claude
```
<!-- install-methods:end -->

## Check the install

```sh
gskill version
```

If your shell cannot find `gskill`, add the install location to your `PATH`:

- Install script: `/usr/local/bin` when it is writable, otherwise `~/.local/bin`. Set
  `INSTALL_DIR` before running the script to choose another directory.
- Go: `$(go env GOPATH)/bin`.

## Verify a release

Every release archive is checksummed, cosign-signed, and ships with SBOMs and build-provenance
attestations. To check them yourself, see [Cut and verify a release](how-to/releasing.md).

## Next steps

- [Getting started](tutorials/getting-started.md): from an empty directory to a committed,
  reproducible skill environment.
- [Add a skill from Git](how-to/add-a-git-skill.md)
- [Supported agents](reference/agents.md)
