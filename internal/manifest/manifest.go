// Package manifest reads and writes skills.toml, the committed, hand-authored
// declaration of a project's skills, overrides, and project configuration
// (spec 023). It is the intent half of the pair; skills-lock.json records what
// that intent resolved to.
package manifest

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"

	"github.com/knadh/koanf/parsers/toml/v2"

	"github.com/glapsfun/gskill/internal/config"
	"github.com/glapsfun/gskill/internal/errs"
)

// FileName is the manifest's fixed name at the repository root.
//
// Deliberately NOT "gskill.toml": that name belonged to a manifest released in
// v0.0.1–v0.2.0 and retired in #32 ("skills-lock.json is the only project
// state"), and lockonly_guard_test.go still guards against its return. A
// pre-v0.3.0 project may hold one on disk with an incompatible schema
// (schema_version, [defaults], install_mode, path), so reusing the name would
// either misread it or hard-fail every command in that project.
const FileName = "skills.toml"

// Install modes a skill may declare (data-model.md §1.1).
const (
	ModeSymlink = "symlink"
	ModeCopy    = "copy"
	ModeAuto    = "auto"
)

// Manifest is a parsed skills.toml. Warnings carry non-fatal findings (V3, V9)
// that the CLI surfaces without failing the run.
type Manifest struct {
	Skills   map[string]Skill
	Config   map[string]any
	Warnings []string
}

// Skill is one `[skills.<name>]` declaration.
type Skill struct {
	Name     string
	Source   string
	Skill    string
	Ref      string
	Commit   string
	Agents   []string
	Mode     string
	Override *Override
}

// Pin returns the revision this declaration selects: the exact commit when one
// is declared, otherwise the ref. `commit` beats `ref` (V9), because an exact
// revision is strictly more specific than a movable one.
func (s Skill) Pin() string {
	if s.Commit != "" {
		return s.Commit
	}
	return s.Ref
}

// SkillName is the name to look for inside the source, which defaults to the
// installed name.
func (s Skill) SkillName() string {
	if s.Skill != "" {
		return s.Skill
	}
	return s.Name
}

// Override is one `[skills.<name>.override]` declaration (data-model.md §2).
// Pinning is deliberately absent: it selects the input at resolve time rather
// than transforming bytes (FR-004).
type Override struct {
	Replace map[string]string
	Patch   []string
	Prepend []string
	Append  []string
}

// Empty reports whether the declaration transforms nothing, in which case the
// entry behaves exactly as an un-overridden one (FR-008).
func (o *Override) Empty() bool {
	return o == nil ||
		(len(o.Replace) == 0 && len(o.Patch) == 0 && len(o.Prepend) == 0 && len(o.Append) == 0)
}

// Inputs returns every repo-relative file the declaration references, in a
// stable order: replace sources sorted by their target, then patch, prepend,
// and append in declared order. The order is part of the digest, so it must
// never depend on map iteration (Constitution I).
func (o *Override) Inputs() []string {
	if o == nil {
		return nil
	}
	var out []string
	for _, k := range sortedKeys(o.Replace) {
		out = append(out, o.Replace[k])
	}
	out = append(out, o.Patch...)
	out = append(out, o.Prepend...)
	out = append(out, o.Append...)
	return out
}

// Load reads the manifest at path. A missing file is not an error: a pre-023
// project has none until a mutating command generates one (FR-015), so callers
// receive (nil, nil) and treat the project as unmanifested.
func Load(path string) (*Manifest, error) {
	data, err := os.ReadFile(path) //nolint:gosec // caller supplies the project-root manifest path
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil //nolint:nilnil // absent manifest is a state, not a failure
		}
		return nil, errs.Wrap(errs.CodeUsage, "read "+FileName, err)
	}
	return Parse(data)
}

// Parse decodes and structurally validates manifest bytes: rules V1, V2, V4,
// V7, V8, and V9 from contracts/manifest.md. Filesystem-dependent rules (V5,
// V6) belong to Validate, which needs the repository root.
func Parse(data []byte) (*Manifest, error) {
	raw, err := toml.Parser().Unmarshal(data)
	if err != nil {
		return nil, errs.WithHint(
			errs.Wrap(errs.CodeUsage, "parse "+FileName, err),
			"fix the TOML syntax and re-run")
	}

	m := &Manifest{Skills: map[string]Skill{}, Config: map[string]any{}}

	for key := range raw {
		switch key {
		case "config", "skills":
		default:
			return nil, usagef("%s: unknown top-level table %q (expected [config] or [skills.<name>])", FileName, key)
		}
	}

	if cfg, ok := raw["config"].(map[string]any); ok {
		m.Config = cfg
		m.Warnings = append(m.Warnings, unknownConfigKeys(cfg)...)
	}

	skills, ok := raw["skills"].(map[string]any)
	if !ok {
		return m, nil
	}
	for _, name := range sortedAnyKeys(skills) {
		table, ok := skills[name].(map[string]any)
		if !ok {
			return nil, usagef("%s: [skills.%s] must be a table", FileName, name)
		}
		s, warns, err := parseSkill(name, table)
		if err != nil {
			return nil, err
		}
		m.Skills[name] = s
		m.Warnings = append(m.Warnings, warns...)
	}
	return m, nil
}

// skillKeys and overrideKeys are the closed sets rule V2 enforces. Strictness
// here is deliberate: a typo like `apend` would otherwise silently drop a
// transformation the user believes is applied, producing content that is wrong
// yet internally consistent and hash-verified.
var (
	skillKeys    = map[string]bool{"source": true, "skill": true, "ref": true, "commit": true, "agents": true, "mode": true, "override": true}
	overrideKeys = map[string]bool{"replace": true, "patch": true, "prepend": true, "append": true}
)

