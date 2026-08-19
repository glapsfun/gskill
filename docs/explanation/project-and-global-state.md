# Project state vs global state

GSKILL divides state so projects stay reproducible and self-contained: the
repository owns the content, the home directory is only a cache.

## Global user state (`~/.gskill`, relocatable via `GSKILL_HOME`)

| Path | Purpose |
|------|---------|
| `cache/` | commit-keyed git clones (`cache/<40-hex-commit>/`) |
| `locks/` | file locks |
| `tmp/` | staging for in-flight work |
| `config.toml` | user-level configuration |

The home is a **pure performance cache**: nothing in the repository ever
references it, and deleting it just causes re-fetches. Nothing in it is ever
required to reproduce a project.

## Project state (inside the repository)

- `skills-lock.json` — committed; the single source of truth: sources,
  versions, exact commits, content hashes, agents, install mode. It never
  contains a user-specific path.
- `.agents/skills/<name>/` — the skill content itself, a real directory,
  **committed**.
- `.claude/skills/`, `.codex/skills/`, … — **committed relative symlinks**
  into `.agents/skills/<name>` (e.g. `../../.agents/skills/<name>`).
- `.gskill/state.json` — schema v2: gitignored machine-local bookkeeping
  only (the stable project ID plus per-agent target and mode). Safe to
  delete; never needed for reproduction.

Because content and links are committed, a fresh `git clone` yields working
skills with zero gskill commands.

## Invariants

- A project is always restorable from `skills-lock.json` alone
  (`gskill install --frozen-lockfile`).
- Deleting `~/.gskill` breaks nothing — missing material is re-fetched on
  demand.
- Updating or removing a skill in one project never changes another project.

## See also

- [Repo-owned storage](repo-owned-storage.md)
- [The clone cache](store-and-cache.md)
