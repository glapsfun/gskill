package manifest

import (
	"errors"
	"fmt"
	"os"
	"regexp"

	"github.com/pelletier/go-toml/v2"

	"github.com/glapsfun/gskill/internal/fsutil"
)

// SchemaVersion is the manifest schema this build understands (FR-006).
const SchemaVersion = 1

// Sentinel manifest errors.
var (
	// ErrUnsupportedSchema is returned for a schema_version newer than this build.
	ErrUnsupportedSchema = errors.New("unsupported manifest schema version")
	// ErrInvalid is returned for a structurally or semantically invalid manifest.
	ErrInvalid = errors.New("invalid manifest")
)

// nameRE matches lowercase-kebab skill keys (FR-013).
var nameRE = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// knownInstallModes and knownScopes bound the enum fields.
var (
	knownInstallModes = map[string]bool{"symlink": true, "copy": true, "auto": true}
	knownScopes       = map[string]bool{"project": true, "global": true}
)

// Manifest is the human-editable record of install intent (FR-002).
type Manifest struct {
	SchemaVersion int              `toml:"schema_version"`
	Defaults      Defaults         `toml:"defaults,omitempty"`
	Skills        map[string]Skill `toml:"skills,omitempty"`
}

// Defaults are project-wide defaults applied when a skill omits a setting.
type Defaults struct {
	Agents      []string `toml:"agents,omitempty"`
	InstallMode string   `toml:"install_mode,omitempty"`
	Scope       string   `toml:"scope,omitempty"`
}

// Skill is one declared skill's intent.
type Skill struct {
	Source      string   `toml:"source"`
	Path        string   `toml:"path,omitempty"`
	Version     string   `toml:"version,omitempty"`
	Ref         string   `toml:"ref,omitempty"`
	Commit      string   `toml:"commit,omitempty"`
	Agents      []string `toml:"agents,omitempty"`
	InstallMode string   `toml:"install_mode,omitempty"`
}

// New returns an empty manifest at the current schema version.
func New() *Manifest {
	return &Manifest{SchemaVersion: SchemaVersion, Skills: make(map[string]Skill)}
}

// Marshal serializes the manifest to TOML.
func Marshal(m *Manifest) ([]byte, error) {
	data, err := toml.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("marshal manifest: %w", err)
	}
	return data, nil
}

// Unmarshal parses manifest bytes, rejecting a newer schema and unknown
// top-level sections, then validates (FR-002, FR-006).
func Unmarshal(data []byte) (*Manifest, error) {
	if err := rejectUnknownTopLevel(data); err != nil {
		return nil, err
	}

	var m Manifest
	if err := toml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if m.SchemaVersion > SchemaVersion {
		return nil, fmt.Errorf("%w: %d (this build understands up to %d; upgrade gskill)",
			ErrUnsupportedSchema, m.SchemaVersion, SchemaVersion)
	}
	if m.Skills == nil {
		m.Skills = make(map[string]Skill)
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

// rejectUnknownTopLevel fails when the manifest contains a top-level key outside
// the v1 schema (manifest integrity).
func rejectUnknownTopLevel(data []byte) error {
	var raw map[string]any
	if err := toml.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	allowed := map[string]bool{"schema_version": true, "defaults": true, "skills": true}
	for key := range raw {
		if !allowed[key] {
			return fmt.Errorf("%w: unknown top-level key %q", ErrInvalid, key)
		}
	}
	return nil
}

// Validate checks schema, names, sources, and enum fields (FR-002, FR-013).
func (m *Manifest) Validate() error {
	if m.Defaults.InstallMode != "" && !knownInstallModes[m.Defaults.InstallMode] {
		return fmt.Errorf("%w: defaults.install_mode %q", ErrInvalid, m.Defaults.InstallMode)
	}
	if m.Defaults.Scope != "" && !knownScopes[m.Defaults.Scope] {
		return fmt.Errorf("%w: defaults.scope %q", ErrInvalid, m.Defaults.Scope)
	}
	for name, skill := range m.Skills {
		if !nameRE.MatchString(name) {
			return fmt.Errorf("%w: skill key %q must be lowercase kebab-case", ErrInvalid, name)
		}
		if skill.Source == "" {
			return fmt.Errorf("%w: skill %q is missing required 'source'", ErrInvalid, name)
		}
		if skill.InstallMode != "" && !knownInstallModes[skill.InstallMode] {
			return fmt.Errorf("%w: skill %q install_mode %q", ErrInvalid, name, skill.InstallMode)
		}
	}
	return nil
}

// Load reads and parses the manifest at path.
func Load(path string) (*Manifest, error) {
	data, err := os.ReadFile(path) //nolint:gosec // project-root manifest path
	if err != nil {
		return nil, fmt.Errorf("read manifest %s: %w", path, err)
	}
	return Unmarshal(data)
}

// Save writes the manifest to path atomically.
func Save(path string, m *Manifest) error {
	data, err := Marshal(m)
	if err != nil {
		return err
	}
	if err := fsutil.WriteFileAtomic(path, data, 0o600); err != nil {
		return fmt.Errorf("write manifest %s: %w", path, err)
	}
	return nil
}
