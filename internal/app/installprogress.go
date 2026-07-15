package app

// Install-level progress vocabulary (spec 014). These events describe the
// lifecycle of one skill inside a multi-skill install run — distinct from the
// download telemetry in internal/progress, which streams git transfer detail.
// The string values are wire values: they serialize verbatim into the --json
// document (contracts/install-result-json.md) and must never be renamed.

// InstallPhase identifies the pipeline step a skill is in. Not every install
// emits every phase (the up-to-date fast path skips the fetch phases), but
// within one skill phases only ever advance (see Rank).
type InstallPhase string

// Install pipeline phases, in execution order.
const (
	InstallPhaseResolving       InstallPhase = "resolving"
	InstallPhaseFetching        InstallPhase = "fetching"
	InstallPhaseReadingMetadata InstallPhase = "reading-metadata"
	InstallPhaseHashing         InstallPhase = "hashing"
	InstallPhaseVerifying       InstallPhase = "verifying"
	InstallPhaseStoring         InstallPhase = "storing"
	InstallPhaseLinking         InstallPhase = "linking"
	InstallPhaseLocking         InstallPhase = "locking"
	InstallPhaseCleaning        InstallPhase = "cleaning"
	InstallPhaseComplete        InstallPhase = "complete"
)

// phaseOrder maps each phase to its position in the pipeline.
var phaseOrder = map[InstallPhase]int{
	InstallPhaseResolving:       0,
	InstallPhaseFetching:        1,
	InstallPhaseReadingMetadata: 2,
	InstallPhaseHashing:         3,
	InstallPhaseVerifying:       4,
	InstallPhaseStoring:         5,
	InstallPhaseLinking:         6,
	InstallPhaseLocking:         7,
	InstallPhaseCleaning:        8,
	InstallPhaseComplete:        9,
}

// Rank returns the phase's pipeline position for monotonicity checks, or -1
// for an unknown (including zero-value) phase.
func (p InstallPhase) Rank() int {
	if r, ok := phaseOrder[p]; ok {
		return r
	}
	return -1
}

// InstallStatus is a skill's state within an install run. The terminal values
// reuse the existing lock-install status strings (LockSkillInstalled etc.) so
// legacy result consumers keep working unchanged.
type InstallStatus string

// Install statuses. pending and running are event-only; the rest are terminal.
const (
	InstallStatusPending      InstallStatus = "pending"
	InstallStatusRunning      InstallStatus = "running"
	InstallStatusInstalled    InstallStatus = InstallStatus(LockSkillInstalled)
	InstallStatusUpToDate     InstallStatus = InstallStatus(LockSkillUpToDate)
	InstallStatusRepaired     InstallStatus = InstallStatus(LockSkillRepaired)
	InstallStatusSkipped      InstallStatus = "skipped"
	InstallStatusFailed       InstallStatus = InstallStatus(LockSkillFailed)
	InstallStatusCancelled    InstallStatus = "cancelled"
	InstallStatusNotAttempted InstallStatus = "not-attempted"
	InstallStatusPlanned      InstallStatus = InstallStatus(LockSkillPlanned)
)

// terminalStatuses is the processed-skill predicate set (FR-002): progress
// denominators and summary counters both key on membership here.
var terminalStatuses = map[InstallStatus]bool{
	InstallStatusInstalled:    true,
	InstallStatusUpToDate:     true,
	InstallStatusRepaired:     true,
	InstallStatusSkipped:      true,
	InstallStatusFailed:       true,
	InstallStatusCancelled:    true,
	InstallStatusNotAttempted: true,
	InstallStatusPlanned:      true,
}

// IsTerminal reports whether the status is a final per-skill outcome.
func (s InstallStatus) IsTerminal() bool { return terminalStatuses[s] }

// InstallProgressEvent is one observation of an install run. Events are
// emitted synchronously and strictly sequentially: at most one skill is in a
// non-terminal state at any time, and each skill emits exactly one terminal
// event (contracts/install-progress-events.md). SkillName, Source, Version,
// Ref, and Message may originate from remote content and are untrusted:
// renderers must sanitize them.
type InstallProgressEvent struct {
	SkillIndex int // 1-based position in the run
	SkillTotal int // total skills in the run
	SkillName  string
	Source     string
	SourceType string
	Version    string // resolved version when known
	Ref        string // requested ref when known
	Commit     string // resolved commit when known

	Phase   InstallPhase
	Status  InstallStatus
	Message string
	Err     error // set only on failed/cancelled terminal events
}
