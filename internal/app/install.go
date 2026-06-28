package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/discovery"
	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/fsutil"
	"github.com/glapsfun/gskill/internal/installer"
	"github.com/glapsfun/gskill/internal/lockfile"
	"github.com/glapsfun/gskill/internal/manifest"
	"github.com/glapsfun/gskill/internal/resolver"
	"github.com/glapsfun/gskill/internal/selection"
	"github.com/glapsfun/gskill/internal/source"
)

const defaultLockTimeout = 30 * time.Second

// InitResult reports what Init created.
type InitResult struct {
	ManifestPath string
	Created      []string
}

// Init scaffolds the project: a gskill.toml manifest, a .gskill state dir, and
// a .gitignore hint (FR-001). It is idempotent.
func (a *App) Init(_ context.Context, root string) (InitResult, error) {
	p := openProject(root)
	res := InitResult{ManifestPath: p.manifestPath}

	if !p.manifestExists() {
		if err := manifest.Save(p.manifestPath, manifest.New()); err != nil {
			return InitResult{}, err
		}
		res.Created = append(res.Created, ManifestName)
	}
	if err := os.MkdirAll(filepath.Join(root, stateDirName), 0o750); err != nil {
		return InitResult{}, fmt.Errorf("create state dir: %w", err)
	}
	added, err := ensureGitignore(root)
	if err != nil {
		return InitResult{}, err
	}
	if added {
		res.Created = append(res.Created, ".gitignore")
	}
	return res, nil
}

// AddRequest describes an `add` invocation.
type AddRequest struct {
	Root    string
	Source  string
	Version string
	Ref     string
	Commit  string
	Agents  []string
	Force   bool
	Scope   string
	Mode    string

	// Selection (US2). Selectors are raw --skill values (incl. "*"); All maps
	// --all; Path is the --path disambiguator; ListOnly maps --list.
	Selectors   []string
	All         bool
	Path        string
	ListOnly    bool
	Interactive bool

	// Discovery filters (FR-012).
	MaxDepth int
	Include  []string
	Exclude  []string

	// Chooser, when set and Interactive, picks among multiple discovered skills.
	// It receives every in-scope skill (invalid ones shown but not selectable)
	// and returns the chosen subset. The CLI wires this to the TUI picker; the
	// app stays independent of the view layer (FR-021).
	Chooser func([]discovery.DiscoveredSkill) ([]discovery.DiscoveredSkill, error)
}

// InstalledSkill reports one installed skill in a (possibly multi-skill) add.
type InstalledSkill struct {
	Name        string            `json:"name"`
	Path        string            `json:"path"`
	ContentHash string            `json:"content_hash"`
	Targets     map[string]string `json:"targets"`
}

// AddResult reports the outcome of an add (one or more skills).
type AddResult struct {
	Installed []InstalledSkill
	Listed    []discovery.DiscoveredSkill // populated for --list (no install)
	Warnings  []string
}

// Add resolves, installs, and records a new skill, updating the manifest and
// lockfile. It errors on an already-declared key unless Force is set (FR-047),
// and writes nothing when no target agent is available (FR-029).
func (a *App) Add(ctx context.Context, req AddRequest) (AddResult, error) {
	p := openProject(req.Root)
	if !p.manifestExists() {
		return AddResult{}, fmt.Errorf("%w: no %s; run 'gskill init' first", errs.ErrInvalidManifest, ManifestName)
	}
	m, err := manifest.Load(p.manifestPath)
	if err != nil {
		return AddResult{}, err
	}

	ref, err := source.Parse(req.Source)
	if err != nil {
		return AddResult{}, err
	}
	ref = promoteLocalGit(ref)

	rev, warnings, err := resolver.Resolve(ctx, a.git, ref, resolver.Requested{
		Version: req.Version, Ref: req.Ref, Commit: req.Commit,
	})
	if err != nil {
		return AddResult{}, err
	}

	// Discovery and listing are read-only and do not need a target agent; agents
	// are resolved only when an install is actually about to happen (so `--list`
	// works even with no agents detected).
	ireq := a.installRequest(req.Root, ref, rev, nil, req.Scope, modeOr(req.Mode, m.Defaults.InstallMode))
	inst := a.installerForScope(p, string(ireq.Scope))
	scan, err := inst.DiscoverAll(ctx, ireq, discovery.Options{
		MaxDepth: req.MaxDepth, Include: req.Include, Exclude: req.Exclude,
	})
	if err != nil {
		return AddResult{}, err
	}

	if req.ListOnly {
		return AddResult{Listed: skillsInScope(scan, ref.Path), Warnings: warnings}, nil
	}

	// An install needs a target agent. Resolve it before any interactive
	// selection so the user is not asked to pick skills only to fail afterward.
	agents, err := a.targetAgents(ctx, req.Root, req.Agents, m.Defaults.Agents)
	if err != nil {
		return AddResult{}, err
	}
	ireq.Agents = agents

	selected, err := a.resolveSelection(scan, req, ref.Path)
	if err != nil {
		return AddResult{}, err
	}
	return a.installSelected(ctx, p, m, req, ref, rev, ireq, inst, selected, warnings)
}

