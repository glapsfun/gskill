package resolver

// RefKind records how a requested reference was resolved to a revision.
type RefKind string

// Ref kinds (FR-009, FR-010). semver, tag, and commit are immutable; branch and
// local are mutable.
const (
	RefKindSemver RefKind = "semver"
	RefKindTag    RefKind = "tag"
	RefKindBranch RefKind = "branch"
	RefKindCommit RefKind = "commit"
	RefKindLocal  RefKind = "local"
)

// Valid reports whether k is a recognized ref kind.
func (k RefKind) Valid() bool {
	switch k {
	case RefKindSemver, RefKindTag, RefKindBranch, RefKindCommit, RefKindLocal:
		return true
	default:
		return false
	}
}

// Mutable reports whether a revision resolved by this kind can change under the
// same reference over time (branch and local), which triggers a warning
// (FR-044, SC-008).
func (k RefKind) Mutable() bool {
	return k == RefKindBranch || k == RefKindLocal
}
