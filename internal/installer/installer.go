package installer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/cache"
	"github.com/glapsfun/gskill/internal/discovery"
	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/fsutil"
	"github.com/glapsfun/gskill/internal/git"
	"github.com/glapsfun/gskill/internal/integrity"
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
}

// Result is the outcome of a successful install, sufficient to build a lock entry.
type Result struct {
	Skill         discovery.Skill
	ContentHash   string
	SkillFileHash string
	Mode          Mode              // representative mode (the first agent's)
	Modes         map[string]string // agentID -> actual mode used
	Agents        []string
	Targets       map[string]string // agentID -> recorded dir (relative for project scope)
	Warnings      []string
}

// Installer runs the staging-verify-activate transaction over the store, cache,
// and git runner.
type Installer struct {
	git   git.Runner
	cache *cache.Cache
	store *store.Store
}

// New builds an Installer. The git runner may be nil for local-only installs.
func New(g git.Runner, c *cache.Cache, s *store.Store) *Installer {
	return &Installer{git: g, cache: c, store: s}
}

// Install materializes, verifies, and activates the requested skill (FR-015,
// FR-018, FR-019, FR-020). It verifies content before activating into any agent
// directory, failing closed on a checksum mismatch.
func (i *Installer) Install(ctx context.Context, req Request) (Result, error) {
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

	storePath, err := i.stageAndVerify(hashes.ContentHash, skill.Dir)
	if err != nil {
		return Result{}, err
	}

	mode, targets, modes, err := i.activateAll(ctx, req, installName(req, skill), storePath)
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
		Targets:       targets,
		Warnings:      warnings,
	}, nil
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
	material, err := i.materialize(ctx, req)
	if err != nil {
		return discovery.Result{}, err
	}
	if opts.RootID == "" {
		opts.RootID = req.Ref.Repo
	}
	return discovery.DiscoverAll(material, opts)
}

// materialize returns a directory holding the source tree: the local path for
// local sources, or a cached/fetched checkout for git sources.
func (i *Installer) materialize(ctx context.Context, req Request) (string, error) {
	if req.Ref.Type == source.TypeLocal {
		return req.Ref.LocalPath, nil
	}

	commit := req.Revision.Commit
	if commit == "" {
		return "", fmt.Errorf("%w: git source resolved without a commit", errs.ErrSourceUnavailable)
	}
	if i.cache.Has(commit) {
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

	if err := i.git.FetchCommit(ctx, req.Ref.URL, commit, tmp); err != nil {
		return "", err
	}
	return i.cache.Put(commit, tmp)
}

// stageAndVerify stores the skill content and re-hashes the stored copy,
// failing closed if it does not match the expected hash (FR-015).
func (i *Installer) stageAndVerify(contentHash, skillDir string) (string, error) {
	storePath, err := i.store.Put(contentHash, skillDir)
	if err != nil {
		return "", err
	}
	check, err := integrity.HashDir(storePath)
	if err != nil {
		return "", err
	}
	if check.ContentHash != contentHash {
		return "", fmt.Errorf("%w: stored content %s != expected %s",
			errs.ErrIntegrity, check.ContentHash, contentHash)
	}
	return storePath, nil
}

// activateAll links or copies the stored content into every target agent dir,
// returning the representative mode (the first agent's), the per-agent modes,
// and the per-agent target paths. Modes can differ per agent — e.g. a symlink
// falls back to a copy on a filesystem that rejects it — so each is recorded
// rather than collapsed to one value.
func (i *Installer) activateAll(ctx context.Context, req Request, name, storePath string) (Mode, map[string]string, map[string]string, error) {
	targets := make(map[string]string, len(req.Agents))
	modes := make(map[string]string, len(req.Agents))
	primary := ModeSymlink

	for idx, ag := range req.Agents {
		dest := i.targetDir(ag, req, name)
		usedMode, err := activate(storePath, dest, wantCopy(req.ModePref, ag))
		if err != nil {
			return "", nil, nil, fmt.Errorf("%w: activate %s for %s: %w", errs.ErrPartialInstall, name, ag.ID(), err)
		}
		if idx == 0 {
			primary = usedMode
		}
		modes[ag.ID()] = string(usedMode)
		if err := ag.ValidateInstallation(ctx, dest); err != nil {
			return "", nil, nil, fmt.Errorf("%w: %w", errs.ErrPartialInstall, err)
		}
		targets[ag.ID()] = i.recordTarget(req, dest)
	}
	return primary, targets, modes, nil
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

// wantCopy reports whether activation must copy rather than symlink.
func wantCopy(modePref string, ag agent.Agent) bool {
	return modePref == "copy" || !ag.SupportsSymlinks()
}

// activate links or copies storePath into dest and reports the mode used.
func activate(storePath, dest string, forceCopy bool) (Mode, error) {
	if forceCopy {
		if err := clearAndCopy(storePath, dest); err != nil {
			return "", err
		}
		return ModeCopy, nil
	}
	symlinked, err := fsutil.SymlinkOrCopy(storePath, dest)
	if err != nil {
		return "", err
	}
	if symlinked {
		return ModeSymlink, nil
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
