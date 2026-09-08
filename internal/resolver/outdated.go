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
	StatusPinnedVersion      OutdatedStatus = "pinned-version"
	StatusLookupFailed       OutdatedStatus = "lookup-failed"
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
	// LookupErr records a failed remote lookup. With StatusLookupFailed the
	// skill's eligibility is unknown; with a pinned status only the
	// informational newest-release hint is missing.
	LookupErr error
}

// Available reports whether a normal update can act on this result.
func (r OutdatedResult) Available() bool { return r.Status == StatusUpdateAvailable }

// Outdated reports update eligibility for a skill given its current locked
// revision and the declared intent (spec 018 FR-004/FR-005, spec 024 FR-006).
// The declaration's shape drives the classification: floating ranges compare
// against the highest satisfying tag, branch tracking compares the branch
// head, and exact versions, tags, commits, and local sources are pinned or
// local states a normal update never moves. A failed remote lookup is a
// result, not an error, so one dead remote cannot fail a whole plan.
func Outdated(ctx context.Context, runner git.Runner, ref source.Ref, req Requested, current Revision) (OutdatedResult, error) {
	decl := Declaration{Version: req.Version, Ref: req.Ref, Commit: req.Commit, Local: current.RefKind == RefKindLocal}
	shape, err := ClassifyDeclaration(decl, current.RefKind)
	if err != nil {
		return OutdatedResult{Current: current.Version, Latest: current.Version, Status: StatusLookupFailed, LookupErr: err}, nil //nolint:nilerr // a lookup failure is a result, not an error
	}
	if shape == ShapeUnpinned {
		shape = shapeFromRevision(current)
	}
	return OutdatedShaped(ctx, runner, ref, shape, req, current)
}

// shapeFromRevision infers a shape from a locked revision when no intent was
// declared, so a lock written before intent was recorded still classifies by
// what it resolved to.
func shapeFromRevision(current Revision) DeclarationShape {
	switch current.RefKind {
	case RefKindCommit:
		return ShapeCommit
	case RefKindTag:
		return ShapeTag
	case RefKindBranch:
		return ShapeBranch
	case RefKindLocal:
		return ShapeLocal
	case RefKindSemver:
		return ShapeUnpinned
	default:
		return ShapeUnpinned
	}
}

// OutdatedShaped is Outdated with the declaration shape already known.
func OutdatedShaped(ctx context.Context, runner git.Runner, ref source.Ref, shape DeclarationShape, req Requested, current Revision) (OutdatedResult, error) {
	switch shape {
	case ShapeCommit:
		return OutdatedResult{Current: current.Commit, Latest: current.Commit, Status: StatusPinnedCommit}, nil
	case ShapeLocal:
		return OutdatedResult{Current: "local", Latest: "local", Status: StatusLocalSource}, nil
	case ShapeBranch:
		return outdatedBranch(ctx, runner, ref, req, current)
	case ShapeTag:
		return outdatedTag(ctx, runner, ref, current)
	case ShapeExactVersion:
		return outdatedExactVersion(ctx, runner, ref, req, current)
	case ShapeRangeCaret, ShapeRangeTilde, ShapeRangeOther:
		return outdatedSemver(ctx, runner, ref, req, current)
	case ShapeUnpinned:
		return outdatedSemver(ctx, runner, ref, Requested{}, current)
	default:
		return OutdatedResult{}, fmt.Errorf("unknown declaration shape %q", shape)
	}
}

// outdatedExactVersion reports an exact version pin as stable and never
// actionable; the newest stable release is fetched only to power the
// "move it with upgrade" hint.
func outdatedExactVersion(ctx context.Context, runner git.Runner, ref source.Ref, req Requested, current Revision) (OutdatedResult, error) {
	res := OutdatedResult{Current: current.Version, Latest: current.Version, Status: StatusPinnedVersion}
	tags, err := runner.LsRemoteTags(ctx, ref.URL)
	if err != nil {
		res.LookupErr = err
		return res, nil //nolint:nilerr // a lookup failure is a result, not an error
	}
	if best, _, ok := highestStable(tags, admitsPrerelease(req.Version)); ok {
		if cur, curErr := semver.NewVersion(current.Version); curErr == nil && best.GreaterThan(cur) {
			res.Informational = best.String()
		}
	}
	return res, nil
}

