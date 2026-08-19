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
        "contentHash": "sha256:…",
        "skillFileHash": "sha256:…",
        "installedAt": "2026-07-10T12:00:00Z",
        "updatedAt": "2026-07-10T12:00:00Z",
        "state": { "…": "residual install state (targets, metadata)" }
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
| `gskill` | `sourceUrl` / `ref` / `commit` / `version` | The resolved, immutable identity gskill pinned to; `commit` is also the clone-cache key for restores. |
| `gskill` | `agents` / `installMode` | How and where the skill is installed. |
| `gskill` | `contentHash` / `skillFileHash` | gskill's own canonical checksums (`sha256:`-prefixed). `contentHash` is the full-content identity of the committed skill copy — it covers symlinks that the shared `computedHash` skips, so restores and `verify` check against it. |
| `gskill` | `installedAt` / `updatedAt` | Audit timestamps; excluded from reproducibility. |
| `gskill` | `state` | Residual machine state (per-agent targets and modes, frontmatter metadata) that keeps every existing command working. |

Pre-022 entries may still carry `scope` and `storeHash`. They parse cleanly, are never written
anymore, and are dropped on the entry's first rewrite.

**Core fields are shared property.** gskill fills them only when absent and never rewrites them
(except `computedHash`, the shared verification fact). Unknown top-level fields, unknown entry
fields, and other tools' extension blocks are preserved verbatim.

## Determinism

The lockfile is serialised deterministically: stable key order, fixed indentation, atomic writes,
minimal diffs. Map order, timestamps, and ambient environment never leak into the reproducible
fields — that is what makes committing `skills-lock.json` worthwhile. Under
`install --frozen-lockfile` the file is never modified, byte for byte, even on failing runs.

## See also

- [Reproduce with --frozen-lockfile](../how-to/reproduce-with-frozen-lockfile.md)
- [The reproducibility model](../explanation/reproducibility-model.md)
- [Repo-owned storage](../explanation/repo-owned-storage.md)
