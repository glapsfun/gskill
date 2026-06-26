package app_test

import (
	"testing"

	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/config"
)

func TestNew_FillsDefaults(t *testing.T) {
	t.Parallel()

	a := app.New(app.Options{})
	if a.Config() == nil {
		t.Error("Config() = nil, want default")
	}
	if a.Logger() == nil {
		t.Error("Logger() = nil, want default")
	}
	if a.Agents() == nil {
		t.Error("Agents() = nil, want default")
	}
}

func TestNew_UsesInjectedDeps(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{LogLevel: "debug"}
	reg := agent.NewRegistry()

	a := app.New(app.Options{Config: cfg, Agents: reg})
	if a.Config() != cfg {
		t.Error("Config() did not return injected config")
	}
	if a.Agents() != reg {
		t.Error("Agents() did not return injected registry")
	}
}
