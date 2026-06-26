package resolver

import (
	"context"
	"errors"
	"fmt"

	"github.com/Masterminds/semver/v3"

	"github.com/glapsfun/gskill/internal/git"
	"github.com/glapsfun/gskill/internal/source"
)

// ErrNoMatchingVersion is returned when no tag satisfies a version constraint.
var ErrNoMatchingVersion = errors.New("no version matches constraint")

// Requested is the human's version intent (at most one field drives resolution).
type Requested struct {
	Version string // semver constraint
	Ref     string // branch or tag
	Commit  string // explicit commit
}

// Revision is the resolved, possibly-immutable identity to pin (FR-009, FR-010).
type Revision struct {
	RefKind    RefKind
	Version    string
	Tag        string
	Branch     string
	Commit     string
	MutableRef bool
}

// Resolve turns a source reference plus requested version into a Revision,
// returning any advisory warnings (e.g. mutable refs, SC-008).
func Resolve(ctx context.Context, runner git.Runner, ref source.Ref, req Requested) (Revision, []string, error) {
	if ref.Type == source.TypeLocal {
		return Revision{RefKind: RefKindLocal, MutableRef: true}, nil, nil
	}
	return resolveGit(ctx, runner, ref, req)
}

// resolveGit handles git sources by dispatching on which intent field is set.
func resolveGit(ctx context.Context, runner git.Runner, ref source.Ref, req Requested) (Revision, []string, error) {
	switch {
	case req.Commit != "":
		return Revision{RefKind: RefKindCommit, Commit: req.Commit}, nil, nil
	case req.Version != "":
		return resolveConstraint(ctx, runner, ref, req.Version)
	case req.Ref != "":
		return resolveRef(ctx, runner, ref, req.Ref)
	default:
		return resolveLatest(ctx, runner, ref)
	}
}

// resolveConstraint picks the highest tag satisfying a semver constraint.
func resolveConstraint(ctx context.Context, runner git.Runner, ref source.Ref, constraint string) (Revision, []string, error) {
	c, err := semver.NewConstraint(constraint)
	if err != nil {
		return Revision{}, nil, fmt.Errorf("parse constraint %q: %w", constraint, err)
	}
	tags, err := runner.LsRemoteTags(ctx, ref.URL)
	if err != nil {
		return Revision{}, nil, err
	}
	best, tag, ok := highestTag(tags, c)
	if !ok {
		return Revision{}, nil, fmt.Errorf("%w %q in %s", ErrNoMatchingVersion, constraint, ref.URL)
	}
	return Revision{
		RefKind: RefKindSemver,
		Version: best.String(),
		Tag:     tag.Name,
		Commit:  tag.Commit,
	}, nil, nil
}

// resolveRef resolves an explicit ref as a tag (immutable) or branch (mutable).
func resolveRef(ctx context.Context, runner git.Runner, ref source.Ref, name string) (Revision, []string, error) {
	tags, err := runner.LsRemoteTags(ctx, ref.URL)
	if err != nil {
		return Revision{}, nil, err
	}
	for _, tag := range tags {
		if tag.Name == name {
			return Revision{RefKind: RefKindTag, Tag: name, Commit: tag.Commit}, nil, nil
		}
	}

	commit, err := runner.ResolveRef(ctx, ref.URL, name)
	if err != nil {
		return Revision{}, nil, err
	}
	rev := Revision{RefKind: RefKindBranch, Branch: name, Commit: commit, MutableRef: true}
	return rev, []string{mutableWarning(name)}, nil
}

// resolveLatest picks the highest tag, falling back to the default branch when
// the repo has no tags (SC-008).
func resolveLatest(ctx context.Context, runner git.Runner, ref source.Ref) (Revision, []string, error) {
	tags, err := runner.LsRemoteTags(ctx, ref.URL)
	if err != nil {
		return Revision{}, nil, err
	}
	if best, tag, ok := highestTag(tags, nil); ok {
		return Revision{
			RefKind: RefKindSemver,
			Version: best.String(),
			Tag:     tag.Name,
			Commit:  tag.Commit,
		}, nil, nil
	}

	commit, err := runner.ResolveRef(ctx, ref.URL, "HEAD")
	if err != nil {
		return Revision{}, nil, err
	}
	rev := Revision{RefKind: RefKindBranch, Branch: "HEAD", Commit: commit, MutableRef: true}
	return rev, []string{"no tags found; pinned to a mutable branch HEAD (unversioned)"}, nil
}

// highestTag returns the highest semver tag, optionally filtered by constraint.
func highestTag(tags []git.TagRef, c *semver.Constraints) (*semver.Version, git.TagRef, bool) {
	var (
		best    *semver.Version
		bestTag git.TagRef
		found   bool
	)
	for _, tag := range tags {
		v, err := semver.NewVersion(tag.Name)
		if err != nil {
			continue
		}
		if c != nil && !c.Check(v) {
			continue
		}
		if best == nil || v.GreaterThan(best) {
			best, bestTag, found = v, tag, true
		}
	}
	return best, bestTag, found
}

// mutableWarning formats the advisory for a mutable branch ref.
func mutableWarning(name string) string {
	return fmt.Sprintf("ref %q is a mutable branch; installs may drift over time", name)
}
