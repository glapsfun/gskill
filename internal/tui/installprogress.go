package tui

import (
	"fmt"
	"strings"

	pbar "github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/glapsfun/gskill/internal/app"
)

// Shared install-progress view (spec 014 US1): a terminal-count-driven bar,
// the current skill and phase, and live counters. Both wizards feed it
// install lifecycle events; it never invents time-based progress (FR-002).

// installNarrowWidth is the pinned compact-layout breakpoint
// (contracts/tui-install-progress.md): below 60 columns the view drops the
// labeled Current panel for a stacked compact form.
const installNarrowWidth = 60

// installBarWidth bounds the bar's cell width in the wide layout.
const installBarWidth = 30

// phaseTitles translates typed phase identifiers into user-facing text
// (data-model.md phase table). Unknown phases render their wire value so a
// new phase is never hidden.
var phaseTitles = map[app.InstallPhase]string{
	app.InstallPhaseResolving:       "Resolving source",
	app.InstallPhaseFetching:        "Fetching source",
	app.InstallPhaseReadingMetadata: "Reading skill metadata",
	app.InstallPhaseHashing:         "Computing content hash",
	app.InstallPhaseVerifying:       "Verifying integrity",
	app.InstallPhaseStoring:         "Writing to store",
	app.InstallPhaseLinking:         "Linking agent targets",
	app.InstallPhaseLocking:         "Updating lockfile",
	app.InstallPhaseCleaning:        "Cleaning temporary files",
	app.InstallPhaseComplete:        "Completed",
}

// PhaseTitle returns the display text for a phase.
func PhaseTitle(p app.InstallPhase) string {
	if t, ok := phaseTitles[p]; ok {
		return t
	}
	return string(p)
}

// emDash renders unknown values (FR-014).
const emDash = "—"

// OrDash substitutes — for an empty untrusted value, sanitizing otherwise:
// the single FR-014 placeholder rule shared by every renderer (TUI and CLI).
func OrDash(s string) string {
	if s == "" {
		return emDash
	}
	return Sanitize(s)
}

// InstallProgress renders a run's live progress. It is a value type in the
// Bubble Tea style: Observe and SetWidth return the updated copy.
type InstallProgress struct {
	st  Theme
	bar pbar.Model

	width     int
	total     int
	processed int // terminal events seen
	completed int // successful terminal events
	failed    int // failed terminal events only
	stopped   int // cancelled/not-attempted terminal events (never "failed")
	current   app.InstallProgressEvent
	hasCur    bool
}

// NewInstallProgress builds the component with the shared theme and the same
// accent-filled bar construction the CLI's fetch renderer uses.
func NewInstallProgress() InstallProgress {
	fill := AccentColor().Light
	if lipgloss.HasDarkBackground() {
		fill = AccentColor().Dark
	}
	return InstallProgress{
		st: DefaultTheme(),
		bar: pbar.New(
			pbar.WithWidth(installBarWidth),
			pbar.WithoutPercentage(),
			pbar.WithSolidFill(fill),
		),
		width: 80,
	}
}

// SetWidth adapts the layout (and bar width) to the terminal width.
func (m InstallProgress) SetWidth(w int) InstallProgress {
	if w <= 0 {
		return m
	}
	m.width = w
	m.bar.Width = max(5, min(installBarWidth, w-4))
	return m
}

// Observe folds one lifecycle event into the model. Run-scoped events
// (SkillName == "") never count toward progress or replace the current skill
// (contract guarantee 6).
func (m InstallProgress) Observe(e app.InstallProgressEvent) InstallProgress {
	if e.SkillTotal > m.total {
		m.total = e.SkillTotal
	}
	if e.SkillName == "" {
		return m
	}
	if e.Status.IsTerminal() {
		m.processed++
		switch e.Status { //nolint:exhaustive // every other terminal status is a success
		case app.InstallStatusFailed:
			m.failed++
		case app.InstallStatusCancelled, app.InstallStatusNotAttempted:
			// Interrupted entries are not failures: the live counters must
			// agree with the result screen's cancelled/not-attempted split
			// (FR-016 truthful counters).
			m.stopped++
		default:
			m.completed++
		}
		if m.hasCur && m.current.SkillName == e.SkillName {
			m.hasCur = false
		}
		return m
	}
	m.current = e
	m.hasCur = true
	return m
}

// Percent is the terminal-driven progress percentage (0 for an empty run).
func (m InstallProgress) Percent() int {
	if m.total == 0 {
		return 0
	}
	return m.processed * 100 / m.total
}

// Done reports whether every skill has a terminal result.
func (m InstallProgress) Done() bool { return m.total > 0 && m.processed >= m.total }

// fraction is the bar's fill ratio, guarded against an empty run.
func (m InstallProgress) fraction() float64 {
	if m.total == 0 {
		return 0
	}
	return float64(m.processed) / float64(m.total)
}

// View renders the wide or compact layout depending on the last SetWidth.
func (m InstallProgress) View() string {
	if m.width < installNarrowWidth {
		return m.viewNarrow()
	}
	return m.viewWide()
}

func (m InstallProgress) viewWide() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s  %d / %d  %d%%\n\n",
		m.bar.ViewAs(m.fraction()), m.processed, m.total, m.Percent())

	if m.hasCur {
		b.WriteString(m.st.Title.Render("Current") + "\n")
		fmt.Fprintf(&b, "  Skill:    %s\n", m.cell(m.st.Accent.Render(Sanitize(m.current.SkillName))))
		fmt.Fprintf(&b, "  Source:   %s\n", m.cell(OrDash(m.current.Source)))
		fmt.Fprintf(&b, "  Version:  %s\n", m.cell(OrDash(m.current.Version)))
		fmt.Fprintf(&b, "  Phase:    %s\n", m.cell(PhaseTitle(m.current.Phase)))
		b.WriteString("\n")
	}

	fmt.Fprintf(&b, "%s: %d    %s: %d", m.st.Success.Render("Completed"), m.completed,
		m.st.Error.Render("Failed"), m.failed)
	if m.stopped > 0 {
		fmt.Fprintf(&b, "    %s: %d", m.st.Warning.Render("Stopped"), m.stopped)
	}
	fmt.Fprintf(&b, "    Remaining: %d\n", m.remaining())
	return b.String()
}

func (m InstallProgress) viewNarrow() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Installing %d/%d  %d%%\n", m.processed, m.total, m.Percent())
	b.WriteString(m.bar.ViewAs(m.fraction()) + "\n")
	if m.hasCur {
		b.WriteString(m.cell(m.st.Accent.Render(Sanitize(m.current.SkillName))) + "\n")
		b.WriteString(m.cell(PhaseTitle(m.current.Phase)) + "\n")
	}
	fmt.Fprintf(&b, "%s %d  %s %d", m.st.Success.Render("✓"), m.completed,
		m.st.Error.Render("✗"), m.failed)
	if m.stopped > 0 {
		fmt.Fprintf(&b, "  %s %d", m.st.Warning.Render("○"), m.stopped)
	}
	fmt.Fprintf(&b, "  Remaining %d\n", m.remaining())
	return b.String()
}

// remaining never reports negative even if totals shrink mid-run.
func (m InstallProgress) remaining() int {
	if r := m.total - m.processed; r > 0 {
		return r
	}
	return 0
}

// cell truncates one already-sanitized line to the current width.
func (m InstallProgress) cell(s string) string {
	if m.width > 12 {
		return ansi.Truncate(s, m.width-12, "…")
	}
	return s
}
