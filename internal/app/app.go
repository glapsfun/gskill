// Package app is gskill's orchestration layer. It exposes use-case methods that
// the cli and tui views call, and is the only layer that drives the domain
// packages (resolver, installer, store, and the rest). Views never import the
// domain packages directly.
package app

import (
	"context"
	"log/slog"

	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/config"
	"github.com/glapsfun/gskill/internal/git"
	"github.com/glapsfun/gskill/internal/installer"
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
	log        *slog.Logger
	agents     *agent.Registry
	git        git.Runner
	repos      RepoLister
	gskillHome string
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
	return &App{
		cfg: cfg, log: logger, agents: agents, git: gitRunner, repos: repos,
		gskillHome: opts.GskillHome,
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
