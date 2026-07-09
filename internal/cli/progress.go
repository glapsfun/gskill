package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	pbar "github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"

	"github.com/glapsfun/gskill/internal/progress"
	"github.com/glapsfun/gskill/internal/tui"
)

// Live download-progress rendering for the plain CLI (spec 013): a single
// in-place stderr line driven by progress events, with completed repos
// persisted as ✓ lines above it. No bubbletea program — commands stay
// synchronous and stdout (results, JSON) is never touched. The wizard paths
// deliberately never get this renderer: bubbletea owns the terminal there.

// eraseLiveLine returns the cursor to column 0 and clears the line.
const eraseLiveLine = "\r\x1b[2K"

// barWidth is the progress bar's cell width (design mockup).
const barWidth = 20

// tickInterval advances the spinner during phases git reports nothing for
// (ls-remote round-trips, connection setup).
const tickInterval = 100 * time.Millisecond

// fetchProgress renders download progress onto one live stderr line. All
// state is mutex-guarded: events arrive on the exec goroutine streaming git's
// stderr while the ticker animates the spinner from its own goroutine.
type fetchProgress struct {
	mu     sync.Mutex
	w      io.Writer
	st     tui.Theme
	bar    pbar.Model
	frames []string
	frame  int
	cur    *progress.Event // latest event driving the live line; nil = none
	width  int
	// seen dedups the persisted ✓ lines: add materializes the same commit at
	// discovery time and again at execute time (a cache hit by then).
	seen map[string]bool

	done chan struct{}
	wg   sync.WaitGroup
}

// newFetchProgress builds a renderer writing to w at the given terminal
// width. The spinner ticker is started separately (start) so tests can drive
// frames deterministically.
func newFetchProgress(w io.Writer, width int) *fetchProgress {
	st := tui.DefaultTheme()
	bar := pbar.New(
		pbar.WithWidth(barWidth),
		pbar.WithoutPercentage(),
		pbar.WithSolidFill("#8B83FF"), // theme accent (dark variant)
	)
	return &fetchProgress{
		w: w, st: st, bar: bar, frames: spinner.Dot.Frames, width: width,
		seen: make(map[string]bool),
	}
}

// start launches the spinner ticker.
func (f *fetchProgress) start() {
	f.done = make(chan struct{})
	f.wg.Add(1)
	go func() {
		defer f.wg.Done()
		t := time.NewTicker(tickInterval)
		defer t.Stop()
		for {
			select {
			case <-f.done:
				return
			case <-t.C:
				f.tick()
			}
		}
	}()
}

// Sink returns the progress callback feeding this renderer; a nil renderer
// yields a nil sink so call sites can wire unconditionally.
func (f *fetchProgress) Sink() progress.Sink {
	if f == nil {
		return nil
	}
	return f.handle
}

// Close stops the ticker and erases any live line so the command's final
// stdout summary starts on a clean row. Safe on a nil renderer.
func (f *fetchProgress) Close() {
	if f == nil {
		return
	}
	if f.done != nil {
		close(f.done)
		f.wg.Wait()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cur != nil {
		f.cur = nil
		_, _ = fmt.Fprint(f.w, eraseLiveLine)
	}
}

// tick advances the spinner and redraws the live line.
func (f *fetchProgress) tick() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cur == nil {
		return
	}
	f.frame = (f.frame + 1) % len(f.frames)
	f.redrawLocked()
}

// handle is the progress.Sink: it persists milestone lines and keeps the
// live line showing the latest transfer state.
func (f *fetchProgress) handle(e progress.Event) {
	f.mu.Lock()
	defer f.mu.Unlock()

	switch e.Phase {
	case progress.PhaseResolved:
		// The single-repo add path persists the resolve round-trip; during a
		// multi-repo install the per-skill ✓ line already tells the story.
		if e.Count == 0 {
			f.persistLocked(f.st.Hint.Render("resolving "+tui.Sanitize(e.Repo)+" … done"), nil)
			return
		}
		f.setLiveLocked(e)
	case progress.PhaseFetching:
		f.persistLocked(
			"Fetching "+f.st.Accent.Render(tui.Sanitize(e.Repo))+" @ "+shortSHA(e.Commit), &e)
	case progress.PhaseCached:
		if f.announcedLocked(e) {
			f.setLiveLocked(progress.Event{})
			return
		}
		f.persistLocked(
			f.st.Success.Render("✓ ")+tui.Sanitize(skillOrRepo(e))+f.st.Subtitle.Render(" (cached)"), nil)
	case progress.PhaseDone:
		if e.Skill != "" && !f.announcedLocked(e) {
			f.persistLocked(
				f.st.Success.Render("✓ ")+tui.Sanitize(e.Skill)+" @ "+shortSHA(e.Commit), nil)
			return
		}
		// Add path (or an already-announced unit): the command's own summary
		// follows on stdout.
		f.setLiveLocked(progress.Event{})
	case progress.PhaseResolving, progress.PhaseCounting, progress.PhaseReceiving, progress.PhaseDeltas:
		f.setLiveLocked(e)
	}
}

