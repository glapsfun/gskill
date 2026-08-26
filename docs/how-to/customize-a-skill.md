# Customize a skill without forking it

Take an upstream skill that is *almost* right, adjust it for your project, and keep taking upstream
updates. Your customization is committed, so teammates get it from a plain `git clone`.

## Before you start

- A skill already installed with `gskill add`.
- The project's `skills.toml` (created by `add`, or generated on the first mutating command in an
  older project).

## Steps

Write the content you want to add, anywhere in the repository:

```bash
mkdir -p gskill/code-review
printf '\n## House rules\nAlways cite file:line.\n' > gskill/code-review/house-rules.md
```

Declare it in `skills.toml`:

```toml
[skills.code-review.override]
append = ["gskill/code-review/house-rules.md"]
```

Apply it:

```bash
gskill install
```

## Expected result

- `.agents/skills/code-review/SKILL.md` ends with your house rules.
- `gskill list` marks the skill `(overridden)`.
- `gskill info code-review --json` reports `overridden`, `base_hash`, `override_digest`, and the
  resolved declaration.
- Committing the repository is enough: a teammate who clones it reads the customized skill through
  their agent with no gskill commands at all.

## Choosing a kind

| Kind | Use when |
| --- | --- |
| `append` / `prepend` | Adding project conventions around upstream text. Never conflicts, so updates stay painless. |
| `replace` | You want your own version of one file while upstream keeps supplying the rest. |
| `patch` | You need a surgical change inside upstream text and want to know when upstream moves under it. |
| `version` / `ref` / `commit` | You want a different revision, not different content. |

Prefer layering when it will do. A patch is the only kind that can stop applying, and that is a
feature — it tells you upstream changed the text you were editing — but it is work you take on.

## Keeping upstream updates

```bash
gskill update code-review
```

The override is re-applied on top of the newer upstream. If a patch no longer applies, the update
fails with the patch and the change named, and your installed content is untouched until you
re-generate the patch or drop it.

## If you edit an override input later

Editing `house-rules.md` after installing changes the declaration's identity, so gskill reports it:

```bash
gskill check      # names the skill and the input that changed
gskill install    # re-applies, bringing committed content back in line
```

Under `--frozen-lockfile` the same edit fails closed instead — a CI run must not quietly
re-materialize content that no longer matches what was committed.
