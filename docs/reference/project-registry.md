# Project registry

`~/.gskill/projects/<project-id>.json` — one advisory entry per project that
has used the global store. Owner-readable only (0600); contents never leave
the machine and never include credentials.

```json
{
  "schemaVersion": 1,
  "projectId": "p-4f1a…",
  "root": "/Users/you/dev/repo1",
  "lockfile": "/Users/you/dev/repo1/skills-lock.json",
  "lastSeen": "2026-07-17T10:30:00Z",
  "references": [
    {"skill": "argocd", "storeHash": "sha256:…"}
  ]
}
```

- **Advisory, never authoritative**: entries are rebuilt from the project
  whenever gskill touches it; deleting any or all entries breaks nothing.
- **Uses**: `store gc` reference marking (combined with a live re-read of
  each project's lockfile, state, and active links), `store inspect`
  used-by reporting, and `projects list` diagnostics.
- **Privacy** (`privacy.project_registry`): `full` records everything above;
  `minimal` omits `root` and `lockfile`; `disabled` writes no entries (GC
  then reports degraded reference marking).
- **Project identity**: a random stable ID generated into
  `.gskill/state.json` on first use; it survives project moves, and a
  deleted state file simply yields a fresh ID (the stale registry entry is
  pruned later).
