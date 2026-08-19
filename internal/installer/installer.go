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
	// PreserveForeign is set (a copy-mode install is a real directory), and
	// as the replaceable previous version of the repo-owned active entry.
	PriorContentHash string
	// LegacyStoreRoots are pre-022 store roots (e.g. the old home store):
	// stale active symlinks into them are replaced by the real copy.
	LegacyStoreRoots []string
	// ReplaceActive allows replacing a repo-owned active entry whose content
	// matches neither the expected nor the prior hash (drifted committed
	// content). Only explicit force/repair paths set it — plain add, install,
	// update, and sync fail closed on drift instead (spec 022 FR-008).
	ReplaceActive bool
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

// Store-reuse outcomes recorded on Result.StoreReuse: reused means no fetch
// happened (committed content or a clone-cache hit satisfied the install).
const (
	StoreReused     = "reused"
	StoreDownloaded = "downloaded"
)

// ScopeLabelCommitted labels installs served by the repo-owned model
// (spec 022): committed content or the commit-keyed clone cache.
const ScopeLabelCommitted = "committed"

// Installer runs the verify-activate transaction over the commit-keyed clone
// cache and git runner (spec 022: the repo owns skill content; there is no
// content store).
type Installer struct {
	git   git.Runner
	cache *cache.Cache
	scans *ScanCache // nil ⇒ no scan memoization
}

// New builds an Installer over the commit-keyed clone cache. The git runner
// may be nil for local-only installs.
func New(g git.Runner, c *cache.Cache) *Installer {
	return &Installer{git: g, cache: c}
}

// Install verifies and activates the requested skill (FR-015, FR-018,
// FR-019, FR-020; spec 022). Committed repo content matching the expected
// lock hash IS the restore — nothing is fetched and no store is consulted.
// Otherwise the source materializes via the commit-keyed clone cache
// (fetching only when cold) and activates directly from the materialization.
// Content is always verified before activating into any agent directory,
// failing closed on a checksum mismatch.
func (i *Installer) Install(ctx context.Context, req Request) (Result, error) {
	if res, done, cErr := i.installFromCommitted(ctx, req); done {
		return res, cErr
	}

	// A warm clone cache means no network fetch — the reuse decision the
	// no-refetch guardrail observes (SC-002).
	reuse := StoreDownloaded
	if req.Revision.Commit != "" && i.cache != nil && i.cache.Has(req.Revision.Commit) {
		reuse = StoreReused
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

	mode, activePath, targets, modes, err := i.activateAll(ctx, req, installName(req, skill), skill.Dir, hashes.ContentHash)
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
		StoreReuse:    reuse,
		StoreScope:    ScopeLabelCommitted,
	}, nil
}

// installFromCommitted implements the committed-content fast path (spec 022
// FR-007): the repo copy at .agents/skills/<name> matching the expected lock
// hash is the restore. Committed content that exists but mismatches is drift
// — an error with the repair hint (FR-008) — unless the caller explicitly
// reconciles (ReplaceActive), in which case the full pipeline replaces it.
func (i *Installer) installFromCommitted(ctx context.Context, req Request) (Result, bool, error) {
	dest, state := committedCandidate(req)
	switch state { //nolint:exhaustive // committedMatch falls through to the hit path below
	case committedAbsent:
		return Result{}, false, nil
	case committedDrifted:
		if req.ReplaceActive {
			return Result{}, false, nil // force/repair restores lock-true content
		}
		return Result{}, true, errs.WithHint(
			fmt.Errorf("%w: committed content for skill %q at %s no longer matches skills-lock.json",
				errs.ErrInvalidLock, req.Name, active.Rel(req.Name)),
			"run 'gskill repair' (or 'gskill install --force') to restore lock-true content, or re-add the skill to adopt the edited content as a new version")
	}

	skill, err := discovery.Discover(dest, "")
	if err != nil {
		return Result{}, true, fmt.Errorf("discover committed content for %q: %w", req.Name, err)
	}
	warnings, err := validateContent(dest)
	if err != nil {
		return Result{}, true, err
	}
	warnings = append(warnings, identityWarning(req.Name, skill.Frontmatter.Name)...)
	skillFile, err := os.ReadFile(filepath.Join(dest, integrity.SkillFileName)) //nolint:gosec // repo-owned active entry
	if err != nil {
		return Result{}, true, fmt.Errorf("read committed %s: %w", integrity.SkillFileName, err)
	}

	// Close any live progress line: nothing is fetched for a committed hit.
	progress.Emit(ctx, progress.Event{Phase: progress.PhaseDone, Repo: req.Ref.Display()})

	mode, activePath, targets, modes, err := i.activateAll(ctx, req, installName(req, skill), dest, req.ExpectContentHash)
	if err != nil {
		return Result{}, true, err
	}
	return Result{
		Skill:         skill,
		ContentHash:   req.ExpectContentHash,
		SkillFileHash: integrity.HashContent(skillFile),
		Mode:          mode,
		Modes:         modes,
		Agents:        agentIDs(req.Agents),
		ActivePath:    activePath,
		Targets:       targets,
		Warnings:      warnings,
		StoreReuse:    StoreReused,
		StoreScope:    ScopeLabelCommitted,
	}, true, nil
}

