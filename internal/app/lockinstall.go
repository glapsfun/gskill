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
	"github.com/glapsfun/gskill/internal/integrity"
	"github.com/glapsfun/gskill/internal/lockfile"
	"github.com/glapsfun/gskill/internal/manifest"
	"github.com/glapsfun/gskill/internal/resolver"
	"github.com/glapsfun/gskill/internal/skillslock"
	"github.com/glapsfun/gskill/internal/source"
)

// InstallFromLockRequest describes a lock-file-first install (spec 012 US1/US2):
// restore every skill declared in skills-lock.json for the selected agents.
type InstallFromLockRequest struct {
	Root        string
	Agents      []string // agent IDs; empty falls back to manifest defaults
	InstallMode string   // auto | symlink | copy ("" = manifest default)
	NoInit      bool     // refuse instead of auto-initializing
	Force       bool     // accept changed upstream content, rewrite computedHash
	DryRun      bool     // report the plan, write nothing
	Offline     bool     // restore from local store/cache only
	Frozen      bool     // never modify the lock file; fail closed on drift
}

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
	Initialized       bool
	Migrated          bool
	ManifestGenerated bool
	Agents            []string
	Skills            []LockSkillResult
	Changed           bool
}

// InstallFromLock implements the lock-file-first install pipeline: locate and
// validate skills-lock.json, auto-initialize the project (FR-019/FR-020),
// generate or merge the manifest (FR-021), then per entry resolve, verify the
// npx-compatible computedHash before activation, install for every selected
// agent, and record the namespaced gskill metadata (FR-016). Failures are
// isolated per skill: verified successes stay installed and recorded
// (FR-016a).
func (a *App) InstallFromLock(ctx context.Context, req InstallFromLockRequest) (InstallFromLockResult, error) {
	p := openProject(req.Root)
	var res InstallFromLockResult

	if !req.Frozen && !req.DryRun {
		legacyWasPresent := fileExists(filepath.Join(req.Root, LockName))
		if err := a.maybeMigrate(ctx, req.Root); err != nil {
			return res, err
		}
		res.Migrated = legacyWasPresent
	}

	l, err := a.loadSharedLock(p)
	if err != nil {
		return res, err
	}

	m, initialized, err := a.ensureProject(ctx, p, req)
	if err != nil {
		return res, err
	}
	res.Initialized = initialized

	ids := req.Agents
	if len(ids) == 0 {
		ids = m.Defaults.Agents
	}
	if len(ids) == 0 {
		return res, errs.WithHint(
			fmt.Errorf("%w: no target agents selected", errs.ErrUsage),
			"pass --agent <id>[,<id>...] or record defaults.agents in gskill.toml")
	}
	agents, err := a.agentsByID(ids)
	if err != nil {
		return res, err
	}
	res.Agents = ids

	res.ManifestGenerated, err = a.mergeManifestFromLock(p, m, l, ids, req.DryRun)
	if err != nil {
		return res, err
	}

	installErr := a.withLock(ctx, p, func() error {
		return a.installAllLockEntries(ctx, p, m, l, agents, req, &res)
	})
	return res, installErr
}

// installAllLockEntries runs the per-entry pipeline over every lock entry,
// aggregating per-skill outcomes into partial-failure semantics (FR-016a):
// mixed results return ErrPartialInstall, total failure returns the first
// cause, and successes are persisted either way.
func (a *App) installAllLockEntries(ctx context.Context, p *project, m *manifest.Manifest, l *skillslock.Lock, agents []agent.Agent, req InstallFromLockRequest, res *InstallFromLockResult) error {
	lf, err := loadOrNewLock(p.lockPath)
	if err != nil {
		return err
	}
	var failures, successes int
	var firstErr error
	for _, name := range sortedLockNames(l) {
		e, _ := l.Entry(name)
		r := a.installLockEntry(ctx, p, m, lf, name, e, agents, req)
		res.Skills = append(res.Skills, r)
		switch {
		case r.Err != nil:
			failures++
			if firstErr == nil {
				firstErr = r.Err
			}
		case r.Status == LockSkillInstalled || r.Status == LockSkillRepaired:
			successes++
			res.Changed = true
		}
	}
	if !req.DryRun && !req.Frozen {
		if saveErr := saveLock(p.lockPath, lf); saveErr != nil {
			return saveErr
		}
	}
	switch {
	case failures > 0 && successes > 0:
		return fmt.Errorf("%w: %d of %d skills failed",
			errs.ErrPartialInstall, failures, failures+successes)
	case failures > 0:
		return firstErr
	default:
		return nil
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
			return nil, errs.WithHint(
				fmt.Errorf("%w: no %s found", errs.ErrInvalidManifest, skillslock.FileName),
				"run 'gskill add <source>' to install a first skill, or clone a project that commits one")
		}
		return nil, fmt.Errorf("stat %s: %w", skillslock.FileName, err)
	}
	l, err := skillslock.Load(p.lockPath)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errs.ErrInvalidManifest, err)
	}
	if err := l.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %w", errs.ErrInvalidManifest, err)
	}
	return l, nil
}

// ensureProject auto-initializes a missing project structure (FR-019) without
// ever overwriting existing files (FR-020: Init only creates what is absent,
// and an unreadable existing manifest aborts untouched).
func (a *App) ensureProject(ctx context.Context, p *project, req InstallFromLockRequest) (*manifest.Manifest, bool, error) {
	initialized := false
	if !p.manifestExists() {
		if req.NoInit {
			return nil, false, errs.WithHint(
				fmt.Errorf("%w: project is not initialized and --no-init is set", errs.ErrInvalidManifest),
				"drop --no-init or run 'gskill init' first")
		}
		if req.DryRun {
			return manifest.New(), true, nil
		}
		if _, err := a.Init(ctx, req.Root); err != nil {
			return nil, false, err
		}
		initialized = true
	}
	m, err := manifest.Load(p.manifestPath)
	if err != nil {
		return nil, false, fmt.Errorf("existing %s is unreadable (refusing to overwrite): %w", ManifestName, err)
	}
	return m, initialized, nil
}

