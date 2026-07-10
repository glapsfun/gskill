package app

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/discovery"
	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/installer"
	"github.com/glapsfun/gskill/internal/integrity"

	"github.com/glapsfun/gskill/internal/resolver"
	"github.com/glapsfun/gskill/internal/skillslock"
	"github.com/glapsfun/gskill/internal/source"
)

// InstallFromLockRequest describes an install (spec 012 US1/US2): restore
// every skill declared in skills-lock.json for its declared agents, unioned
// with any explicit --agent override.
type InstallFromLockRequest struct {
	Root        string
	Agents      []string // explicit agent IDs; unioned into each entry's declared set
	InstallMode string   // auto | symlink | copy ("" = per-entry gskill.installMode)
	NoInit      bool     // refuse instead of auto-initializing
	Force       bool     // accept changed upstream content, rewrite computedHash
	DryRun      bool     // report the plan, write nothing
	Offline     bool     // restore from local store/cache only
	Frozen      bool     // never modify the lock file; fail closed on drift
	Prune       bool     // afterwards, remove managed installs the lock no longer declares
}

// sourceTypeGitHub is the shared lock's GitHub sourceType value.
const sourceTypeGitHub = "github"

// Lock-install per-skill statuses (contracts/cli-install-migrate.md).
const (
	LockSkillInstalled = "installed"
	LockSkillUpToDate  = "up-to-date"
	LockSkillRepaired  = "repaired"
	LockSkillFailed    = "failed"
	LockSkillPlanned   = "planned" // dry-run only
)

// LockSkillResult is one skill's outcome in an InstallFromLock run.
type LockSkillResult struct {
	Name         string
	Source       string
	Status       string
	ComputedHash string
	Err          error
}

// InstallFromLockResult is the run summary.
type InstallFromLockResult struct {
	Initialized bool
	Agents      []string
	Skills      []LockSkillResult
	Pruned      []string
	Changed     bool
}

// InstallFromLock implements the install pipeline: locate and validate
// skills-lock.json, auto-initialize local state (FR-019/FR-020), then per
// entry resolve, verify the npx-compatible computedHash before activation,
// install for the entry's declared agents (unioned with any --agent
// override), and record the namespaced gskill metadata (FR-016). Failures are
// isolated per skill: verified successes stay installed and recorded
// (FR-016a).
func (a *App) InstallFromLock(ctx context.Context, req InstallFromLockRequest) (InstallFromLockResult, error) {
	p := openProject(req.Root)
	var res InstallFromLockResult

	l, err := a.loadSharedLock(p)
	if err != nil {
		return res, err
	}

	initialized, err := a.ensureLocalState(ctx, p, req)
	if err != nil {
		return res, err
	}
	res.Initialized = initialized

	if err := checkFrozenAgents(l, req); err != nil {
		return res, err
	}
	res.Agents = runAgents(l, req.Agents)

	installErr := a.withLock(ctx, p, func() error {
		lf, err := a.installAllLockEntries(ctx, p, l, req, &res)
		if err != nil {
			return err
		}
		if req.Prune && !req.DryRun && !req.Frozen {
			pruned, pErr := a.pruneToDesired(p, lf)
			if pErr != nil {
				return pErr
			}
			res.Pruned = pruned
			res.Changed = res.Changed || len(pruned) > 0
		}
		return nil
	})
	return res, installErr
}

// entryAgents returns an entry's declared gskill agents (nil for raw entries).
func entryAgents(e skillslock.Entry) []string {
	if e.Ext == nil {
		return nil
	}
	return e.Ext.Agents
}

