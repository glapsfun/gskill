# `gskill.lock` lockfile schema (v1)

The lockfile is the **machine-generated record of reality** — everything needed to reproduce and verify
an install. It is never hand-edited. Format: JSON, emitted with sorted keys, 2-space indent, and a
trailing newline for stable diffs. `lockfile_version = 1`.

## Shape

```json
{
  "lockfile_version": 1,
  "skills": {
    "<name>": {
      "source": {
        "type": "git",
        "original": "github.com/owner/repo",
        "url": "https://github.com/owner/repo.git",
        "owner": "owner",
        "repo": "repo",
        "path": "skills/<name>"
      },
      "requested": { "version": "^2.0.0" },
      "resolved": {
        "version": "2.1.3",
        "ref_kind": "semver",
        "tag": "v2.1.3",
        "commit": "6c58cfd49a71d86d7d225c61ea63d98c3df19bd1",
        "tree_hash": "sha256:…",
        "content_hash": "sha256:…",
        "skill_file_hash": "sha256:…",
        "mutable_ref": false
      },
      "metadata": { "name": "<name>", "description": "…", "license": "MIT" },
      "requires": {
        "skills": ["shell-scripting >=1.2.0"],
        "commands": ["kubectl", "helm"],
        "environment": ["KUBECONFIG"],
        "mcp": []
      },
      "installation": {
        "scope": "project",
        "mode": "symlink",
        "agents": ["claude-code", "codex"],
        "targets": {
          "claude-code": ".claude/skills/<name>",
          "codex": ".codex/skills/<name>"
        }
      },
      "provenance": {
        "fetched_at": "2026-06-26T10:00:00Z",
        "updated_at": "2026-06-26T10:00:00Z",
        "trust": "unverified"
      }
    }
  }
}
```

## Field rules

| Group | Field | Notes |
| --- | --- | --- |
| top | `lockfile_version` | int; refused if newer than the tool understands. |
| `source` | `type` | `git` \| `local` \| `url`. |
| `source` | `original` / `url` / `owner` / `repo` / `path` | Normalised source identity; a changed URL is flagged as a source substitution. |
| `requested` | `version` / `ref` / `commit` | Echoes the intent from the manifest. |
| `resolved` | `version` / `tag` / `commit` | The immutable resolved revision. |
| `resolved` | `ref_kind` | `semver` \| `tag` \| `branch` \| `commit` \| `local`. |
| `resolved` | `tree_hash` / `content_hash` / `skill_file_hash` | Checksums used by `verify`. |
| `resolved` | `mutable_ref` | `true` when resolved from a mutable reference (e.g. a branch). |
| `metadata` | `name` / `description` / `license` | Captured from frontmatter. |
| `requires` | skills/commands/environment/mcp | Recorded, not resolved transitively. |
| `installation` | `scope` / `mode` / `agents` / `targets` | How and where the skill is installed. |
| `provenance` | `fetched_at` / `updated_at` / `trust` | Audit fields. Timestamps never affect reproducibility/content determinism. |

## Determinism

The lockfile is serialised deterministically (sorted keys, fixed indentation). Map order, timestamps,
and ambient environment never leak into the reproducible fields — that is what makes committing
`gskill.lock` worthwhile.

## See also

- [`gskill.toml` manifest schema](manifest-schema.md)
- [Reproduce with --frozen-lockfile](../how-to/reproduce-with-frozen-lockfile.md)
- [The reproducibility model](../explanation/reproducibility-model.md)
