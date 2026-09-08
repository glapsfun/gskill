# Hermetic, binary-driven lifecycle tests

This package builds `cmd/gskill` once per run (with the `testseams` build tag) and drives
the real binary against local git repositories it creates itself. It has **no build tag**, so
it runs in `go test ./...`, in `./scripts/verify.sh`, and in both CI operating-system jobs.
Nothing here touches the network or the developer's home directory: every process gets a
scrubbed environment with a private `HOME` and `GSKILL_HOME`.

```bash
go test -count=1 -timeout 5m ./test/e2e/cli/...
```

The suite covers the lifecycle end to end (spec 024 US5): `add`, `install`,
`install --frozen-lockfile`, `update --list`, `update`, `upgrade`, `verify`, `check`, `sync`,
and `remove`. `TestMain` fails the package if any of those was never exercised, so the
coverage cannot silently shrink.

Typical duration (one build plus a few dozen sub-second invocations), measured on 2026-09-07:

| Platform | Duration |
| --- | --- |
| macOS, Apple silicon | about 3 seconds after the build |
| Linux CI runner | well under one minute |

The live-network suite against a real GitHub repository lives one directory up, behind the
`e2e` build tag and `GSKILL_E2E=1`.
