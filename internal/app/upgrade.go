package app

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/Masterminds/semver/v3"

	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/fsutil"
	"github.com/glapsfun/gskill/internal/git"
	"github.com/glapsfun/gskill/internal/manifest"
	"github.com/glapsfun/gskill/internal/resolver"
	"github.com/glapsfun/gskill/internal/skillslock"
)

// UpgradeRequest configures an upgrade run (spec 024 US3).
type UpgradeRequest struct {
	Root    string
	Names   []string
	To      string
	Latest  bool
	Offline bool
	NoCache bool
	DryRun  bool
}

// UpgradeAction is what an upgrade plan decided for one skill.
type UpgradeAction string

// Plan actions.
const (
	UpgradeActionRewrite    UpgradeAction = "rewrite"     // the declaration moves
	UpgradeActionUpdateOnly UpgradeAction = "update-only" // candidate already satisfies the declaration
	UpgradeActionNone       UpgradeAction = "none"        // nothing newer exists
	UpgradeActionRefused    UpgradeAction = "refused"
)

// UpgradeOutcome is the result of applying one plan item.
type UpgradeOutcome string

// Outcomes.
const (
	UpgradeOutcomeUpgraded   UpgradeOutcome = "upgraded"
	UpgradeOutcomeDowngraded UpgradeOutcome = "downgraded"
	UpgradeOutcomeUpdated    UpgradeOutcome = "updated"
	UpgradeOutcomeRedeclared UpgradeOutcome = "redeclared" // the declaration moved; the resolved revision did not
	UpgradeOutcomeUnchanged  UpgradeOutcome = "unchanged"
	UpgradeOutcomeWould      UpgradeOutcome = "would upgrade"
	UpgradeOutcomeRefused    UpgradeOutcome = "refused"
	UpgradeOutcomeFailed     UpgradeOutcome = "failed"
)

// UpgradePlanItem is one skill's upgrade decision (data-model.md §4).
type UpgradePlanItem struct {
	Name        string
	Shape       resolver.DeclarationShape
	Current     string
	CurrentDecl string
	Candidate   resolver.Candidate
	Key         string
	NewDecl     string
	Action      UpgradeAction
	Reason      string
	Hint        string
}

// UpgradePlan is the name-sorted set of decisions for one run.
type UpgradePlan struct {
	Items []UpgradePlanItem
}

// Upgradable returns the items that would change something.
func (p UpgradePlan) Upgradable() []UpgradePlanItem {
	var out []UpgradePlanItem
	for _, it := range p.Items {
		if it.Action == UpgradeActionRewrite || it.Action == UpgradeActionUpdateOnly {
			out = append(out, it)
		}
	}
	return out
}

// UpgradeSkillResult is one applied item.
type UpgradeSkillResult struct {
	Name       string
	Shape      resolver.DeclarationShape
	From       string
	To         string
	DeclBefore string
	DeclAfter  string
	Action     UpgradeAction
	Outcome    UpgradeOutcome
	Reason     string
	Err        error
}

// UpgradeResult aggregates an upgrade run.
type UpgradeResult struct {
	Skills    []UpgradeSkillResult
	Upgraded  int
	Unchanged int
	Refused   int
	Failed    int
	Changed   bool
	DryRun    bool
}

// upgradeFailAfterManifestWrite is a test seam: when set, it runs after the
// manifest was rewritten and before resolution, to prove the rollback.
var upgradeFailAfterManifestWrite func() error

var hexCommit = regexp.MustCompile(`^[0-9a-f]{7,40}$`)

// PlanUpgrade decides, per skill, whether and how its declaration moves
// (R5). It is read-only and refuses before any write: a refusal for one
// skill refuses the run, so nothing is half-applied.
func (a *App) PlanUpgrade(ctx context.Context, req UpgradeRequest) (UpgradePlan, error) {
	ctx = git.WithMemo(ctx)
	p := openProject(req.Root)
	lf, err := loadOrNewLock(p.lockPath)
	if err != nil {
		return UpgradePlan{}, err
	}
	names, err := updateNames(lf, req.Names)
	if err != nil {
		return UpgradePlan{}, err
	}
	if req.To != "" && len(names) != 1 {
		return UpgradePlan{}, errs.New(errs.CodeUsage, "--to applies to exactly one skill")
	}
	var plan UpgradePlan
	for _, name := range names {
		plan.Items = append(plan.Items, a.planUpgradeOne(ctx, req, name, lf.Skills[name]))
		if err := ctx.Err(); err != nil {
			return UpgradePlan{}, err
		}
	}
	return plan, nil
}

