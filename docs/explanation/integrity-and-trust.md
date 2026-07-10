# Integrity and trust

GSKILL installs instructions that steer AI agents. A tampered or unexpected skill is a supply-chain
risk for every machine that restores it. This page explains how GSKILL establishes trust through
verification rather than assumption.

## Trust is verified, never assumed

The lockfile records content checksums for everything it installs. Before any downloaded artifact is
written into an agent's skill directory, GSKILL verifies it against that recorded checksum. A mismatch
**aborts** the operation. `gskill project verify` re-runs this check on demand, re-hashing installed content
and exiting `6` on any difference.

## Fail closed

When verification or integrity checks fail, GSKILL **fails closed**: it stops with a non-zero exit and
a clear diagnostic, rather than partially installing or falling back to an unverified source. The same
discipline applies to the lockfile itself — when recorded facts and content disagree, GSKILL reports it (and
`--frozen-lockfile` exits `4`) instead of silently "fixing" things.

This is the difference between a package manager and a download script: a download script fetches files
and hopes; GSKILL proves what it installed and refuses anything it can't.

## Fetched content is never executed

Skill content is **data, not code**, as far as installation is concerned. GSKILL never executes fetched
content as part of installing it. Files that arrive with an executable bit are surfaced as warnings, not
run. When content is previewed (for example in the [TUI](../how-to/use-the-tui.md)), terminal escape
sequences are sanitised first so a malicious skill can't hijack your terminal.

## Auditable and reversible

Every install is recorded in the lockfile, scoped to known agent directories, and restorable to a
previously committed state. That makes installs auditable (you can see exactly what landed and from
where) and reversible (you can restore an earlier committed lock).

## Forward-readable lockfile

A newer GSKILL refuses, with a clear message, to misinterpret a lockfile schema it doesn't understand —
rather than guessing and producing a subtly wrong install.

## See also

- [Verify integrity](../how-to/verify-integrity.md)
- [The reproducibility model](reproducibility-model.md)
- [`skills-lock.json` schema](../reference/lockfile-schema.md)
