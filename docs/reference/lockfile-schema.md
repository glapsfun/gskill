# `skills-lock.json` lockfile schema (v1)

The lockfile is the **machine-maintained record of reality** — everything needed to reproduce and
verify an install. It is never hand-edited. gskill uses the shared, project-level `skills-lock.json`
v1 format (the same file written by compatible external tooling such as `npx skills`), and co-owns
it losslessly: fields gskill does not understand survive every rewrite byte-for-byte, and all
gskill-specific data lives under a namespaced per-entry `gskill` field.

Format: JSON, 2-space indent, trailing newline, stable key order (new entries append sorted).
`version = 1`; any other version is refused, never guessed at.

## Shape

```json
{
  "version": 1,
  "skills": {
    "<name>": {
      "source": "owner/repo",
      "sourceType": "github",
      "skillPath": "skills/<name>/SKILL.md",
      "computedHash": "03e0eaaa9bf1…9a8feaa7c9",
      "gskill": {
        "sourceUrl": "https://github.com/owner/repo.git",
        "ref": "v2.1.3",
        "commit": "6c58cfd49a71d86d7d225c61ea63d98c3df19bd1",
        "version": "2.1.3",
        "agents": ["claude", "codex"],
        "installMode": "symlink",
        "scope": "project",
        "storeHash": "sha256:…",
        "skillFileHash": "sha256:…",
        "installedAt": "2026-07-10T12:00:00Z",
        "updatedAt": "2026-07-10T12:00:00Z",
        "state": { "…": "residual install state (targets, pins, metadata)" }
      }
    }
  }
}
```

## Field rules

| Group | Field | Notes |
| --- | --- | --- |
| top | `version` | int; must be `1` — anything else is refused with a clear message. |
| core | `source` | Where the skill came from: `owner/repo` for GitHub, a path for local sources. |
| core | `ref` | Optional branch/tag used for installation. |
| core | `sourceType` | `github` \| `local` are installable by gskill; unknown types fail that entry clearly. |
| core | `skillPath` | Path to the skill's `SKILL.md` inside the source; validated against traversal. |
| core | `computedHash` | SHA-256 over the skill folder's files (path + raw bytes, locale-sorted), hex, no prefix — identical to the external tool's hash and verified before every install. Only `install --force` may rewrite it. |
| `gskill` | `sourceUrl` / `ref` / `commit` / `version` | The resolved, immutable identity gskill pinned to. |
| `gskill` | `agents` / `installMode` / `scope` | How and where the skill is installed. |
| `gskill` | `storeHash` / `skillFileHash` | gskill's own canonical checksums (`sha256:`-prefixed) used by `verify` and the content store. |
| `gskill` | `installedAt` / `updatedAt` | Audit timestamps; excluded from reproducibility. |
| `gskill` | `state` | Residual machine state (per-agent targets and modes, requested pins, frontmatter metadata) that keeps every existing command working. |

**Core fields are shared property.** gskill fills them only when absent and never rewrites them
(except `computedHash`, the shared verification fact). Unknown top-level fields, unknown entry
fields, and other tools' extension blocks are preserved verbatim.

## Determinism

The lockfile is serialised deterministically: stable key order, fixed indentation, atomic writes,
minimal diffs. Map order, timestamps, and ambient environment never leak into the reproducible
fields — that is what makes committing `skills-lock.json` worthwhile. Under
`install --frozen-lockfile` the file is never modified, byte for byte, even on failing runs.

## Migrating from `gskill.lock`

Projects created before spec 012 hold the legacy `gskill.lock`. Run `gskill migrate lockfile` (or
any lock-touching command — `install`, `update`, `project lock`, `project sync` — which migrates
automatically): the legacy file is converted losslessly, backed up as `gskill.lock.backup`, and
never written again.

## See also

- [`gskill.toml` manifest schema](manifest-schema.md)
- [Reproduce with --frozen-lockfile](../how-to/reproduce-with-frozen-lockfile.md)
- [The reproducibility model](../explanation/reproducibility-model.md)
