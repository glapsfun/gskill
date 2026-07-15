package app_test

import (
	"testing"

	"github.com/glapsfun/gskill/internal/app"
)

func results(statuses ...string) []app.LockSkillResult {
	out := make([]app.LockSkillResult, 0, len(statuses))
	for i, s := range statuses {
		r := app.LockSkillResult{Name: string(rune('a' + i)), Status: s}
		if s == app.LockSkillFailed {
			r.Err = errFake
		}
		out = append(out, r)
	}
	return out
}

var errFake = &fakeError{}

type fakeError struct{}

func (*fakeError) Error() string { return "boom" }

// counterSum re-derives the total from the individual counters — the FR-015
// invariant every summary must satisfy.
func counterSum(s app.InstallSummary) int {
	return s.Installed + s.Repaired + s.UpToDate + s.Skipped +
		s.Failed + s.Cancelled + s.NotAttempted + s.Planned
}

func TestAggregate_CountersAndInvariant(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      []app.LockSkillResult
		want    app.InstallSummary
		outcome app.InstallOutcome
	}{
		{
			name:    "all successful",
			in:      results("installed", "installed", "repaired"),
			want:    app.InstallSummary{Total: 3, Installed: 2, Repaired: 1},
			outcome: app.InstallOutcomeSuccess,
		},
		{
			name:    "all failed",
			in:      results("failed", "failed"),
			want:    app.InstallSummary{Total: 2, Failed: 2},
			outcome: app.InstallOutcomeFailure,
		},
		{
			name:    "partial",
			in:      results("installed", "failed", "up-to-date"),
			want:    app.InstallSummary{Total: 3, Installed: 1, Failed: 1, UpToDate: 1},
			outcome: app.InstallOutcomePartial,
		},
		{
			name:    "repaired and up-to-date mix",
			in:      results("repaired", "up-to-date", "up-to-date", "skipped"),
			want:    app.InstallSummary{Total: 4, Repaired: 1, UpToDate: 2, Skipped: 1},
			outcome: app.InstallOutcomeSuccess,
		},
		{
			name:    "cancelled run",
			in:      results("installed", "cancelled", "not-attempted", "not-attempted"),
			want:    app.InstallSummary{Total: 4, Installed: 1, Cancelled: 1, NotAttempted: 2},
			outcome: app.InstallOutcomeCancelled,
		},
		{
			name:    "dry run all planned",
			in:      results("planned", "planned"),
			want:    app.InstallSummary{Total: 2, Planned: 2},
			outcome: app.InstallOutcomePlanned,
		},
		{
			name:    "empty run",
			in:      nil,
			want:    app.InstallSummary{},
			outcome: app.InstallOutcomeSuccess,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := app.Aggregate(tt.in)
			tt.want.Outcome = tt.outcome
			if got != tt.want {
				t.Errorf("Aggregate() = %+v, want %+v", got, tt.want)
			}
			if got.Total != counterSum(got) {
				t.Errorf("sum invariant violated: Total = %d, counters sum to %d (FR-015)", got.Total, counterSum(got))
			}
			if got.Total != len(tt.in) {
				t.Errorf("Total = %d, want %d entries", got.Total, len(tt.in))
			}
		})
	}
}

// A failed entry alongside a cancellation must still surface as cancelled —
// the run ended by user choice, and exit 130 is the contract.
func TestAggregate_CancelledDominatesPartial(t *testing.T) {
	t.Parallel()
	got := app.Aggregate(results("installed", "failed", "cancelled", "not-attempted"))
	if got.Outcome != app.InstallOutcomeCancelled {
		t.Errorf("Outcome = %q, want cancelled", got.Outcome)
	}
	if got.Total != counterSum(got) {
		t.Errorf("sum invariant violated: %+v", got)
	}
}
