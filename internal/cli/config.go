package cli

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/glapsfun/gskill/internal/config"
)

// configCmd groups configuration subcommands.
type configCmd struct {
	Path configPathCmd `cmd:"" help:"Print the config file path."`
	List configListCmd `cmd:"" help:"Print the effective configuration."`
	Get  configGetCmd  `cmd:"" help:"Print one configuration value."`
}

// effectiveConfig loads the merged configuration as a key/value map.
func effectiveConfig() (map[string]string, error) {
	cfg, err := config.Load(config.Sources{})
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

// Run prints the effective configuration.
func (configListCmd) Run(out *Output) error {
	values, err := effectiveConfig()
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
	return out.Result(human, obj)
}

type configGetCmd struct {
	Key string `arg:"" help:"Configuration key."`
}

// Run prints a single configuration value.
func (c configGetCmd) Run(out *Output) error {
	values, err := effectiveConfig()
	if err != nil {
		return err
	}
	value, ok := values[c.Key]
	if !ok {
		return fmt.Errorf("unknown config key %q", c.Key)
	}
	return out.Result(value, map[string]any{"key": c.Key, "value": value})
}
