package app

import (
	"context"
	"strings"

	"github.com/glapsfun/gskill/internal/git"
	"github.com/glapsfun/gskill/internal/manifest"
	"github.com/glapsfun/gskill/internal/resolver"
	"github.com/glapsfun/gskill/internal/skillslock"
)

// UpdateStatus classifies one skill's update eligibility in an update plan
// (spec 018 FR-003). Values are stable: they appear in JSON output.
type UpdateStatus string

// Plan item statuses (spec 024 FR-006). Only StatusUpdateAvailable is
// actionable by a normal update. StatusLookupFailed records a skill whose
// eligibility could not be determined — a per-skill discovery failure, or an
// offline run for a floating declaration — without failing the whole plan.
const (
	StatusUpdateAvailable    UpdateStatus = UpdateStatus(resolver.StatusUpdateAvailable)
	StatusUpToDate           UpdateStatus = UpdateStatus(resolver.StatusUpToDate)
	StatusPinnedVersion      UpdateStatus = UpdateStatus(resolver.StatusPinnedVersion)
	StatusPinnedTag          UpdateStatus = UpdateStatus(resolver.StatusPinnedTag)
	StatusPinnedCommit       UpdateStatus = UpdateStatus(resolver.StatusPinnedCommit)
	StatusLocalSource        UpdateStatus = UpdateStatus(resolver.StatusLocalSource)
	StatusNoCompatibleUpdate UpdateStatus = UpdateStatus(resolver.StatusNoCompatibleUpdate)
	StatusLookupFailed       UpdateStatus = UpdateStatus(resolver.StatusLookupFailed)
)

// Pinned reports whether the status means only a change of declared intent
// can move the skill.
func (s UpdateStatus) Pinned() bool {
	return s == StatusPinnedVersion || s == StatusPinnedTag || s == StatusPinnedCommit
}

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
	// Shape is the declaration's tracking kind; Pinned mirrors Status.Pinned
	// for consumers; NextAction names the command that moves a skill a
	// normal update cannot, empty when nothing needs doing (spec 024 FR-008).
	Shape      resolver.DeclarationShape
	Pinned     bool
	NextAction string
	// DiscoveryErr carries a per-skill discovery failure. It distinguishes a
	// real error (Status is StatusLookupFailed) from an offline skip, and a
	// pin whose informational lookup failed keeps its pinned status.
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

// planOne classifies a single skill from its declaration (spec 024 FR-004):
// the manifest entry when the project has one, else the projection a manifest
// would be generated from.
func (a *App) planOne(ctx context.Context, name string, rec skillslock.Record, req UpdatePlanRequest) UpdatePlanItem {
	decl, declared := a.declarationFor(req.Root, name, rec)
	item := UpdatePlanItem{
		Name:   name,
		Source: rec.Source.Original,
		Policy: policyLabel(declaredRequested(decl), rec),
	}
	rev := revFromLock(rec.Resolved)
	shape, err := resolver.ClassifyDeclaration(resolverDeclaration(decl, rec), rev.RefKind)
	if err != nil {
		item.Current = RevisionLabel(rev)
		item.Status = StatusLookupFailed
		item.Reason = "invalid declaration: " + err.Error()
		item.DiscoveryErr = err.Error()
		return item
	}
	if shape == resolver.ShapeUnpinned {
		shape = shapeFromRecord(rec)
	}
	item.Shape = shape

	if req.Offline {
		return finishPlanItem(planOffline(item, shape, rev), name, decl, declared, rec)
	}

	res, err := resolver.OutdatedShaped(ctx, a.git, refFromLock(rec.Source), shape, declaredRequested(decl), rev)
	if err != nil {
		item.Current = RevisionLabel(rev)
		item.Status = StatusLookupFailed
		item.Reason = "discovery failed: " + err.Error()
		item.DiscoveryErr = err.Error()
		return finishPlanItem(item, name, decl, declared, rec)
	}

	item.Status = UpdateStatus(res.Status)
	item.Current = res.Current
	item.Informational = res.Informational
	switch {
	case res.Available():
		item.Candidate = res.Latest
	case item.Status == StatusLookupFailed:
		item.Reason = "discovery failed: " + res.LookupErr.Error()
		item.DiscoveryErr = res.LookupErr.Error()
	default:
		item.Reason = notActionableReason(item.Status)
		if res.LookupErr != nil {
			// A pin's eligibility never depended on the lookup; only its
			// newest-release hint is missing.
			item.Reason += " (newest-tag lookup failed: " + res.LookupErr.Error() + ")"
			item.DiscoveryErr = res.LookupErr.Error()
		}
	}
	if item.Status == StatusPinnedCommit {
		item.Current = shortCommit(rev.Commit)
	}
	return finishPlanItem(item, name, decl, declared, rec)
}