func (a *App) planUpgradeOne(ctx context.Context, req UpgradeRequest, name string, rec skillslock.Record) UpgradePlanItem {
	decl, _ := a.declarationFor(req.Root, name, rec)
	rev := revFromLock(rec.Resolved)
	item := UpgradePlanItem{Name: name, Current: RevisionLabel(rev), CurrentDecl: declLabel(decl)}
	shape, err := resolver.ClassifyDeclaration(resolverDeclaration(decl, rec), rev.RefKind)
	if err != nil {
		return refuse(item, "invalid declaration: "+err.Error(), "fix the version in skills.toml")
	}
	if shape == resolver.ShapeUnpinned {
		shape = shapeFromRecord(rec)
	}
	item.Shape = shape
	if rec.Resolved.RefKind == string(resolver.RefKindCommit) {
		item.Current = shortCommit(rec.Resolved.Commit)
	}
	if req.Offline {
		return refuse(item, "upgrade needs the remote to discover releases", "drop --offline")
	}
	if _, _, rErr := resolver.RewriteDeclaration(shape, declValue(decl), resolver.Candidate{Version: "0", Tag: "t", Commit: "c"}); rErr != nil {
		return refuse(item, strings.TrimPrefix(rErr.Error(), resolver.ErrUnrewritable.Error()+": "), upgradeHint(shape, name))
	}

	tags, err := a.git.LsRemoteTags(ctx, refFromLock(rec.Source).URL)
	if err != nil {
		return refuse(item, "lookup failed: "+err.Error(), "check network access and the source URL")
	}
	candidate, found := a.upgradeCandidate(req, shape, decl, rec, tags)
	if !found {
		if req.To != "" {
			return refuse(item, "no release "+req.To+" in "+rec.Source.Original, "run `gskill update --list --all` to see what exists")
		}
		item.Action = UpgradeActionNone
		item.Reason = "already the newest release"
		return item
	}
	item.Candidate = candidate
	// An explicit target is written verbatim even inside the range (FR-012);
	// only --latest degrades to a plain update when nothing needs to move.
	if req.To == "" && shape.Floating() && satisfies(decl.Version, candidate.Version) {
		item.Action = UpgradeActionUpdateOnly
		item.Reason = candidate.Version + " already satisfies " + decl.Version + "; only the lock moves"
		return item
	}
	key, value, err := resolver.RewriteDeclaration(shape, declValue(decl), candidate)
	if err != nil {
		return refuse(item, err.Error(), upgradeHint(shape, name))
	}
	item.Key, item.NewDecl = key, key+` = "`+value+`"`
	item.Action = UpgradeActionRewrite
	return item
}

// upgradeCandidate finds the release an item moves to: the explicit --to
// target, or the newest release beyond the current one.
func (a *App) upgradeCandidate(req UpgradeRequest, shape resolver.DeclarationShape, decl manifest.Skill, rec skillslock.Record, tags []git.TagRef) (resolver.Candidate, bool) {
	if req.To != "" {
		if c, ok := resolver.FindRelease(tags, req.To); ok {
			return c, true
		}
		if shape == resolver.ShapeCommit && hexCommit.MatchString(req.To) {
			return resolver.Candidate{Commit: req.To}, true
		}
		return resolver.Candidate{}, false
	}
	current := rec.Resolved.Version
	if shape == resolver.ShapeCommit {
		current = ""
	}
	return resolver.NewestBeyond(tags, current, decl.Version)
}

func satisfies(constraint, version string) bool {
	c, err := semver.NewConstraint(constraint)
	if err != nil {
		return false
	}
	v, err := semver.NewVersion(version)
	if err != nil {
		return false
	}
	return c.Check(v)
}

