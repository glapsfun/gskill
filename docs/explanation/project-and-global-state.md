# Project state vs global state

GSKILL divides state so projects stay reproducible and independent while
content is shared.

## Global user state (`~/.gskill`, relocatable via `GSKILL_HOME`)

| Path | Purpose |
|------|---------|
| `store/` | immutable, content-addressed skill objects |
| `cache/` | download cache |
| `tmp/` | staging for in-flight admissions (atomic rename into the store) |
| `locks/` | file locks: per-object, per-project, `gc.lock`, `projects.lock` |
| `projects/` | advisory project registry (rebuildable, never authoritative) |
| `pins/` | GC exemption markers |
| `quarantine/` | corrupted objects moved aside, fail-closed |
| `config.toml` | user-level configuration |

Everything under the home is machine-local. Nothing in it is ever required to
reproduce a project.

## Project state (inside the repository)

- `skills-lock.json` — the only committed artifact and the single source of
  truth: sources, versions, exact commits, content hashes, agents, install
  mode. It never contains a user-specific path.
- `.agents/skills/<name>` — the project-active layer (generated, gitignored).
- `.claude/skills/`, `.codex/skills/`, … — agent links through the active
  layer (generated).
- `.gskill/state.json` — gitignored machine-local bookkeeping: the stable
  project ID, which object each skill activates, which links gskill owns,
  and the materialization mode. Safe to delete; regenerated on the next run.

## Invariants

- A project is always restorable from `skills-lock.json` alone
  (`gskill install --frozen-lockfile`).
- Deleting `~/.gskill/projects/` (the registry) breaks nothing — entries
  rebuild as projects are touched again.
- Updating or removing a skill in one project never changes another project.
