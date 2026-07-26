package resolver

import (
	"context"
	"fmt"

	"github.com/Masterminds/semver/v3"

	"github.com/glapsfun/gskill/internal/git"
	"github.com/glapsfun/gskill/internal/source"
)

// OutdatedStatus classifies a skill's update eligibility (spec 018 FR-003).
type OutdatedStatus string

// Eligibility statuses. Only StatusUpdateAvailable is actionable by a normal
// update; every other status explains why the skill cannot move without
// changing its requested tracking policy.
const (
	StatusUpdateAvailable    OutdatedStatus = "update-available"
	StatusUpToDate           OutdatedStatus = "up-to-date"
	StatusPinnedTag          OutdatedStatus = "pinned-tag"
	StatusPinnedCommit       OutdatedStatus = "pinned-commit"
	StatusLocalSource        OutdatedStatus = "local-source"
	StatusNoCompatibleUpdate OutdatedStatus = "no-compatible-update"
)

// OutdatedResult reports a skill's update eligibility. Status is the single
// source of truth for actionability (StatusUpdateAvailable and nothing else);
// Latest is the actionable candidate (equal to Current when there is none);
// Informational names the newest upstream revision that exists outside the
// requested policy and is never applied by a normal update (FR-004, FR-007).
type OutdatedResult struct {
	Current       string
	Latest        string
	Status        OutdatedStatus
	Informational string
}

// Available reports whether a normal update can act on this result.
func (r OutdatedResult) Available() bool { return r.Status == StatusUpdateAvailable }

// Outdated reports update eligibility for a skill given its current locked
// revision and the requested constraint (spec 018 FR-004/FR-005). Semver
// constraints compare against the highest satisfying tag; branch tracking
// compares the branch head; exact tag and commit pins and local sources are
// stable, never-actionable states.
func Outdated(ctx context.Context, runner git.Runner, ref source.Ref, req Requested, current Revision) (OutdatedResult, error) {
	switch current.RefKind {
	case RefKindCommit:
		return OutdatedResult{Current: current.Commit, Latest: current.Commit, Status: StatusPinnedCommit}, nil
	case RefKindLocal:
		return OutdatedResult{Current: "local", Latest: "local", Status: StatusLocalSource}, nil
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

// outdatedSemver compares the locked version against the highest tag
// satisfying the constraint, distinguishing "up to date" from "newer exists
// but the constraint forbids it".
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

	upToDate := OutdatedResult{Current: current.Version, Latest: current.Version, Status: StatusUpToDate}
	bestAll, _, anyTag := highestTag(tags, nil)

	bestSat, _, ok := highestTag(tags, constraint)
	if ok {
		if res := compareVersions(current.Version, bestSat); res.Available() {
			return res, nil
		}
	}

	// Nothing actionable within the constraint: report a strictly newer
	// upstream release as informational only.
	if anyTag {
		if cur, curErr := semver.NewVersion(current.Version); curErr == nil && bestAll.GreaterThan(cur) {
			upToDate.Status = StatusNoCompatibleUpdate
			upToDate.Informational = bestAll.String()
		}
	}
	return upToDate, nil
}

// outdatedTag reports an exact tag pin as stable and never actionable: a
// normal update preserves the pin, so a newer repository tag is informational
// only. A newest-tag lookup failure propagates — the caller decides whether a
// pin stays classifiable without it (the app plan layer does); swallowing it
// here would fabricate healthy results for dead remotes and eat cancellation.
func outdatedTag(ctx context.Context, runner git.Runner, ref source.Ref, current Revision) (OutdatedResult, error) {
	res := OutdatedResult{Current: current.Tag, Latest: current.Tag, Status: StatusPinnedTag}
	tags, err := runner.LsRemoteTags(ctx, ref.URL)
	if err != nil {
		return OutdatedResult{}, err
	}
	if best, _, ok := highestTag(tags, nil); ok {
		if cur, curErr := semver.NewVersion(current.Tag); curErr == nil && best.GreaterThan(cur) {
			res.Informational = best.String()
		}
	}
	return res, nil
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
	res := OutdatedResult{
		Current: shortSHA(current.Commit),
		Latest:  shortSHA(head),
		Status:  StatusUpToDate,
	}
	if head != "" && head != current.Commit {
		res.Status = StatusUpdateAvailable
	}
	return res, nil
}

// compareVersions builds a result comparing a current version string against a
// resolved best version.
func compareVersions(current string, best *semver.Version) OutdatedResult {
	res := OutdatedResult{Current: current, Latest: best.String(), Status: StatusUpToDate}
	newer := best.String() != current
	if cur, err := semver.NewVersion(current); err == nil {
		newer = best.GreaterThan(cur)
	}
	if newer {
		res.Status = StatusUpdateAvailable
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
