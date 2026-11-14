package integration_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// fakeSSH writes an ssh stand-in that prints message to stderr and exits 255,
// the way ssh reports a refused key or an unknown host.
func fakeSSH(t *testing.T, message string) string {
	t.Helper()
	script := filepath.Join(t.TempDir(), "ssh")
	body := "#!/bin/sh\nprintf '%s\\n' \"" + message + "\" >&2\nexit 255\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil { //nolint:gosec // an executable test stub
		t.Fatal(err)
	}
	return script
}

// TestGitFailures_Classified: each failure class has its own exit code and a
// message that names the host and repository and never says the repository
// does not exist (spec 035 FR-020, SC-003).
func TestGitFailures_Classified(t *testing.T) {
	tests := []struct {
		name, ssh, source string
		code              int
		want              string
	}{
		{
			"key refused", "git@gitlab.com: Permission denied (publickey).", "git@gitlab.com:acme/skills.git", 11,
			"credentials for gitlab.com are missing, expired, or not allowed to read gitlab.com/acme/skills",
		},
		{
			"host key", "Host key verification failed.", "git@bitbucket.org:acme/skills.git", 11,
			"the SSH host key for bitbucket.org is unknown or has changed",
		},
		{
			"not visible", "ERROR: The project you were looking for could not be found or you don't have permission to view it.",
			"git@gitlab.com:acme/platform/skills.git", 5,
			"repository gitlab.com/acme/platform/skills not found or not accessible with the current credentials",
		},
		{"dns", "", "https://nope.invalid/acme/skills.git", 5, "cannot reach nope.invalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hermeticGitConfig(t, "", nil)
			for _, proxy := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "ALL_PROXY", "all_proxy"} {
				t.Setenv(proxy, "")
			}
			if tt.ssh != "" {
				t.Setenv("GIT_SSH_COMMAND", fakeSSH(t, tt.ssh))
			}
			start := time.Now()
			stdout, stderr, code := runGskillWithApp(t, noAPIApp(t), newProject(t),
				"--no-interactive", "add", tt.source, "--agent", "claude")
			if elapsed := time.Since(start); elapsed > 30*time.Second {
				t.Errorf("failure took %s, want under 30s", elapsed)
			}
			if code != tt.code {
				t.Errorf("exit %d, want %d\nstdout: %s\nstderr: %s", code, tt.code, stdout, stderr)
			}
			if !strings.Contains(stderr, tt.want) {
				t.Errorf("stderr lacks %q:\n%s", tt.want, stderr)
			}
			summary, _, _ := strings.Cut(stderr, ": fatal")
			if strings.Contains(summary, "does not exist") {
				t.Errorf("message claims the repository does not exist:\n%s", stderr)
			}
		})
	}
}

// TestRedaction_SourcesEchoedWithoutSecrets: a source carrying a password is
// refused, and every message or listing that echoes a source, including a
// legacy lock entry that recorded one, hides the secret (spec 035 FR-004,
// FR-016).
func TestRedaction_SourcesEchoedWithoutSecrets(t *testing.T) { //nolint:paralleltest // sets GIT_CONFIG_*
	secretURL := "https://u:" + plantedSecret + "@gitlab.com/acme/skills.git"

	proj := newProject(t)
	var outputs []string
	for _, args := range [][]string{
		{"search", "demo", "--source", secretURL},
		{"--json", "search", "demo", "--source", secretURL},
		{"add", secretURL, "--agent", "claude"},
		{"--json", "add", secretURL, "--agent", "claude"},
	} {
		stdout, stderr, _ := runGskill(t, proj, append([]string{"--no-interactive"}, args...)...)
		outputs = append(outputs, stdout, stderr)
	}
	if _, stderr, code := runGskill(t, proj, "--no-interactive", "add", secretURL, "--agent", "claude"); code != 2 {
		t.Errorf("add with a password exit %d, want 2\n%s", code, stderr)
	}

	// A lock written by an earlier release kept the user's spelling, secret
	// and all, in its source fields while the clone address stayed clean.
	fixture := providerFixture(t)
	rewriteRemotes(t, fixture, "https://gitlab.com/acme/skills.git")
	legacy := newProject(t)
	runOK(t, newApp(t), legacy, "add", "https://gitlab.com/acme/skills.git", "--skill", "demo", "--ref", "v1.0.0", "--agent", "claude")
	original := regexp.MustCompile(`("(?:source|sourceOriginal)": )"https://gitlab\.com/acme/skills\.git"`)
	lockPath := filepath.Join(legacy, "skills-lock.json")
	patched := original.ReplaceAll(readFile(t, lockPath), []byte(`${1}"`+secretURL+`"`))
	if !strings.Contains(string(patched), secretURL) {
		t.Fatal("could not plant the legacy secret in the lockfile")
	}
	if err := os.WriteFile(lockPath, patched, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"list"},
		{"--json", "list"},
		{"info", "demo"},
		{"--json", "info", "demo"},
		{"install", "--frozen-lockfile"},
		{"install"},
		{"update"},
	} {
		stdout, stderr, _ := runGskill(t, legacy, append([]string{"--no-interactive"}, args...)...)
		outputs = append(outputs, stdout, stderr)
	}
	assertNoSecret(t, outputs)
}
