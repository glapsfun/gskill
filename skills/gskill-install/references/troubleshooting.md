# Setup troubleshooting

Each entry gives the symptom, the cause, and the next step to tell the user.

## Unsupported platform

- **Symptom**: `uname -s` is not `Linux` or `Darwin`, or `uname -m` is not `x86_64`, `amd64`,
  `aarch64`, or `arm64`. The install script stops with "unsupported OS" or "unsupported
  architecture".
- **Cause**: GSKILL ships for Linux and macOS on amd64 and arm64 only. Windows is not supported.
- **Next step**: say so and stop. Do not try another method. Where a container runtime is
  available, the Linux container image can run GSKILL on a mounted project (see
  [install methods](install-methods.md#container-image)).

## No network

- **Symptom**: `download failed`, `could not determine the latest release`, or a DNS or TLS
  error from `curl`, `brew`, `go`, or `npm`.
- **Cause**: the machine cannot reach GitHub or the package registry.
- **Next step**: name the hosts the method needs (for the install script: `api.github.com`,
  `github.com`, `objects.githubusercontent.com`) and ask the user to restore access or set
  their proxy. Nothing was installed. Setting `VERSION` skips the `api.github.com` lookup.

## Permission denied on the install directory

- **Symptom**: the install fails writing `/usr/local/bin`, or `npm install --global` fails with
  `EACCES`.
- **Cause**: the directory belongs to root.
- **Next step**: install into the user's own directory instead
  (`INSTALL_DIR="$HOME/.local/bin"` for the install script). Use `sudo` only if the user
  approves it explicitly.

## Installed but not on PATH

- **Symptom**: the install succeeded, but `command -v gskill` prints nothing. The install script
  printed `install: add <dir> to your PATH`.
- **Cause**: the install directory is not on `PATH`. In many agent shells, an `export` does
  not carry over to the next command either.
- **Next step**: run the binary by its absolute path for the rest of the session, for example
  `"$HOME/.local/bin/gskill" version`. Give the user the line for new shells, without editing
  their profile unless they agree:

  ```sh
  export PATH="$HOME/.local/bin:$PATH"
  ```

  It goes in `~/.bashrc`, `~/.zshrc`, or the profile file of their shell.

## Missing prerequisites

- **Symptom**: `need curl or wget`, `need sha256sum or shasum`, `brew: command not found`,
  `go: command not found`, `npm: command not found`, or Node.js older than 20.
- **Cause**: the chosen method needs a tool the machine lacks.
- **Next step**: pick another method whose needs are met (see
  [install methods](install-methods.md)), or name the tool to install first.

## Two installs present

- **Symptom**: `which -a gskill` lists more than one path, or `gskill version` shows a
  different version than the one just installed.
- **Cause**: an earlier install from another channel (for example Homebrew and the install
  script) comes first on `PATH`.
- **Next step**: report both paths and which one `PATH` runs. Do not install a third copy. Ask
  which one the user wants to keep, and remove the other with the method that installed it.

## Installed version too old

- **Symptom**: `gskill --json version` reports a version older than 0.12.0.
- **Cause**: an old release. Releases 0.9.0 and earlier also lack `gskill self upgrade`.
- **Next step**: upgrade through the method that owns the install (see the upgrade steps in
  [SKILL.md](../SKILL.md)). For 0.9.0 and earlier, rerun that method's install command.

## Something else

Run `gskill doctor` once the CLI works. It checks `git`, the detected agents, and declared
requirements, and prints a fix for each warning.
