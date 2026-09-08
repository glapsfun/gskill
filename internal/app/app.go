// Package app is gskill's orchestration layer. It exposes use-case methods that
// the cli and tui views call, and is the only layer that drives the domain
// packages (resolver, installer, store, and the rest). Views never import the
// domain packages directly.
package app

import (
	"context"
	"io"
	"log/slog"
	"os"
	"sync"

	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/config"
	"github.com/glapsfun/gskill/internal/git"
	"github.com/glapsfun/gskill/internal/installer"
	"github.com/glapsfun/gskill/internal/logging"
	"github.com/glapsfun/gskill/internal/registry"
)

// RepoLister lists a GitHub owner's repositories, so `find --owner` can fan out
// across them. The default implementation calls the GitHub REST API; tests
// inject a fake.
type RepoLister interface {
	ListOwnerRepos(ctx context.Context, owner string) ([]registry.RepoRef, error)
}

// App holds the injected dependencies shared by every use-case. Business logic
// is added by sibling files (install.go, inspect.go, lifecycle.go, ...).
type App struct {
	cfg        *config.Config
	manifests  map[string]manifestCacheEntry
	manifestMu sync.Mutex
	log        *slog.Logger
	agents     *agent.Registry
	git        git.Runner
	repos      RepoLister
	gskillHome string
	notice     io.Writer // one-line user notices (auto-migration); default os.Stderr
	// scans memoizes repo scans per immutable commit across the per-call
	// installer instances (spec: Layer C).
	scans *installer.ScanCache
}

// Options configures New. Nil dependencies are replaced with safe defaults.
type Options struct {
	Config *config.Config
	Logger *slog.Logger
	Agents *agent.Registry
	Git    git.Runner
	Repos  RepoLister
	// GskillHome overrides the resolved gskill home directory (default:
	// GSKILL_HOME env, else ~/.gskill). Tests use it for isolated stores.
	GskillHome string
	// Notice receives one-line user notices (the auto-migration summary,
	// spec 022 FR-013). Defaults to os.Stderr.
	Notice io.Writer
}

// New builds an App from opts, filling in defaults for any nil dependency.
func New(opts Options) *App {
	cfg := opts.Config
	if cfg == nil {
		// Built-in defaults, not a zero value: zero-valued fields (e.g.
		// StoreVerifyOnUse=false, StoreLockTimeout=0) would silently disable
		// documented safety behavior.
		cfg = config.Default()
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	agents := opts.Agents
	if agents == nil {
		agents = agent.NewRegistry()
	}
	gitRunner := opts.Git
	if gitRunner == nil {
		gitRunner = git.NewSystemRunner()
	}
	// Every runner — injected or default — is memoize-wrapped; caching only
	// activates under a context armed by git.WithMemo at a batch entry
	// point, so single-shot library calls and tests see passthrough.
	gitRunner = git.Memoize(gitRunner)
	repos := opts.Repos
	if repos == nil {
		repos = registry.New()
	}
	notice := opts.Notice
	if notice == nil {
		notice = os.Stderr
	}
	return &App{
		cfg: cfg, log: logger, agents: agents, git: gitRunner, repos: repos,
		gskillHome: opts.GskillHome,
		notice:     notice,
		scans:      installer.NewScanCache(),
	}
}

// Config returns the resolved configuration.
func (a *App) Config() *config.Config { return a.cfg }

// GskillHome returns the App-level home override ("" when the environment
// resolution applies). Tests use it to locate their private store.
func (a *App) GskillHome() string { return a.gskillHome }

// Logger returns the structured logger.
func (a *App) Logger() *slog.Logger { return a.log }

// Agents returns the agent registry.
func (a *App) Agents() *agent.Registry { return a.agents }

// ApplyProjectConfig re-resolves configuration with the project's own [config]
// table in the project layer, and rebuilds the logger to match (spec 023
// FR-002).
//
// It runs once the project directory is known, which is necessarily after
// startup: configuration is loaded before any command parses -C, so the
// project layer cannot exist yet at that point. Without this step a declared
// log_level would appear in `config list` and change nothing, which is worse
// than not supporting it.
//
// A missing or malformed manifest leaves the current configuration untouched:
// the manifest's own validation reports the problem where it can say something
// useful, and startup is not that place.
func (a *App) ApplyProjectConfig(root string) {
	if root == "" {
		return
	}
	cfg, err := a.projectConfig(root)
	if err != nil || cfg == nil {
		return
	}
	a.cfg = cfg
	a.log = logging.New(logging.Options{
		Level:  logging.ParseLevel(cfg.LogLevel),
		Format: logging.Format(cfg.LogFormat),
	})
}
