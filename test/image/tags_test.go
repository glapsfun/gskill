package image

import (
	"bytes"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestImageTags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		args     []string
		wantOut  string
		wantCode int
	}{
		{name: "stable at the highest", args: []string{"v0.9.0", "v0.9.0"}, wantOut: "0.9.0,latest"},
		{name: "stable backport", args: []string{"v0.8.1", "v0.9.0"}, wantOut: "0.8.1"},
		{name: "stable above the highest", args: []string{"v1.0.0", "v0.9.0"}, wantOut: "1.0.0,latest"},
		{name: "stable above an older highest", args: []string{"v0.9.0", "v0.8.0"}, wantOut: "0.9.0,latest"},
		{name: "prerelease at the highest", args: []string{"v0.9.0-rc.1", "v0.9.0-rc.1"}, wantOut: "0.9.0-rc.1,next"},
		{name: "prerelease above an older one", args: []string{"v0.9.0-rc.2", "v0.9.0-rc.1"}, wantOut: "0.9.0-rc.2,next"},
		{name: "rc.10 above rc.9", args: []string{"v0.9.0-rc.10", "v0.9.0-rc.9"}, wantOut: "0.9.0-rc.10,next"},
		{name: "re-run of an older prerelease", args: []string{"v0.9.0-rc.1", "v0.9.0-rc.2"}, wantOut: "0.9.0-rc.1"},
		{name: "prerelease of a major", args: []string{"v1.0.0-rc.1", "v0.9.0-rc.3"}, wantOut: "1.0.0-rc.1,next"},
		{name: "no v prefix", args: []string{"0.9.0", "v0.9.0"}, wantCode: 2},
		{name: "two components", args: []string{"v0.9", "v0.9.0"}, wantCode: 2},
		{name: "not a version", args: []string{"vX", "v0.9.0"}, wantCode: 2},
		{name: "invalid highest", args: []string{"v0.9.0", "bogus"}, wantCode: 2},
		{name: "prerelease against a stable highest", args: []string{"v0.9.0-rc.1", "v0.8.0"}, wantCode: 2},
		{name: "stable against a prerelease highest", args: []string{"v0.9.0", "v0.9.0-rc.1"}, wantCode: 2},
		{name: "too many arguments", args: []string{"v0.9.0", "v0.9.0", "v0.9.0"}, wantCode: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cmd := exec.CommandContext(t.Context(), "bash", append([]string{"scripts/image.sh", "tags"}, tt.args...)...) //nolint:gosec // the test table's own arguments
			cmd.Dir = repoRoot
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			code := 0
			if err := cmd.Run(); err != nil {
				var exitErr *exec.ExitError
				if !errors.As(err, &exitErr) {
					t.Fatalf("run image.sh tags: %v", err)
				}
				code = exitErr.ExitCode()
			}
			if code != tt.wantCode {
				t.Fatalf("exit code = %d, want %d (stderr: %q)", code, tt.wantCode, stderr.String())
			}
			if got := strings.TrimSpace(stdout.String()); got != tt.wantOut {
				t.Errorf("tags %v = %q, want %q", tt.args, got, tt.wantOut)
			}
		})
	}
}
