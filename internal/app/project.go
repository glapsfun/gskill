package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/glapsfun/gskill/internal/cache"
	"github.com/glapsfun/gskill/internal/config"
	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/home"
	"github.com/glapsfun/gskill/internal/installer"
	"github.com/glapsfun/gskill/internal/manifest"
	"github.com/glapsfun/gskill/internal/projstate"
	"github.com/glapsfun/gskill/internal/skillslock"
)

// Project directory names. The canonical committed lockfile is
// skills-lock.json (skillslock.FileName, spec 012).
const stateDirName = ".gskill"

// errNoLock is the shared missing-lock failure, carrying the next step as a
// hint so every command reports it identically.
func errNoLock() error {
	return errs.WithHint(
		fmt.Errorf("%w: no %s found", errs.ErrInvalidLock, skillslock.FileName),
		"run 'gskill add <source>' to install a first skill, or clone a project that commits one",
	)
}

// project bundles the resolved paths for one project root (spec 022): the
// repo owns skill content; the home contributes only the commit-keyed clone
// cache and the locks directory.
type project struct {
	root     string
	lockPath string
	cache    *cache.Cache // home commit-keyed clone cache
	locksDir string       // home locks dir
	homeRoot string       // gskill home root (legacy-store detection only)
}

// openProject resolves the project's repo-level paths only — enough for
// read-only lock access. Mutating flows need openProjectScoped (home-backed
// cache and locks).
func openProject(root string) *project {
	return &project{
		root:     root,
		lockPath: filepath.Join(root, skillslock.FileName),
	}
}

// openProjectScoped resolves the project layout under root: repo-level paths
// plus the home-backed clone cache and locks (spec 022 — exactly one storage
// concept, the repo's .agents/skills; the home is a cache).
func (a *App) openProjectScoped(root string) (*project, error) {
	h, err := a.openHome()
	if err != nil {
		return nil, fmt.Errorf("open gskill home: %w", err)
	}
	p := openProject(root)
	p.cache = cache.New(h.CacheDir())
	p.locksDir = h.LocksDir()
	p.homeRoot = h.Root()
	return p, nil
}

// legacyStoreRoots names the pre-022 store locations whose stale symlinks are
// still recognized (classified legacy, converted by migration): the old home
// store and the old project-local store.
func (p *project) legacyStoreRoots() []string {
	roots := []string{filepath.Join(p.root, stateDirName, "store")}
	if p.homeRoot != "" {
		roots = append(roots, filepath.Join(p.homeRoot, "store"))
	}
	return roots
}

// storeLockTimeout returns the configured lock-acquisition timeout, clamping
// non-positive values (a zero-valued config, or "0s" in config.toml) to the
// documented 60s default. Every store/registry Locker construction must go
// through this — a raw 0 makes fsutil.Acquire fail instantly even uncontended.
// storeLockTimeout resolves the timeout with the project's own [config]
// table merged in (spec 023 FR-002). A lock timeout is inherently
// project-scoped — it describes contention on one repository — so a project
// that needs a longer one must be able to declare it and have every teammate
// inherit it from the committed manifest.
func (a *App) storeLockTimeout(root string) time.Duration {
	cfg := a.cfg
	if root != "" {
		if merged, err := a.projectConfig(root); err == nil && merged != nil {
			cfg = merged
		}
	}
	if t := cfg.StoreLockTimeout; t > 0 {
		return t
	}
	return 60 * time.Second
}

// projectConfig re-resolves configuration with the manifest's [config] table
// occupying the project layer. It is computed per call rather than cached on
// the App: the manifest is a committed file a user edits between runs, and a
// stale cached layer would silently ignore their edit.
//
// It carries the user config file in effect (a.configFile, empty until
// ApplyRuntimeConfig has run) so that this second resolution agrees with
// a.cfg. Omitting it would drop the user layer for every caller whose project
// declares any [config] table at all — silently reverting a user-configured
// store.lock_timeout to the default, which is exactly the class of
// disagreement spec 026 exists to remove (FR-008).
//
// The file is not required here: it was already validated when the run
// resolved configuration, and a file that vanished mid-run should not fail a
// lock acquisition.
func (a *App) projectConfig(root string) (*config.Config, error) {
	projectMap, err := manifest.ProjectConfig(root)
	if err != nil || len(projectMap) == 0 {
		return nil, err
	}
	return config.Load(config.Sources{UserFile: a.configFile, ProjectMap: projectMap})
}

