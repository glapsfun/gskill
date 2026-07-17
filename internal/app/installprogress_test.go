package app_test

import (
	"testing"

	"github.com/glapsfun/gskill/internal/app"
)

// Wire values are part of the JSON contract
// (specs/014-install-progress-report/contracts/install-result-json.md) and must
// never drift from data-model.md's phase table.
func TestInstallPhaseWireValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		phase app.InstallPhase
		want  string
	}{
		{app.InstallPhaseResolving, "resolving"},
		{app.InstallPhaseFetching, "fetching"},
		{app.InstallPhaseReadingMetadata, "reading-metadata"},
		{app.InstallPhaseHashing, "hashing"},
		{app.InstallPhaseVerifying, "verifying"},
		{app.InstallPhaseStoring, "storing"},
		{app.InstallPhaseLinking, "linking"},
		{app.InstallPhaseLocking, "locking"},
		{app.InstallPhaseCleaning, "cleaning"},
		{app.InstallPhaseComplete, "complete"},
	}
	for _, tt := range tests {
		if got := string(tt.phase); got != tt.want {
			t.Errorf("phase wire value = %q, want %q", got, tt.want)
		}
	}
}

func TestInstallStatusWireValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status app.InstallStatus
		want   string
	}{
		{app.InstallStatusPending, "pending"},
		{app.InstallStatusRunning, "running"},
		{app.InstallStatusInstalled, "installed"},
		{app.InstallStatusUpToDate, "up-to-date"},
		{app.InstallStatusRepaired, "repaired"},
		{app.InstallStatusSkipped, "skipped"},
		{app.InstallStatusFailed, "failed"},
		{app.InstallStatusCancelled, "cancelled"},
		{app.InstallStatusNotAttempted, "not-attempted"},
		{app.InstallStatusPlanned, "planned"},
	}
	for _, tt := range tests {
		if got := string(tt.status); got != tt.want {
			t.Errorf("status wire value = %q, want %q", got, tt.want)
		}
	}

	// The existing lock-install status strings are the same wire values, so
	// results built from the legacy constants aggregate correctly.
	if string(app.InstallStatusInstalled) != app.LockSkillInstalled ||
		string(app.InstallStatusUpToDate) != app.LockSkillUpToDate ||
		string(app.InstallStatusRepaired) != app.LockSkillRepaired ||
		string(app.InstallStatusFailed) != app.LockSkillFailed ||
		string(app.InstallStatusPlanned) != app.LockSkillPlanned {
		t.Error("InstallStatus values drifted from the legacy LockSkill* status strings")
	}
}

// A skill counts as processed exactly when its status is terminal (spec
// FR-002/FR-010): progress denominators and the summary counters both key on
// this predicate.
func TestInstallStatusIsTerminal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status app.InstallStatus
		want   bool
	}{
		{app.InstallStatusPending, false},
		{app.InstallStatusRunning, false},
		{app.InstallStatusInstalled, true},
		{app.InstallStatusUpToDate, true},
		{app.InstallStatusRepaired, true},
		{app.InstallStatusSkipped, true},
		{app.InstallStatusFailed, true},
		{app.InstallStatusCancelled, true},
		{app.InstallStatusNotAttempted, true},
		{app.InstallStatusPlanned, true},
		{app.InstallStatus(""), false},
		{app.InstallStatus("bogus"), false},
	}
	for _, tt := range tests {
		if got := tt.status.IsTerminal(); got != tt.want {
			t.Errorf("IsTerminal(%q) = %v, want %v", tt.status, got, tt.want)
		}
	}
}

// Rank orders phases for the per-skill monotonicity assertions in the event
// contract tests; unknown phases rank below every known one.
func TestInstallPhaseRank(t *testing.T) {
	t.Parallel()

	ordered := []app.InstallPhase{
		app.InstallPhasePrefetching,
		app.InstallPhaseResolving,
		app.InstallPhaseFetching,
		app.InstallPhaseReadingMetadata,
		app.InstallPhaseHashing,
		app.InstallPhaseVerifying,
		app.InstallPhaseStoring,
		app.InstallPhaseLinking,
		app.InstallPhaseLocking,
		app.InstallPhaseCleaning,
		app.InstallPhaseComplete,
	}
	for i := 1; i < len(ordered); i++ {
		if ordered[i-1].Rank() >= ordered[i].Rank() {
			t.Errorf("Rank(%q) = %d not below Rank(%q) = %d",
				ordered[i-1], ordered[i-1].Rank(), ordered[i], ordered[i].Rank())
		}
	}
	if got := app.InstallPhase("bogus").Rank(); got != -1 {
		t.Errorf("Rank(bogus) = %d, want -1", got)
	}
	if got := app.InstallPhase("").Rank(); got != -1 {
		t.Errorf(`Rank("") = %d, want -1`, got)
	}
}

// The zero value must be safely usable: renderers receive events through a
// plain callback and must not need constructor discipline.
func TestInstallProgressEventZeroValue(t *testing.T) {
	t.Parallel()

	var e app.InstallProgressEvent
	if e.Status.IsTerminal() {
		t.Error("zero-value event status must not be terminal")
	}
	if e.Phase.Rank() != -1 {
		t.Errorf("zero-value event phase rank = %d, want -1", e.Phase.Rank())
	}
	if e.Err != nil || e.SkillName != "" || e.SkillIndex != 0 || e.SkillTotal != 0 {
		t.Error("zero-value event carries unexpected data")
	}
}
