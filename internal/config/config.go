// Package config resolves gskill's layered configuration. Precedence, lowest to
// highest, is: built-in defaults, user config file, project config file,
// GSKILL_-prefixed environment variables, then explicit flags. Storage paths
// follow the platform convention (XDG on Linux, the OS equivalents elsewhere)
// and may be overridden with GSKILL_CONFIG_DIR / GSKILL_CACHE_DIR.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/knadh/koanf/parsers/toml/v2"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

const (
	// EnvPrefix is the prefix for environment-variable overrides.
	EnvPrefix = "GSKILL_"

	// EnvConfigDir overrides the configuration directory, and with it the
	// discovered user config file. It is the escape hatch on a machine where
	// the platform convention cannot be resolved.
	EnvConfigDir = EnvPrefix + "CONFIG_DIR"

	appDir = "gskill"
)

// Config is the resolved, layered gskill configuration.
type Config struct {
	LogLevel  string
	LogFormat string
	Offline   bool
	NoCache   bool
	Jobs      int
	// Repositories are the known skill repositories an unscoped `find` searches
	// (FR-038). Configured in a config file (TOML array) or via
	// GSKILL_REPOSITORIES (comma-separated).
	Repositories []string
	// StoreLockTimeout bounds how long a contended project mutate lock is
	// waited on before failing. (The key name predates spec 022 and is kept
	// unchanged so existing configs keep working.)
	StoreLockTimeout time.Duration
}

// Sources are the inputs to Load. Empty fields are skipped; later layers
// override earlier ones in the documented precedence order.
type Sources struct {
	// Defaults seeds the lowest layer. When nil, DefaultMap is used.
	Defaults map[string]any
	// UserFile and ProjectFile are optional TOML config paths. A missing file
	// is skipped without error, which is right for a path this package
	// discovered on the caller's behalf — absence is the normal case.
	//
	// RequireUserFile reverses that for UserFile alone: a path the *user*
	// named (gskill --config) is a statement of intent, so a missing one is an
	// error rather than a silent no-op (spec 026 FR-003). ProjectFile has no
	// equivalent because nothing names it explicitly.
	UserFile        string
	RequireUserFile bool
	ProjectFile     string
	// ProjectMap is the project layer supplied in memory rather than as a
	// file: skills.toml holds project configuration inside its [config] table
	// alongside skill declarations, so the manifest hands over just that
	// sub-table (spec 023 FR-002). It shares the project layer's precedence
	// slot with ProjectFile — above the user file, below environment and flags.
	ProjectMap map[string]any
	// Environ is a list of "KEY=VALUE" entries (as from os.Environ); only
	// GSKILL_-prefixed entries are consulted. When nil, the process environment
	// is used.
	Environ []string
	// Flags is the highest-precedence layer of explicitly set flag values.
	Flags map[string]any
}

// DefaultMap returns the built-in configuration defaults.
func DefaultMap() map[string]any {
	return map[string]any{
		"log_level":          "info",
		"log_format":         "text",
		"offline":            false,
		"no_cache":           false,
		"jobs":               0,
		"repositories":       []string{},
		"store.lock_timeout": "60s",
	}
}

// Default returns the built-in defaults as a resolved Config, with no file,
// environment, or flag layers consulted.
func Default() *Config {
	cfg, err := Load(Sources{Environ: []string{}})
	if err != nil {
		panic("config: built-in defaults failed to load: " + err.Error())
	}
	return cfg
}

