package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/fsutil"
	"github.com/glapsfun/gskill/internal/installer"
	"github.com/glapsfun/gskill/internal/lockfile"
	"github.com/glapsfun/gskill/internal/manifest"
	"github.com/glapsfun/gskill/internal/resolver"
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
}

// AddResult reports the outcome of an add.
type AddResult struct {
	Name        string
	ContentHash string
	Targets     map[string]string
	Warnings    []string
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
	agents, err := a.targetAgents(ctx, req.Root, req.Agents, m.Defaults.Agents)
	if err != nil {
		return AddResult{}, err
	}

	rev, warnings, err := resolver.Resolve(ctx, a.git, ref, resolver.Requested{
		Version: req.Version, Ref: req.Ref, Commit: req.Commit,
	})
	if err != nil {
		return AddResult{}, err
	}

	ireq := a.installRequest(req.Root, ref, rev, agents, req.Scope, modeOr(req.Mode, m.Defaults.InstallMode))
	inst := a.installerForScope(p, string(ireq.Scope))
	skill, err := inst.Discover(ctx, ireq)
	if err != nil {
		return AddResult{}, err
	}
	name := skill.Frontmatter.Name
	if _, exists := m.Skills[name]; exists && !req.Force {
		return AddResult{}, fmt.Errorf("%w: skill %q already declared; use 'gskill update %s' or --force",
			errs.ErrInvalidManifest, name, name)
	}
	ireq.Name = name

	var result installer.Result
	err = a.withLock(ctx, p, func() error {
		result, err = inst.Install(ctx, ireq)
		if err != nil {
			return err
		}
		m.Skills[name] = manifestEntry(req, ref)
		if saveErr := manifest.Save(p.manifestPath, m); saveErr != nil {
			return saveErr
		}
		return a.writeLockEntry(p, name, ref, rev, ireq, result, m.Skills[name])
	})
	if err != nil {
		return AddResult{}, err
	}

	allWarnings := make([]string, 0, len(warnings)+len(skill.Warnings)+len(result.Warnings))
	allWarnings = append(allWarnings, warnings...)
	allWarnings = append(allWarnings, skill.Warnings...)
	allWarnings = append(allWarnings, result.Warnings...)
	return AddResult{
		Name:        name,
		ContentHash: result.ContentHash,
		Targets:     result.Targets,
		Warnings:    allWarnings,
	}, nil
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

// writeLockEntry updates the lockfile with one entry and saves it.
func (a *App) writeLockEntry(p *project, name string, ref source.Ref, rev resolver.Revision, ireq installer.Request, result installer.Result, ms manifest.Skill) error {
	lf, err := loadOrNewLock(p.lockPath)
	if err != nil {
		return err
	}
	lf.Skills[name] = buildLockEntry(ref, rev, ireq, result, ms)
	return lockfile.Save(p.lockPath, lf)
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
	if len(detected) == 0 {
		return nil, fmt.Errorf("%w: no target agents specified and none detected", errs.ErrUnsupportedAgent)
	}
	return detected, nil
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

// manifestEntry builds the manifest record for an add (intent only).
func manifestEntry(req AddRequest, ref source.Ref) manifest.Skill {
	return manifest.Skill{
		Source:      req.Source,
		Path:        ref.Path,
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
			Owner: ref.Owner, Repo: ref.Repo, Path: ref.Path,
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
			Agents: result.Agents, Targets: result.Targets,
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
