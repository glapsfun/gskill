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
	// StoreScope selects the physical content-store location: "global"
	// (user-level store under the gskill home) or "project" (legacy
	// <project>/.gskill/store). Empty means auto-detect per project. Distinct
	// from the installer's agent-target scope.
	StoreScope string
	// StoreVerifyOnUse re-hashes a global store object's content on every
	// activation when true (the default).
	StoreVerifyOnUse bool
	// StoreGCGracePeriod is the minimum age before an unreferenced global
	// store object becomes eligible for garbage collection.
	StoreGCGracePeriod time.Duration
	// StoreLockTimeout bounds how long a contended global-store or project
	// lock is waited on before failing.
	StoreLockTimeout time.Duration
	// ProjectsRegistry enables the advisory per-user project registry.
	ProjectsRegistry bool
	// PrivacyProjectRegistry controls how much project detail the registry
	// records: "full", "minimal" (no absolute paths), or "disabled".
	PrivacyProjectRegistry string
}

// Store scope values accepted for StoreScope (empty = auto-detect).
const (
	StoreScopeGlobal  = "global"
	StoreScopeProject = "project"
)

// Privacy modes accepted for PrivacyProjectRegistry.
const (
	PrivacyFull     = "full"
	PrivacyMinimal  = "minimal"
	PrivacyDisabled = "disabled"
)

// Sources are the inputs to Load. Empty fields are skipped; later layers
// override earlier ones in the documented precedence order.
type Sources struct {
	// Defaults seeds the lowest layer. When nil, DefaultMap is used.
	Defaults map[string]any
	// UserFile and ProjectFile are optional TOML config paths. A missing file
	// is skipped without error.
	UserFile    string
	ProjectFile string
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
		"log_level":                "info",
		"log_format":               "text",
		"offline":                  false,
		"no_cache":                 false,
		"jobs":                     0,
		"repositories":             []string{},
		"store.scope":              "",
		"store.verify_on_use":      true,
		"store.gc_grace_period":    "30d",
		"store.lock_timeout":       "60s",
		"projects.registry":        true,
		"privacy.project_registry": PrivacyFull,
	}
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

	for _, path := range []string{s.UserFile, s.ProjectFile} {
		if err := loadFile(k, path); err != nil {
			return nil, err
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

	grace, err := ParseFlexDuration(k.String("store.gc_grace_period"))
	if err != nil {
		return nil, fmt.Errorf("store.gc_grace_period: %w", err)
	}
	lockTimeout, err := ParseFlexDuration(k.String("store.lock_timeout"))
	if err != nil {
		return nil, fmt.Errorf("store.lock_timeout: %w", err)
	}

	cfg := &Config{
		LogLevel:               k.String("log_level"),
		LogFormat:              k.String("log_format"),
		Offline:                k.Bool("offline"),
		NoCache:                k.Bool("no_cache"),
		Jobs:                   k.Int("jobs"),
		Repositories:           k.Strings("repositories"),
		StoreScope:             k.String("store.scope"),
		StoreVerifyOnUse:       k.Bool("store.verify_on_use"),
		StoreGCGracePeriod:     grace,
		StoreLockTimeout:       lockTimeout,
		ProjectsRegistry:       k.Bool("projects.registry"),
		PrivacyProjectRegistry: k.String("privacy.project_registry"),
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// validate rejects enum values outside their documented sets.
func (c *Config) validate() error {
	switch c.StoreScope {
	case "", StoreScopeGlobal, StoreScopeProject:
	default:
		return fmt.Errorf("store.scope: invalid value %q (want %q or %q)",
			c.StoreScope, StoreScopeGlobal, StoreScopeProject)
	}
	switch c.PrivacyProjectRegistry {
	case PrivacyFull, PrivacyMinimal, PrivacyDisabled:
	default:
		return fmt.Errorf("privacy.project_registry: invalid value %q (want %q, %q, or %q)",
			c.PrivacyProjectRegistry, PrivacyFull, PrivacyMinimal, PrivacyDisabled)
	}
	return nil
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

// loadFile merges a TOML config file into k, skipping a missing path.
func loadFile(k *koanf.Koanf, path string) error {
	if path == "" {
		return nil
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
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
		case "store_scope":
			out["store.scope"] = val
		case "store_verify":
			out["store.verify_on_use"] = val
		case "project_registry":
			out["projects.registry"] = val
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
	if v := os.Getenv(EnvPrefix + "CONFIG_DIR"); v != "" {
		return v, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve config dir: %w", err)
	}
	return filepath.Join(base, appDir), nil
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
