package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/glapsfun/gskill/internal/errs"
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
	// Prefer a peeled tag line if present.
	var sha string
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		sha = fields[0]
		if strings.HasSuffix(fields[1], "^{}") {
			break
		}
	}
	if sha == "" {
		return "", fmt.Errorf("%w: could not resolve ref %q", errs.ErrSourceUnavailable, ref)
	}
	return sha, nil
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

	if _, err := runGit(ctx, dest, "fetch", "--quiet", "--depth", "1", "origin", commit); err != nil {
		// Some servers disallow fetching an arbitrary SHA shallowly; fall back.
		if _, ferr := runGit(ctx, dest, "fetch", "--quiet", "origin"); ferr != nil {
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
