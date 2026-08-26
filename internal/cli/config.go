package cli

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/glapsfun/gskill/internal/config"
	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/manifest"
)

// configCmd groups configuration subcommands.
type configCmd struct {
	Path configPathCmd `cmd:"" help:"Print the config file path."`
	List configListCmd `cmd:"" help:"Print the effective configuration."`
	Get  configGetCmd  `cmd:"" help:"Print one configuration value."`
}

// effectiveConfig loads the merged configuration as a key/value map, including
// the project layer declared in the manifest's [config] table (spec 023
// FR-002). Without root the project layer is simply absent.
func effectiveConfig(root string) (map[string]string, error) {
	projectMap, err := manifest.ProjectConfig(root)
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(config.Sources{ProjectMap: projectMap})
	if err != nil {
		return nil, err
	}
	return map[string]string{
		"log_level":  cfg.LogLevel,
		"log_format": cfg.LogFormat,
		"offline":    strconv.FormatBool(cfg.Offline),
		"no_cache":   strconv.FormatBool(cfg.NoCache),
		"jobs":       strconv.Itoa(cfg.Jobs),
	}, nil
}

type configPathCmd struct{}

// Help returns the detailed help shown by `gskill config path --help`.
func (configPathCmd) Help() string {
	return examplesHelp("gskill config path")
}

// Run prints the configuration file path.
func (configPathCmd) Run(out *Output) error {
	dir, err := config.Dir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "config.toml")
	return out.Result(path, map[string]any{"path": path})
}

type configListCmd struct{}

// Help returns the detailed help shown by `gskill config list --help`.
func (configListCmd) Help() string {
	return examplesHelp("gskill config list --json")
}

// Run prints the effective configuration.
func (configListCmd) Run(out *Output, root projectRoot) error {
	values, err := effectiveConfig(string(root))
	if err != nil {
		return err
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	human := ""
	obj := make(map[string]any, len(values))
	for _, k := range keys {
		human += fmt.Sprintf("%s = %s\n", k, values[k])
		obj[k] = values[k]
	}
	if out.Interactive() {
		human = renderConfigListStyled(values)
	}
	return out.Result(human, obj)
}

type configGetCmd struct {
	Key string `arg:"" help:"Configuration key."`
}

// Help returns the detailed help shown by `gskill config get --help`.
func (configGetCmd) Help() string {
	return examplesHelp("gskill config get log_level")
}

// Run prints a single configuration value.
func (c configGetCmd) Run(out *Output, root projectRoot) error {
	values, err := effectiveConfig(string(root))
	if err != nil {
		return err
	}
	value, ok := values[c.Key]
	if !ok {
		return errs.WithHint(
			fmt.Errorf("unknown config key %q", c.Key),
			"run 'gskill config list' to see available keys")
	}
	return out.Result(value, map[string]any{"key": c.Key, "value": value})
}
