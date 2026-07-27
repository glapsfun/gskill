package app

import (
	"context"

	"github.com/glapsfun/gskill/internal/git"
	"github.com/glapsfun/gskill/internal/resolver"
	"github.com/glapsfun/gskill/internal/skillslock"
)

// UpdateStatus classifies one skill's update eligibility in an update plan
// (spec 018 FR-003). Values are stable: they appear in JSON output.
type UpdateStatus string

// Plan item statuses. Only StatusUpdateAvailable is actionable by a normal
// update. StatusUnknown records a skill whose eligibility could not be
// determined (per-skill discovery failure, or offline mode for a mutable
// tracking policy) without failing the whole plan.
const (
	StatusUpdateAvailable    UpdateStatus = UpdateStatus(resolver.StatusUpdateAvailable)
	StatusUpToDate           UpdateStatus = UpdateStatus(resolver.StatusUpToDate)
	StatusPinnedTag          UpdateStatus = UpdateStatus(resolver.StatusPinnedTag)
	StatusPinnedCommit       UpdateStatus = UpdateStatus(resolver.StatusPinnedCommit)
	StatusLocalSource        UpdateStatus = UpdateStatus(resolver.StatusLocalSource)
	StatusNoCompatibleUpdate UpdateStatus = UpdateStatus(resolver.StatusNoCompatibleUpdate)
	StatusUnknown            UpdateStatus = "unknown"
)

// UpdatePlanItem is one skill's update assessment (spec 018 FR-001): the
// single source every update surface renders from. Candidate is set exactly
// when the item is actionable; Informational names a newer upstream revision
// the requested policy forbids (display only, never selectable).
type UpdatePlanItem struct {
	Name          string
	Source        string
	Current       string
	Candidate     string
	Policy        string
	Status        UpdateStatus
	Reason        string
	Informational string
	// DiscoveryErr carries a per-skill discovery failure (Status is then
	// StatusUnknown). It distinguishes a real error from an offline skip:
	// execution reports the former as a failure and the latter as a no-op.
	DiscoveryErr string
}

// Actionable reports whether a normal update can apply this item without
// changing its requested tracking policy (FR-004).
func (it UpdatePlanItem) Actionable() bool { return it.Status == StatusUpdateAvailable }

// UpdatePlan is the computed, name-sorted update view shared by
// `update --list`, `outdated`, interactive selection, and update execution
// (FR-002, FR-018).
type UpdatePlan struct {
	Items []UpdatePlanItem
}

// Actionable returns the items a normal update can apply, in plan order.
func (p UpdatePlan) Actionable() []UpdatePlanItem {
	var out []UpdatePlanItem
	for _, it := range p.Items {
		if it.Actionable() {
			out = append(out, it)
		}
	}
	return out
}

// AnyAvailable reports whether at least one item is actionable; it drives the
// `outdated --exit-code` contract (FR-008).
func (p UpdatePlan) AnyAvailable() bool {
	for _, it := range p.Items {
		if it.Actionable() {
			return true
		}
	}
	return false
}

// WithDiscoveryMemo returns a context whose git lookups are memoized for the
// life of one command invocation, so interactive discovery and the
// post-confirm execution share results instead of re-querying every remote.
// It exists so the CLI keeps depending only on the app layer.
func WithDiscoveryMemo(ctx context.Context) context.Context { return git.WithMemo(ctx) }

// UpdatePlanRequest configures update discovery.
type UpdatePlanRequest struct {
	Root    string
	Offline bool
	NoCache bool
}

// PlanUpdate computes the update plan for every locked skill (FR-001). The
// plan is read-only: it never touches installed files or the lockfile. A
// single skill's discovery failure is recorded on its item as StatusUnknown
// rather than failing the run — pin-policy classification must survive one
// unreachable remote.
func (a *App) PlanUpdate(ctx context.Context, req UpdatePlanRequest) (UpdatePlan, error) {
	ctx = git.WithMemo(ctx)
	p := openProject(req.Root)
	lf, err := loadOrNewLock(p.lockPath)
	if err != nil {
		return UpdatePlan{}, err
	}

	var plan UpdatePlan
	for _, name := range sortedKeys(lf.Skills) {
		plan.Items = append(plan.Items, a.planOne(ctx, name, lf.Skills[name], req))
		// A cancelled context must abort the run, not surface as fabricated
		// per-skill "discovery failed" items.
		if err := ctx.Err(); err != nil {
			return UpdatePlan{}, err
		}
	}
	return plan, nil
}

