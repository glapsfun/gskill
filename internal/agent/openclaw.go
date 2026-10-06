package agent

// NewOpenClaw returns the OpenClaw adapter. It reads global skills from
// ~/.openclaw/skills and project skills from the shared store, which OpenClaw
// sees only when the repository is its workspace. It has no project marker, so
// it is only ever targeted explicitly (spec 027 FR-008, FR-009).
func NewOpenClaw() Agent {
	return sharedAgent{
		id:        "openclaw",
		name:      "OpenClaw",
		globalDir: []string{".openclaw", "skills"},
	}
}