// runAgents reports the run's overall agent set: the explicit override plus
// every declared per-entry agent.
func runAgents(l *skillslock.Lock, explicit []string) []string {
	out := append([]string(nil), explicit...)
	seen := make(map[string]bool, len(out))
	for _, id := range out {
		seen[id] = true
	}
	for _, name := range l.Names() {
		e, _ := l.Entry(name)
		for _, id := range entryAgents(e) {
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	return out
}

// checkFrozenAgents rejects an explicit --agent override that contradicts the
// locked metadata under --frozen-lockfile: the flags demand a state the lock
// does not declare, so the whole run fails before anything is touched.
// Per-entry agent problems (raw entries, empty declared sets) are handled with
// per-skill failure isolation in installOneLockEntry instead.
func checkFrozenAgents(l *skillslock.Lock, req InstallFromLockRequest) error {
	if !req.Frozen || len(req.Agents) == 0 {
		return nil
	}
	for _, name := range sortedLockNames(l) {
		e, _ := l.Entry(name)
		if e.Ext == nil {
			continue // reported per-skill during the run
		}
		if extra := subtract(req.Agents, entryAgents(e)); len(extra) > 0 {
			return fmt.Errorf("%w: --agent %s conflicts with skill %q's locked agents (%s) under --frozen-lockfile",
				errs.ErrLockMismatch, strings.Join(extra, ","), name, strings.Join(entryAgents(e), ","))
		}
	}
	return nil
}

// installAllLockEntries runs the per-entry pipeline over every lock entry,
// aggregating per-skill outcomes into partial-failure semantics (FR-016a):
// mixed results return ErrPartialInstall, total failure returns the first
// cause, and successes are persisted either way.
func (a *App) installAllLockEntries(ctx context.Context, p *project, l *skillslock.Lock, req InstallFromLockRequest, res *InstallFromLockResult) (*skillslock.State, error) {
	lf, err := loadOrNewLock(p.lockPath)
	if err != nil {
		return nil, err
	}
	var failures, healthy int
	var firstErr error
	names := sortedLockNames(l)
	for k, name := range names {
		e, _ := l.Entry(name)
		sctx := stampSkill(ctx, name, k+1, len(names))
		r := a.installOneLockEntry(sctx, p, lf, name, e, req)
		res.Skills = append(res.Skills, r)
		switch {
		case r.Err != nil:
			failures++
			if firstErr == nil {
				firstErr = r.Err
			}
		default:
			healthy++
			if r.Status == LockSkillInstalled || r.Status == LockSkillRepaired {
				res.Changed = true
			}
		}
	}
	if !req.DryRun && !req.Frozen {
		if saveErr := saveLock(p.lockPath, lf); saveErr != nil {
			return nil, saveErr
		}
	}
	switch {
	case failures > 0 && healthy > 0:
		return lf, fmt.Errorf("%w: %d of %d skills failed",
			errs.ErrPartialInstall, failures, failures+healthy)
	case failures > 0:
		return lf, firstErr
	default:
		return lf, nil
	}
}

// LockPreview describes the shared lock for the interactive install flow.
type LockPreview struct {
	Path   string
	Skills []LockPreviewSkill
}

// LockPreviewSkill is one entry's display line.
type LockPreviewSkill struct {
	Name   string
	Source string
}

// PreviewLock loads and validates the shared lock for display. found=false
// (with a nil error) means the project has no skills-lock.json.
func (a *App) PreviewLock(root string) (LockPreview, bool, error) {
	p := openProject(root)
	if _, err := os.Stat(p.lockPath); err != nil {
		if os.IsNotExist(err) {
			return LockPreview{}, false, nil
		}
		return LockPreview{}, false, fmt.Errorf("stat %s: %w", skillslock.FileName, err)
	}
	l, err := a.loadSharedLock(p)
	if err != nil {
		return LockPreview{}, true, err
	}
	pv := LockPreview{Path: skillslock.FileName}
	for _, name := range l.Names() {
		e, _ := l.Entry(name)
		pv.Skills = append(pv.Skills, LockPreviewSkill{Name: name, Source: e.Source})
	}
	return pv, true, nil
}

// loadSharedLock loads and validates the shared lock, failing with a clear
// diagnostic when it is missing, unparsable, or structurally invalid (FR-002,
// FR-030).
func (a *App) loadSharedLock(p *project) (*skillslock.Lock, error) {
	if _, err := os.Stat(p.lockPath); err != nil {
		if os.IsNotExist(err) {
			return nil, errNoLock()
		}
		return nil, fmt.Errorf("stat %s: %w", skillslock.FileName, err)
	}
	l, err := skillslock.Load(p.lockPath)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errs.ErrInvalidLock, err)
	}
	if err := l.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %w", errs.ErrInvalidLock, err)
	}
	return l, nil
}

