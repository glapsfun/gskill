package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/progress"
)

// SystemRunner implements Runner by shelling out to the system git binary.
type SystemRunner struct{}

// NewSystemRunner returns a Runner backed by the system git binary.
func NewSystemRunner() SystemRunner { return SystemRunner{} }

// LsRemoteTags lists tags via "git ls-remote --tags", preferring the peeled
// (^{}) commit for annotated tags.
func (SystemRunner) LsRemoteTags(ctx context.Context, url string) ([]TagRef, error) {
	out, err := runGit(ctx, "", "ls-remote", "--tags", url)
	if err != nil {
		return nil, err
	}

	commits := make(map[string]string)
	order := make([]string, 0)
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		sha, ref, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		name := strings.TrimPrefix(ref, "refs/tags/")
		peeled := strings.HasSuffix(name, "^{}")
		name = strings.TrimSuffix(name, "^{}")
		if _, seen := commits[name]; !seen {
			order = append(order, name)
		}
		// A peeled entry always overrides the annotated-tag object SHA.
		if peeled || commits[name] == "" {
			commits[name] = sha
		}
	}

	tags := make([]TagRef, 0, len(order))
	for _, name := range order {
		tags = append(tags, TagRef{Name: name, Commit: commits[name]})
	}
	return tags, nil
}

// LsRemoteHeads lists branch heads via "git ls-remote --heads".
func (SystemRunner) LsRemoteHeads(ctx context.Context, url string) ([]BranchRef, error) {
	out, err := runGit(ctx, "", "ls-remote", "--heads", url)
	if err != nil {
		return nil, err
	}

	var heads []BranchRef
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		sha, ref, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		heads = append(heads, BranchRef{Name: strings.TrimPrefix(ref, "refs/heads/"), Commit: sha})
	}
	return heads, nil
}

// ResolveRef resolves a ref to an immutable commit SHA. A full SHA is returned
// as-is; otherwise the ref is looked up via "git ls-remote".
func (SystemRunner) ResolveRef(ctx context.Context, url, ref string) (string, error) {
	if shaRE.MatchString(ref) {
		return ref, nil
	}

	out, err := runGit(ctx, "", "ls-remote", url, ref)
	if err != nil {
		return "", err
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return "", fmt.Errorf("%w: ref %q not found", errs.ErrSourceUnavailable, ref)
	}
	if sha := pickRef(out, ref); sha != "" {
		return sha, nil
	}
	return "", fmt.Errorf("%w: could not resolve ref %q", errs.ErrSourceUnavailable, ref)
}

// pickRef chooses a commit SHA from "git ls-remote" output for ref, resolving
// the ambiguity when the same name exists as both a branch and a tag. Branch
// heads win (ResolveRef is the branch-resolution path); among tags the peeled
// commit (^{}) beats the annotated-tag object. The first SHA is the fallback.
func pickRef(out, ref string) string {
	var head, peeledTag, tag, other, first string
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		sha, name := fields[0], fields[1]
		if first == "" {
			first = sha
		}
		switch name {
		case "refs/heads/" + ref:
			head = sha
		case "refs/tags/" + ref + "^{}":
			peeledTag = sha
		case "refs/tags/" + ref:
			tag = sha
		default:
			if other == "" {
				other = sha
			}
		}
	}
	switch {
	case head != "":
		return head
	case peeledTag != "":
		return peeledTag
	case tag != "":
		return tag
	case other != "":
		return other
	default:
		return first
	}
}

// FetchCommit materializes the tree at commit into dest with no .git directory.
func (SystemRunner) FetchCommit(ctx context.Context, url, commit, dest string) error {
	if err := os.MkdirAll(dest, 0o750); err != nil {
		return fmt.Errorf("create fetch dir: %w", err)
	}
	if _, err := runGit(ctx, "", "init", "--quiet", dest); err != nil {
		return err
	}
	if _, err := runGit(ctx, dest, "remote", "add", "origin", url); err != nil {
		return err
	}

	// With a progress sink on the context, the fetches trade --quiet for
	// --progress and stream git's stderr through the parser; without one the
	// exec path is byte-identical to before.
	fetch := func(args ...string) error {
		sink := progress.FromContext(ctx)
		if sink == nil {
			_, err := runGit(ctx, dest, append([]string{"fetch", "--quiet"}, args...)...)
			return err
		}
		_, err := runGitProgress(ctx, dest, sink, append([]string{"fetch", "--progress"}, args...)...)
		return err
	}
	if err := fetch("--depth", "1", "origin", commit); err != nil {
		// Some servers disallow fetching an arbitrary SHA shallowly; fall back.
		if ferr := fetch("origin"); ferr != nil {
			return ferr
		}
	}
	if _, err := runGit(ctx, dest, "checkout", "--quiet", "--detach", commit); err != nil {
		return err
	}

	if err := os.RemoveAll(filepath.Join(dest, ".git")); err != nil {
		return fmt.Errorf("strip .git from fetched material: %w", err)
	}
	return nil
}
