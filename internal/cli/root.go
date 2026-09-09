package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/alecthomas/kong"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/selection"
	"github.com/glapsfun/gskill/internal/tui"
	"github.com/glapsfun/gskill/internal/version"
)

// rootCLI is the gskill command grammar: global flags plus the command tree,
// organized into the CORE / INSPECT / PROJECT / MORE help sections. Field
// order matters: kong renders command groups in order of first appearance.
type rootCLI struct {
	JSON          bool   `help:"Emit machine-readable JSON on stdout."`
	Quiet         bool   `help:"Suppress diagnostics on stderr."`
	NoInteractive bool   `name:"no-interactive" help:"Disable prompts and colored output."`
	Yes           bool   `help:"Assume yes for confirmation prompts."`
	Offline       bool   `help:"Operate without network access."`
	NoCache       bool   `name:"no-cache" help:"Bypass the content cache."`
	DryRun        bool   `name:"dry-run" help:"Report actions without applying them."`
	Config        string `help:"Path to a TOML config file; overrides the user config file. Must exist." type:"existingfile"`
	Verbose       bool   `short:"v" help:"Enable verbose diagnostics."`
	Dir           string `short:"C" help:"Run as if gskill started in this directory." type:"path"`

	Add     addCmd     `cmd:"" group:"core" help:"Add and install a new skill (auto-initializes the project if needed)."`
	Onboard onboardCmd `cmd:"" group:"core" help:"Guided skill installation without a predefined source."`
	Install installCmd `cmd:"" group:"core" help:"Realize what skills.toml declares (resolve changes, install, record the lockfile). Never edits skills.toml."`
	Update  updateCmd  `cmd:"" group:"core" help:"Re-resolve declared skills to the newest revision their skills.toml constraint allows. Never edits skills.toml."`
	Upgrade upgradeCmd `cmd:"" group:"core" help:"Move a skill's declared version in skills.toml, then resolve, lock, install, and verify it."`
	Remove  removeCmd  `cmd:"" group:"core" help:"Uninstall skills and clean up."`

	List     listCmd     `cmd:"" group:"inspect" help:"List installed skills, their status, and per-agent health."`
	Info     infoCmd     `cmd:"" group:"inspect" help:"Show details for one skill."`
	Search   searchCmd   `cmd:"" group:"inspect" aliases:"find" help:"Search for skills in a source, a GitHub owner, or configured repositories."`
	Outdated outdatedCmd `cmd:"" group:"inspect" help:"Show skills with newer versions available."`

	Project projectCmd `cmd:"" group:"project" help:"Manage this project's lockfile and installed state."`

	Cache      cacheCmd      `cmd:"" group:"more" help:"Manage the content cache."`
	ConfigCmd  configCmd     `cmd:"" name:"config" group:"more" help:"Inspect layered configuration."`
	Doctor     doctorCmd     `cmd:"" group:"more" help:"Check the environment and declared requirements."`
	Dashboard  tuiCmd        `cmd:"" name:"dashboard" group:"more" aliases:"tui" help:"Launch the interactive dashboard."`
	Completion completionCmd `cmd:"" group:"more" help:"Print a shell completion script."`
	Version    versionCmd    `cmd:"" group:"more" help:"Print the gskill version."`
}

// projectRoot is the resolved working directory, bound for command use.
type projectRoot string

// Globals carries the persistent flag values that commands consume.
type Globals struct {
	Offline bool
	NoCache bool
	DryRun  bool
	Yes     bool
}

// resolveDir defaults Dir to the current working directory when unset.
func (r *rootCLI) resolveDir() {
	if r.Dir != "" {
		return
	}
	if wd, err := os.Getwd(); err == nil {
		r.Dir = wd
	}
}

// versionCmd prints the build version.
type versionCmd struct{}

// Help returns the detailed help shown by `gskill version --help`.
func (versionCmd) Help() string {
	return examplesHelp(
		"gskill version",
		"gskill version --json",
	)
}

// Run prints the version line (human) or a {"version": ...} object (JSON).
func (versionCmd) Run(out *Output) error {
	if out.JSON() {
		return out.Result("", map[string]string{"version": version.Version()})
	}
	human := version.String()
	if out.Interactive() {
		human = tui.DefaultTheme().Accent.Render(human)
	}
	return out.Result(human, nil)
}

// helpWrapWidth pins help wrapping so output is byte-identical regardless of
// terminal size; the help golden tests depend on it.
const helpWrapWidth = 80

// grammarOptions returns the kong options that define the gskill grammar and
// help layout. Run and DocsModel share them so the shipped CLI, the help
// golden tests, and the generated reference docs can never disagree.
func grammarOptions() []kong.Option {
	return []kong.Option{
		kong.Name("gskill"),
		kong.Description("Reproducible package manager for agentic AI skills."),
		kong.ExplicitGroups(helpGroupTitles),
		kong.ConfigureHelp(kong.HelpOptions{
			NoExpandSubcommands: true,
			WrapUpperBound:      helpWrapWidth,
		}),
	}
}

