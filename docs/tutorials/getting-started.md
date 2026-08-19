# Getting started with GSKILL

This tutorial takes you from an empty directory to a **committed, reproducible skill environment**.
By the end you will have installed a skill, locked it, committed it, and proven that anyone who
clones the repository gets the exact same files.

It should take about **10–15 minutes**. Follow every step in order — each builds on the last.

## What you need

- GSKILL installed and on your `PATH` (run `gskill version` to check).
- A system `git` binary (run `gskill doctor` to confirm your environment).
- macOS or Linux with working symlinks.
- An AI agent project marker so GSKILL knows where to install. This tutorial uses **Claude Code**, so
  we create a `.claude/` directory.

## Step 1 — Create a project

```bash
mkdir skill-demo && cd skill-demo
mkdir .claude          # marks this as a Claude Code project so GSKILL detects the agent
```

**Expected:** an empty project directory with a `.claude/` marker. Nothing is installed yet.

## Step 2 — Add a skill

> **No setup step needed.** The first `gskill add` (or `gskill install`) automatically prepares the
> local state GSKILL needs — a `.gskill/` state directory and a `.gskill/` line in `.gitignore` —
> so you go straight from an empty project to installing a skill.

For a first run we'll add a **local** skill folder (a directory containing a `SKILL.md`). If you have
a Git source instead, see [Add a skill from Git](../how-to/add-a-git-skill.md) — the rest of this
tutorial is identical.

```bash
gskill add ./path/to/a/skill        # a folder containing SKILL.md
```

**Expected:** GSKILL resolves the source, writes the skill content into `.agents/skills/<name>/`,
links it into Claude Code as `.claude/skills/<name>` (a relative symlink), and records both
**intent** and **resolved reality** in `skills-lock.json`. You'll see
`Added <name> (<content-hash>) into 1 agent(s)`.

## Step 3 — Inspect what you have

```bash
gskill list                 # installed skills + status
gskill list --json          # the same, machine-readable
```

**Expected:** your skill appears in the list with an "ok" status. The `--json` form prints a single
JSON object on stdout — handy for scripts (see [Script with --json](../how-to/script-with-json.md)).

## Step 4 — Commit the lockfile *and* the content

The skill content and its agent link are part of the repository — commit them together with the
lockfile:

```bash
git init
git status                  # lists skills-lock.json, .agents/skills/<name>/, .claude/skills/<name>
git add skills-lock.json .agents .claude
git commit -m "Add first skill via gskill"
```

**Expected:** `git status` shows the new content as addable — `.agents/skills/<name>/` (the real
directory) and `.claude/skills/<name>` (the committed relative symlink) — plus `skills-lock.json`,
which records both your intent (source, tracking constraint, agents) and the resolved reality
(content hash, exact version, targets). Because everything is committed, a fresh `git clone` of
this repository yields **working skills with zero gskill commands**. To understand the model, read
[Repo-owned storage](../explanation/repo-owned-storage.md).

## Step 5 — Prove reproducibility

Simulate lost installed content, then restore **exactly** from the lockfile:

```bash
rm -rf .gskill .claude/skills        # throw away local state and the agent links
gskill install --frozen-lockfile     # restore precisely from skills-lock.json
```

**Expected:** GSKILL re-creates the identical links and files and exits `0`. `--frozen-lockfile`
never modifies the lockfile, and it **fails closed** (exit `4`) if the manifest and lockfile
disagree — so CI can trust it. Try [gating CI on drift](../how-to/gate-ci-on-drift.md) next.

## You did it 🎉

You now have a committed, clone-and-go skill environment backed by `skills-lock.json`.

### Next steps

- **Do more:** browse the [how-to guides](../how-to/index.md) — install into multiple agents, work
  offline, update within constraints, verify integrity, and more.
- **Look things up:** the [command reference](../reference/commands.md) and
  [exit codes](../reference/exit-codes.md).
- **Understand the design:** [repo-owned storage](../explanation/repo-owned-storage.md) and
  [the reproducibility model](../explanation/reproducibility-model.md).
