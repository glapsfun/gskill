# CLI colors and formatting

GSKILL's human-readable output is colored and structured through one shared vocabulary,
`internal/tui.Theme` (`internal/tui/theme.go`), rather than hard-coded ANSI codes scattered
through the command layer. This page explains the palette, how it degrades, and the pattern to
follow when a new command needs styled output.

## The semantic palette

`Theme` maps meaning to style, not the other way around — call sites pick a *role*
(`Success`, `Warning`, `Error`, ...), never a raw color:

- **Success** (green) — a command completed as expected (`✓`, `Output.summary`).
- **Warning** (yellow) — degraded but non-fatal: drift, an outdated version, a non-fatal
  diagnostic (`◐`, `Output.warnSummary`, `Output.Warn`).
- **Error** (red, bold) — failure (`✗`, `Output.errSummary`, `Output.ErrDiag`, and `root.go`'s
  error-reporting paths).
- **Accent** (bold indigo) — the identity of a thing: a skill name, a command name, a flag.
- **Subtitle** — secondary or muted text: sources, hints, neutral notices (`Output.Info`).
- **Hint** — dimmed actionable follow-ups (`→ run 'gskill doctor'`-style lines, `Output.Hint`).
- **TableHeader** — column headers in aligned output.

Every color is a `lipgloss.AdaptiveColor` with a light and a dark value, so the same style reads
correctly on both terminal themes without any of gskill's own code branching on background color.

## Degradation is automatic and guaranteed

Color availability is resolved once, by `lipgloss`/`termenv`'s own profile detection — gskill does
no capability probing of its own. In order of effect:

- **`NO_COLOR`** (or any colorless terminal, or output piped to a file/another process) collapses
  every style to plain text.
- **`--no-interactive`** explicitly disables prompts and colors regardless of terminal
  capability — this is what CI and scripted callers should pass.
- **`--json`** never carries styling: `Output.Result` writes JSON straight to stdout with no
  human-formatting layer involved at all (structured output is a separate code path, not styled
  output with color stripped).
- Every styled command has a plain counterpart selected via `Output.Interactive()`. The pair is
  proven byte-identical without color by tests (e.g. `TestStyleHelp_IdentityWithoutColor`,
  `TestRenderPlanTextStyled_KeepsText`, `TestStyleDiag_IdentityWithoutColor`) — a locked guarantee,
  not an assumption to re-verify by hand each time a command changes.

## The pattern for a new command

1. Build the plain rendering first — the one that ships when color is unavailable. This is also
   what `--json` peers alongside (`Output.Result(human, jsonValue)`).
2. If the command's output benefits from color (a table, a status line, a multi-field report),
   add a second `render*Styled` function next to it that returns the same content run through
   `tui.DefaultTheme()`. Select between them at the call site with `if out.Interactive() { ... }` —
   never inside the render function itself.
3. For one-line diagnostics on stderr, reach for `Output.Info` (neutral), `Output.Warn`
   (non-fatal problem), `Output.ErrDiag` (failure), or `Output.Hint` (actionable follow-up)
   instead of the low-level `Output.Diag` — they apply the matching theme style automatically
   and degrade to `Diag`'s plain behavior without color. Use `Diag` directly only when a line is
   deliberately unstyled (e.g. a user-cancelled run, which is not a failure).
4. Never hand-roll an ANSI escape. If a style the palette doesn't already name, add it to `Theme`
   rather than styling inline — that keeps every surface (CLI, wizard, dashboard) drawing from one
   definition.
