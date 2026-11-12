# Use the TUI dashboard

> `gskill dashboard` is the canonical command; the former `gskill tui` still
> works as a silent alias.

Launch GSKILL's interactive terminal dashboard to browse installed skills, preview their `SKILL.md`
content, and update, change the version of, or remove a skill without leaving the dashboard. Every
mutating action runs through the same services as the matching CLI command, so a dashboard action and
its CLI equivalent leave the project in the same state.

> The TUI is interactive, so it is verified manually rather than in CI.

## Before you start

- A terminal (TTY). The TUI does not run in a non-interactive environment such as CI.
- A project with skills declared or installed.

## Steps

```bash
gskill dashboard
```

## Expected result

- An interactive dashboard opens, listing your skills grouped by source repository, each row showing
  name, source, installed version, and status.
- You can select a skill to see its details (tracking policy and configured agents) and preview its
  rendered `SKILL.md`. Previewed content is sanitised (terminal escape sequences are stripped) before
  display; fetched skill content is never executed.
- Quit to return to your shell.

## Keys

| Key | Action |
| --- | --- |
| `↑` / `↓`, `j` / `k` | Move the cursor |
| `tab` | Focus the preview to scroll it |
| `/` | Search by skill name or source repository (case-insensitive; typed letters are never actions) |
| `u` | Update the selected skill (compatible update only, within its declared constraint) |
| `v` | Change the selected skill's version (pick a release or type an explicit target) |
| `r` | Remove the selected skill |
| `g` | Update the selected skill's whole repository group |
| `enter` / `y` | Confirm the reviewed action |
| `esc` | Leave or clear the search, cancel a confirmation, or quit from the inventory |
| `q`, `ctrl+c` | Quit (`ctrl+c` always interrupts) |

## Search

Press `/`, then type part of a skill name or a source repository label. The table filters as you type
and the match is case-insensitive. Clear the query (`esc`) to restore the full inventory; the selection
returns to the same skill when it is still present. A query that matches nothing shows a distinct
"no matches" state that names the query.

## Update, change version, remove

Each action is reviewed and confirmed once; nothing is written before you confirm.

- **Update (`u`)** is offered only when a compatible update is available. The review shows
  `installed → target`. It stays within the skill's declared constraint and never moves a pin.
- **Change version (`v`)** opens a picker of selectable releases (plus a `latest` row) and a field for
  an explicit target. The review shows `installed → target` and, when `skills.toml` changes, the
  declaration before and after. An unsupported target is reported in place and nothing is written.
- **Remove (`r`)** shows the skill, its source repository, and the affected installations before you
  confirm. Removal follows `gskill remove` exactly, including the ownership check that protects locally
  modified content; it never deletes upstream repositories or local sources.
- **Group update (`g`)** reuses the repository-grouped update flow to update every compatible skill in
  the selected skill's repository, each moving within its own declaration.

After a confirmed action, progress is shown and then the per-skill outcome. The table, details, and
preview refresh in place from the project's new state without restarting the dashboard, and the cursor
rests on a valid remaining row. Each action targets the skill you selected by its identity, so changing
the search or a background refresh never applies it to the wrong row; if the project changed since the
preview, the dashboard revalidates and refuses a stale action rather than applying it. A failed action
is reported without ending the session, and the dashboard exits with the usual partial-failure code.

## Notes

- For scripting or CI, use the non-interactive commands instead, for example
  [`gskill list --json`](script-with-json.md),
  [`gskill update`](update-and-lock.md), [`gskill upgrade`](upgrade-a-skill.md), or
  `gskill remove <skill>`.

## See also

- [Inspect with list, info, and check](inspect-list-info-check.md)
- [Integrity and trust](../explanation/integrity-and-trust.md)
