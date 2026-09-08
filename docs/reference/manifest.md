# `skills.toml` reference

`skills.toml` is the committed, hand-authored declaration of what a project wants: which skills,
which revision, which agents, and how each is customized. `skills-lock.json` records what those
declarations resolved to. You edit the first; gskill writes the second.

A project installed before the manifest existed gains one automatically on the next mutating
command, generated from its lockfile.

## Location and shape

The file lives at the repository root and is committed, so a teammate cloning the repository gets
the same declarations with no local setup.

```toml
# Project-scoped configuration. Unknown keys warn; they never fail a run.
[config]
jobs = 4

[skills.code-review]
source  = "github:org/skills"   # required
skill   = "code-review"         # optional; defaults to the table key
version = "^1.0.0"              # optional semver constraint to track
ref     = "v2.1"                # optional tag or branch
commit  = "abc123def456"        # optional exact pin; wins over ref
agents  = ["claude"]            # optional; which agents receive the skill
mode    = "symlink"             # optional; symlink | copy | auto

[skills.code-review.override]
replace = { "SKILL.md" = "gskill/code-review/SKILL.md" }
patch   = ["gskill/code-review/01-tone.diff"]
prepend = ["gskill/code-review/header.md"]
append  = ["gskill/code-review/house-rules.md"]
```

## `[skills.<name>]`

| Key | Required | Meaning |
| --- | --- | --- |
| `source` | yes | Git URL or shorthand (`github:owner/repo`) — the same forms `gskill add` accepts. |
| `skill` | no | The skill's name inside the source, when it differs from the installed name. |
| `version` | no | A semver constraint to track. `gskill update` advances within it; `gskill upgrade` changes it. An exact version (`1.2.0`) is a pin that only `upgrade` or an edit moves. |
| `ref` | no | A tag or branch. Resolved to an immutable commit before it reaches the lockfile. |
| `commit` | no | An exact revision. Wins over `ref`, which then has no effect. |
| `agents` | no | Which agents receive the skill. Editing this adds or removes targets on the next `install`, `update`, or `upgrade`. Under `--frozen-lockfile` a changed list is refused (exit `4`) rather than applied. |
| `mode` | no | `symlink` (default behaviour), `copy`, or `auto`. Recorded only when you ask for it. |

Unknown keys in a skill table are an **error**, not a warning. A typo such as `apend` would
otherwise silently drop a transformation you believe is applied, leaving content that is wrong but
internally consistent — the hardest kind of mistake to notice.

## `[skills.<name>.override]`

Four kinds of customization, applied in a fixed order: **replace → patch → layer**. Replace
establishes the file that ships, patch adjusts that file, and layering goes last so appended
content is never itself patched.

| Key | Type | Meaning |
| --- | --- | --- |
| `replace` | table | Swap a file wholesale. The key is a path inside the skill; the value is a repo-relative source file. |
| `patch` | list | Unified diffs, applied in the order listed. |
| `prepend` | list | Content inserted before `SKILL.md`, in the order listed. |
| `append` | list | Content inserted after `SKILL.md`, in the order listed. |

Pinning (`version`, `ref`, `commit`) is not part of this table: it selects *which input* you start
from, before any content exists to transform.

Every referenced path must be repo-relative and must resolve inside the repository, and an override
may only modify files inside its own skill. A patch is a committed file that arrives with every
clone, so one naming `../../.claude/settings.json` would otherwise write into every teammate's
agent configuration. Such a patch is refused before anything is written.

Patches apply **strictly**, with no fuzz. When upstream moves under a patch, the run fails with the
skill, the patch, and the change named — nothing is partially applied, and previously installed
content is left exactly as it was.

## Identity and drift

The lockfile records the hash of the upstream content (`baseHash`), the hash of the result
(`contentHash`), and a digest covering the declaration **and the bytes of every file it
references** (`overrideDigest`).

That last point is what makes editing `house-rules.md` detectable: its content feeds the identity,
so the digest changes and `gskill project check` reports drift naming the input responsible. Run
`gskill install` to re-apply, or revert the file.

## Precedence

`[config]` occupies the project layer. From lowest to highest: built-in defaults, your user
configuration, **project configuration**, environment variables, command-line flags.

Unknown keys here only warn. The manifest is committed and shared, so a key a newer gskill
understands must not break an older one.