// finishPlanItem fills the derived fields every consumer reads: Pinned,
// NextAction, and the declaration-changed reason for a manifest edit that no
// install has applied yet.
func finishPlanItem(item UpdatePlanItem, name string, decl manifest.Skill, declared bool, rec skillslock.Record) UpdatePlanItem {
	item.Pinned = item.Status.Pinned()
	switch {
	case item.Pinned:
		item.NextAction = "gskill upgrade " + name
		item.Reason = pinnedReason(decl, rec)
		if item.DiscoveryErr != "" {
			item.Reason += " (newest-tag lookup failed: " + item.DiscoveryErr + ")"
		}
	case item.Status == StatusNoCompatibleUpdate:
		item.NextAction = "gskill upgrade " + name
	}
	if !declared || declaredRequested(decl) == recordedRequested(rec) {
		return item
	}
	note := "declaration changed in skills.toml; install or update will apply it"
	if item.Pinned && !pinSatisfied(decl, rec) {
		// The lock holds a revision the new pin no longer selects: the move
		// is exactly what the user asked for, so it is actionable now.
		item.Status = StatusUpdateAvailable
		item.Pinned = false
		item.Candidate = declaredPinLabel(decl)
		item.Informational = ""
		item.Reason = note
		item.NextAction = ""
		return item
	}
	item.NextAction = "gskill install"
	if item.Reason == "" {
		item.Reason = note
	} else {
		item.Reason += "; " + note
	}
	return item
}

// pinnedReason names the declaration that pins a skill, so the user sees
// which line of skills.toml holds it in place (spec 024 FR-008).
func pinnedReason(decl manifest.Skill, rec skillslock.Record) string {
	switch {
	case decl.Commit != "":
		return `pinned by skills.toml (commit = "` + shortCommit(decl.Commit) + `")`
	case decl.Version != "":
		return `pinned by skills.toml (version = "` + decl.Version + `")`
	case decl.Ref != "":
		return `pinned by skills.toml (ref = "` + decl.Ref + `")`
	case rec.Resolved.Commit != "":
		return `pinned to commit ` + shortCommit(rec.Resolved.Commit)
	default:
		return "pinned"
	}
}

// pinSatisfied reports whether the locked revision already is what the
// declared pin selects, so only the lock's intent projection is stale.
func pinSatisfied(decl manifest.Skill, rec skillslock.Record) bool {
	switch {
	case decl.Commit != "":
		return strings.HasPrefix(rec.Resolved.Commit, decl.Commit)
	case decl.Version != "":
		return strings.TrimLeft(decl.Version, "=v") == rec.Resolved.Version
	case decl.Ref != "":
		return decl.Ref == rec.Resolved.Tag || decl.Ref == rec.Resolved.Branch
	default:
		return true
	}
}

// declaredPinLabel names the revision a pinned declaration selects.
func declaredPinLabel(decl manifest.Skill) string {
	switch {
	case decl.Commit != "":
		return shortCommit(decl.Commit)
	case decl.Version != "":
		return strings.TrimLeft(decl.Version, "=v")
	default:
		return decl.Ref
	}
}

// declaredRequested maps a declaration onto the resolver's request.
func declaredRequested(decl manifest.Skill) resolver.Requested {
	return resolver.Requested{Version: decl.Version, Ref: decl.Ref, Commit: decl.Commit}
}

