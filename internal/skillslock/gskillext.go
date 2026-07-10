package skillslock

// Ext is the namespaced per-entry "gskill" extension block (FR-004): every
// gskill-owned fact lives here, never in the shared core fields, so the file
// stays fully usable by other tools. Timestamps are confined to this block and
// excluded from reproducible determinism, mirroring the legacy Provenance
// carve-out.
type Ext struct {
	SourceURL     string   `json:"sourceUrl,omitempty"`
	Ref           string   `json:"ref,omitempty"`
	Commit        string   `json:"commit,omitempty"`
	Version       string   `json:"version,omitempty"`
	Agents        []string `json:"agents,omitempty"`
	InstallMode   string   `json:"installMode,omitempty"`
	Scope         string   `json:"scope,omitempty"`
	StoreHash     string   `json:"storeHash,omitempty"`
	SkillFileHash string   `json:"skillFileHash,omitempty"`
	InstalledAt   string   `json:"installedAt,omitempty"`
	UpdatedAt     string   `json:"updatedAt,omitempty"`
}
