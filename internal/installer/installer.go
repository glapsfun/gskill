package installer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/glapsfun/gskill/internal/active"
	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/cache"
	"github.com/glapsfun/gskill/internal/discovery"
	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/fsutil"
	"github.com/glapsfun/gskill/internal/git"
	"github.com/glapsfun/gskill/internal/integrity"
	"github.com/glapsfun/gskill/internal/progress"
	"github.com/glapsfun/gskill/internal/resolver"
	"github.com/glapsfun/gskill/internal/source"
	"github.com/glapsfun/gskill/internal/store"
)

// Request is everything needed to install one skill.
type Request struct {
	Ref         source.Ref
	Revision    resolver.Revision
	Name        string // declared manifest key; must match frontmatter name
	Path        string // explicit in-repo subpath (optional)
	Agents      []agent.Agent
	Scope       Scope
	ModePref    string // symlink | copy | auto
	ProjectRoot string
	Home        string
	// Offline forbids network fetches; material must already be cached (FR-026).
	Offline bool
	// ExpectContentHash, when set, must equal the materialized content hash or
	// the install fails closed (used by frozen restore, FR-015/FR-037).
	ExpectContentHash string
	// PreserveForeign makes activation fail closed instead of replacing a
	// destination gskill does not own (add paths, spec 011 FR-016 — the
	// overwrite guard lives at the point of destruction). Reconcile paths
	// (install/sync/repair/update) leave it false: restoring drifted targets
	// is their contract.
	PreserveForeign bool
	// PriorContentHash is the lockfile-recorded content hash of the previous
	// install at this skill's destinations, accepted as owned content when
	// PreserveForeign is set (a copy-mode install is a real directory).
	PriorContentHash string
}

// Result is the outcome of a successful install, sufficient to build a lock entry.
type Result struct {
	Skill         discovery.Skill
	ContentHash   string
	SkillFileHash string
	Mode          Mode              // representative mode (the first agent's)
	Modes         map[string]string // agentID -> actual mode used
	Agents        []string
	ActivePath    string            // project-relative active entry (empty for global scope)
	Targets       map[string]string // agentID -> recorded dir (relative for project scope)
	Warnings      []string
	// StoreReuse reports whether the content store satisfied the install
	// (StoreReused) or the source was fetched (StoreDownloaded) — spec 015
	// FR-007.
	StoreReuse string
	// StoreScope names the physical store that served the install: "project"
	// or "global".
	StoreScope string
}

// Installer runs the staging-verify-activate transaction over the content
// store, cache, and git runner.
type Installer struct {
	git     git.Runner
	cache   *cache.Cache
	content ContentStore
	scans   *ScanCache // nil ⇒ no scan memoization
}

// New builds an Installer over the legacy project-local store. The git runner
// may be nil for local-only installs.
func New(g git.Runner, c *cache.Cache, s *store.Store) *Installer {
	return NewWithStore(g, c, legacyStore{s: s})
}

// NewWithStore builds an Installer over any ContentStore (spec 015: the
// user-level global store, or the legacy project store).
func NewWithStore(g git.Runner, c *cache.Cache, cs ContentStore) *Installer {
	return &Installer{git: g, cache: c, content: cs}
}

// Install materializes, verifies, and activates the requested skill (FR-015,
// FR-018, FR-019, FR-020). When the expected content already sits verified in
// the content store, the source is not fetched at all — the stored object is
// reused (spec 015 FR-006). Content is always verified before activating into
// any agent directory, failing closed on a checksum mismatch.
func (i *Installer) Install(ctx context.Context, req Request) (Result, error) {
	if req.ExpectContentHash != "" && i.content.Has(req.ExpectContentHash) {
		return i.installFromStore(ctx, req)
	}

	material, err := i.materialize(ctx, req)
	if err != nil {
		return Result{}, err
	}

	skill, err := discovery.Discover(material, req.Path)
	if err != nil {
		return Result{}, err
	}

	warnings, err := validateContent(skill.Dir)
	if err != nil {
		return Result{}, err
	}
	warnings = append(warnings, identityWarning(req.Name, skill.Frontmatter.Name)...)

	hashes, err := integrity.HashDir(skill.Dir)
	if err != nil {
		return Result{}, err
	}
	if req.ExpectContentHash != "" && hashes.ContentHash != req.ExpectContentHash {
		return Result{}, fmt.Errorf("%w: content %s does not match locked %s",
			errs.ErrIntegrity, hashes.ContentHash, req.ExpectContentHash)
	}

	storePath, err := i.content.Put(ctx, hashes.ContentHash, skill.Dir, originFrom(req))
	if err != nil {
		return Result{}, err
	}

	mode, activePath, targets, modes, err := i.activateAll(ctx, req, installName(req, skill), storePath)
	if err != nil {
		return Result{}, err
	}

	return Result{
		Skill:         skill,
		ContentHash:   hashes.ContentHash,
		SkillFileHash: hashes.SkillFileHash,
		Mode:          mode,
		Modes:         modes,
		Agents:        agentIDs(req.Agents),
		ActivePath:    activePath,
		Targets:       targets,
		Warnings:      warnings,
		StoreReuse:    StoreDownloaded,
		StoreScope:    i.content.ScopeLabel(),
	}, nil
}

