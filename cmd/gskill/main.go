// Command gskill is the GSKILL command-line entrypoint. It loads layered
// configuration, builds the app service, and dispatches the CLI, exiting with
// the resolved process code.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/cli"
	"github.com/glapsfun/gskill/internal/config"
	"github.com/glapsfun/gskill/internal/logging"
)

func main() {
	cfg, err := config.Load(config.Sources{})
	if err != nil {
		fmt.Fprintln(os.Stderr, "gskill: load config:", err)
		os.Exit(1)
	}

	logger := logging.New(logging.Options{
		Level:  logging.ParseLevel(cfg.LogLevel),
		Format: logging.Format(cfg.LogFormat),
	})

	application := app.New(app.Options{
		Config: cfg,
		Logger: logger,
		Agents: agent.NewDefaultRegistry(),
	})

	// SIGINT/SIGTERM cancel the run context so non-wizard commands stop
	// gracefully (spec 014 FR-024): the install loop halts between skills,
	// completed work is persisted to the lockfile, and the process exits 130.
	// (Inside the wizards, Bubble Tea's raw mode turns ctrl+c into a key
	// event instead of a signal.) After the run, stop restores the default
	// disposition so a second signal can still kill a hung teardown; os.Exit
	// makes a defer useless here.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	code := cli.Run(ctx, os.Args[1:], os.Stdout, os.Stderr, application)
	stop()
	os.Exit(code)
}
