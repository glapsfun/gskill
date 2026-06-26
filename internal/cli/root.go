package cli

import (
	"context"
	"fmt"
	"io"

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

	Version versionCmd `cmd:"" default:"1" help:"Print the gskill version."`
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
	parser, err := kong.New(&root,
		kong.Name("gskill"),
		kong.Description("Reproducible package manager for agentic AI skills."),
		kong.Writers(stdout, stderr),
		kong.Exit(func(int) {}),
	)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return int(errs.CodeGeneric)
	}

	kctx, err := parser.Parse(args)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return int(errs.CodeUsage)
	}

	out := NewOutput(stdout, stderr, OutputOptions{
		JSON:        root.JSON,
		Quiet:       root.Quiet,
		Interactive: !root.NoInteractive,
	})

	kctx.BindTo(ctx, (*context.Context)(nil))
	kctx.Bind(application)

	if runErr := kctx.Run(out); runErr != nil {
		out.Diag("error: %v", runErr)
		return errs.ExitCode(runErr)
	}
	return 0
}
