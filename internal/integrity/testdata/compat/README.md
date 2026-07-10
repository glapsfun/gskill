# Compat-hash fixture corpus

Pins `integrity.CompatHash` to the external lock producer's `computedHash`
(the `npx skills` CLI, `vercel-labs/skills`). Layout:

- `fixtures/<name>/` — one skill directory per case: `basic`, `crlf`,
  `binary`, `hidden`, `excluded` (`.git`/`node_modules` skipped), `executable`,
  `case-order` and `unicode` (exercise the locale-aware sort where byte order
  and `localeCompare` disagree).
- `expected/<name>.json` — the recorded reference hash:
  `{"computedHash": "…", "recordedWith": "…", "node": "…"}`.
- `generate.mjs` — regenerates `expected/`. Its `computeSkillFolderHash` /
  `collectFiles` are copied VERBATIM from `vercel-labs/skills`
  `src/local-lock.ts` @ commit `4ce6d48`; never edit them independently of
  upstream. Run `node generate.mjs` from this directory (one-time; results are
  committed, CI never needs Node).
- `.gitattributes` (`* -text`) keeps git from normalizing the CRLF/binary
  fixtures.

`TestCompatHashParity` (../../compathash_test.go) asserts parity for every
fixture; per spec 012 (FR-025) gskill must not claim `computedHash`
compatibility unless it is green. Cases git cannot represent (empty dirs,
symlinks) are built at runtime by the sibling tests instead of being stored
here.
