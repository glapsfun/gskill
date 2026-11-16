package git

import (
	"context"
	"errors"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/errs"
)

func TestNonInteractiveEnv_AddsDefaults(t *testing.T) {
	t.Parallel()

	got := nonInteractiveEnv([]string{"HOME=/h"}, func() bool { return false })
	for _, want := range []string{"GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never", "GIT_SSH_COMMAND=ssh -o BatchMode=yes"} {
		if !slices.Contains(got, want) {
			t.Errorf("env %v lacks %q", got, want)
		}
	}
}

func TestNonInteractiveEnv_KeepsUserChoices(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		env    []string
		sshSet bool
		absent string
	}{
		{"prompt enabled", []string{"GIT_TERMINAL_PROMPT=1"}, false, "GIT_TERMINAL_PROMPT=0"},
		{"prompt set empty", []string{"GIT_TERMINAL_PROMPT="}, false, "GIT_TERMINAL_PROMPT=0"},
		{"gcm interactive", []string{"GCM_INTERACTIVE=always"}, false, "GCM_INTERACTIVE=never"},
		{"ssh command", []string{"GIT_SSH_COMMAND=ssh -i key"}, false, "GIT_SSH_COMMAND=ssh -o BatchMode=yes"},
		{"ssh command empty", []string{"GIT_SSH_COMMAND="}, false, "GIT_SSH_COMMAND=ssh -o BatchMode=yes"},
		{"ssh program", []string{"GIT_SSH=/usr/bin/ssh"}, false, "GIT_SSH_COMMAND=ssh -o BatchMode=yes"},
		{"core.sshCommand", nil, true, "GIT_SSH_COMMAND=ssh -o BatchMode=yes"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := nonInteractiveEnv(append([]string{}, tt.env...), func() bool { return tt.sshSet })
			if slices.Contains(got, tt.absent) {
				t.Errorf("env %v gained %q over the user's setting", got, tt.absent)
			}
			for _, kv := range tt.env {
				if !slices.Contains(got, kv) {
					t.Errorf("env %v lost the user's %q", got, kv)
				}
			}
		})
	}
}

func TestNonInteractiveEnv_SkipsLookupWhenSSHIsSet(t *testing.T) {
	t.Parallel()

	nonInteractiveEnv([]string{"GIT_SSH_COMMAND=ssh"}, func() bool {
		t.Error("core.sshCommand lookup ran although GIT_SSH_COMMAND is set")
		return false
	})
}

// TestNonInteractiveEnv_NeverWeakensVerification: the defaults turn prompts
// into failures; they never turn host-key or TLS checks off (FR-019).
func TestNonInteractiveEnv_NeverWeakensVerification(t *testing.T) {
	t.Parallel()

	for _, kv := range nonInteractiveEnv(nil, func() bool { return false }) {
		if strings.Contains(kv, "StrictHostKeyChecking") || strings.HasPrefix(kv, "GIT_SSL_NO_VERIFY") {
			t.Errorf("default %q weakens verification", kv)
		}
	}
}

func TestSSHCommandConfigured_ReadsGitConfig(t *testing.T) {
	// Not parallel: t.Setenv. The cached process-wide lookup is not used.
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "core.sshCommand")
	t.Setenv("GIT_CONFIG_VALUE_0", "ssh -i /k")
	out, err := exec.CommandContext(t.Context(), "git", "config", "--get", "core.sshCommand").Output()
	if err != nil || strings.TrimSpace(string(out)) != "ssh -i /k" {
		t.Fatalf("git config did not see core.sshCommand through GIT_CONFIG_*: %q, %v", out, err)
	}
}