// announcedLocked marks the event's unit (skill+repo+commit) as announced and
// reports whether it already was.
func (f *fetchProgress) announcedLocked(e progress.Event) bool {
	key := e.Skill + "|" + e.Repo + "|" + e.Commit
	if f.seen[key] {
		return true
	}
	f.seen[key] = true
	return false
}

// setLiveLocked replaces (or, for the zero event, clears) the live line.
func (f *fetchProgress) setLiveLocked(e progress.Event) {
	if e == (progress.Event{}) {
		if f.cur != nil {
			f.cur = nil
			_, _ = fmt.Fprint(f.w, eraseLiveLine)
		}
		return
	}
	f.cur = &e
	f.redrawLocked()
}

// persistLocked writes line above the live region, then restores the live
// line (replaced by next when non-nil).
func (f *fetchProgress) persistLocked(line string, next *progress.Event) {
	_, _ = fmt.Fprint(f.w, eraseLiveLine+line+"\n")
	f.cur = next
	if f.cur != nil {
		f.redrawLocked()
	}
}

// redrawLocked renders the live line in place.
func (f *fetchProgress) redrawLocked() {
	if f.cur == nil {
		return
	}
	line := f.frameFor(*f.cur)
	if f.width > 0 {
		line = ansi.Truncate(line, f.width-1, "…")
	}
	_, _ = fmt.Fprint(f.w, eraseLiveLine+line)
}

// frameFor renders one live-line frame for the event.
func (f *fetchProgress) frameFor(e progress.Event) string {
	var b strings.Builder
	if e.Count > 0 {
		b.WriteString(f.st.Badge.Render(fmt.Sprintf("[%d/%d]", e.Index, e.Count)) + " ")
	}
	spin := f.st.Accent.Render(f.frames[f.frame])

	switch e.Phase {
	case progress.PhaseResolving, progress.PhaseResolved:
		b.WriteString(spin + " resolving " + f.st.Accent.Render(tui.Sanitize(e.Repo)) + " …")
	case progress.PhaseFetching:
		b.WriteString(spin + " fetching " + f.st.Accent.Render(tui.Sanitize(e.Repo)) + " …")
	case progress.PhaseCounting, progress.PhaseReceiving, progress.PhaseDeltas:
		b.WriteString(f.transferFrame(e, spin))
	case progress.PhaseCached, progress.PhaseDone:
		// Milestones persist in handle; nothing renders live.
	}
	return b.String()
}

// transferFrame renders the object-transfer phases: a bar when git reported
// totals, a spinner with the running object count otherwise.
func (f *fetchProgress) transferFrame(e progress.Event, spin string) string {
	var label string
	switch e.Phase { //nolint:exhaustive // only the transfer phases reach here
	case progress.PhaseCounting:
		label = "counting objects"
	case progress.PhaseDeltas:
		label = "resolving deltas"
	default:
		label = "receiving objects"
	}
	if e.Percent < 0 {
		return fmt.Sprintf("%s %s (%d)", spin, label, e.Objects)
	}
	line := f.bar.ViewAs(float64(e.Percent)/100) + fmt.Sprintf("  %3d%%", e.Percent)
	if e.Phase != progress.PhaseReceiving {
		line += "  " + f.st.Subtitle.Render(label)
	}
	if e.Detail != "" {
		line += "  " + f.st.Subtitle.Render(tui.Sanitize(e.Detail))
	}
	return line
}

// skillOrRepo names the finished unit: the manifest skill during an install,
// the repo during an add.
func skillOrRepo(e progress.Event) string {
	if e.Skill != "" {
		return e.Skill
	}
	return e.Repo
}

// shortSHA abbreviates a commit for display.
func shortSHA(commit string) string {
	if len(commit) > 7 {
		return commit[:7]
	}
	return commit
}

// fetchProgress returns a live download-progress renderer bound to stderr,
// or nil when the run must stay silent: piped stdout (non-interactive),
// --json, --quiet, or stderr not a terminal (2>file).
func (o *Output) fetchProgress() *fetchProgress {
	if !o.interactive || o.json || o.quiet || !isTTY(o.stderr) {
		return nil
	}
	f := newFetchProgress(o.stderr, stderrWidth(o.stderr))
	f.start()
	return f
}

// stderrWidth reports the terminal width of w, defaulting to 80.
func stderrWidth(w io.Writer) int {
	if file, ok := w.(*os.File); ok {
		if width, _, err := term.GetSize(int(file.Fd())); err == nil && width > 0 {
			return width
		}
	}
	return 80
}