// resolveSelection turns the discovered skills into the set to install, honoring
// explicit selectors or, when none are given, the single-skill auto behavior.
func (a *App) resolveSelection(scan discovery.Result, req AddRequest, explicitPath string) ([]discovery.DiscoveredSkill, error) {
	sels, err := selection.Parse(req.Selectors, req.All, req.Path)
	if err != nil {
		return nil, err
	}
	if len(sels) > 0 {
		selected, resErr := selection.Resolve(scan, sels, req.Interactive)
		if resErr != nil {
			return nil, mapSelectionErr(resErr)
		}
		return selected, nil
	}

	valid, invalid := partitionScope(scan, explicitPath)
	switch {
	case len(valid) == 1:
		return valid, nil
	case len(valid) > 1:
		if req.Interactive && req.Chooser != nil {
			return a.chooseInteractive(req, skillsInScope(scan, explicitPath))
		}
		return nil, fmt.Errorf(
			"%w: source contains %d skills; select with --skill <name>, repeated --skill, or --skill '*'",
			errs.ErrUsage, len(valid))
	case len(invalid) > 0:
		return nil, fmt.Errorf("%w: skill %q is invalid: %s",
			errs.ErrInvalidManifest, invalid[0].ID, firstProblem(invalid[0]))
	default:
		return nil, fmt.Errorf("%w: no SKILL.md found in source", errs.ErrSourceUnavailable)
	}
}

// chooseInteractive delegates to the request's Chooser (the TUI picker) and
// treats an empty selection as a usage error so nothing is installed by mistake.
func (a *App) chooseInteractive(req AddRequest, candidates []discovery.DiscoveredSkill) ([]discovery.DiscoveredSkill, error) {
	chosen, err := req.Chooser(candidates)
	if err != nil {
		return nil, err
	}
	if len(chosen) == 0 {
		return nil, fmt.Errorf("%w: no skill selected", errs.ErrUsage)
	}
	return chosen, nil
}

// installSelected installs the chosen skills atomically: it pre-checks every
// manifest-key collision, then stages and activates each skill, rolling back
// already-activated targets on any failure, and commits the manifest and
// lockfile only after all succeed (FR-046, research R8).
func (a *App) installSelected(ctx context.Context, p *project, m *manifest.Manifest, req AddRequest, ref source.Ref, rev resolver.Revision, ireq installer.Request, inst *installer.Installer, selected []discovery.DiscoveredSkill, warnings []string) (AddResult, error) {
	for _, s := range selected {
		if _, exists := m.Skills[s.ID]; exists && !req.Force {
			return AddResult{}, fmt.Errorf("%w: skill %q already declared; use 'gskill update %s' or --force",
				errs.ErrInvalidManifest, s.ID, s.ID)
		}
	}

	res := AddResult{Warnings: append([]string(nil), warnings...)}
	err := a.withLock(ctx, p, func() error {
		lf, lockErr := loadOrNewLock(p.lockPath)
		if lockErr != nil {
			return lockErr
		}
		var activated []installer.Result
		rollback := func() {
			for _, r := range activated {
				a.removeTargets(req.Root, ireq.Scope, r)
			}
		}

		for _, s := range selected {
			ir := ireq
			ir.Name = s.ID
			ir.Path = s.RepoPath
			result, instErr := inst.Install(ctx, ir)
			if instErr != nil {
				rollback()
				return instErr
			}
			activated = append(activated, result)
			m.Skills[s.ID] = manifestEntry(req, s.RepoPath)
			lf.Skills[s.ID] = buildLockEntry(ref, rev, ir, result, m.Skills[s.ID])
			res.Installed = append(res.Installed, InstalledSkill{
				Name: s.ID, Path: s.RepoPath, ContentHash: result.ContentHash, Targets: result.Targets,
			})
			res.Warnings = append(res.Warnings, result.Warnings...)
		}

		if saveErr := manifest.Save(p.manifestPath, m); saveErr != nil {
			rollback()
			return saveErr
		}
		if saveErr := lockfile.Save(p.lockPath, lf); saveErr != nil {
			rollback()
			return saveErr
		}
		return nil
	})
	if err != nil {
		return AddResult{}, err
	}
	return res, nil
}