func TestClassify_FailureClasses(t *testing.T) {
	t.Parallel()

	const gitlab = "git@gitlab.com:acme/platform/skills.git"
	const github = "https://ci@github.com/acme/skills.git"
	tests := []struct {
		name, stderr, remote string
		code                 errs.Code
		summary              string
	}{
		{
			"https no credentials", "fatal: could not read Username for 'https://github.com': terminal prompts disabled", github,
			errs.CodeAuth, "credentials for github.com are missing, expired, or not allowed to read acme/skills",
		},
		{
			"gitlab basic denied", "remote: HTTP Basic: Access denied\nfatal: Authentication failed for 'https://gitlab.com/acme/x.git/'", "https://gitlab.com/acme/x.git",
			errs.CodeAuth, "credentials for gitlab.com are missing",
		},
		{
			"ssh key rejected", "git@gitlab.com: Permission denied (publickey).\nfatal: Could not read from remote repository.", gitlab,
			errs.CodeAuth, "credentials for gitlab.com are missing, expired, or not allowed to read gitlab.com/acme/platform/skills",
		},
		{
			"http 403", "fatal: unable to access 'https://x/': The requested URL returned error: 403", github,
			errs.CodeAuth, "credentials for github.com",
		},
		{
			"bitbucket ssh denied", "repository access denied.\nfatal: Could not read from remote repository.", "git@bitbucket.org:acme/skills.git",
			errs.CodeAuth, "credentials for bitbucket.org",
		},
		{
			"github not found", "remote: Repository not found.\nfatal: repository 'https://github.com/acme/skills.git/' not found", github,
			errs.CodeSourceUnavailable, "repository acme/skills not found or not accessible with the current credentials",
		},
		{
			"gitlab ssh not found", "ERROR: The project you were looking for could not be found or you don't have permission to view it.", gitlab,
			errs.CodeSourceUnavailable, "repository gitlab.com/acme/platform/skills not found or not accessible",
		},
		{
			"bitbucket ssh not found", "conq: repository does not exist.", "git@bitbucket.org:acme/skills.git",
			errs.CodeSourceUnavailable, "repository bitbucket.org/acme/skills not found or not accessible",
		},
		{
			"http 404", "fatal: unable to access 'x': The requested URL returned error: 404", github,
			errs.CodeSourceUnavailable, "repository acme/skills not found or not accessible",
		},
		{
			"host key", "Host key verification failed.\nfatal: Could not read from remote repository.", gitlab,
			errs.CodeAuth, "the SSH host key for gitlab.com is unknown or has changed",
		},
		{
			"host key changed", "@ WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED! @", "ssh://git@git.example.com:2222/a/b.git",
			errs.CodeAuth, "the SSH host key for git.example.com is unknown or has changed",
		},
		{
			"dns", "fatal: unable to access 'https://nope.invalid/a/b.git/': Could not resolve host: nope.invalid", "https://nope.invalid/a/b.git",
			errs.CodeSourceUnavailable, "cannot reach nope.invalid",
		},
		{
			"ssh dns", "ssh: Could not resolve hostname nope.invalid: Name or service not known", "git@nope.invalid:a/b.git",
			errs.CodeSourceUnavailable, "cannot reach nope.invalid",
		},
		{
			"refused", "ssh: connect to host h port 22: Connection refused", "git@h:a/b.git",
			errs.CodeSourceUnavailable, "cannot reach h",
		},
		{"other", "fatal: something else", github, errs.CodeSourceUnavailable, "git: fatal: something else"},
		{"no remote", "fatal: Authentication failed", "", errs.CodeAuth, "credentials for the remote host are missing, expired, or not allowed to read the repository"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := classify(context.Background(), errors.New("exit status 128"), tt.stderr, tt.remote)
			if got := errs.ExitCode(err); got != int(tt.code) {
				t.Errorf("exit code = %d, want %d (%v)", got, tt.code, err)
			}
			if !strings.HasPrefix(err.Error(), tt.summary) {
				t.Errorf("error = %q, want it to start with %q", err, tt.summary)
			}
			summary, _, _ := strings.Cut(err.Error(), ": ")
			if strings.Contains(summary, "does not exist") {
				t.Errorf("summary %q claims the repository does not exist", summary)
			}
			if strings.Contains(err.Error(), "ci@") {
				t.Errorf("error %q names the URL user", err)
			}
		})
	}
}

func TestClassify_HintsPointAtTheGuide(t *testing.T) {
	t.Parallel()

	err := classify(context.Background(), errors.New("exit 128"), "terminal prompts disabled", "https://github.com/a/b.git")
	if hint := errs.HintOf(err); !strings.Contains(hint, "https://glapsfun.github.io/gskill/how-to/use-private-repositories/") {
		t.Errorf("hint = %q, want the private repositories guide", hint)
	}
	err = classify(context.Background(), errors.New("exit 128"), "Could not resolve host: x", "https://x/a/b.git")
	if hint := errs.HintOf(err); hint != "" {
		t.Errorf("network failure hint = %q, want none", hint)
	}
}

func TestClassify_CancelledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := classify(ctx, errors.New("signal: killed"), "fatal: Authentication failed", "https://github.com/a/b.git")
	if errs.ExitCode(err) != int(errs.CodeCancelled) || !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled classify = %v (exit %d), want the cancellation code", err, errs.ExitCode(err))
	}
}

func TestRedact_SCPPassword(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"fatal: 'deploy:hunter2@git.example.com:team/x.git' failed": "fatal: '***@git.example.com:team/x.git' failed",
		"ssh://deploy:hunter2@git.example.com/team/x.git":           "ssh://***@git.example.com/team/x.git",
		"git@github.com:acme/x.git":                                 "git@github.com:acme/x.git",
		"error: Could not resolve host: x":                          "error: Could not resolve host: x",
	}
	for in, want := range tests {
		if got := redact(in); got != want {
			t.Errorf("redact(%q) = %q, want %q", in, got, want)
		}
	}
}

// Classified failures are final: FetchCommit must not retry them with a full
// fetch. Only an unrecognized failure stays retryable.
func TestClassify_FinalFailures(t *testing.T) {
	t.Parallel()

	for stderr, final := range map[string]bool{
		"fatal: could not read Username for 'https://x': terminal prompts disabled": true,
		"Host key verification failed.":                                             true,
		"remote: Repository not found.":                                             true,
		"Could not resolve host: x":                                                 true,
		"error: Server does not allow request for unadvertised object abc":          false,
	} {
		if got := isFinal(classify(context.Background(), errors.New("exit 128"), stderr, "https://github.com/a/b.git")); got != final {
			t.Errorf("isFinal(%q) = %v, want %v", stderr, got, final)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !isFinal(classify(ctx, errors.New("killed"), "", "")) {
		t.Error("a cancelled fetch is retryable, want final")
	}
}
