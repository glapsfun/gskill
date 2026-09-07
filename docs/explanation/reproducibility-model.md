# The reproducibility model

GSKILL's central promise is that a skill environment can be reproduced **byte-for-byte** on any
machine, agent, or CI runner. This page explains how the two committed files that make that work
divide the job: `skills.toml` says what you want, `skills-lock.json` records what you got.

## Intent vs reality — two files, one contract

- **Intent** lives in `skills.toml`, the file you edit: which source, which tracking constraint
  (a range like `^2.0.0`, an exact version, a tag, a branch, or a commit), which agents, which
  install mode, which overrides. `install`, `update`, and `upgrade` all read intent from here.

- **Reality** is everything resolution produced, recorded in `skills-lock.json`: the shared
  `computedHash`, and — under the per-entry `gskill` block — the exact commit, resolved version,
  content hash, per-agent targets, and a projection of the intent it resolved from
  (`requestedVersion`, `requestedRef`, `requestedCommit`, `declarationKind`). It is
  machine-generated, deterministic, and never hand-edited. The projection exists so a lock can
  reproduce without the manifest and so a manifest can be generated for a project written before
  one existed; it is never read as intent when the manifest is present.

You commit both. The intent stays flexible (`^2.0.0` lets `update` pick up `2.1.3` later); the
resolved reality pins the exact outcome so everyone on the team gets the *same* `2.1.3` today.
Moving is always an explicit act: `gskill update` re-resolves within the declared intent and
rewrites only the resolution, `gskill upgrade` changes the intent and then resolves it. See
[the package lifecycle](lifecycle.md).

## Interoperability

The file is the shared project-level v1 format also written by compatible external tooling (such as
`npx skills`). GSKILL co-owns it losslessly: unknown fields and other tools' entries survive every
rewrite byte-for-byte, and everything gskill-specific stays inside the per-entry `gskill` block.

## Restore order: committed → cache → fetch

Because skill content is [committed in the repository](repo-owned-storage.md), a fresh clone
already *is* the reproduced environment. When `gskill install` does run, it reproduces from
`skills-lock.json` alone, cheapest layer first: committed content whose hash matches the lock is up
to date (zero network); missing content restores from the clone cache by the recorded commit; a
cold cache fetches exactly once from the recorded source.

## What `--frozen-lockfile` guarantees

`gskill install --frozen-lockfile` is the reproducible-restore command:

- It restores exactly what the lockfile records.
- It **never modifies** the lockfile.
- It **fails closed** (exit `4`) if an entry lacks the `gskill` metadata a restore needs, or if an
  explicit `--agent` conflicts with the locked agents; integrity mismatches fail that skill closed
  (exit `6`) — without touching any agent directory.

That combination is what lets CI and teammates trust a restore: either they get precisely the locked
environment, or they get a clear failure — never a silent, subtly-different install.

## Determinism

For the lockfile to be worth committing, it must serialise the same way every time. GSKILL keeps
key order stable, fixes indentation, and keeps non-reproducible data (timestamps, map iteration
order, ambient environment) out of the fields that define content. Mutable references (like a
branch) are resolved to an immutable commit before being written.

## See also

- [`skills-lock.json` schema](../reference/lockfile-schema.md)
- [Repo-owned storage](repo-owned-storage.md)
- [Reproduce with --frozen-lockfile](../how-to/reproduce-with-frozen-lockfile.md)
- [Integrity and trust](integrity-and-trust.md)
