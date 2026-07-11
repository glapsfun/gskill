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
	Config        string `help:"Path to a config file." type:"path"`
	Verbose       bool   `short:"v" help:"Enable verbose diagnostics."`
	Dir           string `short:"C" help:"Run as if gskill started in this directory." type:"path"`

	Init    initCmd    `cmd:"" group:"core" help:"Initialize local gskill state (.gskill, .agents/skills, gitignore)."`
	Add     addCmd     `cmd:"" group:"core" help:"Add and install a new skill."`
	Onboard onboardCmd `cmd:"" group:"core" help:"Guided skill installation without a predefined source."`
	Install installCmd `cmd:"" group:"core" help:"Install all declared skills (additive, idempotent)."`
	Update  updateCmd  `cmd:"" group:"core" help:"Advance skills within their version constraints."`
	Remove  removeCmd  `cmd:"" group:"core" help:"Uninstall skills and clean up."`

	List     listCmd     `cmd:"" group:"inspect" help:"List installed skills and their status."`
	Status   statusCmd   `cmd:"" group:"inspect" help:"Show installed skills, their agents, modes, and per-target health."`
	Info     infoCmd     `cmd:"" group:"inspect" help:"Show details for one skill."`
	Search   searchCmd   `cmd:"" group:"inspect" aliases:"find" help:"Search for skills in a source, a GitHub owner, or configured repositories."`
	Outdated outdatedCmd `cmd:"" group:"inspect" help:"Show skills with newer versions available."`

	// Hidden aliases of the regrouped maintenance commands (see aliasTable).
	// They reuse the same command structs as the project group, so behavior is
	// identical by construction; the group tag keeps them out of kong's
	// ungrouped "Commands:" bucket, hidden keeps them out of every help
	// listing, and declaring them before Project keeps the section's spacing
	// clean (kong emits an entry separator only after visible nodes).
	Sync   syncCmd   `cmd:"" hidden:"" group:"project" help:"Reconcile disk to the lock's declared state (--prune removes managed orphans)."`
	Repair repairCmd `cmd:"" hidden:"" group:"project" help:"Re-materialize broken installs and clean up staging."`
	Verify verifyCmd `cmd:"" hidden:"" group:"project" help:"Re-hash installed content against the lockfile."`
	Check  checkCmd  `cmd:"" hidden:"" group:"project" help:"Report fast drift status."`
	Diff   diffCmd   `cmd:"" hidden:"" group:"project" help:"Show lock/disk differences."`

	Project projectCmd `cmd:"" group:"project" help:"Manage this project's lockfile and installed state."`

	Source     sourceCmd     `cmd:"" group:"more" help:"Inspect a skill source (list/inspect/check) without installing."`
	Cache      cacheCmd      `cmd:"" group:"more" help:"Manage the content cache."`
	ConfigCmd  configCmd     `cmd:"" name:"config" group:"more" help:"Inspect layered configuration."`
	Unlink     unlinkCmd     `cmd:"" group:"more" help:"Detach one agent from a skill (--prune removes it when the last agent goes)."`
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
	})

	root.resolveDir()
	kctx.BindTo(ctx, (*context.Context)(nil))
	kctx.Bind(application)
	kctx.Bind(projectRoot(root.Dir))
	kctx.Bind(Globals{Offline: root.Offline, NoCache: root.NoCache, DryRun: root.DryRun, Yes: root.Yes})

	if runErr := kctx.Run(out); runErr != nil {
		if errors.Is(runErr, errs.ErrCancelled) {
			// A user-initiated cancel is not an error condition: report it
			// plainly and exit 130 (spec 011, contracts/cli-onboarding.md).
			out.Diag("%v", runErr)
			return errs.ExitCode(runErr)
		}
		out.ErrDiag("error: %v", runErr)
		if hint := errs.HintOf(runErr); hint != "" {
			out.Hint("→ %s", hint)
		}
		return errs.ExitCode(runErr)
	}
	return 0
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

	candidates := make([]string, 0, len(model.Children)+len(aliasTable))
	for _, node := range model.Children {
		if node.Type == kong.CommandNode && !node.Hidden {
			candidates = append(candidates, node.Name)
		}
	}
	canonicalOf := make(map[string]string, len(aliasTable))
	for _, a := range aliasTable {
		if a.Kind != aliasKindCommand {
			continue
		}
		candidates = append(candidates, a.Old)
		canonicalOf[a.Old] = a.Canonical
	}
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
