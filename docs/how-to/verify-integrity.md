# Verify integrity

Re-hash installed skill content and compare it against the checksums recorded in `gskill.lock`. Use
this to detect tampering or accidental edits to installed skills.

## Before you start

- At least one installed skill (`gskill add ...` or `gskill install`).
- A `gskill.lock` present.

## Steps

```bash
gskill verify            # re-hash installed content vs the lock
gskill verify --json     # machine-readable result for scripts/CI
```

## Expected result

- If everything matches, `gskill verify` exits `0`.
- If any installed file differs from its recorded checksum, verify **fails closed** and exits **`6`**
  (integrity failure) with a diagnostic naming the affected skill.

### Example: detecting a tampered byte

```bash
# After a clean `gskill verify` (exit 0), change one installed byte:
printf '!' >> .claude/skills/<name>/SKILL.md
gskill verify
echo "exit: $?"          # prints: exit: 6
```

## `check` vs `verify`

- `gskill check` is **fast** — it compares metadata and is meant as a CI gate.
- `gskill verify` is **thorough** — it re-hashes actual content. Use it when you need proof, not just a
  quick status.

## See also

- [Gate CI on drift](gate-ci-on-drift.md)
- [Integrity and trust](../explanation/integrity-and-trust.md)
- [Exit codes](../reference/exit-codes.md)
