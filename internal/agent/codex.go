package agent

// NewCodex returns the Codex adapter, which stores skills under
// .codex/skills/<name> (FR-027).
func NewCodex() Agent {
	return dirAgent{id: "codex", name: "Codex", markerDir: ".codex", symlinks: true}
}
