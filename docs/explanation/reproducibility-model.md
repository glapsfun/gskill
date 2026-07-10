# The reproducibility model

GSKILL's central promise is that a skill environment can be reproduced **byte-for-byte** on any
machine, agent, or CI runner. This page explains how the two files that make that work — the manifest
and the lockfile — relate, and why you commit both.

## Intent vs reality

GSKILL keeps two records:

- **`gskill.toml` — the manifest — is intent.** You write it. It says *what you want*: "install this
  skill from this source, within this version range, for these agents." It is human-editable and
  deliberately loose (a constraint like `^2.0.0`, not an exact version).

- **`skills-lock.json` — the lockfile — is reality.** GSKILL writes it. It records *what was actually
  resolved*: the exact commit, the content hashes, the resolved version, the target agents, and the
  install mode. It is machine-generated, deterministic, and never hand-edited.

You commit **both**. The manifest expresses your goals; the lockfile pins the exact outcome so it can
be recreated.

## Why two files instead of one?

A single "list of skills" can't be both flexible and reproducible. You want to *say* `^2.0.0` (so
`update` can pick up `2.1.3` later), but you also want everyone on the team to get the *same* `2.1.3`
today. The manifest holds the flexible intent; the lockfile freezes the resolved reality. Updating is
then an explicit act (`gskill update`) that rewrites the lock — never an accident.

## What `--frozen-lockfile` guarantees

`gskill install --frozen-lockfile` is the reproducible-restore command:

- It restores exactly what the lockfile records.
- It **never modifies** the lockfile.
- It **fails closed** (exit `4`) if the manifest and lockfile disagree, or if a resolved artifact no
  longer matches its recorded checksum — without touching any agent directory.

That combination is what lets CI and teammates trust a restore: either they get precisely the locked
environment, or they get a clear failure — never a silent, subtly-different install.

## Determinism

For the lockfile to be worth committing, it must serialise the same way every time. GSKILL sorts keys,
fixes indentation, and keeps non-reproducible data (timestamps, map iteration order, ambient
environment) out of the fields that define content. Mutable references (like a branch) are resolved to
an immutable commit before being written.

## See also

- [`gskill.toml` schema](../reference/manifest-schema.md) · [`skills-lock.json` schema](../reference/lockfile-schema.md)
- [Reproduce with --frozen-lockfile](../how-to/reproduce-with-frozen-lockfile.md)
- [Integrity and trust](integrity-and-trust.md)
