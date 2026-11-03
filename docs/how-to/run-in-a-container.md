# Run GSKILL in a container

Run GSKILL from its container image against a project on your machine. Nothing is installed on
the host except a container runtime, which suits CI jobs and machines where you would rather not
add a binary.

## Before you start

- Docker, Podman, or another container runtime. The examples use `docker`; with Podman, write
  `podman` instead.
- A machine that runs Linux images. The image is published for `linux/amd64` and `linux/arm64`,
  and the runtime picks the right one. On macOS and Windows, Docker Desktop and Podman Desktop
  run it in their Linux VM.
- A project directory. For Claude Code, it has a `.claude/` folder.

## Steps

1. From the project directory, add a skill with the project mounted:

   ```sh
   docker run --rm --user "$(id -u):$(id -g)" -e HOME=/tmp \
     -v "$PWD:/workspace" -w /workspace \
     ghcr.io/glapsfun/gskill:latest add github.com/glapsfun/gskill/examples/skills/hello-gskill --agent claude
   ```

   Each flag has a job:
   - `--user "$(id -u):$(id -g)"` runs GSKILL as you, so the files it writes belong to you.
   - `-e HOME=/tmp` gives GSKILL a writable home. Your user has no home directory in the image.
   - `-v "$PWD:/workspace" -w /workspace` mounts the project and runs GSKILL inside it.

2. Restore from the lockfile the same way, for example in a fresh clone or a CI job:

   ```sh
   docker run --rm --user "$(id -u):$(id -g)" -e HOME=/tmp \
     -v "$PWD:/workspace" -w /workspace \
     ghcr.io/glapsfun/gskill:latest install --frozen-lockfile
   ```

## Expected result

- `skills.toml`, `skills-lock.json`, and the skill content appear in the project, owned by you.
- Agent links such as `.claude/skills/hello-gskill` are relative, so they work on the host.
- `docker run` exits with GSKILL's exit code, so a CI step fails when GSKILL does.

## Use a shell function

A function keeps the flags in one place, so the rest of your session reads like a local install:

```sh
gskill() {
  docker run --rm --user "$(id -u):$(id -g)" -e HOME=/tmp \
    -v "$PWD:/workspace" -w /workspace \
    ghcr.io/glapsfun/gskill:latest "$@"
}
gskill version
gskill project verify
```

Commands that prompt, such as `gskill onboard` and `gskill dashboard`, also need `-it` after
`--rm`.

## Keep the cache between runs

Without a cache mount, each run starts with an empty cache that is gone when the container exits,
so every run fetches its sources again. To keep the cache, mount a host directory as GSKILL's home.
Create the directory first: if it does not exist, Docker creates it owned by root, and GSKILL,
running as you, cannot write to it.

```sh
mkdir -p "$HOME/.cache/gskill-container"
docker run --rm --user "$(id -u):$(id -g)" -e HOME=/tmp \
  -e GSKILL_HOME=/cache -v "$HOME/.cache/gskill-container:/cache" \
  -v "$PWD:/workspace" -w /workspace \
  ghcr.io/glapsfun/gskill:latest install --frozen-lockfile
```

With a warm cache, `--offline install --frozen-lockfile` also works with `--network none`. See
[Work offline](work-offline.md).

## Pin a version

| Tag | Points at |
| --- | --- |
| `0.9.0` | one release; it never moves to a different image |
| `0.9.0-rc.1` | one release candidate; it never moves either |
| `latest` | the highest stable release |
| `next` | the highest release candidate |

For an image that cannot change at all, pull it by digest. Each GitHub Release has a "Container
image" section in its notes with the digest and the exact `docker pull` command, in the form
`ghcr.io/glapsfun/gskill@sha256:` followed by the digest.

## Verify the image

You need [cosign](https://docs.sigstore.dev/cosign/system_config/installation/) v3 and the
GitHub CLI 2.68 or newer.

Check that GSKILL's release workflow signed the image for a release tag:

```sh
cosign verify ghcr.io/glapsfun/gskill:0.9.0 \
  --certificate-identity-regexp '^https://github\.com/glapsfun/gskill/\.github/workflows/image-release\.yml@refs/tags/v' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

Check its build provenance. On success, gh lists the repository that built the image,
`glapsfun/gskill`, and the workflow, `.github/workflows/image-release.yml` at the release tag:

```sh
gh attestation verify oci://ghcr.io/glapsfun/gskill:0.9.0 -R glapsfun/gskill \
  --source-ref refs/tags/v0.9.0
```

Each platform image has its own SBOM. This lists the packages in the `linux/amd64` image: the Go
modules compiled into GSKILL and the base image's packages, such as `git`:

```sh
amd64="$(docker manifest inspect ghcr.io/glapsfun/gskill:0.9.0 |
  jq -r '.manifests[] | select(.platform.architecture == "amd64") | .digest')"
gh attestation verify "oci://ghcr.io/glapsfun/gskill@${amd64}" -R glapsfun/gskill \
  --predicate-type https://spdx.dev/Document/v2.3 --format json |
  jq -r '.[].verificationResult.statement.predicate.packages[].name'
```

Then pull the image you verified by its digest, from the release's "Container image" section.

## Troubleshooting

- **`error: open gskill home: create home dir /.gskill: mkdir /.gskill: permission denied`**: the
  run is missing `-e HOME=/tmp`. Your user has no writable home in the image, so add the flag, or
  mount a cache as `GSKILL_HOME`.
- **Files in the project are owned by `65532`**: the run was missing `--user`, so GSKILL ran as the
  image's default user. Add `--user "$(id -u):$(id -g)"`, and give the files back by running
  `sudo chown -R "$(id -u):$(id -g)" .` in the project directory.
- **The cache directory is owned by root**: Docker created it because it did not exist yet. Fix it
  with `sudo chown "$(id -u):$(id -g)" "$HOME/.cache/gskill-container"`, or create it with
  `mkdir -p` before the first run.

## Limits

- The images are Linux only. On macOS and Windows they run in your runtime's Linux VM; for a
  native binary, use another [install method](../installation.md).
- Skill sources must be public and fetched over HTTPS. SSH and private repositories are not
  supported in the container yet ([#93](https://github.com/glapsfun/gskill/issues/93)); git's own
  error is shown.
- Use the image for project-level installs. Agent directories in the user's home, which
  user-level installs write to, are inside the container and gone when it exits.

## About the image

- The base is `cgr.dev/chainguard/git`, pinned by digest in `.ko.yaml`. It provides `git`, which
  GSKILL runs for Git sources, CA certificates for HTTPS, and a non-root default user, in about
  35 MB compressed. With GSKILL, each platform image is 40 to 45 MB.
- The image is built with [ko](https://ko.build) from the Go module, with the same build settings
  as the release archives. Its entrypoint is `/ko-app/gskill`, so the arguments after the image
  name go to GSKILL.
- Each release uses the base digest pinned at its tag. The pin changes only through a reviewed
  pull request.

## See also

- [Install GSKILL](../installation.md)
- [Reproduce with --frozen-lockfile](reproduce-with-frozen-lockfile.md)
- [Work offline](work-offline.md)
- [Cut and verify a release](releasing.md)