// installFromStore activates req directly from the verified content store,
// with no source fetch (spec 015 FR-006, FR-018). The stored object is
// verified before activation and fails closed on corruption (FR-020/021).
func (i *Installer) installFromStore(ctx context.Context, req Request) (Result, error) {
	hash := req.ExpectContentHash
	if err := i.content.Verify(hash); err != nil {
		return Result{}, err
	}
	contentPath := i.content.Path(hash)

	skill, err := discovery.Discover(contentPath, "")
	if err != nil {
		return Result{}, fmt.Errorf("discover stored content %s: %w", hash, err)
	}
	// Reused content gets the same validation a fresh fetch gets (FR-043):
	// admission validated it once, but the executable-bit warnings were not
	// persisted, and a symlink planted after admission must still fail closed
	// before this project activates the shared object.
	warnings, err := validateContent(contentPath)
	if err != nil {
		return Result{}, err
	}
	warnings = append(warnings, identityWarning(req.Name, skill.Frontmatter.Name)...)

	skillFile, err := os.ReadFile(filepath.Join(contentPath, integrity.SkillFileName)) //nolint:gosec // store-internal path
	if err != nil {
		return Result{}, fmt.Errorf("read stored %s: %w", integrity.SkillFileName, err)
	}

	// Close any live progress line: nothing is fetched for a store hit.
	progress.Emit(ctx, progress.Event{Phase: progress.PhaseDone, Repo: req.Ref.Display()})
	i.content.Touch(ctx, hash)

	if origin := originFrom(req); origin.Commit != "" {
		// Best-effort origin enrichment; identity never changes (FR-003).
		// Stores that record origins expose the metadata-only path; falling
		// back to Put would re-copy and re-verify the whole object just to
		// merge one metadata record.
		if rec, ok := i.content.(OriginRecorder); ok {
			if recErr := rec.RecordOrigin(ctx, hash, origin); recErr != nil {
				warnings = append(warnings, fmt.Sprintf("record origin for %s: %v", hash, recErr))
			}
		}
	}

	mode, activePath, targets, modes, err := i.activateAll(ctx, req, installName(req, skill), contentPath)
	if err != nil {
		return Result{}, err
	}

	return Result{
		Skill:         skill,
		ContentHash:   hash,
		SkillFileHash: integrity.HashContent(skillFile),
		Mode:          mode,
		Modes:         modes,
		Agents:        agentIDs(req.Agents),
		ActivePath:    activePath,
		Targets:       targets,
		Warnings:      warnings,
		StoreReuse:    StoreReused,
		StoreScope:    i.content.ScopeLabel(),
	}, nil
}

// originFrom derives the descriptive store origin from an install request.
func originFrom(req Request) ObjectOrigin {
	ref := req.Revision.Tag
	if ref == "" {
		ref = req.Revision.Branch
	}
	src := req.Ref.URL
	if src == "" {
		src = req.Ref.LocalPath
	}
	if src == "" {
		src = req.Ref.Display()
	}
	return ObjectOrigin{
		SourceType: string(req.Ref.Type),
		Source:     src,
		SkillPath:  req.Path,
		Version:    req.Revision.Version,
		Ref:        ref,
		Commit:     req.Revision.Commit,
	}
}

// Discover materializes the source and discovers the skill without activating
// it, for pre-flight checks such as learning the skill name or detecting a
// manifest conflict. Materialized git content is cached, so a following Install
// reuses it.
func (i *Installer) Discover(ctx context.Context, req Request) (discovery.Skill, error) {
	material, err := i.materialize(ctx, req)
	if err != nil {
		return discovery.Skill{}, err
	}
	skill, err := discovery.Discover(material, req.Path)
	if err != nil {
		return discovery.Skill{}, err
	}
	return skill, nil
}

