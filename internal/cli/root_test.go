package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/cli"
)

func run(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()

	var out, errb bytes.Buffer
	code = cli.Run(context.Background(), args, &out, &errb, nil)
	return out.String(), errb.String(), code
}

func TestRun_VersionGoesToStdoutOnly(t *testing.T) {
	t.Parallel()

	stdout, stderr, code := run(t, "version")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %q)", code, stderr)
	}
	if !strings.Contains(stdout, "gskill") {
		t.Errorf("stdout = %q, want it to contain %q", stdout, "gskill")
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty (diagnostics must not leak)", stderr)
	}
}

func TestRun_JSONEmitsSingleObjectOnStdout(t *testing.T) {
	t.Parallel()

	stdout, _, code := run(t, "version", "--json")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	var obj map[string]any
	if err := json.Unmarshal([]byte(stdout), &obj); err != nil {
		t.Fatalf("stdout is not a single JSON object: %v\nstdout: %q", err, stdout)
	}
	if _, ok := obj["version"]; !ok {
		t.Errorf("JSON object missing %q key: %v", "version", obj)
	}
}

func TestRun_UsageErrorExits2(t *testing.T) {
	t.Parallel()

	_, stderr, code := run(t, "--definitely-not-a-flag")
	if code != 2 {
		t.Errorf("exit code = %d, want 2 (usage)", code)
	}
	if stderr == "" {
		t.Error("stderr empty on usage error, want a diagnostic")
	}
}

func TestRun_BareInvocationRunsDefaultVersion(t *testing.T) {
	t.Parallel()

	stdout, _, code := run(t)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "gskill") {
		t.Errorf("bare invocation stdout = %q, want version line", stdout)
	}
}