// ensureLocalState auto-initializes missing local runtime state (FR-019).
// Init only creates what is absent, so nothing existing is overwritten
// (FR-020).
func (a *App) ensureLocalState(ctx context.Context, p *project, req InstallFromLockRequest) (bool, error) {
	if fileExists(filepath.Join(p.root, stateDirName)) {
		return false, nil
	}
	if req.NoInit {
		return false, errs.WithHint(
			fmt.Errorf("%w: project is not initialized and --no-init is set", errs.ErrInvalidLock),
			"drop --no-init or run 'gskill init' first")
	}
	if req.DryRun {
		return true, nil
	}
	if _, err := a.Init(ctx, req.Root, false); err != nil {
		return false, err
	}
	return true, nil
}

// skillDirOf returns the skill directory recorded by skillPath ("" for a
// repo-root skill). Backslash separators (a Windows-authored lock) are
// normalized first — validSkillPath accepts them, so resolution must too.
func skillDirOf(skillPath string) string {
	d := path.Dir(strings.ReplaceAll(skillPath, "\\", "/"))
	if d == "." || d == "/" {
		return ""
	}
	return d
}

// sortedLockNames returns the lock's entry names sorted for deterministic
// processing order.
func sortedLockNames(l *skillslock.Lock) []string {
	names := l.Names()
	sort.Strings(names)
	return names
}

// lockEntrySourceTypes are the sourceType values this build installs from.
var lockEntrySourceTypes = map[string]bool{sourceTypeGitHub: true, "git": true, "local": true}

// installLockEntry restores one lock entry: resolve, verify the compatible
// computedHash against the fetched content BEFORE any activation (fail closed,
// FR-018a), install for the selected agents, and stage the lock record. All
// failures are reported on the result, never returned, so one bad skill cannot
// take down its siblings (FR-016a).
func (a *App) installOneLockEntry(ctx context.Context, p *project, lf *skillslock.State, name string, e skillslock.Entry, req InstallFromLockRequest) LockSkillResult {
	r := LockSkillResult{Name: name, Source: e.Source, ComputedHash: e.ComputedHash, Status: LockSkillFailed}
	fail := func(err error) LockSkillResult {
		r.Err = fmt.Errorf("skill %q: %w", name, err)
		return r
	}

	if !lockEntrySourceTypes[e.SourceType] {
		return fail(fmt.Errorf("%w: unsupported sourceType %q (supported: github, git, local)",
			errs.ErrInvalidLock, e.SourceType))
	}

	agents, done := a.lockEntryTargets(&r, e, req)
	if done {
		return r
	}

	// Idempotency fast path (FR-017): recorded state matches the lock and the
	// store — skip downloads and store writes, repair only missing links, and
	// leave the entry (and therefore the lock file) untouched.
	if r2, handled := a.lockEntryUpToDate(ctx, p, lf, name, e, agents, req); handled {
		return r2
	}

	staged, err := a.stageAndVerifyLockEntry(ctx, p, lf, name, e, req)
	if err != nil {
		return fail(err)
	}

	if req.DryRun {
		r.Status = LockSkillPlanned
		r.Err = nil
		return r
	}

	staged.ireq.Agents = agents
	// Never silently replace content gskill does not own (§13): the previously
	// recorded hash marks managed copy-mode installs as gskill's own; anything
	// else fails closed until --force approves the overwrite.
	staged.ireq.PreserveForeign = !req.Force
	if prior, ok := lf.Skills[name]; ok {
		staged.ireq.PriorContentHash = prior.Resolved.ContentHash
	}
	if req.Frozen && e.Ext != nil && e.Ext.StoreHash != "" {
		// Frozen means restore exactly: even when the entry carries no
		// computedHash to verify (legal for enriched entries), the recorded
		// store hash must still match what gets activated.
		staged.ireq.ExpectContentHash = e.Ext.StoreHash
	}
	result, err := a.installerForScope(p, string(staged.ireq.Scope)).Install(ctx, staged.ireq)
	if err != nil {
		return fail(err)
	}

	ls, err := buildLockEntry(staged.ref, staged.rev, staged.ireq, result,
		requestedForEntry(lf, name, e, staged.rev))
	if err != nil {
		return fail(err)
	}
	ls.Resolved.CompatHash = staged.compat
	lf.Skills[name] = ls

	r.ComputedHash = staged.compat
	r.Status = LockSkillInstalled
	r.Err = nil
	return r
}

