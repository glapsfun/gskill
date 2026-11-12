---
name: gskill-install
description: Install, locate, verify, or upgrade the GSKILL CLI (gskill) on Linux or macOS, then install the official gskill skill. Use when the user asks to install, set up, configure, or start using GSKILL, when a gskill command is not found, or when they ask which GSKILL version is installed or want a different one.
license: MIT
compatibility: Linux or macOS on amd64 or arm64, with a shell tool. Installs GSKILL 0.12.0 or newer.
---

# Install GSKILL

GSKILL is a package manager for agent skills, shipped as one `gskill` binary. This skill gets
a working `gskill` onto the machine, or confirms one is already there. It needs only a shell.

Work through the steps in order. Stop at the first one that settles the request.

## 1. Check for a working install

```sh
command -v gskill
gskill --json version
```

- Both work and `version` is 0.12.0 or newer (a leading `v` is fine): GSKILL is ready. Report
  the path and version and change nothing. Continue at step 7 only if the user asked for a
  different version, else at step 8.
- A version with a suffix, such as `v0.12.1-0.20261112112558-afb0314d28b3+dirty`, is a build
  from source. Treat it as working and say so.
- Not found: try the usual locations before installing anything:
  `"$HOME/.local/bin/gskill" version`, `/usr/local/bin/gskill version`,
  and, when `go` is installed, `"$(go env GOPATH)/bin/gskill" version`. If one works, it is installed but not on `PATH`;
  go to step 6.
- Older than 0.12.0: go to step 7.

## 2. Check the platform

```sh
uname -s
uname -m
```

GSKILL supports `Linux` and `Darwin` on `x86_64`, `amd64`, `aarch64`, or `arm64`. On
anything else, stop and explain (see [troubleshooting](references/troubleshooting.md)).

## 3. Choose a method

Use the method the user asked for. With no preference, use the install script: it needs only
`curl` or `wget`, `tar`, and `sha256sum` or `shasum`, it verifies the checksum, and
`gskill self upgrade` can update it later. Check what the machine has:

```sh
for t in curl wget tar sha256sum shasum brew go npm node docker podman; do command -v "$t"; done
```

Each method's command, version pin, and needs are in
[install methods](references/install-methods.md). Use only those methods.

## 4. Ask before changing the machine

Tell the user the method, the version, and the install directory, then wait for approval.
Ask again, separately, before using `sudo` and before editing a shell profile. Skip a question
only when the request already answered it ("install GSKILL with Homebrew").

## 5. Install and verify

Run the chosen method's command. Then run the new binary by its absolute path:

```sh
"$HOME/.local/bin/gskill" version
```

Use the path the installer printed. Success means it prints a version. When the user asked for
a version, confirm it matches; if it does not, say so.

## 6. Make it usable in this session

- If `command -v gskill` now finds it, nothing more is needed.
- Otherwise, run every later command in this session by the absolute path. Many agent shells
  start a fresh shell per command, so an `export PATH=...` does not carry over.
- Give the user the line for new shells, and edit their profile only if they agree:
  `export PATH="$HOME/.local/bin:$PATH"` (with the real directory).

## 7. Upgrade or change version

Ask GSKILL how it was installed and what an upgrade would do. Nothing changes in a dry run:

```sh
gskill --json --dry-run self upgrade
gskill --json --dry-run self upgrade --to 0.12.0
```

Read `channel`, `installed_version`, `target_version`, `outcome`, and `next_command`:

- `standalone` (install script or release archive): after approval, run
  `gskill --yes self upgrade`, adding `--to X.Y.Z` for an exact version. Then run
  `gskill version`.
- `homebrew`, `npm-global`, `npm-project`, `npx`, `npm`, `go`, `container`: that tool owns the
  install. Show `next_command` and run it only after approval. Never install a second copy
  through another method.
- `unknown`: GSKILL could not tell which tool owns it, so `next_command` lists every tool's
  command. Show `executable` and ask which tool installed it.
- `development`: a build from source, which cannot upgrade itself (exit 1). Explain and stop.

Exit code 8 means GSKILL printed the upgrade command and changed nothing. `self upgrade` never
downgrades; an older version means reinstalling with that method's exact-version form, after
the user agrees. Releases 0.9.0 and earlier have no `self upgrade`: rerun the install method.

## 8. Install the usage skill

With GSKILL working, offer the official `gskill` skill so the user can manage skills by asking.
For this project:

```sh
gskill add github.com/glapsfun/gskill/skills/gskill --ref v0.13.0 --agent claude
```

Replace `claude` with the user's agent: `claude`, `codex`, `cursor`, `antigravity`,
`opencode`, `openclaw`, or `hermes`. Add `--global` to install it for every project instead.
After a project install for `hermes`, the user runs `hermes skills trust` once.

Do not reinstall this skill (`gskill-install`) with GSKILL. It was copied in by hand, and GSKILL
refuses to overwrite a folder it does not manage.

## 9. When something fails

Match the error in [troubleshooting](references/troubleshooting.md) and give its next step. Do
not retry with `sudo` or another method without asking.

## Report

End with: the path and version of `gskill`, the method used (or "already installed"), and
anything the user still has to do, such as the `PATH` line or `hermes skills trust`.
