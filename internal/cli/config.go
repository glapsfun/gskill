package cli

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/config"
	"github.com/glapsfun/gskill/internal/errs"
)

// configCmd groups configuration subcommands.
type configCmd struct {
	List configListCmd `cmd:"" help:"Print the config file path and the effective configuration."`
	Get  configGetCmd  `cmd:"" help:"Print one configuration value."`
}

// effectiveConfig renders the run's resolved configuration as a key/value map.
//
// It reads what the App already resolved rather than merging the layers again.
// Resolving twice is how `config list` came to advertise a user config file
// that nothing read (spec 026): the reporter and the applier were separate
// code paths over different layer sets, so they could disagree — and did —
// without any test noticing. Reading the applied value makes FR-007
// structural.
func effectiveConfig(a *app.App) map[string]string {
	cfg := a.Config()
	return map[string]string{
		"log_level":  cfg.LogLevel,
		"log_format": cfg.LogFormat,
		"offline":    strconv.FormatBool(cfg.Offline),
		"no_cache":   strconv.FormatBool(cfg.NoCache),
		"jobs":       strconv.Itoa(cfg.Jobs),
	}
}

type configListCmd struct{}

// Help returns the detailed help shown by `gskill config list --help`.
func (configListCmd) Help() string {
	return examplesHelp("gskill config list", "gskill config list --json")
}

// Run prints the config file path and the effective configuration.
//
// Spec 025 folded the former `config path` command in here. The values are
// nested under "values" rather than flattened alongside "path", because the
// flat object's keys are configuration keys: a sibling "path" would put a
// non-configuration name into that namespace and leave `config get path`
// failing as an unknown key. Nesting keeps the two namespaces apart and makes
// the payload self-describing.
func (configListCmd) Run(out *Output, a *app.App) error {
	path := a.ConfigFile()
	if path == "" {
		// The configuration directory could not be resolved, so there is no
		// user layer and no path to print. Every other command carries on
		// without one; this command's whole subject is that path, so it is the
		// right place to surface the failure — and the only place that did
		// before this layer existed.
		var err error
		if path, err = config.UserFile(); err != nil {
			return err
		}
	}
	values := effectiveConfig(a)
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	human := "# user config: " + path + "\n"
	obj := make(map[string]any, len(values))
	for _, k := range keys {
		human += fmt.Sprintf("%s = %s\n", k, values[k])
		obj[k] = values[k]
	}
	if out.Interactive() {
		human = renderConfigListStyled(path, values)
	}
	return out.Result(human, map[string]any{"path": path, "values": obj})
}

type configGetCmd struct {
	Key string `arg:"" help:"Configuration key."`
}

// Help returns the detailed help shown by `gskill config get --help`.
func (configGetCmd) Help() string {
	return examplesHelp("gskill config get log_level")
}

// Run prints a single configuration value.
func (c configGetCmd) Run(out *Output, a *app.App) error {
	values := effectiveConfig(a)
	value, ok := values[c.Key]
	if !ok {
		return errs.WithHint(
			fmt.Errorf("unknown config key %q", c.Key),
			"run 'gskill config list' to see available keys")
	}
	return out.Result(value, map[string]any{"key": c.Key, "value": value})
}