func parseSkill(name string, table map[string]any) (Skill, []string, error) {
	for _, k := range sortedAnyKeys(table) {
		if !skillKeys[k] {
			return Skill{}, nil, usagef("%s: [skills.%s] has unknown key %q (valid: %s)",
				FileName, name, k, strings.Join(sortedSet(skillKeys), ", "))
		}
	}

	s, err := parseSkillFields(name, table)
	if err != nil {
		return Skill{}, nil, err
	}

	var warns []string
	if s.Ref != "" && s.Commit != "" {
		warns = append(warns, fmt.Sprintf(
			"%s: [skills.%s] declares both \"ref\" and \"commit\"; the commit wins and \"ref\" is inert", FileName, name))
	}

	ov, ok := table["override"]
	if !ok {
		return s, warns, nil
	}
	otable, ok := ov.(map[string]any)
	if !ok {
		return Skill{}, nil, usagef("%s: [skills.%s.override] must be a table", FileName, name)
	}
	o, err := parseOverride(name, otable)
	if err != nil {
		return Skill{}, nil, err
	}
	if !o.Empty() {
		s.Override = o
	}
	return s, warns, nil
}

// parseSkillFields decodes the scalar declaration fields and enforces the
// rules that depend only on them (V4 required source, V8 valid mode).
func parseSkillFields(name string, table map[string]any) (Skill, error) {
	s := Skill{Name: name}
	for _, f := range []struct {
		key string
		dst *string
	}{
		{"source", &s.Source},
		{"skill", &s.Skill},
		{"ref", &s.Ref},
		{"commit", &s.Commit},
		{"mode", &s.Mode},
	} {
		v, err := optString(table, f.key, name)
		if err != nil {
			return Skill{}, err
		}
		*f.dst = v
	}
	if s.Source == "" {
		return Skill{}, usagef("%s: [skills.%s] is missing required key \"source\"", FileName, name)
	}
	switch s.Mode {
	case "", ModeSymlink, ModeCopy, ModeAuto:
	default:
		return Skill{}, usagef("%s: [skills.%s] has invalid mode %q (valid: %s, %s, %s)",
			FileName, name, s.Mode, ModeSymlink, ModeCopy, ModeAuto)
	}
	agents, err := optStrings(table, "agents", name)
	if err != nil {
		return Skill{}, err
	}
	s.Agents = agents
	return s, nil
}

func parseOverride(name string, table map[string]any) (*Override, error) {
	for _, k := range sortedAnyKeys(table) {
		if !overrideKeys[k] {
			return nil, usagef("%s: [skills.%s.override] has unknown key %q (valid: %s)",
				FileName, name, k, strings.Join(sortedSet(overrideKeys), ", "))
		}
	}

	o := &Override{}
	if rep, ok := table["replace"]; ok {
		rtable, ok := rep.(map[string]any)
		if !ok {
			return nil, usagef("%s: [skills.%s.override] \"replace\" must be a table of target = source", FileName, name)
		}
		o.Replace = map[string]string{}
		for _, k := range sortedAnyKeys(rtable) {
			v, ok := rtable[k].(string)
			if !ok {
				return nil, usagef("%s: [skills.%s.override] replace[%q] must be a string path", FileName, name, k)
			}
			o.Replace[k] = v
		}
	}

	var err error
	if o.Patch, err = optStrings(table, "patch", name); err != nil {
		return nil, err
	}
	if o.Prepend, err = optStrings(table, "prepend", name); err != nil {
		return nil, err
	}
	if o.Append, err = optStrings(table, "append", name); err != nil {
		return nil, err
	}

	// V7 (replace/patch collision) needs the diffs' real target paths, so it
	// lives in Validate, which may read files. It still runs before any
	// network or cache access, which is the guarantee FR-005 actually makes.
	return o, nil
}

func optString(table map[string]any, key, skill string) (string, error) {
	v, ok := table[key]
	if !ok {
		return "", nil
	}
	s, ok := v.(string)
	if !ok {
		return "", usagef("%s: [skills.%s] key %q must be a string", FileName, skill, key)
	}
	return s, nil
}

func optStrings(table map[string]any, key, skill string) ([]string, error) {
	v, ok := table[key]
	if !ok {
		return nil, nil
	}
	list, ok := v.([]any)
	if !ok {
		return nil, usagef("%s: [skills.%s] key %q must be a list of strings", FileName, skill, key)
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		s, ok := item.(string)
		if !ok {
			return nil, usagef("%s: [skills.%s] key %q must contain only strings", FileName, skill, key)
		}
		out = append(out, s)
	}
	return out, nil
}

// unknownConfigKeys implements V3: unknown configuration keys warn but never
// fail, so a manifest stays readable across gskill versions (spec 022 FR-011).
func unknownConfigKeys(cfg map[string]any) []string {
	known := map[string]bool{}
	for k := range config.DefaultMap() {
		known[k] = true
		if i := strings.Index(k, "."); i > 0 {
			known[k[:i]] = true
		}
	}
	var warns []string
	for _, k := range sortedAnyKeys(cfg) {
		if !known[k] {
			warns = append(warns, fmt.Sprintf("%s: [config] has unknown key %q (ignored)", FileName, k))
		}
	}
	return warns
}

func usagef(format string, args ...any) error {
	return errs.Wrap(errs.CodeUsage, fmt.Sprintf(format, args...), errs.ErrUsage)
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedAnyKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedSet(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
