package cli_test

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/cli"
)

func TestParseArgsAcceptsShippedGrammar(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		{"add", "github.com/x/y", "--agent", "claude"},
		{"--json", "list"},
		{"project", "verify"},
		{"--no-interactive", "remove", "hello-gskill", "--force"},
		{"upgrade", "code-review", "--to", "2.1.0"},
		{"add", "--help"},
		{"--help"},
		{},
		{"project"},
	} {
		if err := cli.ParseArgs(args); err != nil {
			t.Errorf("ParseArgs(%q) = %v, want nil", args, err)
		}
	}
}

func TestParseArgsRejectsStaleInvocations(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		{"frobnicate"},
		{"list", "--no-such-flag"},
		{"info"},
		{"install", "--frozen"},
		{"--json", "project"},
	} {
		if err := cli.ParseArgs(args); err == nil {
			t.Errorf("ParseArgs(%q) = nil, want an error", args)
		}
	}
}

// TestParseArgsWritesNothing guards the "no side effects" contract: even a
// help request or a parse error must not print, because the docs checks call
// ParseArgs for every documented command.
//
//nolint:paralleltest // swaps os.Stdout and os.Stderr
func TestParseArgsWritesNothing(t *testing.T) {
	stdout, stderr := os.Stdout, os.Stderr
	t.Cleanup(func() { os.Stdout, os.Stderr = stdout, stderr })
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = w, w

	_ = cli.ParseArgs([]string{"--help"})
	_ = cli.ParseArgs([]string{"frobnicate"})
	_ = cli.ParseArgs([]string{"list"})

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if _, err := io.Copy(&out, r); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out.String()); got != "" {
		t.Errorf("ParseArgs wrote output: %q", got)
	}
}