// outdatedSemver compares the locked version against the highest tag
// satisfying the constraint, distinguishing "up to date" from "newer exists
// but the constraint forbids it".
func outdatedSemver(ctx context.Context, runner git.Runner, ref source.Ref, req Requested, current Revision) (OutdatedResult, error) {
	upToDate := OutdatedResult{Current: current.Version, Latest: current.Version, Status: StatusUpToDate}
	tags, err := runner.LsRemoteTags(ctx, ref.URL)
	if err != nil {
		upToDate.Status, upToDate.LookupErr = StatusLookupFailed, err
		return upToDate, nil //nolint:nilerr // a lookup failure is a result, not an error
	}

	var constraint *semver.Constraints
	if req.Version != "" {
		constraint, err = semver.NewConstraint(req.Version)
		if err != nil {
			upToDate.Status = StatusLookupFailed
			upToDate.LookupErr = fmt.Errorf("parse constraint %q: %w", req.Version, err)
			return upToDate, nil
		}
	}

	allowPre := admitsPrerelease(req.Version)
	bestAll, _, anyTag := highestStable(tags, allowPre)

	bestSat, _, ok := highestTag(tags, constraint)
	if constraint == nil && !allowPre {
		bestSat, _, ok = highestStable(tags, false)
	}
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
// only. A newest-tag lookup failure is recorded on the result — the pin's
// eligibility never depended on it — so a dead remote costs the hint, not the
// classification.
func outdatedTag(ctx context.Context, runner git.Runner, ref source.Ref, current Revision) (OutdatedResult, error) {
	res := OutdatedResult{Current: current.Tag, Latest: current.Tag, Status: StatusPinnedTag}
	tags, err := runner.LsRemoteTags(ctx, ref.URL)
	if err != nil {
		res.LookupErr = err
		return res, nil //nolint:nilerr // a lookup failure is a result, not an error
	}
	if best, _, ok := highestStable(tags, false); ok {
		if cur, curErr := semver.NewVersion(current.Tag); curErr == nil && best.GreaterThan(cur) {
			res.Informational = best.String()
		}
	}
	return res, nil
}

// outdatedBranch compares the locked commit against the head of the branch the
// declaration names. The declared ref wins over the locked one: the manifest is
// authoritative for intent, and the install will use it, so discovering against
// the lock's branch would describe a branch the run is not going to touch.
func outdatedBranch(ctx context.Context, runner git.Runner, ref source.Ref, req Requested, current Revision) (OutdatedResult, error) {
	branch := req.Ref
	if branch == "" {
		branch = current.Branch
	}
	if branch == "" {
		branch = "HEAD"
	}
	head, err := runner.ResolveRef(ctx, ref.URL, branch)
	if err != nil {
		failed := OutdatedResult{
			Current: shortSHA(current.Commit), Latest: shortSHA(current.Commit),
			Status: StatusLookupFailed, LookupErr: err,
		}
		return failed, nil //nolint:nilerr // a lookup failure is a result, not an error
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

// highestStable returns the highest semver tag, skipping pre-releases unless
// the declaration itself admits them: a stable constraint must never be told
// that a beta is "newer".
func highestStable(tags []git.TagRef, allowPrerelease bool) (*semver.Version, git.TagRef, bool) {
	if allowPrerelease {
		return highestTag(tags, nil)
	}
	stable := make([]git.TagRef, 0, len(tags))
	for _, tag := range tags {
		if v, err := semver.NewVersion(tag.Name); err == nil && v.Prerelease() != "" {
			continue
		}
		stable = append(stable, tag)
	}
	return highestTag(stable, nil)
}
