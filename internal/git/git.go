package git

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/source"
)

// TagRef pairs a tag name with the commit it resolves to.
type TagRef struct {
	Name   string
	Commit string
}

// BranchRef pairs a branch name with its head commit.
type BranchRef struct {
	Name   string
	Commit string
}

// Runner is the git capability gskill needs. The system implementation shells
// out to the git binary; the interface lets it be swapped (e.g. go-git) later.
type Runner interface {
	// LsRemoteTags lists the repo's tags and the commits they point to.
	LsRemoteTags(ctx context.Context, url string) ([]TagRef, error)
	// LsRemoteHeads lists the repo's branches and their head commits.
	LsRemoteHeads(ctx context.Context, url string) ([]BranchRef, error)
	// ResolveRef resolves a branch, tag, or commit ref to an immutable commit SHA.
	ResolveRef(ctx context.Context, url, ref string) (string, error)
	// FetchCommit materializes the tree at commit into dest, without a .git dir.
	FetchCommit(ctx context.Context, url, commit, dest string) error
}

// shaRE matches a full 40-hex commit SHA.
var shaRE = regexp.MustCompile(`^[0-9a-f]{40}$`)

// commitRE matches a full or abbreviated hex object id; skills.toml accepts
// short commit pins.
var commitRE = regexp.MustCompile(`^[0-9a-fA-F]{4,64}$`)

// credentialRE matches userinfo in a URL for redaction (FR-046), and
// scpCredentialRE a user:password@ prefix of an scp-style address.
var (
	credentialRE    = regexp.MustCompile(`//[^/@\s]+@`)
	scpCredentialRE = regexp.MustCompile(`(^|[\s'"])[^\s'"/@:]+:[^\s'"/@]+@`)
)

// redact removes credentials and any occurrence of the raw URL's userinfo from
// text before it is surfaced in errors or logs (FR-046).
func redact(text string) string {
	text = credentialRE.ReplaceAllString(text, "//***@")
	return scpCredentialRE.ReplaceAllString(text, "${1}***@")
}

// runGit executes git with args (optionally in dir) and returns trimmed stdout,
// classifying failures into typed errors with credentials redacted. remote is
// the source URL the command talks to, or "" for a local command; failure
// messages name its host and repository. It shares the exec path with
// runGitProgress (nil sink), so the two can never diverge in classification
// or environment.
func runGit(ctx context.Context, remote, dir string, args ...string) (string, error) {
	return runGitProgress(ctx, remote, dir, nil, args...)
}

// privateReposHint points at the guide for setting up Git access.
const privateReposHint = "set up Git access: https://glapsfun.github.io/gskill/how-to/use-private-repositories/"

// failureClasses sort a failed command's stderr (forced to LC_ALL=C) into the
// failures a user acts on differently. The first class with a matching
// pattern wins, so the host-key class comes before the credential class whose
// SSH output it shares. Summaries never claim a repository does not exist: a
// provider hides a private repository from callers without access.
var failureClasses = []struct {
	code     errs.Code
	summary  func(host, repo string) string
	hint     string
	patterns []string
}{
	{
		code: errs.CodeAuth,
		summary: func(host, _ string) string {
			return "the SSH host key for " + host + " is unknown or has changed"
		},
		hint:     "check the host key with your provider, then add it to ~/.ssh/known_hosts",
		patterns: []string{"host key verification failed", "remote host identification has changed"},
	},
	{
		code: errs.CodeAuth,
		summary: func(host, repo string) string {
			return "credentials for " + host + " are missing, expired, or not allowed to read " + repo
		},
		hint: privateReposHint,
		patterns: []string{
			"terminal prompts disabled", "could not read username", "could not read password",
			"authentication failed", "invalid username or password", "http basic: access denied",
			"permission denied (publickey", "returned error: 401", "returned error: 403",
			"repository access denied",
		},
	},
	{
		code: errs.CodeSourceUnavailable,
		summary: func(_, repo string) string {
			return "repository " + repo + " not found or not accessible with the current credentials"
		},
		hint: privateReposHint,
		patterns: []string{
			"repository not found", "could not be found or you don't have permission",
			"does not appear to be a git repository", "returned error: 404",
			"you may not have access to this repository", "conq: repository does not exist",
		},
	},
	{
		code:    errs.CodeSourceUnavailable,
		summary: func(host, _ string) string { return "cannot reach " + host },
		patterns: []string{
			"could not resolve host", "connection timed out", "connection refused",
			"network is unreachable", "failed to connect", "operation timed out",
		},
	},
}

// finalError marks a classified failure that another fetch of the same
// remote cannot fix: missing credentials, an unknown host key, a repository
// that is not visible, an unreachable host, or cancellation.
type finalError struct{ err error }

func (e *finalError) Error() string { return e.err.Error() }
func (e *finalError) Unwrap() error { return e.err }

// isFinal reports a failure retrying cannot fix.
func isFinal(err error) bool {
	var f *finalError
	return errors.As(err, &f)
}

// classify maps a git failure to a typed error using its stderr. A cancelled
// context maps to the cancellation code whatever git printed.
func classify(ctx context.Context, err error, stderr, remote string) error {
	if errors.Is(ctx.Err(), context.Canceled) {
		return &finalError{errs.Wrap(errs.CodeCancelled, "git cancelled", ctx.Err())}
	}
	msg := redact(strings.TrimSpace(stderr))
	lower := strings.ToLower(msg)
	for _, c := range failureClasses {
		for _, p := range c.patterns {
			if strings.Contains(lower, p) {
				host, repo := remoteNames(remote)
				classified := errs.Wrap(c.code, c.summary(host, repo)+": "+msg, err)
				if c.hint != "" {
					classified = errs.WithHint(classified, c.hint)
				}
				return &finalError{classified}
			}
		}
	}
	return errs.Wrap(errs.CodeSourceUnavailable, "git: "+msg, err)
}

// remoteNames returns the host and repository a failure message names for
// remote: the host without user or port, and the repository the way progress
// lines show it. Unparseable or empty remotes get generic names.
func remoteNames(remote string) (host, repo string) {
	host, repo = "the remote host", "the repository"
	ref, err := source.Parse(remote)
	if err != nil || remote == "" {
		return host, repo
	}
	if ref.Type == source.TypeLocal {
		return ref.LocalPath, ref.LocalPath
	}
	if h := ref.Host(); h != "" {
		host = h
	}
	if d := ref.Display(); d != "" {
		repo = d
	}
	return host, repo
}

// IsFullSHA reports whether s is a full 40-hex commit SHA — the single
// definition shared by ref resolution and the wizard's typed-ref input.
func IsFullSHA(s string) bool { return shaRE.MatchString(s) }

// CheckCommit rejects a commit that is not a hex object id. Commits come from
// skills.toml and skills-lock.json, which whoever wrote the project controls,
// and are used both as git arguments and as clone-cache paths.
func CheckCommit(commit string) error {
	if !commitRE.MatchString(commit) {
		return fmt.Errorf("%w: commit %q is not a hex commit id", errs.ErrSourceUnavailable, commit)
	}
	return nil
}

// checkOperands rejects a URL or ref that git would parse as an option: git
// reads options after positional arguments too, and against a local-path
// remote --upload-pack runs a command.
func checkOperands(operands ...string) error {
	for _, s := range operands {
		if strings.HasPrefix(s, "-") {
			return fmt.Errorf("%w: %q starts with '-'", errs.ErrSourceUnavailable, s)
		}
	}
	return nil
}
