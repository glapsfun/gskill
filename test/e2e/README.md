# Real-source end-to-end tests

These tests drive the `gskill` CLI against the live
[`glapsfun/cnative-skills`](https://github.com/glapsfun/cnative-skills) repository to validate the
full install lifecycle against a real, multi-skill source — not just synthetic fixtures.

## Why they are opt-in

They require network access and a `git` binary, which would make the always-on quality gate
(`./scripts/verify.sh`) flaky and non-hermetic. So they are **double-gated** and excluded from the
default run:

1. **Build tag** — every file is headed `//go:build e2e`, so `go test ./...` and `verify.sh` never
   compile them.
2. **Env gate** — each test calls `requireE2E(t)`, which `t.Skip`s unless `GSKILL_E2E=1` and `git`
   is on `PATH`.

## Running

```bash
# Skipped cleanly (compiles, every scenario skips) — proves the gate:
go test -tags=e2e ./test/e2e/...

# Full real-source run against the live repository:
GSKILL_E2E=1 go test -tags=e2e ./test/e2e/...
```

The default gate never builds this package:

```bash
go test ./...        # test/e2e excluded; no network contacted for it
```

## Scenarios

| Test | Covers |
|------|--------|
| `TestE2E_InstallAllSkills` | install every discovered skill (count derived from discovery) |
| `TestE2E_OneSkillOneAgent` | one named skill for one agent; manifest records agents + version |
| `TestE2E_TwoSkillsMultipleAgents` | two skills for two agents; one store entry per skill |
| `TestE2E_SyncFromManifest` | reconcile from a declared manifest; second sync is a no-op |
| `TestE2E_InstallFromManifest` | install from a manifest; re-run reports no change |
