# Official GSKILL skills

This directory holds the official, redistributable skills for AI agents that use GSKILL. It
holds nothing else.

| Skill | What it does |
| --- | --- |
| [`gskill-install`](gskill-install/SKILL.md) | Finds, installs, verifies, or upgrades the `gskill` CLI. Works before GSKILL is installed. |
| [`gskill`](gskill/SKILL.md) | Manages skills with the `gskill` CLI: discover, add, inspect, update, upgrade, verify, remove. |

The skills under `.agents/skills/` and `.claude/skills/` serve the development of this
repository and are not distributed.

## Install

With GSKILL installed, pin to a release and name your agent:

```sh
gskill add github.com/glapsfun/gskill/skills/gskill --ref v0.13.0 --agent claude
gskill add github.com/glapsfun/gskill/skills/gskill --ref v0.13.0 --agent claude --global
gskill add github.com/glapsfun/gskill --include 'skills/*' --all --ref v0.13.0 --agent claude
```

The first installs the usage skill in this project, the second for every project, the third both
skills in this project. Without GSKILL, copy `gskill-install` by hand as the guide describes.

See [Use GSKILL from an AI agent](../docs/how-to/use-gskill-from-an-agent.md) for setup and
examples.
