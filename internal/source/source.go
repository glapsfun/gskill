package source

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
)

// ErrInvalidSource is returned when a source argument cannot be parsed.
var ErrInvalidSource = errors.New("invalid source")

// defaultGitHost is the host assumed for bare owner/repo shorthands.
const defaultGitHost = "github.com"

// knownGitHosts are hosts treated as git sources even without a .git suffix.
var knownGitHosts = map[string]bool{
	"github.com":    true,
	"gitlab.com":    true,
	"bitbucket.org": true,
	"codeberg.org":  true,
}

// Ref is a parsed, normalized source reference.
type Ref struct {
	Type      Type
	Original  string
	URL       string
	Owner     string
	Repo      string
	Path      string
	LocalPath string
}

// Identity returns the canonical identity string for the reference: the local
// path for local sources, or host/owner/repo[/path] for git sources.
func (s Ref) Identity() string {
	if s.Type == TypeLocal {
		return s.LocalPath
	}
	host := hostFromURL(s.URL)
	parts := []string{host, s.Owner, s.Repo}
	id := strings.Join(parts, "/")
	if s.Path != "" {
		id += "/" + s.Path
	}
	return id
}

// Display returns a short human name for progress lines: owner/repo when
// known, else the repo name (local-promoted git repos have no owner), else
// the canonical identity (never the raw URL, which may carry credentials).
// Identity's empty segments are trimmed so a bare-URL ref renders as its
// host, not "host//".
func (s Ref) Display() string {
	if s.Owner != "" && s.Repo != "" {
		return s.Owner + "/" + s.Repo
	}
	if s.Repo != "" {
		return s.Repo
	}
	return strings.Trim(strings.ReplaceAll(s.Identity(), "//", "/"), "/")
}

// Parse classifies and normalizes a raw source argument.
func Parse(raw string) (Ref, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return Ref{}, fmt.Errorf("%w: empty source", ErrInvalidSource)
	}

	switch {
	case isLocalPath(trimmed):
		return parseLocal(raw, trimmed), nil
	case strings.HasPrefix(trimmed, "git@"):
		return parseSSH(raw, trimmed)
	case hasScheme(trimmed):
		return parseURL(raw, trimmed)
	default:
		return parseShorthand(raw, trimmed)
	}
}

// isLocalPath reports whether raw is a filesystem path rather than a remote.
func isLocalPath(raw string) bool {
	switch {
	case strings.HasPrefix(raw, "./"), strings.HasPrefix(raw, "../"):
		return true
	case strings.HasPrefix(raw, "/"), strings.HasPrefix(raw, "~"):
		return true
	case strings.HasPrefix(raw, "file://"):
		return true
	default:
		return false
	}
}

func parseLocal(original, trimmed string) Ref {
	p := strings.TrimPrefix(trimmed, "file://")
	return Ref{
		Type:      TypeLocal,
		Original:  original,
		LocalPath: filepath.Clean(p),
	}
}

func parseSSH(original, trimmed string) (Ref, error) {
	// git@host:owner/repo(.git)?
	rest := strings.TrimPrefix(trimmed, "git@")
	host, ownerRepo, ok := strings.Cut(rest, ":")
	if !ok {
		return Ref{}, fmt.Errorf("%w: malformed ssh source %q", ErrInvalidSource, original)
	}
	owner, repo, _, err := splitOwnerRepoPath(ownerRepo)
	if err != nil {
		return Ref{}, fmt.Errorf("%w: %w", ErrInvalidSource, err)
	}
	return Ref{
		Type:     TypeGit,
		Original: original,
		URL:      fmt.Sprintf("git@%s:%s/%s.git", host, owner, repo),
		Owner:    owner,
		Repo:     repo,
	}, nil
}

func parseURL(original, trimmed string) (Ref, error) {
	u, err := url.Parse(trimmed)
	if err != nil {
		return Ref{}, fmt.Errorf("%w: %w", ErrInvalidSource, err)
	}
	segs := splitPath(u.Path)
	gitlike := knownGitHosts[u.Host] || strings.HasSuffix(u.Path, ".git")
	if !gitlike || len(segs) < 2 {
		return Ref{Type: TypeURL, Original: original, URL: trimmed}, nil
	}

	owner := segs[0]
	repo := strings.TrimSuffix(segs[1], ".git")
	inRepo := strings.Join(segs[2:], "/")
	return Ref{
		Type:     TypeGit,
		Original: original,
		URL:      fmt.Sprintf("https://%s/%s/%s.git", u.Host, owner, repo),
		Owner:    owner,
		Repo:     repo,
		Path:     inRepo,
	}, nil
}

func parseShorthand(original, trimmed string) (Ref, error) {
	segs := splitPath(trimmed)
	if len(segs) < 2 {
		return Ref{}, fmt.Errorf("%w: %q is not owner/repo", ErrInvalidSource, original)
	}

	host := defaultGitHost
	if strings.Contains(segs[0], ".") {
		host = segs[0]
		segs = segs[1:]
	}
	if len(segs) < 2 {
		return Ref{}, fmt.Errorf("%w: %q is missing owner or repo", ErrInvalidSource, original)
	}

	owner := segs[0]
	repo := strings.TrimSuffix(segs[1], ".git")
	inRepo := strings.Join(segs[2:], "/")
	return Ref{
		Type:     TypeGit,
		Original: original,
		URL:      fmt.Sprintf("https://%s/%s/%s.git", host, owner, repo),
		Owner:    owner,
		Repo:     repo,
		Path:     inRepo,
	}, nil
}

// splitOwnerRepoPath splits an "owner/repo[/path]" string.
func splitOwnerRepoPath(s string) (owner, repo, inRepo string, err error) {
	segs := splitPath(s)
	if len(segs) < 2 {
		return "", "", "", fmt.Errorf("expected owner/repo, got %q", s)
	}
	return segs[0], strings.TrimSuffix(segs[1], ".git"), strings.Join(segs[2:], "/"), nil
}

// splitPath splits a slash path into non-empty segments.
func splitPath(p string) []string {
	var out []string
	for _, seg := range strings.Split(strings.Trim(p, "/"), "/") {
		if seg != "" {
			out = append(out, seg)
		}
	}
	return out
}

// hasScheme reports whether raw begins with a URL scheme like https://.
func hasScheme(raw string) bool {
	i := strings.Index(raw, "://")
	return i > 0
}

// hostFromURL extracts the host from a normalized git URL (https or ssh form).
func hostFromURL(u string) string {
	if strings.HasPrefix(u, "git@") {
		host, _, _ := strings.Cut(strings.TrimPrefix(u, "git@"), ":")
		return host
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return ""
	}
	return parsed.Host
}
