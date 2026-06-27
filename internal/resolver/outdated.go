package resolver

import (
	"context"
	"fmt"

	"github.com/Masterminds/semver/v3"

	"github.com/glapsfun/gskill/internal/git"
	"github.com/glapsfun/gskill/internal/source"
)

// OutdatedResult reports whether a newer version is available for a skill.
type OutdatedResult struct {
	Current   string
	Latest    string
	Available bool
}

// Outdated reports available updates for a skill given its current locked
// revision and the requested constraint, with semantics that depend on the pin
// type (FR-009, design §9.2). Commit and local pins are never outdated; semver
// and tag pins compare against the highest satisfying tag; branch pins compare
// the branch head commit.
func Outdated(ctx context.Context, runner git.Runner, ref source.Ref, req Requested, current Revision) (OutdatedResult, error) {
	switch current.RefKind {
	case RefKindCommit:
		return OutdatedResult{Current: current.Commit, Latest: current.Commit}, nil
	case RefKindLocal:
		return OutdatedResult{Current: "local", Latest: "local"}, nil
	case RefKindBranch:
		return outdatedBranch(ctx, runner, ref, current)
	case RefKindTag:
		return outdatedTag(ctx, runner, ref, current)
	case RefKindSemver:
		return outdatedSemver(ctx, runner, ref, req, current)
	default:
		return OutdatedResult{}, fmt.Errorf("unknown ref kind %q", current.RefKind)
	}
}

// outdatedSemver finds the highest tag satisfying the constraint.
func outdatedSemver(ctx context.Context, runner git.Runner, ref source.Ref, req Requested, current Revision) (OutdatedResult, error) {
	tags, err := runner.LsRemoteTags(ctx, ref.URL)
	if err != nil {
		return OutdatedResult{}, err
	}

	var constraint *semver.Constraints
	if req.Version != "" {
		constraint, err = semver.NewConstraint(req.Version)
		if err != nil {
			return OutdatedResult{}, fmt.Errorf("parse constraint %q: %w", req.Version, err)
		}
	}

	best, _, ok := highestTag(tags, constraint)
	if !ok {
		return OutdatedResult{Current: current.Version, Latest: current.Version}, nil
	}
	return compareVersions(current.Version, best), nil
}

// outdatedTag compares the locked tag against the highest available tag.
func outdatedTag(ctx context.Context, runner git.Runner, ref source.Ref, current Revision) (OutdatedResult, error) {
	tags, err := runner.LsRemoteTags(ctx, ref.URL)
	if err != nil {
		return OutdatedResult{}, err
	}
	best, _, ok := highestTag(tags, nil)
	if !ok {
		return OutdatedResult{Current: current.Tag, Latest: current.Tag}, nil
	}
	return compareVersions(current.Tag, best), nil
}

// outdatedBranch compares the locked commit against the current branch head.
func outdatedBranch(ctx context.Context, runner git.Runner, ref source.Ref, current Revision) (OutdatedResult, error) {
	branch := current.Branch
	if branch == "" {
		branch = "HEAD"
	}
	head, err := runner.ResolveRef(ctx, ref.URL, branch)
	if err != nil {
		return OutdatedResult{}, err
	}
	return OutdatedResult{
		Current:   shortSHA(current.Commit),
		Latest:    shortSHA(head),
		Available: head != "" && head != current.Commit,
	}, nil
}

// compareVersions builds a result comparing a current version string against a
// resolved best version.
func compareVersions(current string, best *semver.Version) OutdatedResult {
	res := OutdatedResult{Current: current, Latest: best.String()}
	if cur, err := semver.NewVersion(current); err == nil {
		res.Available = best.GreaterThan(cur)
	} else {
		res.Available = best.String() != current
	}
	return res
}

// shortSHA truncates a commit SHA for display.
func shortSHA(sha string) string {
	const n = 12
	if len(sha) > n {
		return sha[:n]
	}
	return sha
}