// removeTargets best-effort removes a skill's activated directories during an
// atomic-install rollback.
func (a *App) removeTargets(root string, scope installer.Scope, r installer.Result) {
	for _, target := range r.Targets {
		path := target
		if scope != installer.ScopeGlobal {
			path = filepath.Join(root, target)
		}
		_ = os.RemoveAll(path)
	}
}

// mapSelectionErr maps a selection error to a gskill exit code: an invalid
// explicit selection is exit 3; ambiguity and no-match are usage errors (exit 2).
func mapSelectionErr(err error) error {
	if errors.Is(err, selection.ErrInvalidSelection) {
		return errs.Wrap(errs.CodeInvalidManifest, err.Error(), err)
	}
	return errs.Wrap(errs.CodeUsage, err.Error(), err)
}

// partitionScope splits the discovered skills, scoped to an optional explicit
// in-repo path, into valid and invalid sets.
func partitionScope(scan discovery.Result, explicitPath string) (valid, invalid []discovery.DiscoveredSkill) {
	for _, s := range scan.Skills {
		if explicitPath != "" && s.RepoPath != explicitPath {
			continue
		}
		if s.Valid {
			valid = append(valid, s)
		} else {
			invalid = append(invalid, s)
		}
	}
	return valid, invalid
}

// skillsInScope returns the discovered skills, scoped to an optional explicit
// in-repo path, for --list output.
func skillsInScope(scan discovery.Result, explicitPath string) []discovery.DiscoveredSkill {
	if explicitPath == "" {
		return scan.Skills
	}
	var out []discovery.DiscoveredSkill
	for _, s := range scan.Skills {
		if s.RepoPath == explicitPath {
			out = append(out, s)
		}
	}
	return out
}

// firstProblem returns the first error-severity diagnostic message for a skill.
func firstProblem(s discovery.DiscoveredSkill) string {
	for _, p := range s.Problems {
		if p.Severity == discovery.SeverityError {
			return p.Message
		}
	}
	return "unknown validation error"
}

// InstallRequest describes an `install` invocation over the existing manifest.
type InstallRequest struct {
	Root           string
	Scope          string
	Mode           string
	Frozen         bool
	Offline        bool
	NoCache        bool
	UpdateLockfile bool
}

// SkillChange records the per-skill outcome of an install.
type SkillChange struct {
	Name        string
	ContentHash string
	Changed     bool
}

// InstallResult reports an install run.
type InstallResult struct {
	Skills  []SkillChange
	Changed bool
}

// Install materializes every declared skill, additively and idempotently,
// updating the lockfile only when resolved content changes (FR-022).
func (a *App) Install(ctx context.Context, req InstallRequest) (InstallResult, error) {
	if req.Frozen {
		return a.installFrozen(ctx, req)
	}

	p := openProject(req.Root)
	if !p.manifestExists() {
		return InstallResult{}, fmt.Errorf("%w: no %s; run 'gskill init' first", errs.ErrInvalidManifest, ManifestName)
	}
	m, err := manifest.Load(p.manifestPath)
	if err != nil {
		return InstallResult{}, err
	}

	var out InstallResult
	err = a.withLock(ctx, p, func() error {
		lf, lockErr := loadOrNewLock(p.lockPath)
		if lockErr != nil {
			return lockErr
		}
		for _, name := range sortedKeys(m.Skills) {
			change, applyErr := a.installOne(ctx, p, lf, name, m.Skills[name], req, m.Defaults.Agents)
			if applyErr != nil {
				return applyErr
			}
			out.Skills = append(out.Skills, change)
			out.Changed = out.Changed || change.Changed
		}
		if out.Changed {
			return lockfile.Save(p.lockPath, lf)
		}
		return nil
	})
	if err != nil {
		return InstallResult{}, err
	}
	return out, nil
}

