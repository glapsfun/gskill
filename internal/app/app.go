// Package app is gskill's orchestration layer. It exposes use-case methods that
// the cli and tui views call, and is the only layer that drives the domain
// packages (resolver, installer, store, and the rest). Views never import the
// domain packages directly.
package app

import (
	"log/slog"

	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/config"
)

// App holds the injected dependencies shared by every use-case. Business logic
// is added by sibling files (install.go, inspect.go, lifecycle.go, ...).
type App struct {
	cfg    *config.Config
	log    *slog.Logger
	agents *agent.Registry
}

// Options configures New. Nil dependencies are replaced with safe defaults.
type Options struct {
	Config *config.Config
	Logger *slog.Logger
	Agents *agent.Registry
}

// New builds an App from opts, filling in defaults for any nil dependency.
func New(opts Options) *App {
	cfg := opts.Config
	if cfg == nil {
		cfg = &config.Config{}
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	agents := opts.Agents
	if agents == nil {
		agents = agent.NewRegistry()
	}
	return &App{cfg: cfg, log: logger, agents: agents}
}

// Config returns the resolved configuration.
func (a *App) Config() *config.Config { return a.cfg }

// Logger returns the structured logger.
func (a *App) Logger() *slog.Logger { return a.log }

// Agents returns the agent registry.
func (a *App) Agents() *agent.Registry { return a.agents }
