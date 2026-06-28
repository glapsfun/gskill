package cli

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/alecthomas/kong"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/version"
)

// rootCLI is the gskill command grammar: global flags plus the command tree.
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

	Version  versionCmd  `cmd:"" help:"Print the gskill version."`
	Init     initCmd     `cmd:"" help:"Scaffold a gskill project (manifest, state dir, gitignore)."`
	Add      addCmd      `cmd:"" help:"Add and install a new skill."`
	Source   sourceCmd   `cmd:"" help:"Inspect a skill source (list/inspect/check) without installing."`
	Find     findCmd     `cmd:"" help:"Search for skills in a source, a GitHub owner, or configured repositories."`
	Install  installCmd  `cmd:"" help:"Install all declared skills (additive, idempotent)."`
	Verify   verifyCmd   `cmd:"" help:"Re-hash installed content against the lockfile."`
	Check    checkCmd    `cmd:"" help:"Report fast drift status."`
	Outdated outdatedCmd `cmd:"" help:"Show skills with newer versions available."`
	Update   updateCmd   `cmd:"" help:"Advance skills within their version constraints."`
	Lock     lockCmd     `cmd:"" help:"Recompute the lockfile from the manifest."`
	Remove   removeCmd   `cmd:"" help:"Uninstall skills and clean up."`
	Sync     syncCmd     `cmd:"" help:"Make disk exactly match the lockfile (--prune removes orphans)."`
	Repair   repairCmd   `cmd:"" help:"Re-materialize broken installs and clean up staging."`

	List       listCmd       `cmd:"" help:"List installed skills and their status."`
	Info       infoCmd       `cmd:"" help:"Show details for one skill."`
	Diff       diffCmd       `cmd:"" help:"Show manifest/lock/disk differences."`
	Doctor     doctorCmd     `cmd:"" help:"Check the environment and declared requirements."`
	Cache      cacheCmd      `cmd:"" help:"Manage the content cache."`
	ConfigCmd  configCmd     `cmd:"" name:"config" help:"Inspect layered configuration."`
	Completion completionCmd `cmd:"" help:"Print a shell completion script."`
	TUI        tuiCmd        `cmd:"" name:"tui" help:"Launch the interactive dashboard."`
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

// Run prints the version line (human) or a {"version": ...} object (JSON).
func (versionCmd) Run(out *Output) error {
	if out.JSON() {
		return out.Result("", map[string]string{"version": version.Version()})
	}
	return out.Result(version.String(), nil)
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
	parser, err := kong.New(&root,
		kong.Name("gskill"),
		kong.Description("Reproducible package manager for agentic AI skills."),
		kong.Writers(stdout, stderr),
		kong.Exit(func(int) { helpRequested = true }),
	)
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
		_, _ = fmt.Fprintln(stderr, err)
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
		out.Diag("error: %v", runErr)
		return errs.ExitCode(runErr)
	}
	return 0
}
