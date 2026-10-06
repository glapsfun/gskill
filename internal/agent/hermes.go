package agent

// NewHermes returns the Hermes Agent adapter. It reads project skills from the
// shared store and global skills from ~/.hermes/skills, and is detected by a
// .hermes/ directory (spec 027 FR-008, FR-009).
func NewHermes() Agent {
	return hermes{sharedAgent{
		id:        "hermes",
		name:      "Hermes Agent",
		globalDir: []string{".hermes", "skills"},
		markers:   []string{".hermes"},
	}}
}

// hermes is the Hermes Agent adapter: a shared-location agent that loads a
// repository's skills only once the user trusts the project in Hermes.
type hermes struct{ sharedAgent }

// ProjectNote tells the user about Hermes' one-time project trust step, which
// gskill can neither perform nor observe (spec 027 FR-012).
func (hermes) ProjectNote() string {
	return "hermes: Hermes loads project skills only after you trust this project; run 'hermes skills trust'"
}
