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
	// BaseHash is the content hash of the upstream extract *before* any
	// override is applied (spec 023). With no override declared it equals
	// ContentHash and is omitted, so spec 022 entries stay byte-identical.
	BaseHash string `json:"baseHash,omitempty"`
	// ContentHash is the canonical full-content hash of the committed skill
	// copy (spec 022): it covers symlinks that the shared core computedHash
	// (npx-compat) skips, so restores verify against it. Since spec 023 it is
	// the *post-override* result — the name and its role in verification are
	// unchanged, which is what lets drift detection and restore keep working.
	ContentHash string `json:"contentHash,omitempty"`
	// OverrideDigest is the canonical digest over the resolved override
	// declaration plus the content hash of every file it references (spec 023
	// FR-007). Empty when no override is declared. Because the referenced
	// files' bytes feed the digest, editing one is detected as drift without
	// any file watching.
	OverrideDigest string `json:"overrideDigest,omitempty"`
	// Override records the resolved declaration itself, so the lockfile stays
	// self-describing about what was applied: an audit or a --frozen-lockfile
	// check can detect a changed declaration without consulting the manifest
	// (spec 023 plan.md Deviation 1).
	Override *OverrideDecl `json:"override,omitempty"`
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

// OverrideDecl is the resolved override declaration as recorded in the lock.
// Declared list order is significant — patches and layers apply in sequence —
// so it is preserved verbatim; the Replace map is serialized with sorted keys,
// since map iteration order must never reach output (Constitution I).
type OverrideDecl struct {
	Replace map[string]string `json:"replace,omitempty"`
	Patch   []string          `json:"patch,omitempty"`
	Prepend []string          `json:"prepend,omitempty"`
	Append  []string          `json:"append,omitempty"`
}

// Empty reports whether the declaration transforms nothing.
func (o *OverrideDecl) Empty() bool {
	return o == nil ||
		(len(o.Replace) == 0 && len(o.Patch) == 0 && len(o.Prepend) == 0 && len(o.Append) == 0)
}
