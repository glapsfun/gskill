package cli

import "strings"

// examplesHelp renders the standard Examples: block returned by a command's
// Help method (kong's HelpProvider). Kong formats the string with go/doc, so
// the tab-indented example lines render as a preformatted block in --help
// output, keeping every invocation copy-pasteable.
func examplesHelp(lines ...string) string {
	var b strings.Builder
	b.WriteString("Examples:\n")
	for _, l := range lines {
		b.WriteString("\n\t" + l)
	}
	return b.String()
}