// planOne classifies a single locked skill.
func (a *App) planOne(ctx context.Context, name string, rec skillslock.Record, req UpdatePlanRequest) UpdatePlanItem {
	item := UpdatePlanItem{
		Name:   name,
		Source: rec.Source.Original,
		Policy: policyLabel(rec),
	}
	rev := revFromLock(rec.Resolved)

	if req.Offline {
		return planOffline(item, rev)
	}

	res, err := resolver.Outdated(ctx, a.git, refFromLock(rec.Source),
		resolver.Requested{
			Version: rec.Requested.Version,
			Ref:     rec.Requested.Ref,
			Commit:  rec.Requested.Commit,
		}, rev)
	if err != nil {
		item.Current = RevisionLabel(rev)
		// An exact pin's eligibility is fully determined by the lock; the
		// remote lookup only enriches it with an informational newest tag.
		// Keep the pin classification and record the failed lookup instead
		// of degrading a healthy pin to "unknown".
		if rev.RefKind == resolver.RefKindTag {
			item.Status = StatusPinnedTag
			item.Reason = notActionableReason(StatusPinnedTag) +
				" (newest-tag lookup failed: " + err.Error() + ")"
			item.DiscoveryErr = err.Error()
			return item
		}
		item.Status = StatusUnknown
		item.Reason = "discovery failed: " + err.Error()
		item.DiscoveryErr = err.Error()
		return item
	}

	item.Status = UpdateStatus(res.Status)
	item.Current = res.Current
	item.Informational = res.Informational
	if res.Available() {
		item.Candidate = res.Latest
	} else {
		item.Reason = notActionableReason(item.Status)
	}
	if item.Status == StatusPinnedCommit {
		item.Current = shortCommit(rev.Commit)
	}
	return item
}

// labelLocal is the display label for local sources (current revision and
// tracking policy alike).
const labelLocal = "local"

// planOffline classifies without any remote lookup: pins and local sources
// are fully determined by the lock; mutable tracking policies honestly report
// that they were not checked instead of guessing (FR-014).
func planOffline(item UpdatePlanItem, rev resolver.Revision) UpdatePlanItem {
	// Mutable kinds (semver, branch) and anything unrecognized default to
	// "not checked" — offline mode never guesses.
	item.Current = RevisionLabel(rev)
	item.Status = StatusUnknown
	item.Reason = "offline mode: remote lookup skipped"
	switch rev.RefKind {
	case resolver.RefKindCommit:
		item.Status = StatusPinnedCommit
		item.Current = shortCommit(rev.Commit)
		item.Reason = notActionableReason(StatusPinnedCommit)
	case resolver.RefKindTag:
		item.Status = StatusPinnedTag
		item.Reason = notActionableReason(StatusPinnedTag)
	case resolver.RefKindLocal:
		item.Status = StatusLocalSource
		item.Current = labelLocal
		item.Reason = notActionableReason(StatusLocalSource)
	case resolver.RefKindSemver, resolver.RefKindBranch:
		// keep the not-checked default
	}
	return item
}

// notActionableReason is the human-meaningful explanation attached to every
// non-actionable item (FR-001).
func notActionableReason(s UpdateStatus) string {
	switch s {
	case StatusUpToDate:
		return "already the newest revision the policy allows"
	case StatusPinnedTag:
		return "pinned to an exact tag; not changed by a normal update"
	case StatusPinnedCommit:
		return "pinned to an exact commit; not changed by a normal update"
	case StatusLocalSource:
		return "local source has no remote update candidate"
	case StatusNoCompatibleUpdate:
		return "newer releases exist outside the version constraint"
	case StatusUpdateAvailable, StatusUnknown:
		return "" // actionable items and discovery failures carry their own text
	default:
		return ""
	}
}

// policyLabel renders the requested tracking policy for display.
func policyLabel(rec skillslock.Record) string {
	switch {
	case rec.Requested.Version != "":
		return rec.Requested.Version
	case rec.Requested.Commit != "" || rec.Resolved.RefKind == string(resolver.RefKindCommit):
		return "commit:" + shortCommit(rec.Resolved.Commit)
	case rec.Resolved.RefKind == string(resolver.RefKindLocal):
		return labelLocal
	case rec.Resolved.RefKind == string(resolver.RefKindTag):
		if rec.Requested.Ref != "" {
			return "tag:" + rec.Requested.Ref
		}
		return "tag:" + rec.Resolved.Tag
	case rec.Resolved.RefKind == string(resolver.RefKindBranch):
		if rec.Requested.Ref != "" {
			return "branch:" + rec.Requested.Ref
		}
		return "branch:" + rec.Resolved.Branch
	default:
		return "latest"
	}
}
