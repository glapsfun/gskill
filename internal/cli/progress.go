package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	pbar "github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/progress"
	"github.com/glapsfun/gskill/internal/tui"
)

// Live download-progress rendering for the plain CLI: a single in-place
// stderr line driven by progress events, with completed repos persisted as ✓
// lines above it. No bubbletea program — commands stay synchronous and
// stdout (results, JSON) is never touched. The wizard paths deliberately
// never get this renderer: bubbletea owns the terminal there, and a raw
// stderr writer would corrupt its screen.

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
	f      *os.File // w when it is a terminal; width source
	st     tui.Theme
	bar    pbar.Model
	frames []string
	frame  int
	cur    *progress.Event // latest event driving the live line; nil = none
	width  int
	last   string // last written live line; identical frames are skipped
	// seen dedups the persisted ✓ lines: add materializes the same commit at
	// discovery time and again at execute time (a cache hit by then).
	seen map[string]bool

	done chan struct{}
	wg   sync.WaitGroup
}

// newFetchProgress builds a renderer writing to w at the given fallback
// width. The spinner ticker is started separately (start) so tests can drive
// frames deterministically.
func newFetchProgress(w io.Writer, width int) *fetchProgress {
	fill := tui.AccentColor().Light
	if lipgloss.HasDarkBackground() {
		fill = tui.AccentColor().Dark
	}
	bar := pbar.New(
		pbar.WithWidth(barWidth),
		pbar.WithoutPercentage(),
		pbar.WithSolidFill(fill),
	)
	return &fetchProgress{
		w: w, st: tui.DefaultTheme(), bar: bar, frames: spinner.Dot.Frames, width: width,
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
// stdout summary starts on a clean row. Safe on a nil renderer and safe to
// call more than once.
func (f *fetchProgress) Close() {
	if f == nil {
		return
	}
	if f.done != nil {
		close(f.done)
		f.wg.Wait()
		f.done = nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clearLiveLocked()
}

// tick advances the spinner and redraws the live line (identical frames are
// skipped, so bar-only frames do not rewrite every tick).
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
		// multi-repo run the per-skill ✓ line already tells the story.
		if e.Count == 0 {
			f.persistLocked(f.st.Hint.Render("resolving " + tui.Sanitize(e.Repo) + " … done"))
			return
		}
		f.setLiveLocked(e)
	case progress.PhaseFetching:
		f.persistLocked("Fetching " + f.st.Accent.Render(tui.Sanitize(e.Repo)) + " @ " + displayCommit(e.Commit))
		f.setLiveLocked(e)
	case progress.PhaseCached:
		if f.announcedLocked(e) {
			f.clearLiveLocked()
			return
		}
		f.persistLocked(f.st.Success.Render("✓ ") + tui.Sanitize(skillOrRepo(e)) + f.st.Subtitle.Render(" (cached)"))
	case progress.PhaseDone:
		if f.announcedLocked(e) || e.Skill == "" {
			// Add path (or an already-announced unit): the command's own
			// summary follows on stdout.
			f.clearLiveLocked()
			return
		}
		line := f.st.Success.Render("✓ ") + tui.Sanitize(e.Skill)
		if e.Commit != "" {
			line += " @ " + displayCommit(e.Commit)
		}
		f.persistLocked(line)
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

// clearLiveLocked erases the live line, if any.
func (f *fetchProgress) clearLiveLocked() {
	if f.cur == nil {
		return
	}
	f.cur = nil
	f.last = ""
	_, _ = fmt.Fprint(f.w, eraseLiveLine)
}

// setLiveLocked replaces the live line with the event's frame.
func (f *fetchProgress) setLiveLocked(e progress.Event) {
	f.cur = &e
	f.redrawLocked()
}

// persistLocked writes line above the live region and leaves the live line
// cleared; callers that keep a live frame set it afterwards.
func (f *fetchProgress) persistLocked(line string) {
	_, _ = fmt.Fprint(f.w, eraseLiveLine+line+"\n")
	f.cur = nil
	f.last = ""
}

// redrawLocked renders the live line in place, skipping writes when the
// frame is byte-identical to the previous one (bar frames between ticks).
func (f *fetchProgress) redrawLocked() {
	if f.cur == nil {
		return
	}
	line := f.frameFor(*f.cur)
	if w := f.termWidth(); w > 0 {
		line = ansi.Truncate(line, w-1, "…")
	}
	if line == f.last {
		return
	}
	f.last = line
	_, _ = fmt.Fprint(f.w, eraseLiveLine+line)
}

// termWidth reports the current terminal width — re-queried live so a resize
// mid-fetch cannot leave wrapped junk rows behind — with the construction
// width as fallback.
func (f *fetchProgress) termWidth() int {
	if f.f != nil {
		if w, _, err := term.GetSize(int(f.f.Fd())); err == nil && w > 0 {
			return w
		}
	}
	return f.width
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

// displayCommit abbreviates and sanitizes a commit for the terminal. The
// commit string can come from a human-edited (or hostile) manifest, so it is
// untrusted like every other remote-origin string (constitution VI).
func displayCommit(commit string) string {
	return tui.Sanitize(app.ShortCommit(commit))
}

// withFetchProgress installs the live download-progress renderer on ctx for
// interactive runs and returns a done func that must run before the command
// prints its final summary (idempotent; also safe under defer). Gated runs
// (piped stdout, --json, --quiet, stderr not a terminal) get the context
// unchanged and a no-op done.
func (o *Output) withFetchProgress(ctx context.Context) (context.Context, func()) {
	fp := o.fetchProgress()
	if fp == nil {
		return ctx, func() {}
	}
	var once sync.Once
	return progress.WithSink(ctx, fp.Sink()), func() { once.Do(fp.Close) }
}

// fetchProgress returns a live download-progress renderer bound to stderr,
// or nil when the run must stay silent: piped stdout (non-interactive),
// --json, --quiet, or stderr not a terminal (2>file).
func (o *Output) fetchProgress() *fetchProgress {
	if !o.interactive || o.json || o.quiet || !isTTY(o.stderr) {
		return nil
	}
	f := newFetchProgress(o.stderr, 80)
	if file, ok := o.stderr.(*os.File); ok {
		f.f = file
	}
	f.start()
	return f
}
