package app

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/glapsfun/gskill/internal/cache"
	"github.com/glapsfun/gskill/internal/config"
	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/globalstore"
	"github.com/glapsfun/gskill/internal/home"
	"github.com/glapsfun/gskill/internal/installer"
	"github.com/glapsfun/gskill/internal/projstate"
	"github.com/glapsfun/gskill/internal/skillslock"
	"github.com/glapsfun/gskill/internal/store"
)

// Project directory names. The canonical committed lockfile is
// skills-lock.json (skillslock.FileName, spec 012).
const stateDirName = ".gskill"

// errNoLock is the shared missing-lock failure, carrying the next step as a
// hint so every command reports it identically.
func errNoLock() error {
	return errs.WithHint(
		fmt.Errorf("%w: no %s found", errs.ErrInvalidLock, skillslock.FileName),
		"run 'gskill add <source>' to install a first skill, or clone a project that commits one")
}

// project bundles the resolved paths and content stores for one project root.
type project struct {
	root     string
	lockPath string
	store    *store.Store
	cache    *cache.Cache
	locksDir string

	// storeScope is the resolved physical content-store scope for this
	// project: config.StoreScopeGlobal or config.StoreScopeProject (FR-039).
	// Distinct from the installer's agent-target scope.
	storeScope string
	// global is the user-level content store; set when storeScope is global.
	global *globalstore.Store
}

// openProject resolves the project layout under root with the legacy
// project-local store (scope=project). Behavior is unchanged for existing
// callers; global-scope wiring happens in openProjectScoped.
func openProject(root string) *project {
	stateDir := filepath.Join(root, stateDirName)
	return &project{
		root:       root,
		lockPath:   filepath.Join(root, skillslock.FileName),
		store:      store.New(filepath.Join(stateDir, "store")),
		cache:      cache.New(filepath.Join(stateDir, "cache")),
		locksDir:   filepath.Join(stateDir, "locks"),
		storeScope: config.StoreScopeProject,
	}
}

// openProjectScoped resolves the project layout under root, selecting the
// physical content store per the configured store scope (FR-039). Global
// scope wires the user-level store, cache, and locks under the gskill home;
// project scope preserves the legacy layout byte-for-byte.
func (a *App) openProjectScoped(root string) (*project, error) {
	scope := resolveStoreScope(a.cfg.StoreScope, root)
	p := openProject(root)
	if scope != config.StoreScopeGlobal {
		return p, nil
	}

	h, err := a.openHome()
	if err != nil {
		return nil, fmt.Errorf("open gskill home: %w", err)
	}
	timeout := a.cfg.StoreLockTimeout
	if timeout <= 0 {
		timeout = 60 * time.Second // config default; guards zero-valued configs
	}
	gs := globalstore.New(h)
	gs.SetLocker(globalstore.NewLocker(h, timeout, os.Stderr))

	p.storeScope = config.StoreScopeGlobal
	p.global = gs
	p.cache = cache.New(h.CacheDir())
	p.locksDir = h.LocksDir()
	return p, nil
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

// resolveStoreScope applies the transition-period defaults (research R9):
// an explicit scope wins; otherwise a project with a populated legacy
// .gskill/store keeps project scope until migrated, and everything else uses
// the global store.
func resolveStoreScope(configured, root string) string {
	switch configured {
	case config.StoreScopeGlobal, config.StoreScopeProject:
		return configured
	}
	if hasPopulatedProjectStore(root) {
		return config.StoreScopeProject
	}
	return config.StoreScopeGlobal
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

// installerFor builds an installer wired to this project's resolved content
// store: the user-level global store for scope=global, else the legacy
// project-local store (spec 015 FR-006, FR-039).
func (a *App) installerFor(p *project) *installer.Installer {
	if p.storeScope == config.StoreScopeGlobal && p.global != nil {
		return installer.NewWithStore(a.git, p.cache, newGlobalContentStore(p.global, a.cfg))
	}
	return installer.New(a.git, p.cache, p.store)
}

// contentHas reports whether the project's resolved content store holds hash.
func (p *project) contentHas(hash string) bool {
	if p.storeScope == config.StoreScopeGlobal && p.global != nil {
		return p.global.Has(hash)
	}
	return p.store.Has(hash)
}

// contentPath returns the resolved content directory for hash in the
// project's content store.
func (p *project) contentPath(hash string) string {
	if p.storeScope == config.StoreScopeGlobal && p.global != nil {
		return p.global.ContentPath(hash)
	}
	return p.store.Path(hash)
}

// installerForScope builds an installer using the global store/cache for global
// scope, or the project's for project scope (FR-028).
func (a *App) installerForScope(p *project, scope string) *installer.Installer {
	if scope != string(installer.ScopeGlobal) {
		return a.installerFor(p)
	}
	cfgDir, err1 := config.Dir()
	cacheDir, err2 := config.CacheDir()
	if err1 != nil || err2 != nil {
		return a.installerFor(p)
	}
	return installer.New(a.git,
		cache.New(cacheDir),
		store.New(filepath.Join(cfgDir, "store")))
}

// fileExists reports whether path exists.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// writeProjectState derives the project's machine-local state.json from the
// lock records after a successful run: which store object each skill
// activates, the gskill-owned active and agent targets, and the resolved
// materialization modes (FR-014). The file is bookkeeping for repair and
// removal only — reproduction never needs it (FR-015).
func writeProjectState(p *project, lf *skillslock.State) error {
	st, err := projstate.LoadOrInit(p.root)
	if err != nil {
		return err
	}
	for name, rec := range lf.Skills {
		sk := projstate.SkillState{
			StoreHash:    rec.Resolved.ContentHash,
			StoreScope:   p.storeScope,
			ActiveTarget: rec.Installation.ActivePath,
			ActiveMode:   rec.Installation.Mode,
		}
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