// lockEntryTargets resolves the agents one entry installs for — the declared
// gskill.agents unioned with the explicit override (an explicit --agent is
// additive; nothing is ever uninstalled by an install). done=true means the
// entry's processing ends here: r already carries the per-skill outcome
// (frozen raw entry, agentless managed skip, raw entry with no selection, or
// an unknown agent).
func (a *App) lockEntryTargets(r *LockSkillResult, e skillslock.Entry, req InstallFromLockRequest) ([]agent.Agent, bool) {
	fail := func(err error) ([]agent.Agent, bool) {
		r.Status = LockSkillFailed
		r.Err = fmt.Errorf("skill %q: %w", r.Name, err)
		return nil, true
	}
	if req.Frozen && e.Ext == nil {
		return fail(errs.WithHint(
			fmt.Errorf("%w: entry has no gskill metadata; --frozen-lockfile cannot enrich the lock",
				errs.ErrLockMismatch),
			"run 'gskill install' without --frozen-lockfile once to record it"))
	}
	ids := unionStrings(entryAgents(e), req.Agents)
	if len(ids) == 0 {
		if e.Ext != nil {
			// Managed but declared for no agents (e.g. every agent was
			// unlinked without --prune): nothing to materialize.
			r.Status = LockSkillUpToDate
			r.Err = nil
			return nil, true
		}
		return fail(errs.WithHint(
			fmt.Errorf("%w: no target agents selected", errs.ErrUsage),
			"pass --agent <id>[,<id>...] (the lock entry declares none)"))
	}
	agents, err := a.agentsByID(ids)
	if err != nil {
		return fail(err)
	}
	return agents, false
}

// requestedForEntry derives the tracking intent to record for one installed
// entry: the prior record's intent when it is still consistent with the core
// ref (an external core-ref edit overrides gskill's stale intent), else the
// entry's declared ref, backfilled uniformly from the resolved revision so a
// later update follows what was installed instead of floating.
func requestedForEntry(lf *skillslock.State, name string, e skillslock.Entry, rev resolver.Revision) skillslock.Requested {
	rq := skillslock.Requested{Ref: e.Ref}
	if prior, ok := lf.Skills[name]; ok {
		rq = prior.Requested
		if e.Ref != "" && e.Ref != prior.Requested.Ref {
			rq = skillslock.Requested{Ref: e.Ref}
		}
	} else if rq.Ref == "" && e.Ext != nil {
		rq.Ref = e.Ext.Ref
	}
	return backfillRequested(rq, rev)
}

// stagedLockEntry is a resolved, materialized, hash-verified entry ready to
// activate.
type stagedLockEntry struct {
	ref    source.Ref
	rev    resolver.Revision
	ireq   installer.Request
	compat string
}

