# `SKILL.md` frontmatter schema (v1)

A skill is a directory containing a `SKILL.md` file whose **YAML frontmatter** declares its identity
and (optionally) its requirements. The Markdown body below the frontmatter is the skill's
instructions. Frontmatter is parsed as YAML; malformed or invalid frontmatter fails validation (with
line context) and the install is refused.

## Shape

```yaml
---
name: kubernetes-expert                         # required
description: Kubernetes operational guidance     # required
version: 2.1.3                                   # optional
license: MIT                                     # optional
compatibility: ">=1.0"                           # optional (string or object)
requires:                                        # optional — recorded & warned, NOT resolved
  skills: ["shell-scripting >=1.2.0"]
  commands: ["kubectl", "helm"]
  environment: ["KUBECONFIG"]
  mcp: []
---

# Body: the skill instructions (Markdown)
```

## Field rules

| Field | Required | Type / values | Notes |
| --- | --- | --- | --- |
| `name` | yes | lowercase kebab `[a-z0-9-]` | Must equal the manifest key for the skill. |
| `description` | yes | non-empty string | Short summary. |
| `version` | no | string | Informational. |
| `license` | no | string | SPDX or free-form. |
| `compatibility` | no | string or object | Compatibility hint. |
| `requires.skills` | no | list of strings | `name [constraint]`; recorded, **not** resolved transitively. |
| `requires.commands` | no | list of strings | External command names (checked by `doctor`). |
| `requires.environment` | no | list of strings | Environment variable names. |
| `requires.mcp` | no | list of strings | MCP server names. |

## Validation behaviour

- Missing `name`/`description` or invalid YAML → validation error with line context; install refused.
- A naming-rule violation, or a `name` that doesn't match the manifest key → rejected.
- **Unknown keys** → warning, not an error (forward-compatible).
- Content is **never executed**. Files with an executable bit are surfaced as warnings only. Terminal
  escape sequences in any echoed/previewed content are sanitised before display.

## See also

- [Run doctor](../how-to/run-doctor.md) — checks the `requires` block.
- [Integrity and trust](../explanation/integrity-and-trust.md)
