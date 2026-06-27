# Run doctor

Check that your environment is ready for GSKILL and that declared skill requirements are satisfied.

## Before you start

- GSKILL installed.

## Steps

```bash
gskill doctor            # check environment + declared requirements
gskill doctor --json     # machine-readable report
```

## Expected result

- `gskill doctor` reports on prerequisites such as the `git` binary, detected agents, and any external
  commands/environment variables that installed skills declare in their `requires` block.
- It is read-only. A clean environment exits `0`; problems are reported with actionable diagnostics.

## When to use it

- Right after installing GSKILL, to confirm `git` and your agents are detected.
- When a skill declares `requires.commands` (e.g. `kubectl`) or `requires.environment` (e.g.
  `KUBECONFIG`) and you want to confirm they're present.

## See also

- [`SKILL.md` frontmatter schema](../reference/frontmatter-schema.md) — the `requires` block.
- [Supported agents](../reference/agents.md)
