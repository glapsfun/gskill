# GSKILL TDD & Quality Workflow

Every change to GSKILL follows test-driven development and passes one gate.

## The loop (red → green → refactor)

1. **Red** — write the smallest failing test for the next behavior. Run
   `scripts/test.sh` (or `scripts/tdd.sh` for a watch loop) and watch it fail.
2. **Green** — write the minimal code to pass. Run the tests again.
3. **Refactor** — clean up with the tests green.

## One-time setup

```bash
./scripts/bootstrap.sh   # installs pinned tools into ./bin + git hooks
```

## Definition of done

Work is NOT done until the gate exits 0:

```bash
./scripts/verify.sh
```

`verify.sh` runs, in order: go.mod tidy check, format check, `go vet` +
golangci-lint, race tests, coverage floor (`COVERAGE_MIN`), govulncheck, and a
secret scan. The same gate runs in pre-commit (pre-push) and in CI, so a green
CI run is the authoritative confirmation.

## For AI agents

- Load the relevant skills before coding: `golang-testing`, `golang-lint`,
  `golang-security` (the `golang-how-to` orchestrator routes these).
- Never claim a task complete until `scripts/verify.sh` exits 0 locally.
- Tools live in `./bin` at pinned versions from `.config/tool-versions`; do not
  rely on system installs.
