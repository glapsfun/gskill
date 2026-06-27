package agent

// NewCursor returns the Cursor adapter, which stores skills under
// .cursor/skills/<name> (FR-031, SC-009).
func NewCursor() Agent {
	return dirAgent{id: "cursor", name: "Cursor", markerDir: ".cursor", symlinks: true}
}
