package cli

import "github.com/alecthomas/kong"

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
