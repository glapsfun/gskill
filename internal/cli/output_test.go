package cli_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/cli"
)

func TestOutput_HumanResultGoesToStdout(t *testing.T) {
	t.Parallel()

	var out, errb bytes.Buffer
	o := cli.NewOutput(&out, &errb, cli.OutputOptions{})

	if err := o.Result("plain line", map[string]string{"ignored": "in-human-mode"}); err != nil {
		t.Fatalf("Result: %v", err)
	}
	if strings.TrimSpace(out.String()) != "plain line" {
		t.Errorf("stdout = %q, want %q", out.String(), "plain line")
	}
	if errb.Len() != 0 {
		t.Errorf("stderr = %q, want empty", errb.String())
	}
}

func TestOutput_JSONResultEncodesObject(t *testing.T) {
	t.Parallel()

	var out, errb bytes.Buffer
	o := cli.NewOutput(&out, &errb, cli.OutputOptions{JSON: true})

	if err := o.Result("human ignored", map[string]any{"count": 2}); err != nil {
		t.Fatalf("Result: %v", err)
	}
	var obj map[string]any
	if err := json.Unmarshal(out.Bytes(), &obj); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if count, ok := obj["count"].(float64); !ok || count != 2 {
		t.Errorf("count = %v, want 2", obj["count"])
	}
}

func TestOutput_DiagGoesToStderrAndRespectsQuiet(t *testing.T) {
	t.Parallel()

	var out, errb bytes.Buffer
	o := cli.NewOutput(&out, &errb, cli.OutputOptions{})
	o.Diag("warn: %s", "something")
	if !strings.Contains(errb.String(), "something") {
		t.Errorf("stderr = %q, want diagnostic", errb.String())
	}
	if out.Len() != 0 {
		t.Errorf("stdout = %q, want diagnostics kept off stdout", out.String())
	}

	var out2, errb2 bytes.Buffer
	quiet := cli.NewOutput(&out2, &errb2, cli.OutputOptions{Quiet: true})
	quiet.Diag("warn: %s", "suppressed")
	if errb2.Len() != 0 {
		t.Errorf("quiet stderr = %q, want empty", errb2.String())
	}
}

func TestOutput_InfoGoesToStderrAndRespectsQuiet(t *testing.T) {
	t.Parallel()

	var out, errb bytes.Buffer
	o := cli.NewOutput(&out, &errb, cli.OutputOptions{})
	o.Info("pruned: %s", "old-skill")
	if !strings.Contains(errb.String(), "pruned: old-skill") {
		t.Errorf("stderr = %q, want diagnostic", errb.String())
	}
	if out.Len() != 0 {
		t.Errorf("stdout = %q, want diagnostics kept off stdout", out.String())
	}

	var out2, errb2 bytes.Buffer
	quiet := cli.NewOutput(&out2, &errb2, cli.OutputOptions{Quiet: true})
	quiet.Info("pruned: %s", "suppressed")
	if errb2.Len() != 0 {
		t.Errorf("quiet stderr = %q, want empty", errb2.String())
	}
}

func TestOutput_WarnGoesToStderrAndRespectsQuiet(t *testing.T) {
	t.Parallel()

	var out, errb bytes.Buffer
	o := cli.NewOutput(&out, &errb, cli.OutputOptions{})
	o.Warn("warning: %s", "something")
	if !strings.Contains(errb.String(), "warning: something") {
		t.Errorf("stderr = %q, want diagnostic", errb.String())
	}
	if out.Len() != 0 {
		t.Errorf("stdout = %q, want diagnostics kept off stdout", out.String())
	}

	var out2, errb2 bytes.Buffer
	quiet := cli.NewOutput(&out2, &errb2, cli.OutputOptions{Quiet: true})
	quiet.Warn("warning: %s", "suppressed")
	if errb2.Len() != 0 {
		t.Errorf("quiet stderr = %q, want empty", errb2.String())
	}
}

func TestOutput_ErrDiagGoesToStderrAndRespectsQuiet(t *testing.T) {
	t.Parallel()

	var out, errb bytes.Buffer
	o := cli.NewOutput(&out, &errb, cli.OutputOptions{})
	o.ErrDiag("error: %s", "boom")
	if !strings.Contains(errb.String(), "error: boom") {
		t.Errorf("stderr = %q, want diagnostic", errb.String())
	}
	if out.Len() != 0 {
		t.Errorf("stdout = %q, want diagnostics kept off stdout", out.String())
	}

	var out2, errb2 bytes.Buffer
	quiet := cli.NewOutput(&out2, &errb2, cli.OutputOptions{Quiet: true})
	quiet.ErrDiag("error: %s", "suppressed")
	if errb2.Len() != 0 {
		t.Errorf("quiet stderr = %q, want empty", errb2.String())
	}
}

func TestOutput_HintGoesToStderrAndRespectsQuiet(t *testing.T) {
	t.Parallel()

	var out, errb bytes.Buffer
	o := cli.NewOutput(&out, &errb, cli.OutputOptions{})
	o.Hint("→ %s", "run 'gskill doctor'")
	if !strings.Contains(errb.String(), "→ run 'gskill doctor'") {
		t.Errorf("stderr = %q, want diagnostic", errb.String())
	}
	if out.Len() != 0 {
		t.Errorf("stdout = %q, want diagnostics kept off stdout", out.String())
	}

	var out2, errb2 bytes.Buffer
	quiet := cli.NewOutput(&out2, &errb2, cli.OutputOptions{Quiet: true})
	quiet.Hint("→ %s", "suppressed")
	if errb2.Len() != 0 {
		t.Errorf("quiet stderr = %q, want empty", errb2.String())
	}
}

func TestOutput_NonFileWriterIsNotInteractive(t *testing.T) {
	t.Parallel()

	var out, errb bytes.Buffer
	o := cli.NewOutput(&out, &errb, cli.OutputOptions{Interactive: true})
	// A bytes.Buffer is not a TTY, so interactive must be forced off.
	if o.Interactive() {
		t.Error("Interactive() = true for non-TTY writer, want false")
	}
}
