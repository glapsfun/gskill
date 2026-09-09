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
	"github.com/glapsfun/gskill/internal/manifest"
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
	configFile string // user config file in effect; see ApplyRuntimeConfig
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

// ConfigFile returns the user configuration file in effect for this run: the
// path named by --config when one was given, otherwise the discovered
// config.UserFile(). It is the path `gskill config list` reports, and it is
// meaningful whether or not the file exists.
func (a *App) ConfigFile() string { return a.configFile }

// ApplyRuntimeConfig re-resolves configuration with the two layers that only
// become knowable once the command line has been parsed — the user file and
// the project's own [config] table — rebuilds the logger to match, and records
// the user config file in effect.
//
// It runs after the parse because it must: configuration is loaded at startup,
// before any command parses -C or --config, so neither layer can exist at that
// point. Without this step a declared log_level would appear in `config list`
// and change nothing, which is worse than not supporting it (spec 023 FR-002,
// spec 026 FR-001).
//
// A missing or malformed manifest leaves the project layer empty rather than
// failing the run: the manifest's own validation reports the problem where it
// can say something useful, and configuration resolution is not that place. A
// malformed *config* file is different — nothing else will report it, so it is
// returned (spec 026 FR-003).
//
// It replaces any Config and Logger supplied through Options, because the
// layers resolved here sit above anything an embedder could have known at
// construction time.
func (a *App) ApplyRuntimeConfig(root, userFile string) error {
	// The user layer comes from --config when one was given and from the
	// discovered path otherwise; the two never merge, so a key the named file
	// leaves unset falls through to the defaults rather than to the discovered
	// file (spec 026 FR-005). A named file is required — a path the user typed
	// and misspelled must fail rather than be skipped (FR-003) — while the
	// discovered one is optional, because not having one is the normal case
	// (FR-006).
	src := config.Sources{UserFile: userFile, RequireUserFile: userFile != ""}
	if src.UserFile == "" {
		// Discovery is deliberately lazy and non-fatal. It needs a resolvable
		// configuration directory, which a bare environment (no HOME, no
		// XDG_CONFIG_HOME) does not have — and commands that read no
		// configuration at all, `version` among them, worked there before this
		// layer existed and must keep working. When the directory cannot be
		// resolved there is simply no user layer; `config list`, whose subject
		// *is* the path, reports the failure where it means something.
		if discovered, dErr := config.UserFile(); dErr == nil {
			src.UserFile = discovered
		}
	}
	if root != "" {
		if projectMap, mErr := manifest.ProjectConfig(root); mErr == nil {
			src.ProjectMap = projectMap
		}
	}

	cfg, err := config.Load(src)
	if err != nil {
		return err
	}

	a.cfg = cfg
	a.configFile = src.UserFile
	a.log = logging.New(logging.Options{
		Level:  logging.ParseLevel(cfg.LogLevel),
		Format: logging.Format(cfg.LogFormat),
	})
	return nil
}