func refuse(item UpgradePlanItem, reason, hint string) UpgradePlanItem {
	item.Action, item.Reason, item.Hint = UpgradeActionRefused, reason, hint
	return item
}

func upgradeHint(shape resolver.DeclarationShape, name string) string {
	switch shape {
	case resolver.ShapeBranch:
		return "run `gskill update " + name + "` to advance to the branch head"
	case resolver.ShapeLocal, resolver.ShapeUnpinned, resolver.ShapeRangeOther:
		return "edit skills.toml, then run `gskill install`"
	case resolver.ShapeCommit:
		return "pass --to <commit>"
	case resolver.ShapeRangeCaret, resolver.ShapeRangeTilde, resolver.ShapeExactVersion, resolver.ShapeTag:
		return ""
	default:
		return ""
	}
}

// declValue is the current spelling of the declaration's pin.
func declValue(decl manifest.Skill) string {
	switch {
	case decl.Commit != "":
		return decl.Commit
	case decl.Version != "":
		return decl.Version
	default:
		return decl.Ref
	}
}

// declLabel renders a declaration's pin as it appears in skills.toml.
func declLabel(decl manifest.Skill) string {
	switch {
	case decl.Commit != "":
		return `commit = "` + shortCommit(decl.Commit) + `"`
	case decl.Version != "":
		return `version = "` + decl.Version + `"`
	case decl.Ref != "":
		return `ref = "` + decl.Ref + `"`
	default:
		return "(unpinned)"
	}
}

// Upgrade rewrites the selected declarations and realizes them: manifest,
// then resolve, lock, install, verify (R5). The run is atomic across all four
// layers (FR-014): on any error, panic, or cancellation after the first
// write, skills.toml and skills-lock.json are restored from a byte snapshot
// and the installed content is reconciled back from the restored lock (R7).
func (a *App) Upgrade(ctx context.Context, req UpgradeRequest) (UpgradeResult, error) {
	ctx = git.WithMemo(ctx)
	plan, err := a.PlanUpgrade(ctx, req)
	if err != nil {
		return UpgradeResult{}, err
	}
	out := UpgradeResult{DryRun: req.DryRun}
	for _, it := range plan.Items {
		if it.Action == UpgradeActionRefused {
			out.Skills = append(out.Skills, resultFromPlan(it, UpgradeOutcomeRefused))
			out.Refused++
		}
	}
	if out.Refused > 0 {
		first := out.Skills[0]
		return out, errs.WithHint(errs.New(errs.CodeUsage, first.Name+": "+first.Reason), refusedHint(plan))
	}
	if req.DryRun {
		for _, it := range plan.Items {
			out.Skills = append(out.Skills, dryRunResult(it))
		}
		countUpgradeOutcomes(&out)
		return out, nil
	}
	p, err := a.openProjectScoped(req.Root)
	if err != nil {
		return UpgradeResult{}, err
	}
	err = a.withLock(ctx, p, func() error {
		return a.runUpgrade(ctx, p, req, plan, &out)
	})
	countUpgradeOutcomes(&out)
	return out, err
}

func refusedHint(plan UpgradePlan) string {
	for _, it := range plan.Items {
		if it.Action == UpgradeActionRefused && it.Hint != "" {
			return it.Hint
		}
	}
	return ""
}

func resultFromPlan(it UpgradePlanItem, outcome UpgradeOutcome) UpgradeSkillResult {
	return UpgradeSkillResult{
		Name: it.Name, Shape: it.Shape, From: it.Current, To: it.Current,
		DeclBefore: it.CurrentDecl, DeclAfter: it.CurrentDecl,
		Action: it.Action, Outcome: outcome, Reason: it.Reason,
	}
}

func dryRunResult(it UpgradePlanItem) UpgradeSkillResult {
	res := resultFromPlan(it, UpgradeOutcomeUnchanged)
	switch it.Action {
	case UpgradeActionRewrite:
		res.To, res.DeclAfter, res.Outcome = candidateLabel(it), it.NewDecl, UpgradeOutcomeWould
	case UpgradeActionUpdateOnly:
		res.To, res.Outcome = candidateLabel(it), UpgradeOutcomeWould
	case UpgradeActionNone, UpgradeActionRefused:
	}
	return res
}