// stageAndVerifyLockEntry resolves and materializes one entry and verifies its
// shared computedHash before anything is activated. A mismatch on the recorded
// pin usually means an external tool updated the entry (new computedHash,
// stale gskill pins): the source is re-resolved once and re-verified before
// failing, so gskill fetches the update instead of demanding --force against
// its own stale pin. An empty recorded hash (an entry never hashed) has
// nothing to verify against; it is recorded after the install.
func (a *App) stageAndVerifyLockEntry(ctx context.Context, p *project, lf *skillslock.State, name string, e skillslock.Entry, req InstallFromLockRequest) (stagedLockEntry, error) {
	ref, rev, pinned, err := a.resolveLockEntry(ctx, lf, name, e)
	if err != nil {
		return stagedLockEntry{}, err
	}
	skillDir := skillDirOf(e.SkillPath)
	extMode, extScope := "", ""
	if e.Ext != nil {
		extMode, extScope = e.Ext.InstallMode, e.Ext.Scope
	}
	inst := a.installerForScope(p, extScope)
	mode := modeOr(req.InstallMode, extMode)

	ireq, compat, err := a.stageLockEntry(ctx, inst, req, name, skillDir, mode, extScope, ref, rev)
	if err != nil {
		return stagedLockEntry{}, err
	}
	staged := stagedLockEntry{ref: ref, rev: rev, ireq: ireq, compat: compat}
	if e.ComputedHash == "" || compat == e.ComputedHash {
		return staged, nil
	}
	return a.retryOrRejectMismatch(ctx, inst, req, name, skillDir, mode, extScope, e, staged, pinned)
}

// retryOrRejectMismatch handles a computedHash mismatch on the recorded pin:
// re-resolve once (an external tool may have updated the entry), then fail
// closed unless --force accepts the changed content.
func (a *App) retryOrRejectMismatch(ctx context.Context, inst *installer.Installer, req InstallFromLockRequest, name, skillDir, mode, scope string, e skillslock.Entry, staged stagedLockEntry, pinned bool) (stagedLockEntry, error) {
	if pinned && !req.Frozen && !req.Offline {
		if ref2, rev2, rErr := a.freshResolveLockEntry(ctx, e, false); rErr == nil {
			if ireq2, compat2, sErr := a.stageLockEntry(ctx, inst, req, name, skillDir, mode, scope, ref2, rev2); sErr == nil && compat2 == e.ComputedHash {
				return stagedLockEntry{ref: ref2, rev: rev2, ireq: ireq2, compat: compat2}, nil
			}
		}
	}
	if !req.Force || req.Frozen {
		return stagedLockEntry{}, errs.WithHint(
			fmt.Errorf("%w: computedHash mismatch: lock records %s, source content is %s",
				errs.ErrIntegrity, e.ComputedHash, staged.compat),
			"re-run with --force to accept the changed upstream content")
	}
	return staged, nil
}

// stageLockEntry materializes a revision (no activation), locates the skill at
// skillDir, and computes its shared computedHash. The returned request is
// ready for Install once agents are attached.
func (a *App) stageLockEntry(ctx context.Context, inst *installer.Installer, req InstallFromLockRequest, name, skillDir, mode, scope string, ref source.Ref, rev resolver.Revision) (installer.Request, string, error) {
	ref.Path = skillDir
	ireq := a.installRequest(req.Root, ref, rev, nil, scope, mode)
	ireq.Name = name
	ireq.Offline = req.Offline

	scan, err := inst.DiscoverAll(ctx, ireq, discovery.Options{})
	if err != nil {
		return installer.Request{}, "", err
	}
	found, ok := skillAtRepoPath(scan, skillDir)
	if !ok {
		return installer.Request{}, "", fmt.Errorf("%w: skillPath %q not found in source %s",
			errs.ErrInvalidLock, path.Join(skillDir, integrity.SkillFileName), ref.Original)
	}
	compat, err := integrity.CompatHash(found.Dir)
	if err != nil {
		return installer.Request{}, "", err
	}
	return ireq, compat, nil
}

