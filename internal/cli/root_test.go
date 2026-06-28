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

func TestRun_BareInvocationPrintsHelp(t *testing.T) {
	t.Parallel()

	stdout, stderr, code := run(t)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %q)", code, stderr)
	}
	if !strings.Contains(stdout, "Usage:") {
		t.Errorf("bare invocation stdout = %q, want it to contain the usage line", stdout)
	}
	if !strings.Contains(stdout, "Reproducible package manager for agentic AI skills.") {
		t.Errorf("bare invocation stdout = %q, want it to contain the project description", stdout)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty (help is a result, not a diagnostic)", stderr)
	}
}

func TestRun_HelpFlagsPrintHelp(t *testing.T) {
	t.Parallel()

	for _, flag := range []string{"--help", "-h"} {
		t.Run(flag, func(t *testing.T) {
			t.Parallel()

			stdout, stderr, code := run(t, flag)
			if code != 0 {
				t.Fatalf("%s exit code = %d, want 0 (stderr: %q)", flag, code, stderr)
			}
			if !strings.Contains(stdout, "Usage:") {
				t.Errorf("%s stdout = %q, want it to contain the usage line", flag, stdout)
			}
			if stderr != "" {
				t.Errorf("%s stderr = %q, want empty", flag, stderr)
			}
		})
	}
}

func TestRun_HelpInvocationsByteIdentical(t *testing.T) {
	t.Parallel()

	bare, _, _ := run(t)
	long, _, _ := run(t, "--help")
	short, _, _ := run(t, "-h")

	if bare != long {
		t.Errorf("bare invocation help differs from --help:\nbare:  %q\n--help: %q", bare, long)
	}
	if long != short {
		t.Errorf("--help differs from -h:\n--help: %q\n-h:     %q", long, short)
	}
}

func TestRun_HelpListsMainCommandsAndFlags(t *testing.T) {
	t.Parallel()

	stdout, _, _ := run(t)

	for _, cmd := range []string{"install", "add", "verify", "sync", "list"} {
		if !strings.Contains(stdout, cmd) {
			t.Errorf("help stdout missing main command %q\nstdout: %q", cmd, stdout)
		}
	}
	for _, flag := range []string{"--json", "--offline"} {
		if !strings.Contains(stdout, flag) {
			t.Errorf("help stdout missing global flag %q\nstdout: %q", flag, stdout)
		}
	}
}

func TestRun_UnknownCommandExitsNonZero(t *testing.T) {
	t.Parallel()

	_, stderr, code := run(t, "nonsense-command")
	if code == 0 {
		t.Errorf("exit code = 0, want non-zero for an unknown command")
	}
	if stderr == "" {
		t.Error("stderr empty on unknown command, want a diagnostic")
	}
}
