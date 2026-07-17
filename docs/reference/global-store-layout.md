# Global store layout

Resolved root: `$GSKILL_HOME` if set, else `~/.gskill` (all platforms).

```text
~/.gskill/
├── store/sha256/<hash>/     # one object per canonical content hash
│   ├── content/             # immutable skill tree (SKILL.md, …)
│   └── metadata.json        # schemaVersion, contentHash, sizeBytes,
│                            # createdAt, lastUsedAt, origins[]
├── cache/                   # download cache
├── tmp/                     # owner-only staging (object-<hash>-<rand>/)
├── locks/                   # store-<hash>.lock, project-<id>.lock,
│                            # gc.lock, projects.lock
├── projects/<id>.json       # advisory registry entries
├── pins/<algo>-<hash>       # GC exemption markers (empty files)
├── quarantine/<hash>-<ts>/  # corrupted objects, moved fail-closed
└── config.toml              # user-level configuration
```

## Object metadata (`metadata.json`, schema 1)

| Field | Meaning |
|-------|---------|
| `schemaVersion` | `1`; readers refuse unknown versions |
| `contentHash` | must equal the directory key |
| `sizeBytes` | admitted content size |
| `createdAt` / `lastUsedAt` | admission time / best-effort activation stamp |
| `origins[]` | sourceType, source, skillPath, version, ref, commit — descriptive only, deduplicated, sorted |

`content/` is immutable: gskill never modifies it in place; repair replaces
it atomically and GC removes it whole.

## Configuration keys

| Key | Env | Default |
|-----|-----|---------|
| `store.scope` | `GSKILL_STORE_SCOPE` | auto (global for new projects, project for unmigrated legacy stores) |
| `store.verify_on_use` | `GSKILL_STORE_VERIFY` | `true` |
| `store.gc_grace_period` | — | `30d` |
| `store.lock_timeout` | — | `60s` |
| `projects.registry` | `GSKILL_PROJECT_REGISTRY` | `true` |
| `privacy.project_registry` | — | `full` (`minimal` omits paths, `disabled` writes nothing) |

The home location itself is env-only (`GSKILL_HOME`), never a config key, and
never recorded in `skills-lock.json`.
