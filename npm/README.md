# @glapsfun/gskill

GSKILL is a reproducible package manager for agentic AI skills: it installs, versions, locks,
verifies, and restores `SKILL.md`-based instruction bundles across agents, machines, and CI.
This package is the official npm distribution of the native GSKILL CLI.

Full documentation: <https://github.com/glapsfun/gskill>

## Usage

Run without installing anything globally:

```sh
npx @glapsfun/gskill version
npx @glapsfun/gskill add github.com/owner/repo --skill example --agent codex
```

Or install globally to get `gskill` on your PATH:

```sh
npm install --global @glapsfun/gskill
gskill version
```

Pin an exact version for reproducible CI:

```sh
npx --yes @glapsfun/gskill@0.5.0 install --frozen-lockfile
```

The package version pins the native binary version. `--frozen-lockfile` governs your project's
skills; it does not select the CLI version.

`npx @glapsfun/gskill` is the only supported spelling — there is no unscoped alias, so
`npx glapsfungskill` and `npx glapsfun/gskill` do not work.

## How it works

This package contains a small dependency-free launcher. The actual CLI is a native binary
shipped in one of four platform packages, installed automatically as an exact-version optional
dependency matching your machine:

| Platform | Package |
| --- | --- |
| macOS x64 | `@glapsfun/gskill-darwin-x64` |
| macOS arm64 | `@glapsfun/gskill-darwin-arm64` |
| Linux x64 | `@glapsfun/gskill-linux-x64` |
| Linux arm64 | `@glapsfun/gskill-linux-arm64` |

The platform packages are internal dependencies — always install `@glapsfun/gskill`. The
launcher execs the binary with your arguments and terminal attached and returns its exit code;
it never downloads anything at run time. Package version `X.Y.Z` contains exactly the binaries
of GitHub Release `vX.Y.Z`.

## Requirements

- Node.js >= 20 (only for this npm distribution path — the CLI itself is a native binary).
- macOS or Linux on x64 or arm64. Windows is not supported; on unsupported platforms the
  launcher exits with a clear error before running anything.

Other install methods (Homebrew, curl installer, `go install`) are documented in the
[main README](https://github.com/glapsfun/gskill#install).