// installOne installs a single declared skill and updates lf in place.
func (a *App) installOne(ctx context.Context, p *project, lf *lockfile.Lockfile, name string, ms manifest.Skill, req InstallRequest, defaults []string) (SkillChange, error) {
	ref, err := source.Parse(ms.Source)
	if err != nil {
		return SkillChange{}, err
	}
	ref = promoteLocalGit(ref)
	agents, err := a.targetAgents(ctx, p.root, ms.Agents, defaults)
	if err != nil {
		return SkillChange{}, err
	}
	rev, _, err := resolver.Resolve(ctx, a.git, ref, resolver.Requested{
		Version: ms.Version, Ref: ms.Ref, Commit: ms.Commit,
	})
	if err != nil {
		return SkillChange{}, err
	}

	ireq := a.installRequest(p.root, ref, rev, agents, req.Scope, modeOr(req.Mode, ms.InstallMode))
	ireq.Name = name
	ireq.Offline = req.Offline
	result, err := a.installerForScope(p, string(ireq.Scope)).Install(ctx, ireq)
	if err != nil {
		return SkillChange{}, err
	}

	old, existed := lf.Skills[name]
	changed := !existed || old.Resolved.ContentHash != result.ContentHash || old.Resolved.Commit != rev.Commit
	lf.Skills[name] = buildLockEntry(ref, rev, ireq, result, ms)
	return SkillChange{Name: name, ContentHash: result.ContentHash, Changed: changed}, nil
}

// targetAgents resolves the agents to install into: explicit, then defaults,
// then detected; none available is exit 9 (FR-029, FR-030).
func (a *App) targetAgents(ctx context.Context, root string, explicit, defaults []string) ([]agent.Agent, error) {
	ids := explicit
	if len(ids) == 0 {
		ids = defaults
	}
	if len(ids) > 0 {
		out := make([]agent.Agent, 0, len(ids))
		for _, id := range ids {
			ag, ok := a.agents.Get(id)
			if !ok {
				return nil, fmt.Errorf("%w: unknown agent %q", errs.ErrUnsupportedAgent, id)
			}
			out = append(out, ag)
		}
		return out, nil
	}

	detected, err := a.agents.Detect(ctx, root)
	if err != nil {
		return nil, err
	}
	if len(detected) > 0 {
		return detected, nil
	}

	// Nothing specified or detected: default to Claude so installs work out of
	// the box.
	if def, ok := a.agents.Get(agent.DefaultID); ok {
		return []agent.Agent{def}, nil
	}
	known := make([]string, 0)
	for _, ag := range a.agents.All() {
		known = append(known, ag.ID())
	}
	return nil, fmt.Errorf(
		"%w: no target agent specified and none detected; pass --agent <id> (known: %s)",
		errs.ErrUnsupportedAgent, strings.Join(known, ", "))
}

// installRequest assembles an installer.Request with shared defaults.
func (a *App) installRequest(root string, ref source.Ref, rev resolver.Revision, agents []agent.Agent, scope, mode string) installer.Request {
	home, _ := os.UserHomeDir()
	return installer.Request{
		Ref:         ref,
		Revision:    rev,
		Path:        ref.Path,
		Agents:      agents,
		Scope:       scopeOr(scope),
		ModePref:    modeOr(mode, ""),
		ProjectRoot: root,
		Home:        home,
	}
}

// withLock runs fn while holding the project's exclusive mutate lock (FR-021).
func (a *App) withLock(ctx context.Context, p *project, fn func() error) error {
	if err := os.MkdirAll(p.locksDir, 0o750); err != nil {
		return fmt.Errorf("create locks dir: %w", err)
	}
	lock, err := fsutil.Acquire(ctx, filepath.Join(p.locksDir, "mutate.lock"), fsutil.LockExclusive, defaultLockTimeout)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()
	return fn()
}

// manifestEntry builds the manifest record for an add (intent only). The path
// is the selected skill's in-repo location, so the manifest pins which skill
// inside the source was installed (FR-028/FR-030).
func manifestEntry(req AddRequest, repoPath string) manifest.Skill {
	return manifest.Skill{
		Source:      req.Source,
		Path:        repoPath,
		Version:     req.Version,
		Ref:         req.Ref,
		Commit:      req.Commit,
		Agents:      req.Agents,
		InstallMode: req.Mode,
	}
}