// committedState classifies the repo-owned active entry for the fast path.
type committedState int

const (
	committedAbsent  committedState = iota // no usable committed dir: full pipeline
	committedMatch                         // matches the expected lock hash
	committedDrifted                       // a real dir that no longer matches
)

// committedCandidate inspects the active entry for the fast path: only a
// real directory counts (legacy links and artifacts take the full pipeline),
// and only project-scope lock-driven installs are eligible.
func committedCandidate(req Request) (string, committedState) {
	if req.ExpectContentHash == "" || req.Name == "" || req.Scope == ScopeGlobal {
		return "", committedAbsent
	}
	dest := active.Path(req.ProjectRoot, req.Name)
	info, err := os.Lstat(dest)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return dest, committedAbsent
	}
	ok, _, err := integrity.VerifyDir(dest, req.ExpectContentHash)
	if err != nil {
		return dest, committedAbsent // unreadable: let the full pipeline decide
	}
	if !ok {
		return dest, committedDrifted
	}
	return dest, committedMatch
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
		return "", errs.WithHint(
			fmt.Errorf("%w: offline and commit %s is not cached", errs.ErrSourceUnavailable, commit),
			"drop --offline to fetch the commit, or restore on a machine whose clone cache holds it")
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
// modes. For project scope the repo owns the content (spec 022): the active
// entry .agents/skills/<name> is a real copied directory verified against
// contentHash, and each agent target is a *relative* symlink into it, so both
// are committable and survive a clone. For global scope there is no project
// active layer, so agents derive directly from the store. Modes can differ
// per agent — a symlink falls back to a copy on a filesystem that rejects it
// — so each is recorded rather than collapsed to one value.
func (i *Installer) activateAll(ctx context.Context, req Request, name, storePath, contentHash string) (Mode, string, map[string]string, map[string]string, error) {
	// linkTarget is what symlinked agents point at; copySource is the real
	// directory copy-mode agents (and copy fallbacks) read from. For project
	// scope both are the repo-owned active entry itself.
	linkTarget := storePath
	copySource := storePath
	var activeRel string
	if req.Scope != ScopeGlobal {
		// Propagate EnsureActive's error code verbatim so a foreign-occupant
		// collision fails closed with its own exit code rather than being masked
		// as a generic partial install. Stale symlinks into a pre-022 store
		// root (resolved content root or legacy project-local store) are
		// replaced by the real copy; anything else foreign fails closed.
		activePath, err := active.EnsureActive(req.ProjectRoot, name, storePath, active.EnsureOptions{
			ExpectedHash: contentHash,
			AcceptHashes: []string{req.PriorContentHash, req.ExpectContentHash},
			Replace:      req.ReplaceActive,
			LegacyRoots:  append([]string{filepath.Join(req.ProjectRoot, ".gskill", "store")}, req.LegacyStoreRoots...),
		})
		if err != nil {
			return "", "", nil, nil, fmt.Errorf("ensure active %s: %w", name, err)
		}
		linkTarget = activePath
		copySource = activePath
		activeRel = i.recordTarget(req, activePath)
	} else if abs, err := filepath.Abs(linkTarget); err == nil {
		linkTarget = abs
	}

	targets := make(map[string]string, len(req.Agents))
	modes := make(map[string]string, len(req.Agents))
	primary := ModeSymlink

	for idx, ag := range req.Agents {
		dest := i.targetDir(ag, req, name)
		if err := i.guardForeignTarget(req, dest, storePath); err != nil {
			return "", "", nil, nil, err
		}
		// Project-scope agent links store a relative target (spec 022: no
		// committed artifact may reference a path outside the repo).
		linkRef := linkTarget
		if req.Scope != ScopeGlobal {
			if rel, rErr := filepath.Rel(filepath.Dir(dest), linkTarget); rErr == nil {
				linkRef = rel
			}
		}
		act := agentActivation(req.ModePref, ag)
		if req.Scope == ScopeGlobal {
			// Agent-global installs are direct copies from the materialized
			// content (spec 022: no store to link into; cache entries are
			// evictable and must not be link targets).
			act = activateCopy
		}
		usedMode, err := activateAgent(linkRef, copySource, dest, act)
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
	// Ownership keys on the repo's .agents/skills root (spec 022).
	roots := []string{active.Dir(req.ProjectRoot)}
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

// recordTarget returns the path stored in the lockfile: repo-root-relative
// with forward slashes for project scope (spec 022 path rule), absolute for
// global scope.
func (i *Installer) recordTarget(req Request, dest string) string {
	if req.Scope == ScopeGlobal {
		return dest
	}
	rel, err := filepath.Rel(req.ProjectRoot, dest)
	if err != nil {
		return dest
	}
	return filepath.ToSlash(rel)
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
// A symlinked target stores linkTarget verbatim — the caller passes a
// relative path for project scope (spec 022: committed links must not embed
// absolute paths) and an absolute one for global scope. A copied target reads
// the real content from copySource (the repo-owned active entry for project
// scope).
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
	linkErr := os.Symlink(linkTarget, dest)
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