// unknownCommandRE captures the offending token of an unknown-command error,
// with or without kong's own trailing suggestion.
var unknownCommandRE = regexp.MustCompile(`^unexpected argument (\S+?)(, did you mean .*)?$`)

// parseErrorNode returns the command node kong had selected when the parse
// failed, or nil when the failure happened before any command matched (e.g.
// an unknown top-level token or a flags-only invocation).
func parseErrorNode(err error) *kong.Node {
	var pe *kong.ParseError
	if errors.As(err, &pe) && pe.Context != nil {
		return pe.Context.Selected()
	}
	return nil
}

// isMissingSubcommand reports whether err is kong's missing-subcommand
// validation error for the given selected node: a group command was named but
// no leaf was chosen. Missing positional arguments produce `expected "<arg>"`
// instead, so they keep failing as usage errors.
func isMissingSubcommand(err error, selected *kong.Node) bool {
	return selected != nil && len(selected.Children) > 0 &&
		strings.HasPrefix(err.Error(), "expected one of ")
}

// styledUsageError renders a kong usage-error message and its follow-up
// hint line: red / dimmed on an interactive terminal, unchanged otherwise —
// so piped, NO_COLOR, and --no-interactive output stays byte-identical.
func styledUsageError(interactive bool, msg string) (errLine, hintLine string) {
	errLine = styleDiag(interactive, tui.DefaultTheme().Error, msg)
	hintLine = styleDiag(interactive, tui.DefaultTheme().Hint, "Run 'gskill --help' for usage.")
	return errLine, hintLine
}

// noInteractiveRequested reports whether --no-interactive appears anywhere in
// args, scanned directly rather than through kong's parsed root struct: on a
// usage error kong never applies flag values to root (Parse's Apply step
// only runs after a fully successful trace), so root.NoInteractive stays
// false regardless of what was typed or where. Scanning args directly keeps
// the usage-error path honoring --no-interactive even when the parse itself
// failed.
func noInteractiveRequested(args []string) bool {
	for _, a := range args {
		if a == "--no-interactive" {
			return true
		}
	}
	return false
}

// Run parses args and executes the selected command, writing to stdout/stderr,
// and returns the process exit code. Usage errors map to code 2; any error a
// command returns is mapped through errs.ExitCode.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer, application *app.App) int {
	var root rootCLI

	// helpRequested records that Kong handled a --help/-h flag: its handler
	// prints the help screen to stdout and then calls Exit, which we capture
	// here instead of terminating the process so Run stays testable.
	var helpRequested bool
	options := append(grammarOptions(),
		kong.Writers(stdout, stderr),
		kong.Exit(func(int) { helpRequested = true }),
		kong.Help(styledHelpPrinter(stdout)),
	)
	parser, err := kong.New(&root, options...)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return int(errs.CodeGeneric)
	}

	// A bare invocation shows the root help screen. Routing it through Kong's
	// own --help flag guarantees byte-identical output to `gskill --help`.
	if len(args) == 0 {
		args = []string{"--help"}
	}

	kctx, err := parser.Parse(args)
	// A help request is a successful result, not a usage error: Kong already
	// wrote help to stdout, so exit 0 before inspecting the parse error (which,
	// with no command selected, would otherwise be "expected one of ...").
	if helpRequested {
		return int(errs.CodeOK)
	}
	if err != nil {
		selected := parseErrorNode(err)
		// A group command without a subcommand (e.g. `gskill project`) is a
		// navigation step, not a mistake: show that group's help and succeed,
		// exactly as if --help had been passed. Not in --json mode, though —
		// machine consumers need the strict usage error and a clean stdout.
		if isMissingSubcommand(err, selected) && !root.JSON && retryWithHelp(args, options, &helpRequested) {
			return int(errs.CodeOK)
		}
		// Only rewrite root-level unknown-command errors: deeper in the tree
		// (a selected command with a stray or misspelled argument) kong's own
		// context-aware message is the correct one.
		msg := err.Error()
		if selected == nil {
			msg = suggestAlternative(err, parser.Model)
		}
		errLine, hintLine := styledUsageError(!noInteractiveRequested(args) && isTTY(stderr), msg)
		_, _ = fmt.Fprintln(stderr, errLine)
		_, _ = fmt.Fprintln(stderr, hintLine)
		return int(errs.CodeUsage)
	}

	out := NewOutput(stdout, stderr, OutputOptions{
		JSON:        root.JSON,
		Quiet:       root.Quiet,
		Interactive: !root.NoInteractive,
		Verbose:     root.Verbose,
	})

	root.resolveDir()
	// A nil App reaches Run only from tests that exercise the grammar alone.
	// Configuration resolution below is not optional, so give them the same
	// all-defaults App the real entrypoint would build.
	if application == nil {
		application = app.New(app.Options{})
	}
	// The user and project layers only become resolvable once --config and -C
	// have been parsed, so configuration is completed here rather than at
	// startup (spec 023 FR-002, spec 026 FR-001). A failure here is a real
	// configuration error — a file that will not parse, or a config directory
	// that will not resolve — and must stop the run rather than leave it on
	// values the user did not ask for.
	if cfgErr := application.ApplyRuntimeConfig(root.Dir, root.Config); cfgErr != nil {
		return reportRunError(out, cfgErr)
	}
	kctx.BindTo(ctx, (*context.Context)(nil))
	kctx.Bind(application)
	kctx.Bind(projectRoot(root.Dir))
	kctx.Bind(Globals{Offline: root.Offline, NoCache: root.NoCache, DryRun: root.DryRun, Yes: root.Yes})

	if runErr := kctx.Run(out); runErr != nil {
		return reportRunError(out, runErr)
	}
	return 0
}