// buildLockEntry assembles the lockfile record from resolution + install reality.
func buildLockEntry(ref source.Ref, rev resolver.Revision, ireq installer.Request, result installer.Result, intent manifest.Skill) lockfile.LockedSkill {
	now := time.Now().UTC().Format(time.RFC3339)
	fm := result.Skill.Frontmatter
	resolved := lockfile.Resolved{
		Version:       rev.Version,
		RefKind:       string(rev.RefKind),
		Tag:           rev.Tag,
		Branch:        rev.Branch,
		Commit:        rev.Commit,
		ContentHash:   result.ContentHash,
		SkillFileHash: result.SkillFileHash,
		MutableRef:    rev.MutableRef,
	}
	if rev.RefKind == resolver.RefKindLocal {
		resolved.LocalPathHash = result.ContentHash
	}
	return lockfile.LockedSkill{
		Source: lockfile.Source{
			Type: string(ref.Type), Original: ref.Original, URL: ref.URL,
			Owner: ref.Owner, Repo: ref.Repo, Path: ireq.Path,
		},
		Requested: lockfile.Requested{Version: intent.Version, Ref: intent.Ref, Commit: intent.Commit},
		Resolved:  resolved,
		Metadata: lockfile.Metadata{
			Name: fm.Name, Description: fm.Description, Version: fm.Version, License: fm.License,
		},
		Requires: lockfile.Requires{
			Skills: fm.Requires.Skills, Commands: fm.Requires.Commands,
			Environment: fm.Requires.Environment, MCP: fm.Requires.MCP,
		},
		Installation: lockfile.Installation{
			Scope: string(ireq.Scope), Mode: string(result.Mode),
			Agents: result.Agents, Targets: result.Targets, Modes: result.Modes,
		},
		Provenance: lockfile.Provenance{FetchedAt: now, UpdatedAt: now, Trust: "checksum-ok"},
	}
}

// promoteLocalGit upgrades a local path that is a git repo to a git source so it
// resolves tags and commits like a remote.
func promoteLocalGit(ref source.Ref) source.Ref {
	if ref.Type != source.TypeLocal {
		return ref
	}
	if _, err := os.Stat(filepath.Join(ref.LocalPath, ".git")); err != nil {
		return ref
	}
	abs, err := filepath.Abs(ref.LocalPath)
	if err != nil {
		return ref
	}
	ref.Type = source.TypeGit
	ref.URL = abs
	ref.Repo = filepath.Base(abs)
	return ref
}

// refFromLock reconstructs a source.Ref from a locked source record.
func refFromLock(src lockfile.Source) source.Ref {
	ref := source.Ref{
		Type:     source.Type(src.Type),
		Original: src.Original,
		URL:      src.URL,
		Owner:    src.Owner,
		Repo:     src.Repo,
		Path:     src.Path,
	}
	if ref.Type == source.TypeLocal {
		ref.LocalPath = src.Original
	}
	return ref
}

// revFromLock reconstructs a resolver.Revision from a locked resolution record.
func revFromLock(res lockfile.Resolved) resolver.Revision {
	return resolver.Revision{
		RefKind:    resolver.RefKind(res.RefKind),
		Version:    res.Version,
		Tag:        res.Tag,
		Branch:     res.Branch,
		Commit:     res.Commit,
		MutableRef: res.MutableRef,
	}
}

// loadOrNewLock loads the lockfile at path, or returns a fresh one if absent.
func loadOrNewLock(path string) (*lockfile.Lockfile, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return lockfile.New(), nil
		}
		return nil, fmt.Errorf("stat lockfile: %w", err)
	}
	return lockfile.Load(path)
}

// ensureGitignore appends a .gskill/ ignore hint, returning whether it changed.
func ensureGitignore(root string) (bool, error) {
	path := filepath.Join(root, ".gitignore")
	existing, err := os.ReadFile(path) //nolint:gosec // project-root .gitignore
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("read .gitignore: %w", err)
	}
	if strings.Contains(string(existing), ".gskill/") {
		return false, nil
	}
	content := string(existing)
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	content += ".gskill/\n"
	if err := fsutil.WriteFileAtomic(path, []byte(content), 0o600); err != nil {
		return false, err
	}
	return true, nil
}

// sortedKeys returns the map keys in sorted order for deterministic iteration.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// modeOr returns the first non-empty install mode, defaulting to "symlink".
func modeOr(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return "symlink"
}

// scopeOr maps an optional scope string to an installer.Scope, default project.
func scopeOr(scope string) installer.Scope {
	if scope == string(installer.ScopeGlobal) {
		return installer.ScopeGlobal
	}
	return installer.ScopeProject
}
