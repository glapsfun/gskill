package app

// InstallOutcome is a run's overall result classification, serialized as the
// --json document's top-level status (contracts/install-result-json.md).
type InstallOutcome string

// Run outcomes. Cancellation dominates: a user-interrupted run reports
// cancelled (exit 130) even when some entries had already failed.
const (
	InstallOutcomeSuccess   InstallOutcome = "success"
	InstallOutcomePartial   InstallOutcome = "partial"
	InstallOutcomeFailure   InstallOutcome = "failure"
	InstallOutcomeCancelled InstallOutcome = "cancelled"
	InstallOutcomePlanned   InstallOutcome = "planned"
)

// InstallSummary aggregates a run's per-skill results into the counters every
// renderer shares. The invariant Total == sum of all per-status counters
// (FR-015) holds by construction: Aggregate counts each entry exactly once.
type InstallSummary struct {
	Total        int
	Installed    int
	Repaired     int
	UpToDate     int
	Skipped      int
	Failed       int
	Cancelled    int
	NotAttempted int
	Planned      int
	Outcome      InstallOutcome
}

// Aggregate computes the summary for a run's results. Unknown status strings
// count toward Total (never silently dropped) and force the failed counter so
// a miscounted entry can never inflate the success story.
func Aggregate(skills []LockSkillResult) InstallSummary {
	var s InstallSummary
	s.Total = len(skills)
	for _, r := range skills {
		switch InstallStatus(r.Status) {
		case InstallStatusInstalled:
			s.Installed++
		case InstallStatusRepaired:
			s.Repaired++
		case InstallStatusUpToDate:
			s.UpToDate++
		case InstallStatusSkipped:
			s.Skipped++
		case InstallStatusCancelled:
			s.Cancelled++
		case InstallStatusNotAttempted:
			s.NotAttempted++
		case InstallStatusPlanned:
			s.Planned++
		case InstallStatusFailed, InstallStatusPending, InstallStatusRunning:
			s.Failed++
		default:
			s.Failed++
		}
	}
	s.Outcome = outcomeOf(s)
	return s
}

// outcomeOf derives the overall outcome from the counters.
func outcomeOf(s InstallSummary) InstallOutcome {
	succeeded := s.Installed + s.Repaired + s.UpToDate + s.Skipped
	switch {
	case s.Cancelled+s.NotAttempted > 0:
		return InstallOutcomeCancelled
	case s.Planned > 0 && s.Failed == 0 && succeeded == 0:
		return InstallOutcomePlanned
	case s.Failed > 0 && succeeded == 0 && s.Planned == 0:
		return InstallOutcomeFailure
	case s.Failed > 0:
		return InstallOutcomePartial
	default:
		return InstallOutcomeSuccess
	}
}