// recordedRequested is the intent projection the lock carries.
func recordedRequested(rec skillslock.Record) resolver.Requested {
	return resolver.Requested{Version: rec.Requested.Version, Ref: rec.Requested.Ref, Commit: rec.Requested.Commit}
}

// shapeFromRecord infers a shape from what an intent-less entry resolved to.
func shapeFromRecord(rec skillslock.Record) resolver.DeclarationShape {
	switch resolver.RefKind(rec.Resolved.RefKind) {
	case resolver.RefKindCommit:
		return resolver.ShapeCommit
	case resolver.RefKindTag:
		return resolver.ShapeTag
	case resolver.RefKindBranch:
		return resolver.ShapeBranch
	case resolver.RefKindLocal:
		return resolver.ShapeLocal
	case resolver.RefKindSemver:
		return resolver.ShapeUnpinned
	default:
		return resolver.ShapeUnpinned
	}
}

// labelLocal is the display label for local sources (current revision and
// tracking policy alike).
const labelLocal = "local"

// planOffline classifies without any remote lookup: pins and local sources
// are fully determined by the declaration; floating declarations honestly
// report that they were not checked instead of guessing (FR-014).
func planOffline(item UpdatePlanItem, shape resolver.DeclarationShape, rev resolver.Revision) UpdatePlanItem {
	item.Current = RevisionLabel(rev)
	switch shape {
	case resolver.ShapeCommit:
		item.Status = StatusPinnedCommit
		item.Current = shortCommit(rev.Commit)
	case resolver.ShapeTag:
		item.Status = StatusPinnedTag
	case resolver.ShapeExactVersion:
		item.Status = StatusPinnedVersion
	case resolver.ShapeLocal:
		item.Status = StatusLocalSource
		item.Current = labelLocal
	case resolver.ShapeRangeCaret, resolver.ShapeRangeTilde, resolver.ShapeRangeOther,
		resolver.ShapeBranch, resolver.ShapeUnpinned:
		item.Status = StatusLookupFailed
		item.Reason = "offline mode: remote lookup skipped"
		return item
	default:
		item.Status = StatusLookupFailed
		item.Reason = "offline mode: remote lookup skipped"
		return item
	}
	item.Reason = notActionableReason(item.Status)
	return item
}

// notActionableReason is the human-meaningful explanation attached to every
// non-actionable item (FR-001).
func notActionableReason(s UpdateStatus) string {
	switch s {
	case StatusUpToDate:
		return "already the newest revision the policy allows"
	case StatusPinnedVersion:
		return "pinned to an exact version; not changed by a normal update"
	case StatusPinnedTag:
		return "pinned to an exact tag; not changed by a normal update"
	case StatusPinnedCommit:
		return "pinned to an exact commit; not changed by a normal update"
	case StatusLocalSource:
		return "local source has no remote update candidate"
	case StatusNoCompatibleUpdate:
		return "newer releases exist outside the version constraint"
	case StatusUpdateAvailable, StatusLookupFailed:
		return "" // actionable items and discovery failures carry their own text
	default:
		return ""
	}
}

// policyLabel renders the declared tracking policy for display.
func policyLabel(req resolver.Requested, rec skillslock.Record) string {
	switch {
	case req.Version != "":
		return req.Version
	case req.Commit != "" || rec.Resolved.RefKind == string(resolver.RefKindCommit):
		return "commit:" + shortCommit(rec.Resolved.Commit)
	case rec.Resolved.RefKind == string(resolver.RefKindLocal):
		return labelLocal
	case rec.Resolved.RefKind == string(resolver.RefKindTag):
		if req.Ref != "" {
			return "tag:" + req.Ref
		}
		return "tag:" + rec.Resolved.Tag
	case rec.Resolved.RefKind == string(resolver.RefKindBranch):
		if req.Ref != "" {
			return "branch:" + req.Ref
		}
		return "branch:" + rec.Resolved.Branch
	default:
		return "latest"
	}
}
