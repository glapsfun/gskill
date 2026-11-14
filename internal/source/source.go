package source

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/glapsfun/gskill/internal/errs"
)

// ErrInvalidSource is returned when a source argument cannot be parsed.
var ErrInvalidSource = errors.New("invalid source")

// ErrAmbiguousSource is returned for a GitLab path that could name a
// repository in a nested group or a folder inside a shallower repository.
// It is a usage error: the user fixes it by marking the boundary.
var ErrAmbiguousSource = errors.New("ambiguous source")

// ErrCredentialInSource is returned for a source address that carries a
// password or token, which would otherwise be written to skills.toml and the
// lockfile.
var ErrCredentialInSource = errors.New("credential in source")

// defaultGitHost is the host assumed for bare owner/repo shorthands.
const defaultGitHost = "github.com"

// knownGitHosts are hosts treated as git sources even without a .git suffix.
var knownGitHosts = map[string]bool{
	"github.com":    true,
	"gitlab.com":    true,
	"bitbucket.org": true,
	"codeberg.org":  true,
}

// scpRE matches an scp-style SSH address, user@host:path. The user and host
// exclude '/' and ':', so a URL with a scheme never matches.
var scpRE = regexp.MustCompile(`^([^@/:\s]+)@([^@/:\s]+):(.*)$`)

// tokenPrefixes are the leading characters of provider tokens. A token used
// as the user name of an HTTPS address (https://ghp_x@github.com/...) is as
// secret as a password.
var tokenPrefixes = []string{
	"ghp_", "gho_", "ghu_", "ghs_", "ghr_", "github_pat_",
	"glpat-", "gldt-", "glcbt-",
	"ATBB", "ATCTT", "ATATT",
}

// Ref is a parsed, normalized source reference. For git sources Owner holds
// the whole namespace (acme/platform on a nested GitLab group) and Repo the
// last segment, so Owner/Repo is always the full repository path.
type Ref struct {
	Type      Type
	Original  string
	URL       string
	Owner     string
	Repo      string
	Path      string
	LocalPath string
}

// Host returns the host name of a git source's URL, without user or port, or
// "" when the URL has no host (a promoted local repository). A cloud
// provider's alternate SSH host is reported as its canonical host, so the
// repository's identity does not depend on which endpoint the address names.
func (s Ref) Host() string {
	host := hostFromURL(s.URL)
	if host == "" {
		return ""
	}
	return CanonicalHost(host)
}

// Provider returns the hosting service of a git source.
func (s Ref) Provider() Provider {
	return ProviderOf(s.Host())
}

// Identity returns the canonical identity string for the reference: the local
// path for local sources, or host/owner/repo[/path] for git sources.
func (s Ref) Identity() string {
	if s.Type == TypeLocal {
		return s.LocalPath
	}
	parts := []string{s.Host(), s.Owner, s.Repo}
	id := strings.Join(parts, "/")
	if s.Path != "" {
		id += "/" + s.Path
	}
	return id
}

// Display returns a short human name for progress lines: the repository path
// the way a user would write its shortest source (owner/repo on GitHub,
// host/namespace/repo elsewhere), else the repo name (local-promoted git
// repos have no owner), else the canonical identity (never the raw URL,
// which may carry credentials). For git identities the empty host/owner
// segments are trimmed so a bare-URL ref renders as its host, not "host//";
// local paths pass through untouched.
func (s Ref) Display() string {
	if s.Owner != "" && s.Repo != "" {
		if host := s.Host(); host != "" && host != defaultGitHost {
			return host + "/" + s.Owner + "/" + s.Repo
		}
		return s.Owner + "/" + s.Repo
	}
	if s.Repo != "" {
		return s.Repo
	}
	if s.Type == TypeLocal {
		return s.LocalPath
	}
	return strings.Trim(strings.ReplaceAll(s.Identity(), "//", "/"), "/")
}

