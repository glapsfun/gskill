package source

// Type classifies where a skill comes from, as recorded in the lockfile.
type Type string

// Source types (FR-007, FR-008).
const (
	TypeGit   Type = "git"
	TypeLocal Type = "local"
	TypeURL   Type = "url"
)

// Valid reports whether t is a recognized source type.
func (t Type) Valid() bool {
	switch t {
	case TypeGit, TypeLocal, TypeURL:
		return true
	default:
		return false
	}
}
