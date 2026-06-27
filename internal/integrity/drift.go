package integrity

// SkillState is the observed manifest, lockfile, and filesystem state of one
// skill, used to classify its drift without coupling this package to the
// manifest/lockfile types.
type SkillState struct {
	InManifest      bool
	InLock          bool
	SourceChanged   bool // normalized source/owner/repo differs between manifest and lock (FR-044)
	SourceAvailable bool
	ContentMismatch bool // installed content hash != locked hash (set only by content checks)
	TargetsTotal    int
	TargetsPresent  int
}

// Classify maps a SkillState to a DriftStatus (FR-016, FR-017). The checks are
// ordered from most to least severe so the first matching condition wins.
func Classify(s SkillState) DriftStatus {
	switch {
	case s.InLock && !s.InManifest:
		return DriftOrphaned
	case s.InManifest && !s.InLock:
		return DriftManifestLockMismatch
	case s.SourceChanged:
		return DriftManifestLockMismatch
	case !s.SourceAvailable:
		return DriftSourceUnavailable
	case s.ContentMismatch:
		return DriftChecksumMismatch
	case s.TargetsPresent == 0:
		return DriftMissing
	case s.TargetsPresent < s.TargetsTotal:
		return DriftPartiallyInstalled
	default:
		return DriftInstalled
	}
}
