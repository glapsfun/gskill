# Getting started with GSKILL

This tutorial takes you from an empty directory to a **committed, reproducible skill environment**.
By the end you will have installed a skill, locked it, and proven that anyone can restore the exact
same files with one command.

It should take about **10–15 minutes**. Follow every step in order — each builds on the last.

## What you need

- GSKILL installed and on your `PATH` (run `gskill version` to check).
- A system `git` binary (run `gskill doctor` to confirm your environment).
- An AI agent project marker so GSKILL knows where to install. This tutorial uses **Claude Code**, so
  we create a `.claude/` directory.

## Step 1 — Create a project

```bash
mkdir skill-demo && cd skill-demo
mkdir .claude          # marks this as a Claude Code project so GSKILL detects the agent
```

**Expected:** an empty project directory with a `.claude/` marker. Nothing is installed yet.

## Step 2 — Initialise GSKILL

```bash
gskill init
```

**Expected:** GSKILL scaffolds a `gskill.toml` manifest, a `.gskill/` state directory, and `.gitignore`
hints. `gskill init` exits `0`.

## Step 3 — Add a skill

For a first run we'll add a **local** skill folder (a directory containing a `SKILL.md`). If you have
a Git source instead, see [Add a skill from Git](../how-to/add-a-git-skill.md) — the rest of this
tutorial is identical.

```bash
gskill add ./path/to/a/skill        # a folder containing SKILL.md
```

**Expected:** GSKILL resolves the source, installs the skill into `.claude/skills/<name>/`, records
**intent** in `gskill.toml`, and records **resolved reality** in `skills-lock.json`. You'll see
`Added <name> (<content-hash>) into 1 agent(s)`.

## Step 4 — Inspect what you have

```bash
gskill list                 # installed skills + status
gskill list --json          # the same, machine-readable
```

**Expected:** your skill appears in the list with an "ok" status. The `--json` form prints a single
JSON object on stdout — handy for scripts (see [Script with --json](../how-to/script-with-json.md)).

## Step 5 — Commit intent + reality

```bash
git init
git add gskill.toml skills-lock.json
git commit -m "Add first skill via gskill"
```

**Expected:** both files are committed. `gskill.toml` is human-editable intent; `skills-lock.json` is the
machine-generated, deterministic record that makes restores reproducible. To understand why both
exist, read [The reproducibility model](../explanation/reproducibility-model.md).

## Step 6 — Prove reproducibility

Simulate a fresh checkout, then restore **exactly** from the lockfile:

```bash
rm -rf .gskill .claude/skills        # throw away installed state
gskill install --frozen-lockfile     # restore precisely from skills-lock.json
```

**Expected:** GSKILL re-creates the identical skill files and exits `0`. `--frozen-lockfile` never
modifies the lockfile, and it **fails closed** (exit `4`) if the manifest and lockfile disagree — so
CI can trust it. Try [gating CI on drift](../how-to/gate-ci-on-drift.md) next.

## You did it 🎉

You now have a committed `gskill.toml` + `skills-lock.json` and a one-command reproducible restore.

### Next steps

- **Do more:** browse the [how-to guides](../how-to/index.md) — install into multiple agents, work
  offline, update within constraints, verify integrity, and more.
- **Look things up:** the [command reference](../reference/commands.md) and
  [exit codes](../reference/exit-codes.md).
- **Understand the design:** [the reproducibility model](../explanation/reproducibility-model.md) and
  [integrity and trust](../explanation/integrity-and-trust.md).
