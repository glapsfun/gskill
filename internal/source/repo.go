package source

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// RepoKind classifies a repository identity.
type RepoKind string

// Repository identity kinds.
const (
	RepoRemote  RepoKind = "remote"
	RepoLocal   RepoKind = "local"
	RepoInvalid RepoKind = "invalid"
)

// Repo is the repository a source belongs to, for grouping and selecting
// skills by repository (spec 032 FR-001 to FR-003). It exists next to
// Identity because grouping must drop the in-repository path and compare
// hosts the way they compare names: case-insensitively, and owner and repo
// case-insensitively only on the known hosts.
type Repo struct {
	Kind RepoKind
	// Key is what grouping and --repo matching compare.
	Key string
	// Label is shown to people, written to JSON, and accepted back by --repo.
	Label string
}

// Equal reports whether two identities name the same repository. Labels are
// never compared.
func (r Repo) Equal(o Repo) bool { return r.Kind == o.Kind && r.Key == o.Key }

// RepoPathError reports a --repo value that names a path inside a
// repository instead of the repository itself.
type RepoPathError struct {
	// Value is the raw --repo value.
	Value string
	// Repo is the label of the repository the value points into.
	Repo string
}

func (e *RepoPathError) Error() string {
	return fmt.Sprintf("--repo %q names a path inside a repository", e.Value)
}

// RepoOf returns the repository identity of a parsed, unpromoted reference.
// Relative local paths resolve against root, the project root.
func RepoOf(ref Ref, root string) Repo {
	switch ref.Type {
	case TypeGit:
		host := strings.ToLower(hostFromURL(ref.URL))
		if host == "" {
			// A git URL that is a filesystem path (a promoted local repo).
			return localRepo(ref.URL, root)
		}
		return remoteRepo(host, ref.Owner, ref.Repo)
	case TypeURL:
		return urlRepo(ref)
	case TypeLocal:
		return localRepo(ref.LocalPath, root)
	default:
		return Repo{Kind: RepoInvalid, Key: ref.Original, Label: ref.Original}
	}
}

// ParseRepo parses a --repo value. It accepts every spelling Parse accepts
// for a repository and rejects one that names a path inside it with a
// *RepoPathError; other parse errors are returned unchanged.
func ParseRepo(raw, root string) (Repo, error) {
	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(trimmed, "git@") {
		// parseSSH drops an in-repository path, so check for one here.
		if _, rest, ok := strings.Cut(trimmed, ":"); ok && len(splitPath(rest)) > 2 {
			ref, err := Parse(trimmed)
			if err != nil {
				return Repo{}, err
			}
			return Repo{}, &RepoPathError{Value: raw, Repo: RepoOf(ref, root).Label}
		}
	}
	ref, err := Parse(lowerURLHost(trimmed))
	if err != nil {
		return Repo{}, err
	}
	if ref.Type == TypeGit && ref.Path != "" {
		return Repo{}, &RepoPathError{Value: raw, Repo: RepoOf(ref, root).Label}
	}
	return RepoOf(ref, root), nil
}

// remoteRepo builds a host/owner/repo identity. Owner and repo compare
// exactly except on the known hosts, which treat them case-insensitively.
func remoteRepo(host, owner, repo string) Repo {
	label := host + "/" + owner + "/" + repo
	if knownGitHosts[host] {
		return Repo{Kind: RepoRemote, Key: strings.ToLower(label), Label: label}
	}
	return Repo{Kind: RepoRemote, Key: label, Label: label}
}

// urlRepo identifies a non-git URL source. A URL on a known git host is that
// host's owner/repo even when Parse missed it because of the host's case.
func urlRepo(ref Ref) Repo {
	u, err := url.Parse(ref.URL)
	if err != nil {
		return Repo{Kind: RepoInvalid, Key: ref.Original, Label: ref.Original}
	}
	host := strings.ToLower(u.Host)
	if segs := splitPath(u.Path); knownGitHosts[host] && len(segs) >= 2 {
		return remoteRepo(host, segs[0], strings.TrimSuffix(segs[1], ".git"))
	}
	path := strings.TrimSuffix(strings.TrimRight(u.Path, "/"), ".git")
	return Repo{Kind: RepoRemote, Key: strings.ToLower(u.Scheme) + "://" + host + path, Label: ref.URL}
}

// localRepo identifies a local directory by its absolute, cleaned path.
// Symlinks are not resolved: the user knows the directory by the path they
// declared.
func localRepo(p, root string) Repo {
	label := filepath.Clean(p)
	if !isLocalPath(label) {
		// Keep the label a path --repo reads back as one (tools -> ./tools).
		label = "./" + label
	}
	abs := p
	if rest, ok := strings.CutPrefix(p, "~"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			abs = filepath.Join(home, rest)
		}
	} else if !filepath.IsAbs(p) {
		abs = filepath.Join(root, p)
	}
	return Repo{Kind: RepoLocal, Key: filepath.Clean(abs), Label: label}
}

// lowerURLHost lowercases the host of a URL so Parse recognizes a known git
// host whatever its case. Anything that is not a URL is returned as is.
func lowerURLHost(raw string) string {
	if !hasScheme(raw) {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	u.Host = strings.ToLower(u.Host)
	return u.String()
}
