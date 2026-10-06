package agent

// NewAntigravity returns the Antigravity CLI (agy) adapter. It reads project
// skills from the shared store and global skills from the CLI's own folder; the
// ~/.gemini/config/skills folder shared with the Antigravity IDE is deliberately
// not used. It has no project marker, so it is only ever targeted explicitly
// (spec 027 FR-008, FR-009).
func NewAntigravity() Agent {
	return sharedAgent{
		id:        "antigravity",
		name:      "Antigravity CLI",
		globalDir: []string{".gemini", "antigravity-cli", "skills"},
	}
}
