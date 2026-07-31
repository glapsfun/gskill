---
id: T06
epic: repo-owned-storage
milestone: M3
title: Retire the global store machinery and its CLI surface
status: todo
priority: must
depends-on: [T04]
estimate: L
owner: unassigned
updated: 2026-07-31
---

# T06 — Retire the global store machinery and its CLI surface

## Context

Once no flow consumes stores (T04), the spec-015 machinery is dead weight the
epic exists to delete: `internal/globalstore` (admit/verify/gc/repair/pins/
quarantine), `internal/projreg`, the legacy project store (`internal/store`),
the `gskill store|projects|migrate` commands, and the store-related config
keys. The user decision recorded in the epic: home keeps only `cache/`,
`locks/`, `tmp/`, `config.toml`.

## What to do

- Delete packages: `internal/globalstore`, `internal/projreg`,
  `internal/store` (legacy project store), `internal/migrate` (global-store
  direction; T07 owns the new migration).
- Delete CLI + app surface: `gskill store …`, `gskill projects …`,
  `gskill migrate global-store` (`internal/cli/store.go`, `projects.go`,
  `migrate.go`; `internal/app/storecmd.go`, `projectscmd.go`,
  `migratecmd.go`, `doctor.go` home-audit parts that die with the store);
  removed commands follow the project's retired-command error/hint pattern
  (see the 021 epic for the convention).
- Shrink `internal/home`: `Ensure` creates `cache/`, `locks/`, `tmp/` only;
  delete `StoreDir/PinsDir/QuarantineDir/ProjectsDir`.
- Config: remove `storeScope`, `storeVerifyOnUse`, `storeGCGracePeriod`,
  `projectsRegistry`, `privacy.projectRegistry`; unknown keys in existing
  config files must warn-and-continue, not fail.
- Reconcile stray store/cache locations: `scanInstaller`
  (`internal/app/scan.go:55`) and `installerForScope`
  (`internal/app/project.go:216`) stop using `config.Dir()/store` /
  `config.CacheDir()`; `gskill cache` CLI (`internal/cli/cache.go:19`)
  points at the home clone cache; `--global` installs are backed by the
  home clone cache + direct copy.
- Regenerate command reference and help goldens.

## Acceptance criteria

- [ ] `grep -r "globalstore\|projreg\|StoreDir\|PinsDir\|QuarantineDir\|ProjectsDir"`
      over `internal/` returns no hits.
- [ ] `gskill store`, `gskill projects`, `gskill migrate` fail with the
      retired-command error and hint; help/goldens regenerated.
- [ ] A fresh run creates exactly `cache/`, `locks/`, `tmp/` under
      `$GSKILL_HOME` (plus `config.toml` when written).
- [ ] Existing `config.toml` containing dead keys produces a warning, not an
      error; `gskill cache …` operates on the home clone cache.
- [ ] `./scripts/verify.sh` exits 0.

## Out of scope

- Deleting users' existing `~/.gskill/store` data — never; it is left in
  place (epic non-goal).
- Migration of legacy projects (T07) — this task only removes machinery.

## Notes

Largest deletion of the epic; land as reviewable commits per package.
Doctor/health/repair keep the parts that audit the repo and clone cache
(`internal/app/doctor.go`, `health.go`, `repair.go` need pruning, not
deletion).