// Parse classifies and normalizes a raw source argument.
func Parse(raw string) (Ref, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return Ref{}, fmt.Errorf("%w: empty source", ErrInvalidSource)
	}
	if isLocalPath(trimmed) {
		return parseLocal(raw, trimmed), nil
	}
	if hasCredential(trimmed) {
		return Ref{}, errs.Wrap(errs.CodeUsage, "", fmt.Errorf(
			"%w: the source address contains a password or token; remove it and let a Git credential helper or an SSH key supply access",
			ErrCredentialInSource))
	}

	switch {
	case scpRE.MatchString(trimmed):
		return parseSCP(raw, trimmed)
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

// parseSCP parses user@host:path, keeping the user and host as written so an
// SSH config alias keeps working.
func parseSCP(original, trimmed string) (Ref, error) {
	m := scpRE.FindStringSubmatch(trimmed)
	user, host, p := m[1], m[2], m[3]
	repo, folder, err := splitRepository(original, host, p)
	if err != nil {
		return Ref{}, err
	}
	// A leading slash makes the path absolute on the server rather than
	// relative to the SSH user's home directory, so it must survive.
	abs := ""
	if strings.HasPrefix(p, "/") {
		abs = "/"
	}
	ref := gitRef(original, repo, folder)
	ref.URL = fmt.Sprintf("%s@%s:%s%s.git", user, host, abs, strings.Join(repo, "/"))
	return ref, nil
}

// parseURL parses a URL with a scheme. ssh:// keeps its user and port; every
// other scheme becomes https://, keeping a password-free user name and port.
func parseURL(original, trimmed string) (Ref, error) {
	u, err := url.Parse(trimmed)
	if err != nil {
		return Ref{}, fmt.Errorf("%w: %w", ErrInvalidSource, err)
	}
	ssh := strings.EqualFold(u.Scheme, "ssh")
	segs := splitPath(u.Path)
	gitlike := ssh || knownGitHosts[u.Hostname()] || hasGitSegment(segs)
	if !gitlike || len(segs) < 2 {
		if ssh {
			return Ref{}, fmt.Errorf("%w: %q is missing a repository path", ErrInvalidSource, original)
		}
		return Ref{Type: TypeURL, Original: original, URL: trimmed}, nil
	}

	repo, folder, err := splitRepository(original, u.Hostname(), u.Path)
	if err != nil {
		return Ref{}, err
	}
	scheme := "https"
	if ssh {
		scheme = "ssh"
	}
	userPrefix := ""
	if u.User != nil && u.User.Username() != "" {
		userPrefix = u.User.Username() + "@"
	}
	ref := gitRef(original, repo, folder)
	ref.URL = fmt.Sprintf("%s://%s%s/%s.git", scheme, userPrefix, u.Host, strings.Join(repo, "/"))
	return ref, nil
}

// parseShorthand parses [host/]owner/repo[/path]. A first segment with a dot
// is a host; otherwise the host is GitHub.
func parseShorthand(original, trimmed string) (Ref, error) {
	if len(splitPath(trimmed)) < 2 {
		return Ref{}, fmt.Errorf("%w: %q is not owner/repo", ErrInvalidSource, original)
	}
	host, rest := defaultGitHost, trimmed
	if first, after, ok := strings.Cut(trimmed, "/"); ok && strings.Contains(first, ".") {
		host, rest = first, after
	}
	repo, folder, err := splitRepository(original, host, rest)
	if err != nil {
		return Ref{}, err
	}
	ref := gitRef(original, repo, folder)
	ref.URL = fmt.Sprintf("https://%s/%s.git", host, strings.Join(repo, "/"))
	return ref, nil
}

// gitRef builds a git Ref from repository segments and a folder; the caller
// sets the URL.
func gitRef(original string, repo []string, folder string) Ref {
	return Ref{
		Type:     TypeGit,
		Original: original,
		Owner:    strings.Join(repo[:len(repo)-1], "/"),
		Repo:     repo[len(repo)-1],
		Path:     folder,
	}
}

// splitRepository finds where the repository ends in p and returns its
// segments (without a .git suffix) and the folder after it. An explicit "//"
// wins, then a segment ending in .git, then the host's default: two segments,
// except that GitLab, whose groups nest, refuses an unmarked longer path.
func splitRepository(original, host, p string) (repo []string, folder string, err error) {
	provider := ProviderOf(host)
	// Trim only leading slashes before looking for "//": a trailing "//"
	// still marks a boundary, one with no folder after it.
	trimmed := strings.TrimLeft(p, "/")
	if before, after, ok := strings.Cut(trimmed, "//"); ok {
		repo, folder = trimGitSuffix(splitPath(before)), strings.Join(splitPath(after), "/")
		if folder == "" {
			return nil, "", fmt.Errorf("%w: %q has no folder after //", ErrInvalidSource, original)
		}
		return checkDepth(original, provider, repo, folder)
	}

	segs := splitPath(trimmed)
	if provider.nestedNamespaces() && slices.Contains(segs, "-") {
		return nil, "", webAddress(original)
	}
	if i := gitSegment(segs); i >= 0 && (i == 1 || !provider.fixedDepth()) {
		return checkDepth(original, provider, trimGitSuffix(segs[:i+1]), strings.Join(segs[i+1:], "/"))
	}
	if provider.nestedNamespaces() && len(segs) > 2 {
		return nil, "", ambiguous(original)
	}
	if len(segs) < 2 {
		return nil, "", fmt.Errorf("%w: %q is missing owner or repo", ErrInvalidSource, original)
	}
	return checkDepth(original, provider, trimGitSuffix(segs[:2]), strings.Join(segs[2:], "/"))
}

// checkDepth rejects a repository path the provider cannot have: fewer than
// two segments anywhere, or other than two on GitHub and Bitbucket, and any
// "." or ".." segment.
func checkDepth(original string, provider Provider, repo []string, folder string) ([]string, string, error) {
	if len(repo) < 2 {
		return nil, "", fmt.Errorf("%w: %q is missing owner or repo", ErrInvalidSource, original)
	}
	if provider.fixedDepth() && len(repo) != 2 {
		return nil, "", fmt.Errorf("%w: repositories on %s are owner/repo, got %q", ErrInvalidSource, provider.GitHost(), strings.Join(repo, "/"))
	}
	for _, seg := range append(append([]string{}, repo...), splitPath(folder)...) {
		if seg == "." || seg == ".." {
			return nil, "", fmt.Errorf("%w: %q has a %q segment", ErrInvalidSource, original, seg)
		}
	}
	return repo, folder, nil
}

// ambiguous reports an unmarked GitLab path with both ways to mark it.
func ambiguous(original string) error {
	base := strings.TrimRight(strings.TrimSpace(original), "/")
	asRepo := base + ".git"
	asFolder := base
	if i := strings.LastIndex(base, "/"); i >= 0 {
		asFolder = base[:i] + "//" + base[i+1:]
	}
	return errs.Wrap(errs.CodeUsage, "", fmt.Errorf(
		"%w: %q could be a repository in a nested GitLab group or a folder inside a repository; write %q for the repository or %q for a folder",
		ErrAmbiguousSource, original, asRepo, asFolder))
}

// webAddress reports a GitLab web address (one with the /-/ route separator,
// such as .../-/tree/main/skills), suggesting the clone address with the
// folder after //. A ref with slashes makes the folder a guess.
func webAddress(original string) error {
	base, rest, _ := strings.Cut(strings.TrimSpace(original), "/-/")
	suggestion := base + ".git"
	if parts := strings.SplitN(rest, "/", 3); len(parts) == 3 && (parts[0] == "tree" || parts[0] == "blob") {
		suggestion += "//" + strings.TrimSuffix(strings.TrimSuffix(parts[2], "/SKILL.md"), "/")
	}
	return fmt.Errorf("%w: %q is a GitLab web address; use the project's clone address, with // before a folder, such as %q",
		ErrInvalidSource, original, suggestion)
}

// gitSegment returns the index of the first segment ending in .git, or -1.
func gitSegment(segs []string) int {
	for i, seg := range segs {
		if len(seg) > len(".git") && strings.HasSuffix(seg, ".git") {
			return i
		}
	}
	return -1
}

func hasGitSegment(segs []string) bool { return gitSegment(segs) >= 0 }

// trimGitSuffix returns segs with a .git suffix removed from the last one.
func trimGitSuffix(segs []string) []string {
	if len(segs) == 0 {
		return segs
	}
	out := append([]string{}, segs...)
	out[len(out)-1] = strings.TrimSuffix(out[len(out)-1], ".git")
	return out
}

// hasCredential reports whether raw carries a password, or a token in place
// of a user name, in its user info.
func hasCredential(raw string) bool {
	if hasScheme(raw) {
		u, err := url.Parse(raw)
		if err != nil || u.User == nil {
			return false
		}
		if _, ok := u.User.Password(); ok {
			return true
		}
		return tokenShaped(u.User.Username())
	}
	at := strings.Index(raw, "@")
	if at <= 0 || strings.Contains(raw[:at], "/") {
		return false
	}
	userinfo := raw[:at]
	return strings.Contains(userinfo, ":") || tokenShaped(userinfo)
}

func tokenShaped(user string) bool {
	for _, prefix := range tokenPrefixes {
		if strings.HasPrefix(user, prefix) {
			return true
		}
	}
	return false
}

// userinfoRE matches the user info of a URL with a scheme, and scpUserinfoRE
// that of an scp-style address, for Redact.
var (
	userinfoRE    = regexp.MustCompile(`^(\s*[A-Za-z][A-Za-z0-9+.-]*://)([^/\s]+)@`)
	scpUserinfoRE = regexp.MustCompile(`^(\s*)([^/\s]+)@`)
)

// Redact returns raw with any password or token in its user info replaced by
// "***", for messages that echo a source as the user wrote it. A plain user
// name is kept: it identifies, it does not authenticate.
func Redact(raw string) string {
	if m := userinfoRE.FindStringSubmatch(raw); m != nil {
		if strings.Contains(m[2], ":") || tokenShaped(m[2]) {
			return m[1] + "***@" + raw[len(m[0]):]
		}
		return raw
	}
	if m := scpUserinfoRE.FindStringSubmatch(raw); m != nil && !hasScheme(raw) {
		if strings.Contains(m[2], ":") || tokenShaped(m[2]) {
			return m[1] + "***@" + raw[len(m[0]):]
		}
	}
	return raw
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

// hostFromURL extracts the host name from a normalized git URL (https, ssh://,
// or scp-style), without user or port.
func hostFromURL(u string) string {
	if m := scpRE.FindStringSubmatch(u); m != nil {
		return m[2]
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}
