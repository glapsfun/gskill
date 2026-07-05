# Use the TUI dashboard

> `gskill dashboard` is the canonical command; the former `gskill tui` still
> works as a silent alias.

Launch GSKILL's interactive terminal dashboard to browse installed skills and preview their `SKILL.md`
content.

> The TUI is interactive, so it is verified manually rather than in CI.

## Before you start

- A terminal (TTY). The TUI does not run in a non-interactive environment such as CI.
- A project with skills declared or installed.

## Steps

```bash
gskill dashboard
```

## Expected result

- An interactive dashboard opens, listing your skills and their status.
- You can select a skill to preview its rendered `SKILL.md`. Previewed content is sanitised (terminal
  escape sequences are stripped) before display — fetched skill content is never executed.
- Quit to return to your shell.

## Notes

- For scripting or CI, use the non-interactive commands instead — e.g.
  [`gskill list --json`](script-with-json.md).

## See also

- [Inspect with list, info, and diff](inspect-list-info-diff.md)
- [Integrity and trust](../explanation/integrity-and-trust.md)