// lockEntryUpToDate implements the no-op/repair fast path: when the entry's
// recorded computedHash matches the lock, every requested agent is already
// recorded, and the canonical content sits in the store, no resolution or
// fetch happens. Intact targets short-circuit to up-to-date; missing targets
// are relinked from the store only (US5). handled=false falls through to the
// full pipeline.
func (a *App) lockEntryUpToDate(ctx context.Context, p *project, lf *skillslock.State, name string, e skillslock.Entry, agents []agent.Agent, req InstallFromLockRequest) (LockSkillResult, bool) {
	prior, ok := lf.Skills[name]
	if !ok || e.ComputedHash == "" {
		return LockSkillResult{}, false
	}
	ids := agentIDs(agents)
	if len(subtract(ids, prior.Installation.Agents)) > 0 {
		return LockSkillResult{}, false // new agents requested: full path
	}
	if !p.store.Has(prior.Resolved.ContentHash) {
		return LockSkillResult{}, false
	}
	// The recorded hash must match the actual stored content — comparing the
	// entry against itself would let an edited or corrupted computedHash pass
	// as "up to date" (it must fail closed, or be accepted via --force, on
	// the full path).
	if compat, err := integrity.CompatHash(p.store.Path(prior.Resolved.ContentHash)); err != nil || compat != e.ComputedHash {
		return LockSkillResult{}, false
	}

	var missing []agent.Agent
	for _, ag := range agents {
		recorded, ok := prior.Installation.Targets[ag.ID()]
		if !ok || !fileExists(resolveTarget(p.root, recorded)) {
			missing = append(missing, ag)
		}
	}
	r := LockSkillResult{Name: name, Source: e.Source, ComputedHash: e.ComputedHash, Status: LockSkillUpToDate}
	if len(missing) == 0 {
		return r, true
	}
	if req.DryRun {
		// A real run would relink the missing targets; the plan must say so
		// rather than report "up to date".
		r.Status = LockSkillPlanned
		return r, true
	}

	result, err := a.reconcileFromLock(ctx, p, name, prior, missing,
		SyncRequest{Root: p.root, Offline: req.Offline}, false)
	if err != nil {
		return LockSkillResult{}, false // repair failed: retry via the full path
	}
	mergeAgentInstall(&prior, result)
	lf.Skills[name] = prior
	r.Status = LockSkillRepaired
	return r, true
}

// resolveLockEntry pins an entry to a revision: a previously recorded gskill
// installation reuses its exact pin (reproduction path) — but only while the
// entry's computedHash still matches what that pin produced. When an external
// tool updated the entry (new computedHash), the stale pin would refetch the
// OLD revision and --force would then overwrite the external update, so the
// source is re-resolved instead.
func (a *App) resolveLockEntry(ctx context.Context, lf *skillslock.State, name string, e skillslock.Entry) (ref source.Ref, rev resolver.Revision, pinned bool, err error) {
	if prior, ok := lf.Skills[name]; ok && prior.Resolved.Commit != "" {
		return refFromLock(prior.Source), revFromLock(prior.Resolved), true, nil
	}
	ref, rev, err = a.freshResolveLockEntry(ctx, e, true)
	return ref, rev, false, err
}

// freshResolveLockEntry resolves an entry from its source. usePins applies the
// gskill extension's commit pin; the mismatch-retry path passes false so an
// external tool's update (new computedHash, stale gskill block) resolves the
// ref's current head instead of refetching the old revision.
func (a *App) freshResolveLockEntry(ctx context.Context, e skillslock.Entry, usePins bool) (source.Ref, resolver.Revision, error) {
	srcStr := e.Source
	if e.Ext != nil && e.Ext.SourceURL != "" {
		srcStr = e.Ext.SourceURL
	}
	ref, err := source.Parse(srcStr)
	if err != nil {
		return source.Ref{}, resolver.Revision{}, err
	}
	ref = promoteLocalGit(ref)

	requested := resolver.Requested{Ref: e.Ref}
	if e.Ext != nil {
		if requested.Ref == "" {
			requested.Ref = e.Ext.Ref
		}
		if usePins {
			requested.Commit = e.Ext.Commit
		}
	}
	rev, _, err := resolver.Resolve(ctx, a.git, ref, requested)
	if err != nil {
		return source.Ref{}, resolver.Revision{}, err
	}
	return ref, rev, nil
}

// skillAtRepoPath finds the discovered skill living exactly at repoPath ("" =
// repository root).
func skillAtRepoPath(scan discovery.Result, repoPath string) (discovery.DiscoveredSkill, bool) {
	for _, s := range scan.Skills {
		if s.RepoPath == repoPath {
			return s, true
		}
	}
	return discovery.DiscoveredSkill{}, false
}
