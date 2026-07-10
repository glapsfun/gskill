package app

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/glapsfun/gskill/internal/cache"
	"github.com/glapsfun/gskill/internal/config"
	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/installer"
	"github.com/glapsfun/gskill/internal/skillslock"
	"github.com/glapsfun/gskill/internal/store"
)

// Project file and directory names. The canonical committed lockfile is
// skills-lock.json (skillslock.FileName, spec 012); LockName is the legacy
// gskill.lock, read only for migration.
const (
	ManifestName = "gskill.toml"
	LockName     = "gskill.lock"
	stateDirName = ".gskill"
)

// errNoManifest is the shared missing-manifest failure, carrying the next
// step as a hint so every command reports it identically.
func errNoManifest() error {
	return errs.WithHint(
		fmt.Errorf("%w: no %s found", errs.ErrInvalidManifest, ManifestName),
		"run 'gskill init' to create one")
}

// project bundles the resolved paths and content stores for one project root.
type project struct {
	root         string
	manifestPath string
	lockPath     string
	store        *store.Store
	cache        *cache.Cache
	locksDir     string
}

// openProject resolves the project layout under root.
func openProject(root string) *project {
	stateDir := filepath.Join(root, stateDirName)
	return &project{
		root:         root,
		manifestPath: filepath.Join(root, ManifestName),
		lockPath:     filepath.Join(root, skillslock.FileName),
		store:        store.New(filepath.Join(stateDir, "store")),
		cache:        cache.New(filepath.Join(stateDir, "cache")),
		locksDir:     filepath.Join(stateDir, "locks"),
	}
}

// installerFor builds an installer wired to this project's stores.
func (a *App) installerFor(p *project) *installer.Installer {
	return installer.New(a.git, p.cache, p.store)
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

// manifestExists reports whether the project has a manifest.
func (p *project) manifestExists() bool {
	_, err := os.Stat(p.manifestPath)
	return err == nil
}

// ManifestExists reports whether root holds a gskill.toml (used by the CLI to
// route between the lock-first and manifest-driven install paths, spec 012).
func (a *App) ManifestExists(root string) bool {
	return openProject(root).manifestExists()
}
