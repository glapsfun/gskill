package integrity_test

import (
	"testing"

	"github.com/glapsfun/gskill/internal/integrity"
)

func TestDriftStatus_Valid(t *testing.T) {
	t.Parallel()

	all := []integrity.DriftStatus{
		integrity.DriftInstalled, integrity.DriftMissing, integrity.DriftModified,
		integrity.DriftOutdated, integrity.DriftOrphaned, integrity.DriftPartiallyInstalled,
		integrity.DriftSourceUnavailable, integrity.DriftChecksumMismatch,
		integrity.DriftManifestLockMismatch,
	}
	for _, s := range all {
		if !s.Valid() {
			t.Errorf("%q.Valid() = false, want true", s)
		}
	}
	if integrity.DriftStatus("exploded").Valid() {
		t.Error(`DriftStatus("exploded").Valid() = true, want false`)
	}
}

func TestDriftStatus_Clean(t *testing.T) {
	t.Parallel()

	if !integrity.DriftInstalled.Clean() {
		t.Error("DriftInstalled.Clean() = false, want true")
	}
	if integrity.DriftModified.Clean() {
		t.Error("DriftModified.Clean() = true, want false")
	}
}