// Load merges the configuration layers in Sources and returns the result.
func Load(s Sources) (*Config, error) {
	k := koanf.New(".")

	defaults := s.Defaults
	if defaults == nil {
		defaults = DefaultMap()
	}
	if err := k.Load(confmap.Provider(defaults, "."), nil); err != nil {
		return nil, fmt.Errorf("load defaults: %w", err)
	}

	if err := loadFile(k, s.UserFile, s.RequireUserFile); err != nil {
		return nil, err
	}
	if err := loadFile(k, s.ProjectFile, false); err != nil {
		return nil, err
	}

	if len(s.ProjectMap) > 0 {
		if err := k.Load(confmap.Provider(s.ProjectMap, "."), nil); err != nil {
			return nil, fmt.Errorf("load project config: %w", err)
		}
	}

	environ := s.Environ
	if environ == nil {
		environ = os.Environ()
	}
	if env := parseEnviron(environ); len(env) > 0 {
		if err := k.Load(confmap.Provider(env, "."), nil); err != nil {
			return nil, fmt.Errorf("load environment: %w", err)
		}
	}

	if len(s.Flags) > 0 {
		if err := k.Load(confmap.Provider(s.Flags, "."), nil); err != nil {
			return nil, fmt.Errorf("load flags: %w", err)
		}
	}

	lockTimeout, err := ParseFlexDuration(k.String("store.lock_timeout"))
	if err != nil {
		return nil, fmt.Errorf("store.lock_timeout: %w", err)
	}

	cfg := &Config{
		LogLevel:         k.String("log_level"),
		LogFormat:        k.String("log_format"),
		Offline:          k.Bool("offline"),
		NoCache:          k.Bool("no_cache"),
		Jobs:             k.Int("jobs"),
		Repositories:     k.Strings("repositories"),
		StoreLockTimeout: lockTimeout,
	}
	return cfg, nil
}

// ParseFlexDuration parses a duration that may use a whole-day suffix ("30d")
// in addition to the standard time.ParseDuration units. Negative durations are
// rejected.
func ParseFlexDuration(s string) (time.Duration, error) {
	if s == "" {
		return 0, errors.New("empty duration")
	}
	var d time.Duration
	if days, ok := strings.CutSuffix(s, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q: %w", s, err)
		}
		d = time.Duration(n) * 24 * time.Hour
	} else {
		var err error
		d, err = time.ParseDuration(s)
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q: %w", s, err)
		}
	}
	if d < 0 {
		return 0, fmt.Errorf("negative duration %q", s)
	}
	return d, nil
}

// loadFile merges a TOML config file into k. A missing path is skipped unless
// required, in which case it is reported — a path the user named must not
// vanish silently.
func loadFile(k *koanf.Koanf, path string, required bool) error {
	if path == "" {
		return nil
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) && !required {
			return nil
		}
		return fmt.Errorf("stat config %s: %w", path, err)
	}
	if err := k.Load(file.Provider(path), toml.Parser()); err != nil {
		return fmt.Errorf("load config %s: %w", path, err)
	}
	return nil
}

// parseEnviron extracts GSKILL_-prefixed entries into a flat config map. The
// path-override keys (CONFIG_DIR, CACHE_DIR) are handled separately and skipped
// here.
func parseEnviron(environ []string) map[string]any {
	out := make(map[string]any)
	for _, entry := range environ {
		key, val, ok := strings.Cut(entry, "=")
		if !ok || !strings.HasPrefix(key, EnvPrefix) {
			continue
		}
		name := strings.ToLower(strings.TrimPrefix(key, EnvPrefix))
		switch name {
		case "config_dir", "cache_dir", "home":
			// Path overrides are resolved separately (Dir, CacheDir, home.Dir).
			continue
		case "repositories":
			out[name] = splitList(val)
		case "store_scope", "store_verify", "project_registry":
			// Pre-022 store/registry knobs: ignored with the machinery they
			// configured (spec 022) — never an error for existing setups.
			continue
		default:
			out[name] = val
		}
	}
	return out
}

// splitList splits a comma-separated env value into a trimmed, non-empty list.
func splitList(val string) []string {
	var out []string
	for _, part := range strings.Split(val, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Dir returns the gskill configuration directory, honoring GSKILL_CONFIG_DIR
// and otherwise following the platform convention.
func Dir() (string, error) {
	if v := os.Getenv(EnvConfigDir); v != "" {
		return v, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve config dir: %w", err)
	}
	return filepath.Join(base, appDir), nil
}

// UserFile returns the discovered user configuration file: config.toml inside
// the configuration directory. It is the path `gskill config list` reports,
// and the source of the user layer whenever --config names nothing else
// (spec 026 FR-004). The file need not exist; absence is normal and skipped.
func UserFile() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.toml"), nil
}

// CacheDir returns the gskill cache directory, honoring GSKILL_CACHE_DIR and
// otherwise following the platform convention.
func CacheDir() (string, error) {
	if v := os.Getenv(EnvPrefix + "CACHE_DIR"); v != "" {
		return v, nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve cache dir: %w", err)
	}
	return filepath.Join(base, appDir), nil
}
