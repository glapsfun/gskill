package integrity_test

import (
	"testing"

	"github.com/glapsfun/gskill/internal/integrity"
)

func TestClassify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		state integrity.SkillState
		want  integrity.DriftStatus
	}{
		{
			name:  "fully installed",
			state: integrity.SkillState{InManifest: true, InLock: true, SourceAvailable: true, TargetsTotal: 2, TargetsPresent: 2},
			want:  integrity.DriftInstalled,
		},
		{
			name:  "missing",
			state: integrity.SkillState{InManifest: true, InLock: true, SourceAvailable: true, TargetsTotal: 2, TargetsPresent: 0},
			want:  integrity.DriftMissing,
		},
		{
			name:  "partially installed",
			state: integrity.SkillState{InManifest: true, InLock: true, SourceAvailable: true, TargetsTotal: 2, TargetsPresent: 1},
			want:  integrity.DriftPartiallyInstalled,
		},
		{
			name:  "orphaned (locked, not declared)",
			state: integrity.SkillState{InManifest: false, InLock: true, SourceAvailable: true, TargetsTotal: 1, TargetsPresent: 1},
			want:  integrity.DriftOrphaned,
		},
		{
			name:  "declared but not locked",
			state: integrity.SkillState{InManifest: true, InLock: false},
			want:  integrity.DriftManifestLockMismatch,
		},
		{
			name:  "source substitution",
			state: integrity.SkillState{InManifest: true, InLock: true, SourceChanged: true, SourceAvailable: true, TargetsTotal: 1, TargetsPresent: 1},
			want:  integrity.DriftManifestLockMismatch,
		},
		{
			name:  "source unavailable",
			state: integrity.SkillState{InManifest: true, InLock: true, SourceAvailable: false, TargetsTotal: 1, TargetsPresent: 1},
			want:  integrity.DriftSourceUnavailable,
		},
		{
			name:  "checksum mismatch",
			state: integrity.SkillState{InManifest: true, InLock: true, SourceAvailable: true, ContentMismatch: true, TargetsTotal: 1, TargetsPresent: 1},
			want:  integrity.DriftChecksumMismatch,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := integrity.Classify(tt.state); got != tt.want {
				t.Errorf("Classify(%s) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}
