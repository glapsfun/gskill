package skillslock

// State is the in-memory working model of the project's managed skills: the
// typed view commands operate on, hydrated from skills-lock.json entries
// carrying a gskill block and written back through the lossless Lock. It is
// never serialized directly.
type State struct {
	Skills map[string]Record
}

// NewState returns an empty in-memory state.
func NewState() *State {
	return &State{Skills: make(map[string]Record)}
}

// Record is the full reproduction record for one managed skill.
type Record struct {
	Source       Source
	Requested    Requested
	Resolved     Resolved
	Metadata     Metadata
	Requires     Requires
	Installation Installation
	Provenance   Provenance
}

// Source is the normalized origin of a skill.
type Source struct {
	Type     string
	Original string
	URL      string
	Owner    string
	Repo     string
	Path     string
}

// Requested echoes the human's intent (FR-010).
type Requested struct {
	Version string
	Ref     string
	Commit  string
}

// Resolved is the immutable identity gskill pinned to (FR-009, FR-010).
type Resolved struct {
	Version       string
	RefKind       string
	Tag           string
	Branch        string
	Commit        string
	TreeHash      string
	ContentHash   string
	SkillFileHash string
	MutableRef    bool
	LocalPathHash string
	// CompatHash is the npx-skills-compatible computedHash (spec 012). The
	// shared skills-lock.json persists it as the core computedHash field.
	CompatHash string
}

// Metadata is the captured SKILL.md frontmatter.
type Metadata struct {
	Name        string
	Description string
	Version     string
	License     string
}

// Requires records declared needs, surfaced but never resolved (FR-032).
type Requires struct {
	Skills      []string
	Commands    []string
	Environment []string
	MCP         []string
}

// Installation is the placement record (FR-019, FR-020, FR-027, FR-028). Mode is
// the representative install mode; Modes records the actual mode per agent for
// the case where they differ (e.g. a symlink falls back to a copy). ActivePath
// is the project-relative active-layer entry (.agents/skills/<name>) that every
// agent target derives from.
type Installation struct {
	Scope      string
	Mode       string
	Agents     []string
	ActivePath string
	Targets    map[string]string
	Modes      map[string]string
}

// Provenance is best-effort trust info. Timestamps are excluded from
// reproducible determinism (FR-004).
type Provenance struct {
	FetchedAt string
	UpdatedAt string
	Trust     string
}
