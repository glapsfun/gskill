# The global store

GSKILL keeps canonical skill content in a single user-level, content-addressed
store under `~/.gskill/store` (relocatable only with the `GSKILL_HOME`
environment variable). Identical skill content is downloaded and stored once
per machine; every project that locks the same content links to the same
immutable object.

## Content addressing

A store object's identity is its canonical content hash — the same
`sha256:<hex>` value recorded in `skills-lock.json`. Names never determine
physical layout, so:

- multiple versions of one skill coexist as separate objects;
- identical content from different repositories deduplicates to one object;
- verification is deterministic: re-hash the content, compare to the key.

Each object lives at `~/.gskill/store/sha256/<hash>/` as an immutable
`content/` directory plus a descriptive `metadata.json` (size, timestamps,
known origins). Origins never affect identity — the same bytes from two
sources share one object with both origins recorded.

## The core principle

**Global content, project-local intent, project-local activation.**

- The *global store* owns immutable content.
- The *project lockfile* (`skills-lock.json`) selects the exact content the
  project requires — it records content identity, never machine paths.
- The *project-active layer* (`.agents/skills/<name>`) links that content
  into the repository.
- *Agent links* (`.claude/skills/...`, `.codex/skills/...`) expose the
  project-selected content to each agent through the active layer.

Removing a skill from one project never deletes shared content; deletion
happens only through `gskill store gc`. Every activation re-verifies the
object's content hash and fails closed on corruption, quarantining the object
under `~/.gskill/quarantine/`.

## Trust and safety

- Objects are admitted through staging plus an atomic rename; a partially
  written object is never visible.
- The whole home is owner-only (0700/0600); unsafely owned or writable
  objects are refused at activation.
- Fetched content is never executed, and packages are validated against
  path traversal and escaping symlinks before admission.

See also: [project and global state](project-and-global-state.md),
[reuse skills across projects](../how-to/reuse-skills-across-projects.md).
