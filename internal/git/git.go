package git

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/glapsfun/gskill/internal/errs"
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

// credentialRE matches userinfo in a URL for redaction (FR-046).
var credentialRE = regexp.MustCompile(`//[^/@\s]+@`)

// redact removes credentials and any occurrence of the raw URL's userinfo from
// text before it is surfaced in errors or logs (FR-046).
func redact(text string) string {
	return credentialRE.ReplaceAllString(text, "//***@")
}

// runGit executes git with args (optionally in dir) and returns trimmed stdout,
// classifying failures into typed errors with credentials redacted. It shares
// the exec path with runGitProgress (nil sink), so the two can never diverge
// in classification or environment.
func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	return runGitProgress(ctx, dir, nil, args...)
}

// classify maps a git failure to a typed error using its stderr.
func classify(err error, stderr string) error {
	msg := redact(strings.TrimSpace(stderr))
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "authentication failed"),
		strings.Contains(lower, "could not read username"),
		strings.Contains(lower, "permission denied (publickey)"),
		strings.Contains(lower, "invalid username or password"):
		return errs.Wrap(errs.CodeAuth, "git authentication failed: "+msg, err)
	default:
		return errs.Wrap(errs.CodeSourceUnavailable, "git: "+msg, err)
	}
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
