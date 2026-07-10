# The reproducibility model

GSKILL's central promise is that a skill environment can be reproduced **byte-for-byte** on any
machine, agent, or CI runner. This page explains how the single committed file that makes that work —
`skills-lock.json` — records both what you asked for and what you got.

## Intent vs reality — one file, two layers

Every entry in `skills-lock.json` carries two kinds of facts:

- **Intent** lives in the entry's namespaced `gskill` block, recorded from your add/install flags:
  which source, which tracking constraint (a range like `^2.0.0`, a branch, a tag), which agents,
  which install mode. `update` follows this intent.

- **Reality** is everything the resolution produced: the shared `computedHash`, and — under
  `gskill` — the exact commit, resolved version, store hash, and per-agent targets. It is
  machine-generated, deterministic, and never hand-edited.

You commit the one file. The intent stays flexible (`^2.0.0` lets `update` pick up `2.1.3` later);
the resolved reality pins the exact outcome so everyone on the team gets the *same* `2.1.3` today.
Updating is an explicit act (`gskill update`) that re-resolves within the recorded intent and
rewrites the resolution — never an accident.

## Interoperability

The file is the shared project-level v1 format also written by compatible external tooling (such as
`npx skills`). GSKILL co-owns it losslessly: unknown fields and other tools' entries survive every
rewrite byte-for-byte, and everything gskill-specific stays inside the per-entry `gskill` block.

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
- [Reproduce with --frozen-lockfile](../how-to/reproduce-with-frozen-lockfile.md)
- [Integrity and trust](integrity-and-trust.md)