// installName is the directory name a skill activates under: the selected
// folder-derived identity (req.Name) when set, else the frontmatter name. This
// keeps the on-disk skill directory keyed by identity, not editable frontmatter.
func installName(req Request, skill discovery.Skill) string {
	if req.Name != "" {
		return req.Name
	}
	return skill.Frontmatter.Name
}

// identityWarning reports a non-fatal warning when a skill's frontmatter name
// disagrees with the selected folder-derived identity. Identity comes from the
// folder (research R2/R3), so a mismatch is advisory, not a failure.
func identityWarning(selectedID, frontmatterName string) []string {
	if selectedID == "" || frontmatterName == "" {
		return nil
	}
	if discovery.NormalizeID(frontmatterName) == selectedID {
		return nil
	}
	return []string{fmt.Sprintf("frontmatter name %q does not match selected skill identity %q", frontmatterName, selectedID)}
}

// DiscoverAll materializes req's source (cache/clone, honoring Offline) then
// recursively scans it for skills. It is read-only: no staging, activation, or
// manifest/lock writes. Used by source inspection, search, and the add
// pre-flight (contracts/discovery.md).
func (i *Installer) DiscoverAll(ctx context.Context, req Request, opts discovery.Options) (discovery.Result, error) {
	// RootID defaults before the memo lookup so the key sees the effective
	// identity. A memo hit answers before materialize: the scan and even the
	// cache check are skipped, which per-skill progress may observe as
	// skipped phases (allowed — phases may be skipped, never regress).
	if opts.RootID == "" {
		opts.RootID = req.Ref.Repo
	}
	key, cacheable := i.scanCacheKeyFor(req, opts)
	if cacheable {
		if result, ok := i.scanCacheHit(ctx, req, key); ok {
			return result, nil
		}
	}
	material, err := i.materialize(ctx, req)
	if err != nil {
		return discovery.Result{}, err
	}
	result, err := discovery.DiscoverAll(material, opts)
	if err == nil && cacheable {
		i.scans.put(key, material, result)
	}
	return result, err
}

// scanCacheKeyFor reports the memo key for req/opts and whether this
// installer has a scan cache to consult at all.
func (i *Installer) scanCacheKeyFor(req Request, opts discovery.Options) (string, bool) {
	if i.scans == nil {
		return "", false
	}
	return scanCacheKey(req, opts)
}

// scanCacheHit reports a live memo hit for key, emitting the terminal cache
// event the materialize path would have fired (so a renderer's live line
// finishes) and handing out a defensive copy of the Skills slice so an
// in-place sort or filter by a consumer cannot corrupt the memo. A hit whose
// material was pruned mid-run (store gc) is forgotten so the caller falls
// through to materialize, exactly as a pre-memo cache miss would.
func (i *Installer) scanCacheHit(ctx context.Context, req Request, key string) (discovery.Result, bool) {
	e, ok := i.scans.get(key)
	if !ok {
		return discovery.Result{}, false
	}
	if _, err := os.Stat(e.dir); err != nil {
		i.scans.drop(key)
		return discovery.Result{}, false
	}
	progress.Emit(ctx, progress.Event{
		Phase: progress.PhaseCached,
		Repo:  req.Ref.Display(), Commit: req.Revision.Commit,
	})
	res := e.res
	res.Skills = slices.Clone(res.Skills)
	return res, true
}