// reportRunError renders a failed run on stderr and maps it to its exit code.
func reportRunError(out *Output, runErr error) int {
	var rep reportedError
	if errors.As(runErr, &rep) {
		// The interactive UI already told the full story (spec 014 FR-020):
		// map to the exit code without repeating a generic summary line.
		return errs.ExitCode(runErr)
	}
	if errors.Is(runErr, errs.ErrCancelled) || errors.Is(runErr, context.Canceled) {
		// A user-initiated cancel is not an error condition: report it
		// plainly and exit 130 (spec 011, contracts/cli-onboarding.md). Raw
		// context.Canceled can surface from ctx-aware operations (e.g. a git
		// fetch aborted mid-skill) whose chains never pass an errs sentinel —
		// they map to the same cancellation contract, never a generic error.
		out.Diag("%v", runErr)
		return int(errs.CodeCancelled)
	}
	out.ErrDiag("error: %v", runErr)
	if hint := errs.HintOf(runErr); hint != "" {
		out.Hint("→ %s", hint)
	}
	return errs.ExitCode(runErr)
}

// retryWithHelp re-parses args with --help appended, using a fresh grammar so
// the failed first parse leaves no state behind and the caller's args slice
// is never touched. It reports whether kong rendered a help screen (observed
// through the shared helpRequested exit-capture flag).
func retryWithHelp(args []string, options []kong.Option, helpRequested *bool) bool {
	var retryRoot rootCLI
	retryArgs := append(append(make([]string, 0, len(args)+1), args...), "--help")
	retryParser, err := kong.New(&retryRoot, options...)
	if err != nil {
		return false
	}
	_, _ = retryParser.Parse(retryArgs)
	return *helpRequested
}

// suggestAlternative rewrites an unknown-command parse error with a
// deterministic "did you mean?" suggestion computed over the grammar's
// visible top-level commands and the alias table (kong's own suggester never
// sees aliases and casts a looser net). Alias hits resolve to their canonical
// form so users are always steered toward the documented surface; when this
// suggester finds nothing, kong's original message — including any suggestion
// of its own — is kept as-is.
func suggestAlternative(err error, model *kong.Application) string {
	msg := err.Error()
	m := unknownCommandRE.FindStringSubmatch(msg)
	if m == nil {
		return msg
	}

	candidates, canonicalOf := suggestionCandidates(model)
	hits := selection.Closest(strings.Trim(m[1], `"`), candidates, 1)
	if len(hits) == 0 {
		return msg
	}
	hit := hits[0]
	if canonical, ok := canonicalOf[hit]; ok {
		hit = canonical
	}
	return fmt.Sprintf("unexpected argument %s, did you mean %q?", m[1], hit)
}

// suggestionCandidates returns every name a typo may resolve to, together
// with the canonical form each non-canonical name maps to. Three sources, in
// precedence order: visible top-level commands, alias-table old names, and
// each group's visible leaves.
//
// The leaves are included so the flat spellings retired by spec 025 are
// answered with a pointer rather than with silence — a bare `check` reports
// `did you mean "project check"?`. A leaf whose name collides with a
// top-level command (`list`) is skipped, so the top-level meaning wins.
func suggestionCandidates(model *kong.Application) (candidates []string, canonicalOf map[string]string) {
	candidates = make([]string, 0, len(model.Children)+len(aliasTable))
	canonicalOf = make(map[string]string, len(aliasTable))

	topLevel := make(map[string]bool, len(model.Children))
	for _, node := range model.Children {
		if node.Type == kong.CommandNode && !node.Hidden {
			candidates = append(candidates, node.Name)
			topLevel[node.Name] = true
		}
	}
	for _, a := range aliasTable {
		if a.Kind == aliasKindCommand {
			candidates = append(candidates, a.Old)
			canonicalOf[a.Old] = a.Canonical
		}
	}
	for _, node := range model.Children {
		if node.Type != kong.CommandNode || node.Hidden {
			continue
		}
		for _, sub := range node.Children {
			if sub.Type != kong.CommandNode || sub.Hidden || topLevel[sub.Name] {
				continue
			}
			candidates = append(candidates, sub.Name)
			canonicalOf[sub.Name] = node.Name + " " + sub.Name
		}
	}
	return candidates, canonicalOf
}