func candidateLabel(it UpgradePlanItem) string {
	if it.Candidate.Version != "" {
		return it.Candidate.Version
	}
	if it.Candidate.Tag != "" {
		return it.Candidate.Tag
	}
	return shortCommit(it.Candidate.Commit)
}

// upgradeSnapshot is the pre-run state the rollback restores.
type upgradeSnapshot struct {
	manifest []byte
	lock     []byte
	state    *skillslock.State
}

func (a *App) snapshot(p *project) (upgradeSnapshot, error) {
	m, err := os.ReadFile(manifestPath(p.root))
	if err != nil {
		return upgradeSnapshot{}, err
	}
	l, err := os.ReadFile(p.lockPath)
	if err != nil {
		return upgradeSnapshot{}, err
	}
	st, err := loadOrNewLock(p.lockPath)
	if err != nil {
		return upgradeSnapshot{}, err
	}
	return upgradeSnapshot{manifest: m, lock: l, state: st}, nil
}

func (a *App) runUpgrade(ctx context.Context, p *project, req UpgradeRequest, plan UpgradePlan, out *UpgradeResult) (err error) {
	lf, err := loadOrNewLock(p.lockPath)
	if err != nil {
		return err
	}
	if mErr := a.autoMigrate(ctx, p, lf, migrateRunOptions{offline: req.Offline}); mErr != nil {
		return mErr
	}
	snap, err := a.snapshot(p)
	if err != nil {
		return err
	}
	var touched []string
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("upgrade panicked: %v", r)
		}
		if err == nil {
			return
		}
		a.rollbackUpgrade(p, snap, touched)
	}()

	for _, it := range plan.Items {
		if it.Action == UpgradeActionNone {
			out.Skills = append(out.Skills, resultFromPlan(it, UpgradeOutcomeUnchanged))
			continue
		}
		touched = append(touched, it.Name)
		res, applyErr := a.applyUpgrade(ctx, p, lf, it, req)
		out.Skills = append(out.Skills, res)
		if applyErr != nil {
			return applyErr
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return errs.WithHint(errs.ErrCancelled, "the upgrade was rolled back; nothing changed")
		}
	}
	if err := saveLock(p.lockPath, lf); err != nil {
		return err
	}
	if err := a.verifyUpgraded(ctx, p, touched); err != nil {
		return err
	}
	a.recordProjectState(ctx, p, lf)
	return nil
}

// applyUpgrade rewrites one declaration and realizes it.
func (a *App) applyUpgrade(ctx context.Context, p *project, lf *skillslock.State, it UpgradePlanItem, req UpgradeRequest) (UpgradeSkillResult, error) {
	res := resultFromPlan(it, UpgradeOutcomeFailed)
	rec := lf.Skills[it.Name]
	if it.Action == UpgradeActionRewrite {
		value := strings.TrimSuffix(strings.TrimPrefix(it.NewDecl, it.Key+` = "`), `"`)
		if err := manifest.SetKey(manifestPath(p.root), it.Name, it.Key, value); err != nil {
			res.Reason, res.Err = err.Error(), err
			return res, err
		}
		a.invalidateManifest(p.root)
		res.DeclAfter = it.NewDecl
	}
	if upgradeFailAfterManifestWrite != nil {
		if err := upgradeFailAfterManifestWrite(); err != nil {
			res.Reason, res.Err = err.Error(), err
			return res, err
		}
	}
	pauseBeforeActivate(ctx)
	decl, _ := a.declarationFor(p.root, it.Name, rec)
	before := rec.Resolved.Version
	if _, err := a.installOne(ctx, p, lf, it.Name, intentFromDeclaration(decl, rec), InstallRequest{
		Root: p.root, Offline: req.Offline, NoCache: req.NoCache,
	}); err != nil {
		res.Reason, res.Err = err.Error(), err
		return res, err
	}
	after := lf.Skills[it.Name].Resolved
	res.To = updatedRevLabel(after)
	res.Outcome = upgradeOutcome(it, before, after.Version)
	res.Reason = ""
	return res, nil
}

