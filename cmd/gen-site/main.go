// Command gen-site turns docs/ into the content tree the documentation site in
// website/ builds from, and keeps the README's install section in sync with
// docs/installation.md. Run it from anywhere inside the repository:
//
//	go run ./cmd/gen-site          # write the site content and the README region
//	go run ./cmd/gen-site -check   # fail if the committed README region is stale
//
// The site content is gitignored and rebuilt on every run; only README.md is a
// committed output.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/glapsfun/gskill/internal/docsite"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "gen-site:", err)
		os.Exit(1)
	}
}

// run parses args, builds the plan from the repository containing the working
// directory, and either writes it or checks it.
func run(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("gen-site", flag.ContinueOnError)
	checkOnly := fs.Bool("check", false, "report drift in committed outputs instead of writing")
	if err := fs.Parse(args); err != nil {
		return err
	}

	root, err := docsite.FindRepoRoot(".")
	if err != nil {
		return err
	}
	plan, err := docsite.Build(docsite.DefaultOptions(root))
	if err != nil {
		return err
	}

	if *checkOnly {
		diff, err := plan.Check(root)
		if err != nil {
			return err
		}
		if len(diff) > 0 {
			for _, d := range diff {
				fmt.Fprintln(os.Stderr, "gen-site: stale:", d)
			}
			return fmt.Errorf("%d committed output(s) out of date; run: go run ./cmd/gen-site", len(diff))
		}
		return nil
	}

	written, err := plan.Write(root)
	if err != nil {
		return err
	}
	for _, rel := range written {
		if _, err := fmt.Fprintln(stdout, "wrote", rel); err != nil {
			return err
		}
	}
	return nil
}
