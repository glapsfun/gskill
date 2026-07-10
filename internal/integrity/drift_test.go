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
			state: integrity.SkillState{InLock: true, SourceAvailable: true, TargetsTotal: 2, TargetsPresent: 2},
			want:  integrity.DriftInstalled,
		},
		{
			name:  "missing",
			state: integrity.SkillState{InLock: true, SourceAvailable: true, TargetsTotal: 2, TargetsPresent: 0},
			want:  integrity.DriftMissing,
		},
		{
			name:  "partially installed",
			state: integrity.SkillState{InLock: true, SourceAvailable: true, TargetsTotal: 2, TargetsPresent: 1},
			want:  integrity.DriftPartiallyInstalled,
		},
		{
			name:  "source unavailable",
			state: integrity.SkillState{InLock: true, SourceAvailable: false, TargetsTotal: 1, TargetsPresent: 1},
			want:  integrity.DriftSourceUnavailable,
		},
		{
			name:  "checksum mismatch",
			state: integrity.SkillState{InLock: true, SourceAvailable: true, ContentMismatch: true, TargetsTotal: 1, TargetsPresent: 1},
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
