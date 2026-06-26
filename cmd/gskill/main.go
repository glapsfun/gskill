// Command gskill is the GSKILL command-line entrypoint. It loads layered
// configuration, builds the app service, and dispatches the CLI, exiting with
// the resolved process code.
package main

import (
	"context"
	"fmt"
	"os"

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
		Agents: agent.NewRegistry(),
	})

	code := cli.Run(context.Background(), os.Args[1:], os.Stdout, os.Stderr, application)
	os.Exit(code)
}
