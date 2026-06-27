package integrity

// DriftStatus classifies the state of an installed skill relative to the
// manifest, lockfile, and on-disk content, as surfaced by check and verify
// (FR-016).
type DriftStatus string

// Drift statuses (FR-016).
const (
	DriftInstalled            DriftStatus = "installed"
	DriftMissing              DriftStatus = "missing"
	DriftModified             DriftStatus = "modified"
	DriftOutdated             DriftStatus = "outdated"
	DriftOrphaned             DriftStatus = "orphaned"
	DriftPartiallyInstalled   DriftStatus = "partially-installed"
	DriftSourceUnavailable    DriftStatus = "source-unavailable"
	DriftChecksumMismatch     DriftStatus = "checksum-mismatch"
	DriftManifestLockMismatch DriftStatus = "manifest-lock-mismatch"
)

// Valid reports whether s is a recognized drift status.
func (s DriftStatus) Valid() bool {
	switch s {
	case DriftInstalled, DriftMissing, DriftModified, DriftOutdated, DriftOrphaned,
		DriftPartiallyInstalled, DriftSourceUnavailable, DriftChecksumMismatch,
		DriftManifestLockMismatch:
		return true
	default:
		return false
	}
}

// Clean reports whether s represents an in-sync installation needing no action.
func (s DriftStatus) Clean() bool {
	return s == DriftInstalled
}