func upgradeOutcome(it UpgradePlanItem, before, after string) UpgradeOutcome {
	if it.Action == UpgradeActionUpdateOnly {
		return UpgradeOutcomeUpdated
	}
	if before == after {
		return UpgradeOutcomeRedeclared
	}
	b, bErr := semver.NewVersion(before)
	a, aErr := semver.NewVersion(after)
	if bErr == nil && aErr == nil && a.LessThan(b) {
		return UpgradeOutcomeDowngraded
	}
	return UpgradeOutcomeUpgraded
}

// verifyUpgraded re-hashes the touched skills against the lock just written.
func (a *App) verifyUpgraded(ctx context.Context, p *project, names []string) error {
	report, err := a.Verify(ctx, p.root)
	if err != nil {
		return err
	}
	for _, s := range report.Skills {
		for _, n := range names {
			if s.Name == n && !s.OK {
				return fmt.Errorf("%w: %s failed verification after upgrade: %s", errs.ErrIntegrity, n, s.Issue)
			}
		}
	}
	return nil
}

// rollbackUpgrade restores both files from the snapshot and re-materializes
// every touched skill from the restored lock.
func (a *App) rollbackUpgrade(p *project, snap upgradeSnapshot, touched []string) {
	if wErr := fsutil.WriteFileAtomic(manifestPath(p.root), snap.manifest, 0o644); wErr != nil {
		a.Logger().Warn("upgrade rollback: restore manifest", "error", wErr)
	}
	if wErr := fsutil.WriteFileAtomic(p.lockPath, snap.lock, 0o644); wErr != nil {
		a.Logger().Warn("upgrade rollback: restore lock", "error", wErr)
	}
	a.invalidateManifest(p.root)
	ctx := context.Background()
	for _, name := range touched {
		locked, ok := snap.state.Skills[name]
		if !ok {
			continue
		}
		agents, err := a.agentsByID(locked.Installation.Agents)
		if err != nil {
			a.Logger().Warn("upgrade rollback: agents", "skill", name, "error", err)
			continue
		}
		if _, rErr := a.reconcileFromLock(ctx, p, name, locked, agents, SyncRequest{Root: p.root, Offline: true}, false); rErr != nil {
			a.Logger().Warn("upgrade rollback: reconcile", "skill", name, "error", rErr)
		}
	}
}

func countUpgradeOutcomes(out *UpgradeResult) {
	out.Upgraded, out.Unchanged, out.Refused, out.Failed = 0, 0, 0, 0
	for _, s := range out.Skills {
		switch s.Outcome {
		case UpgradeOutcomeUpgraded, UpgradeOutcomeDowngraded, UpgradeOutcomeUpdated, UpgradeOutcomeRedeclared, UpgradeOutcomeWould:
			out.Upgraded++
		case UpgradeOutcomeUnchanged:
			out.Unchanged++
		case UpgradeOutcomeRefused:
			out.Refused++
		case UpgradeOutcomeFailed:
			out.Failed++
		}
	}
	out.Changed = !out.DryRun && out.Upgraded > 0 && out.Failed == 0
}

// DeclaredSkillNames lists the names a project declares, for argument
// disambiguation in the CLI.
func (a *App) DeclaredSkillNames(root string) []string {
	p := openProject(root)
	lf, err := loadOrNewLock(p.lockPath)
	if err != nil {
		return nil
	}
	names := sortedKeys(lf.Skills)
	if m, mErr := a.loadManifest(root); mErr == nil && m != nil {
		for n := range m.Skills {
			if _, ok := lf.Skills[n]; !ok {
				names = append(names, n)
			}
		}
	}
	return names
}

// SetUpgradeFailureSeam installs a failure injected after the manifest write
// and returns a function that removes it. Tests use it to prove the rollback.
func SetUpgradeFailureSeam(fn func() error) func() {
	upgradeFailAfterManifestWrite = fn
	return func() { upgradeFailAfterManifestWrite = nil }
}