// materialize returns a directory holding the source tree: the local path for
// local sources, or a cached/fetched checkout for git sources.
func (i *Installer) materialize(ctx context.Context, req Request) (string, error) {
	if req.Ref.Type == source.TypeLocal {
		// Local sources have nothing to fetch, but the terminal event still
		// fires so a renderer's live line finishes instead of dangling on the
		// last reported phase.
		progress.Emit(ctx, progress.Event{Phase: progress.PhaseDone, Repo: req.Ref.Display()})
		return req.Ref.LocalPath, nil
	}

	commit := req.Revision.Commit
	if commit == "" {
		return "", fmt.Errorf("%w: git source resolved without a commit", errs.ErrSourceUnavailable)
	}
	// The installer knows the repo identity and the cache outcome, so it
	// stamps both onto every progress event from here down.
	ctx = progress.Stamp(ctx, func(e *progress.Event) {
		e.Repo, e.Commit = req.Ref.Display(), commit
	})
	if i.cache.Has(commit) {
		progress.Emit(ctx, progress.Event{Phase: progress.PhaseCached})
		return i.cache.Path(commit), nil
	}
	if req.Offline {
		return "", fmt.Errorf("%w: offline and commit %s is not cached", errs.ErrSourceUnavailable, commit)
	}
	if i.git == nil {
		return "", fmt.Errorf("%w: no git runner configured", errs.ErrSourceUnavailable)
	}

	tmp, err := fsutil.TempDir(i.cache.Root(), ".fetch-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	progress.Emit(ctx, progress.Event{Phase: progress.PhaseFetching})
	if err := i.git.FetchCommit(ctx, req.Ref.URL, commit, tmp); err != nil {
		return "", err
	}
	dir, err := i.cache.Put(commit, tmp)
	if err != nil {
		return "", err
	}
	progress.Emit(ctx, progress.Event{Phase: progress.PhaseDone})
	return dir, nil
}

// EnsureCached materializes req's source into the commit cache without
// scanning or activating: the prefetch path's cache warmer. A cache hit is
// free; local sources are a no-op by materialize's contract.
func (i *Installer) EnsureCached(ctx context.Context, req Request) error {
	_, err := i.materialize(ctx, req)
	return err
}

// activateAll materializes the active layer and links/copies it into every
// target agent dir, returning the representative mode (the first agent's), the
// project-relative active path, the per-agent target paths, and the per-agent
// modes. For project scope each agent target derives from the shared active
// entry (.agents/skills/<name>), which itself links into the store, so a skill
// shared by N agents exists physically once. For global scope there is no
// project active layer, so agents derive directly from the store. Modes can
// differ per agent — a symlink falls back to a copy on a filesystem that rejects
// it — so each is recorded rather than collapsed to one value.
func (i *Installer) activateAll(ctx context.Context, req Request, name, storePath string) (Mode, string, map[string]string, map[string]string, error) {
	// linkTarget is what symlinked agents point at; copySource is the real
	// directory copy-mode agents (and copy fallbacks) read from. Copies always
	// read the resolved store content, never the active symlink itself.
	linkTarget := storePath
	copySource := storePath
	var activeRel string
	if req.Scope != ScopeGlobal {
		// Propagate EnsureActive's error code verbatim so a foreign-occupant
		// collision fails closed with its own exit code rather than being masked
		// as a generic partial install.
		// Both the resolved content root and the legacy project-local store
		// root are gskill-owned link targets: a stale link into either (e.g.
		// after the project transitions to the global store) re-points rather
		// than failing as foreign (spec 015 FR-011).
		activePath, err := active.EnsureActive(req.ProjectRoot, name, storePath,
			i.content.Root(), filepath.Join(req.ProjectRoot, ".gskill", "store"))
		if err != nil {
			return "", "", nil, nil, fmt.Errorf("ensure active %s: %w", name, err)
		}
		linkTarget = activePath
		activeRel = i.recordTarget(req, activePath)
	}

	targets := make(map[string]string, len(req.Agents))
	modes := make(map[string]string, len(req.Agents))
	primary := ModeSymlink

	for idx, ag := range req.Agents {
		dest := i.targetDir(ag, req, name)
		if err := i.guardForeignTarget(req, dest, storePath); err != nil {
			return "", "", nil, nil, err
		}
		usedMode, err := activateAgent(linkTarget, copySource, dest, agentActivation(req.ModePref, ag))
		if err != nil {
			return "", "", nil, nil, fmt.Errorf("%w: activate %s for %s: %w", errs.ErrPartialInstall, name, ag.ID(), err)
		}
		if idx == 0 {
			primary = usedMode
		}
		modes[ag.ID()] = string(usedMode)
		if err := ag.ValidateInstallation(ctx, dest); err != nil {
			return "", "", nil, nil, fmt.Errorf("%w: %w", errs.ErrPartialInstall, err)
		}
		targets[ag.ID()] = i.recordTarget(req, dest)
	}
	return primary, activeRel, targets, modes, nil
}

// guardForeignTarget fails closed when a PreserveForeign activation would
// replace a destination gskill does not own: not a symlink into the store or
// active layer, and not a directory matching the incoming or previously locked
// content (spec 011 FR-016; the guard sits directly before the destructive
// RemoveAll so no caller can bypass it).
func (i *Installer) guardForeignTarget(req Request, dest, storePath string) error {
	if !req.PreserveForeign {
		return nil
	}
	if _, err := os.Lstat(dest); err != nil {
		return nil //nolint:nilerr // absent destination: nothing to protect, activation proceeds
	}
	roots := []string{
		i.content.Root(), active.Dir(req.ProjectRoot),
		filepath.Join(req.ProjectRoot, ".gskill", "store"),
	}
	hashes := []string{req.PriorContentHash, req.ExpectContentHash}
	if h, err := integrity.HashDir(storePath); err == nil {
		hashes = append(hashes, h.ContentHash)
	}
	if active.Owned(dest, roots, hashes...) {
		return nil
	}
	return errs.WithHint(
		fmt.Errorf("%w: destination %s already exists and is not managed by gskill",
			errs.ErrInvalidLock, dest),
		"remove it, or re-run with --force to overwrite")
}

// targetDir resolves the per-agent destination directory for the skill.
func (i *Installer) targetDir(ag agent.Agent, req Request, name string) string {
	if req.Scope == ScopeGlobal {
		return filepath.Join(ag.GlobalSkillDir(req.Home), name)
	}
	return filepath.Join(ag.ProjectSkillDir(req.ProjectRoot), name)
}

// recordTarget returns the path stored in the lockfile: relative to the project
// root for project scope, absolute for global scope.
func (i *Installer) recordTarget(req Request, dest string) string {
	if req.Scope == ScopeGlobal {
		return dest
	}
	rel, err := filepath.Rel(req.ProjectRoot, dest)
	if err != nil {
		return dest
	}
	return rel
}

// activation is how an agent target is materialized.
type activation int

const (
	// activateAuto prefers a symlink and falls back to a copy when symlinks are
	// unsupported (the default).
	activateAuto activation = iota
	// activateCopy always copies (forced by --copy or an agent that rejects symlinks).
	activateCopy
	// activateSymlinkStrict requires a symlink (--symlink) and fails rather than
	// silently copying, so symlink-policy failures surface instead of being masked.
	activateSymlinkStrict
)

// agentActivation maps the requested mode preference and the agent's capability
// to a concrete activation strategy.
func agentActivation(modePref string, ag agent.Agent) activation {
	switch {
	case modePref == PrefCopy || !ag.SupportsSymlinks():
		return activateCopy
	case modePref == PrefSymlink:
		return activateSymlinkStrict
	default:
		return activateAuto
	}
}

// activateAgent places a skill at an agent's dest, reporting the mode used.
// A symlinked target points at linkTarget (the active entry, or the store for
// global scope); a copied target reads the resolved store content from
// copySource, never the active symlink, so copies hold real content rather than
// a recreated link.
func activateAgent(linkTarget, copySource, dest string, mode activation) (Mode, error) {
	if mode == activateCopy {
		if err := clearAndCopy(copySource, dest); err != nil {
			return "", err
		}
		return ModeCopy, nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		return "", fmt.Errorf("create target parent: %w", err)
	}
	if err := os.RemoveAll(dest); err != nil {
		return "", fmt.Errorf("clear target %s: %w", dest, err)
	}
	abs, err := filepath.Abs(linkTarget)
	if err != nil {
		return "", fmt.Errorf("resolve link target: %w", err)
	}
	linkErr := os.Symlink(abs, dest)
	if linkErr == nil {
		return ModeSymlink, nil
	}
	if mode == activateSymlinkStrict {
		return "", fmt.Errorf("%w: --symlink requested but linking %s failed: %w", errs.ErrPartialInstall, dest, linkErr)
	}
	if err := fsutil.CopyDir(copySource, dest); err != nil {
		return "", fmt.Errorf("copy fallback: %w", err)
	}
	return ModeCopy, nil
}

// clearAndCopy replaces dest with a fresh recursive copy of src.
func clearAndCopy(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return fmt.Errorf("create target parent: %w", err)
	}
	if err := os.RemoveAll(dst); err != nil {
		return fmt.Errorf("clear target: %w", err)
	}
	return fsutil.CopyDir(src, dst)
}

// agentIDs extracts the IDs of the given agents in order.
func agentIDs(agents []agent.Agent) []string {
	ids := make([]string, 0, len(agents))
	for _, ag := range agents {
		ids = append(ids, ag.ID())
	}
	return ids
}