// mergeManifestFromLock appends manifest declarations for lock entries the
// manifest does not know, and records the selected agents as project defaults
// when none are set. Existing declarations and settings are never rewritten
// (FR-021, research R7).
func (a *App) mergeManifestFromLock(p *project, m *manifest.Manifest, l *skillslock.Lock, agentIDs []string, dryRun bool) (bool, error) {
	changed := false
	if m.Skills == nil {
		m.Skills = map[string]manifest.Skill{}
	}
	for _, name := range l.Names() {
		if _, ok := m.Skills[name]; ok {
			continue
		}
		e, _ := l.Entry(name)
		m.Skills[name] = manifest.Skill{
			Source: manifestSourceForEntry(e),
			Path:   skillDirOf(e.SkillPath),
			Ref:    e.Ref,
		}
		changed = true
	}
	if len(m.Defaults.Agents) == 0 {
		m.Defaults.Agents = agentIDs
		changed = true
	}
	if changed && !dryRun {
		if err := manifest.Save(p.manifestPath, m); err != nil {
			return false, err
		}
	}
	return changed, nil
}

// manifestSourceForEntry maps a lock entry's source to the manifest's source
// notation: github shorthand gains the host prefix, everything else passes
// through (research R7).
func manifestSourceForEntry(e skillslock.Entry) string {
	if e.SourceType == "github" && !strings.Contains(e.Source, "://") &&
		!strings.HasPrefix(e.Source, "github.com/") {
		return "github.com/" + e.Source
	}
	return e.Source
}

// skillDirOf returns the skill directory recorded by skillPath ("" for a
// repo-root skill).
func skillDirOf(skillPath string) string {
	d := path.Dir(skillPath)
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
var lockEntrySourceTypes = map[string]bool{"github": true, "local": true}

// installLockEntry restores one lock entry: resolve, verify the compatible
// computedHash against the fetched content BEFORE any activation (fail closed,
// FR-018a), install for the selected agents, and stage the lock record. All
// failures are reported on the result, never returned, so one bad skill cannot
// take down its siblings (FR-016a).
func (a *App) installLockEntry(ctx context.Context, p *project, m *manifest.Manifest, lf *lockfile.Lockfile, name string, e skillslock.Entry, agents []agent.Agent, req InstallFromLockRequest) LockSkillResult {
	r := LockSkillResult{Name: name, Source: e.Source, ComputedHash: e.ComputedHash, Status: LockSkillFailed}
	fail := func(err error) LockSkillResult {
		r.Err = fmt.Errorf("skill %q: %w", name, err)
		return r
	}

	if !lockEntrySourceTypes[e.SourceType] {
		return fail(fmt.Errorf("%w: unsupported sourceType %q (supported: github, local)",
			errs.ErrInvalidManifest, e.SourceType))
	}

	ref, rev, err := a.resolveLockEntry(ctx, lf, name, e)
	if err != nil {
		return fail(err)
	}
	skillDir := skillDirOf(e.SkillPath)
	ref.Path = skillDir

	ireq := a.installRequest(req.Root, ref, rev, nil, "", modeOr(req.InstallMode, m.Defaults.InstallMode))
	ireq.Name = name
	ireq.Offline = req.Offline
	inst := a.installerFor(p)

	// Locate the skill and verify the shared computedHash before anything is
	// activated into an agent directory.
	scan, err := inst.DiscoverAll(ctx, ireq, discovery.Options{})
	if err != nil {
		return fail(err)
	}
	found, ok := skillAtRepoPath(scan, skillDir)
	if !ok {
		return fail(fmt.Errorf("%w: skillPath %q not found in source %s",
			errs.ErrInvalidManifest, e.SkillPath, e.Source))
	}
	compat, err := integrity.CompatHash(found.Dir)
	if err != nil {
		return fail(err)
	}
	// An empty recorded hash (a freshly migrated gskill.lock entry) has
	// nothing to verify against; the hash is recorded after this install.
	if e.ComputedHash != "" && compat != e.ComputedHash && (!req.Force || req.Frozen) {
		return fail(errs.WithHint(
			fmt.Errorf("%w: computedHash mismatch: lock records %s, source content is %s",
				errs.ErrIntegrity, e.ComputedHash, compat),
			"re-run with --force to accept the changed upstream content"))
	}

	if req.DryRun {
		r.Status = LockSkillPlanned
		r.Err = nil
		return r
	}

	ireq.Agents = agents
	result, err := inst.Install(ctx, ireq)
	if err != nil {
		return fail(err)
	}

	ls := buildLockEntry(ref, rev, ireq, result, m.Skills[name])
	ls.Resolved.CompatHash = compat
	lf.Skills[name] = ls

	r.ComputedHash = compat
	r.Status = LockSkillInstalled
	r.Err = nil
	return r
}

// resolveLockEntry pins an entry to a revision: a previously recorded gskill
// installation reuses its exact pin (reproduction path); otherwise the source
// is resolved honoring the entry's optional ref and any gskill extension pins.
func (a *App) resolveLockEntry(ctx context.Context, lf *lockfile.Lockfile, name string, e skillslock.Entry) (source.Ref, resolver.Revision, error) {
	if prior, ok := lf.Skills[name]; ok && prior.Resolved.Commit != "" {
		return refFromLock(prior.Source), revFromLock(prior.Resolved), nil
	}

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
		requested.Commit = e.Ext.Commit
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
