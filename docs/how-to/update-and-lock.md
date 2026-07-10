# Update and re-lock

Advance skills to newer versions within their declared constraints, and recompute the lockfile.

## Before you start

- A committed `gskill.toml` and `skills-lock.json`.

## Update within constraints

```bash
gskill outdated          # see which skills have newer versions available
gskill update            # advance all skills within their version constraints
gskill update <name>     # advance only one skill
```

**Expected:** GSKILL resolves newer versions allowed by each skill's constraint, re-installs them, and
rewrites `skills-lock.json`. Commit the updated lockfile.

## Expected result

- `gskill update` may change resolved versions; `gskill install` never does.
- Both produce a deterministic `skills-lock.json`. Review the diff before committing — the lockfile diff
  should match your intent.

## See also

- [Add a skill from Git](add-a-git-skill.md)
