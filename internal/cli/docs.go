package cli

import (
	"io"

	"github.com/alecthomas/kong"
)

// DocsModel builds the Kong model for the gskill grammar without parsing
// arguments or exiting. It mirrors the parser constructed in Run, so generated
// reference documentation always reflects the real command surface. It exists
// solely for the reference generator (cmd/gen-reference) and changes nothing
// about the shipped CLI behavior.
func DocsModel() (*kong.Application, error) {
	var root rootCLI
	options := append(grammarOptions(), kong.Exit(func(int) {}))
	parser, err := kong.New(&root, options...)
	if err != nil {
		return nil, err
	}
	return parser.Model, nil
}

// ParseArgs parses args (without the leading "gskill") against the shipped
// grammar and runs no command. It exists for the documentation checks, which
// require every documented invocation to name real commands and flags, so it
// writes nothing and never exits the process.
func ParseArgs(args []string) error {
	var root rootCLI
	// Run treats a --help request as success even when the rest of the line
	// would not parse (`gskill add --help` has no source), so mirror that.
	var helpRequested bool
	options := append(grammarOptions(),
		kong.Exit(func(int) { helpRequested = true }),
		kong.Writers(io.Discard, io.Discard),
	)
	parser, err := kong.New(&root, options...)
	if err != nil {
		return err
	}
	// Run shows help for a bare `gskill` and, outside --json, for a group
	// named without a subcommand (`gskill project`), both exiting 0.
	if len(args) == 0 {
		return nil
	}
	_, err = parser.Parse(args)
	if helpRequested {
		return nil
	}
	if err != nil && !root.JSON && isMissingSubcommand(err, parseErrorNode(err)) {
		return nil
	}
	return err
}
