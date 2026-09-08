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
	List configListCmd `cmd:"" help:"Print the config file path and the effective configuration."`
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

// configFilePath resolves the user-level configuration file, the value the
// retired `config path` command used to print on its own.
func configFilePath() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.toml"), nil
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
func (configListCmd) Run(out *Output, root projectRoot) error {
	path, err := configFilePath()
	if err != nil {
		return err
	}
	values, err := effectiveConfig(string(root))
	if err != nil {
		return err
	}
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
