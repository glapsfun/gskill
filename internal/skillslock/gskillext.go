package skillslock

// ScopeGlobal is the user-global agent-target scope (mirrors
// installer.ScopeGlobal, duplicated here to keep skillslock dependency-free).
const ScopeGlobal = "global"

// Ext is the namespaced per-entry "gskill" extension block (FR-004): every
// gskill-owned fact lives here, never in the shared core fields, so the file
// stays fully usable by other tools. Timestamps are confined to this block and
// excluded from reproducible determinism, mirroring the legacy Provenance
// carve-out.
type Ext struct {
	SourceURL   string   `json:"sourceUrl,omitempty"`
	Ref         string   `json:"ref,omitempty"`
	Commit      string   `json:"commit,omitempty"`
	Version     string   `json:"version,omitempty"`
	Agents      []string `json:"agents,omitempty"`
	InstallMode string   `json:"installMode,omitempty"`
	// ContentHash is the canonical full-content hash of the committed skill
	// copy (spec 022): it covers symlinks that the shared core computedHash
	// (npx-compat) skips, so restores verify against it.
	ContentHash string `json:"contentHash,omitempty"`
	// Scope records the agent-target scope and is written ONLY for
	// user-global installs (`add --global`): that placement is not derivable
	// from anything else in the entry, and dropping it would silently
	// re-materialize the skill into the project on the next sync. The
	// pre-022 store-location value "project" is read for migration but never
	// written — entries drop it on their first rewrite (spec 022
	// data-model §3), which is what marks them legacy.
	Scope string `json:"scope,omitempty"`
	// StoreHash is the pre-022 content-hash field: read for migration, never
	// written — superseded by ContentHash.
	StoreHash     string `json:"storeHash,omitempty"`
	SkillFileHash string `json:"skillFileHash,omitempty"`
	InstalledAt   string `json:"installedAt,omitempty"`
	UpdatedAt     string `json:"updatedAt,omitempty"`
	// State nests the residual machine state existing commands still consume;
	// see ExtState (bridge.go).
	State *ExtState `json:"state,omitempty"`
}
