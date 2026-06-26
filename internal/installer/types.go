package installer

// Scope selects whether a skill installs into the current project or the
// user-global location (FR-028).
type Scope string

// Install scopes.
const (
	ScopeProject Scope = "project"
	ScopeGlobal  Scope = "global"
)

// Valid reports whether s is a recognized scope.
func (s Scope) Valid() bool {
	return s == ScopeProject || s == ScopeGlobal
}

// Mode is the actual activation method recorded for an installed skill. The
// requested preference may be "auto", but only symlink or copy is ever recorded
// (FR-020).
type Mode string

// Install modes.
const (
	ModeSymlink Mode = "symlink"
	ModeCopy    Mode = "copy"
)

// Valid reports whether m is a recognized install mode.
func (m Mode) Valid() bool {
	return m == ModeSymlink || m == ModeCopy
}