// openHome resolves and ensures the gskill home: the App-level override when
// set (test isolation), else GSKILL_HOME / ~/.gskill.
func (a *App) openHome() (*home.Home, error) {
	if a.gskillHome != "" {
		h := home.New(a.gskillHome)
		if err := h.Ensure(); err != nil {
			return nil, err
		}
		return h, nil
	}
	return home.Open()
}

// CacheDir returns the home commit-keyed clone-cache directory.
func (a *App) CacheDir() (string, error) {
	h, err := a.openHome()
	if err != nil {
		return "", err
	}
	return h.CacheDir(), nil
}

// hasPopulatedProjectStore reports whether the legacy project-local store
// holds at least one object.
func hasPopulatedProjectStore(root string) bool {
	storeRoot := filepath.Join(root, stateDirName, "store")
	algos, err := os.ReadDir(storeRoot)
	if err != nil {
		return false
	}
	for _, algo := range algos {
		if !algo.IsDir() {
			continue
		}
		entries, err := os.ReadDir(filepath.Join(storeRoot, algo.Name()))
		if err == nil && len(entries) > 0 {
			return true
		}
	}
	return false
}

// installerFor builds an installer over the project's home clone cache
// (spec 022: content activates from committed copies or the cache; there is
// no content store).
func (a *App) installerFor(p *project) *installer.Installer {
	return installer.New(a.git, p.cache).WithScanCache(a.scans)
}

// mutateLockPath returns the project's exclusive mutate-lock file: a
// per-project name inside the shared home locks dir (project-<id>.lock) — a
// fixed name there would serialize every project on the machine.
func (p *project) mutateLockPath() string {
	sum := sha256.Sum256([]byte(canonicalRoot(p.root)))
	return filepath.Join(p.locksDir, "project-"+hex.EncodeToString(sum[:8])+".lock")
}

// canonicalRoot resolves root to one canonical absolute path so every
// spelling of the same project directory (relative -C path, symlinked
// prefix) derives the same identity — the per-project lock and registry
// entry must agree across processes or mutual exclusion silently fails.
func canonicalRoot(root string) string {
	abs, err := filepath.Abs(root)
	if err != nil {
		return filepath.Clean(root)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return abs
}

// installerForScope builds the installer for any agent-target scope: both
// project and agent-global installs are served by the home clone cache
// (spec 022 — global installs are direct copies from materialization).
func (a *App) installerForScope(p *project, _ string) *installer.Installer {
	return a.installerFor(p)
}

// fileExists reports whether path exists.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// recordProjectState persists the machine-local state after a successful
// lock mutation. Pure bookkeeping: failures warn, never fail the run.
func (a *App) recordProjectState(_ context.Context, p *project, lf *skillslock.State) {
	if err := writeProjectState(p, lf); err != nil {
		a.log.Warn("write project state", "error", err)
	}
}

// writeProjectState derives the project's machine-local state.json from the
// lock records after a successful run: the gskill-created agent targets and
// their per-machine materialization modes (spec 022 data-model §4). The file
// is bookkeeping for repair and removal only — reproduction never needs it
// (FR-015); content identity lives in the committed lockfile.
func writeProjectState(p *project, lf *skillslock.State) error {
	st, err := projstate.LoadOrInit(p.root)
	if err != nil {
		return err
	}
	for name, rec := range lf.Skills {
		var sk projstate.SkillState
		if len(rec.Installation.Targets) > 0 {
			sk.Agents = make(map[string]projstate.AgentState, len(rec.Installation.Targets))
			for id, target := range rec.Installation.Targets {
				sk.Agents[id] = projstate.AgentState{
					Target: target,
					Mode:   rec.Installation.Modes[id],
				}
			}
		}
		st.SetSkill(name, sk)
	}
	// Drop state entries the lock no longer manages.
	for name := range st.Skills {
		if _, ok := lf.Skills[name]; !ok {
			st.RemoveSkill(name)
		}
	}
	return st.Save()
}
