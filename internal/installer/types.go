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

// Mode-preference strings accepted on the command line and in the manifest.
// PrefAuto is the default: it prefers a symlink and falls back to a copy where
// symlinks are unsupported. Only ModeSymlink or ModeCopy is ever recorded as the
// resolved mode — never "auto".
const (
	PrefAuto    = "auto"
	PrefSymlink = "symlink"
	PrefCopy    = "copy"
)

// DefaultModePref is the install-mode preference applied when neither the
// command line nor the manifest specifies one (FR-022, FR-023).
const DefaultModePref = PrefAuto
