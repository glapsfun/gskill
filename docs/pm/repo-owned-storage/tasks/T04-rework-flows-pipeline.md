---
id: T04
epic: repo-owned-storage
milestone: M2
title: Rework add/install/update/sync onto the repo-owned pipeline
status: todo
priority: must
depends-on: [T03]
estimate: L
owner: unassigned
updated: 2026-08-01
---

# T04 — Rework add/install/update/sync onto the repo-owned pipeline

## Context

With activation re-keyed (T03), the flows above it must stop consulting
stores. Target pipeline per the epic: `gskill add <url> --skill <name>` →
clone/reuse in `~/.gskill/cache` → record metadata → discover skill → copy to
`.agents/skills/<name>` → relative agent links. Restore (`gskill install`)
gains a new fast path: committed content that matches the lock hash needs no
fetch at all. Store-scope resolution (`internal/app/project.go:120-149`) and
the store-hit shortcuts (`installFromStore`, `lockEntryUpToDate`,
`storedContentUpToDate`) all change meaning or die.

## What to do

- `add` (`internal/app/install.go:132`): wire `openProjectScoped` without
  store-scope resolution — one project shape; materialization uses the home
  clone cache only (`internal/installer/installer.go:344` path stays).
- `install` from lock (`internal/app/lockinstall.go:129`): up-to-date check
  becomes "committed `.agents/skills/<name>` hash == lock hash"; on
  mismatch/absence, fetch via clone cache and re-copy; `--frozen-lockfile`
  semantics preserved; `maybePrefetch` warms the home clone cache.
- `update` (`internal/app/lifecycle.go:72`): re-resolve, fetch newer commit
  into cache, replace repo copy, refresh links, rewrite lock — one command,
  as today.
- `sync` (`internal/app/sync.go:50`): reconcile against `.agents/skills` as
  the managed root; orphan sweeps updated; a symlink-less checkout gets
  T01's error, not silent repair.
- Apply T02's schema note: write the new lock-extension and state fields;
  drop store fields from written records (readers of old records: T07).
- Guardrail tests: same skill@commit added in a second project performs no
  network fetch (assert clone-cache hit); update-to-newer e2e.

## Acceptance criteria

- [ ] Full target pipeline demonstrated by an integration test driving
      `add https://… --skill <name>`: cache populated under home, repo copy
      + relative links present, lock/state contain repo-relative paths and
      no `storeHash`/`scope` fields.
- [ ] `install --frozen-lockfile` on a fresh clone with committed content
      completes with zero network access; with deleted content it restores
      from cache; with cold cache it fetches once.
- [ ] Hand-edited committed content is detected (hash mismatch) by
      `install`/`check` with the T02-specified error and hint.
- [ ] No-refetch and one-command-update guardrail tests green;
      `./scripts/verify.sh` exits 0.

## Out of scope

- Deleting `internal/globalstore`/`projreg`/config keys (T06) — this task
  stops *using* them; removal is separate so the tree stays green.
- Legacy-project migration (T07).
- `--global` install-scope behavior beyond keeping it compiling — its
  store backing is reconciled in T06.

## Notes

Store-hit paths to retire/replace: `installer.installFromStore`
(`internal/installer/installer.go:156`), `lockEntryUpToDate`
(`internal/app/lockinstall.go:1143`), `storedContentUpToDate` (`:1215`).
