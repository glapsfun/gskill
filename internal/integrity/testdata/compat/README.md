# Compat-hash fixture corpus

Each fixture directory under this folder is a skill directory paired with an
`expected.json` recording the `computedHash` the external lock producer
(`npx skills`, the `vercel-labs/skills` CLI) computes for it:

```json
{"computedHash": "<64-char lowercase hex sha256>", "recordedWith": "skills@<version>"}
```

How fixtures are produced (one-time, results committed — CI never needs Node):

1. Create the fixture skill directory (must contain a `SKILL.md`).
2. Run the real tool against it and capture the hash it writes to
   `skills-lock.json` (e.g. add the skill from a local/test repo, or invoke the
   tool's hashing entry point directly).
3. Record the hash and tool version in `expected.json` next to the fixture.

`TestCompatHashParity` (internal/integrity/compathash_test.go) walks every
fixture and asserts `integrity.CompatHash(dir)` matches `expected.json`.
Per spec 012 (FR-025), gskill must not claim `computedHash` compatibility
until this parity suite is green against tool-recorded values.

Notes:

- Git cannot track empty directories; the empty-dir case is created by the
  test at runtime, not stored here.
- Keep fixtures small and deterministic (no timestamps, no generated data).
