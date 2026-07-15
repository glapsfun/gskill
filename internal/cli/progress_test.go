package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/progress"
)

const eraseLine = "\r\x1b[2K"

func newTestFetchProgress(buf *bytes.Buffer) *fetchProgress {
	return newFetchProgress(buf, 80)
}

// TestFetchProgress_AddFlow drives the single-repo add sequence: a resolving
// spinner line, the persisted fetch header, a live percentage bar, and a
// clean erase on completion (the stdout summary follows elsewhere).
func TestFetchProgress_AddFlow(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	f := newTestFetchProgress(&buf)
	sink := f.Sink()

	sink(progress.Event{Phase: progress.PhaseResolving, Repo: "acme/skills"})
	if got := buf.String(); !strings.Contains(got, "resolving") || !strings.Contains(got, "acme/skills") {
		t.Fatalf("resolving live line missing: %q", got)
	}

	sink(progress.Event{Phase: progress.PhaseResolved, Repo: "acme/skills", Commit: "a1b2c3d4e5f6a7b8c9d0a1b2c3d4e5f6a7b8c9d0"})
	if got := buf.String(); !strings.Contains(got, "done\n") {
		t.Fatalf("resolve completion not persisted: %q", got)
	}

	sink(progress.Event{Phase: progress.PhaseFetching, Repo: "acme/skills", Commit: "a1b2c3d4e5f6a7b8c9d0a1b2c3d4e5f6a7b8c9d0"})
	if got := buf.String(); !strings.Contains(got, "Fetching acme/skills @ a1b2c3d4e5f6\n") {
		t.Fatalf("fetch header not persisted: %q", got)
	}

	sink(progress.Event{Phase: progress.PhaseReceiving, Percent: 62, Objects: 124, Total: 200, Detail: "4.10 MiB | 2.30 MiB/s"})
	got := buf.String()
	if !strings.Contains(got, "62%") || !strings.Contains(got, "4.10 MiB | 2.30 MiB/s") {
		t.Fatalf("live bar missing percent/detail: %q", got)
	}

	sink(progress.Event{Phase: progress.PhaseDone, Repo: "acme/skills", Commit: "a1b2c3d4e5f6a7b8c9d0a1b2c3d4e5f6a7b8c9d0"})
	if !strings.HasSuffix(buf.String(), eraseLine) {
		t.Fatalf("done (add path) must erase the live line and persist nothing: %q", buf.String())
	}

	// Execute-phase re-materialize is a cache hit on the commit the user just
	// watched download: no misleading "(cached)" line may follow.
	sink(progress.Event{Phase: progress.PhaseCached, Repo: "acme/skills", Commit: "a1b2c3d4e5f6a7b8c9d0a1b2c3d4e5f6a7b8c9d0"})
	if strings.Contains(buf.String(), "(cached)") {
		t.Fatalf("cache hit after a fresh download printed a cached line: %q", buf.String())
	}
}

// TestFetchProgress_InstallFlow drives the multi-repo install sequence:
// cached repos collapse to instant ✓ lines, the live line carries the [k/N]
// counter, and a finished fetch persists a per-skill ✓ line.
func TestFetchProgress_InstallFlow(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	f := newTestFetchProgress(&buf)
	sink := f.Sink()

	sink(progress.Event{Phase: progress.PhaseCached, Skill: "alpha", Repo: "acme/skills", Index: 1, Count: 3})
	if got := buf.String(); !strings.Contains(got, "✓ alpha (cached)\n") {
		t.Fatalf("cached skill line missing: %q", got)
	}

	sink(progress.Event{Phase: progress.PhaseReceiving, Skill: "beta", Index: 2, Count: 3, Percent: 31, Detail: "1.2 MiB | 800 KiB/s"})
	if got := buf.String(); !strings.Contains(got, "[2/3]") || !strings.Contains(got, "31%") {
		t.Fatalf("live line missing [k/N] counter or percent: %q", got)
	}

	sink(progress.Event{
		Phase: progress.PhaseDone, Skill: "beta", Index: 2, Count: 3,
		Commit: "9f8e7d6c5b4a39281706f5e4d3c2b1a098765432",
	})
	if got := buf.String(); !strings.Contains(got, "✓ beta @ 9f8e7d6c5b4a\n") {
		t.Fatalf("finished skill line missing: %q", got)
	}

	// A local-source skill finishes with no commit: the ✓ line must not
	// render a dangling "@".
	sink(progress.Event{Phase: progress.PhaseDone, Skill: "gamma", Index: 3, Count: 3, Repo: "/local/path"})
	if got := buf.String(); !strings.Contains(got, "✓ gamma\n") || strings.Contains(got, "gamma @") {
		t.Fatalf("local-source completion line wrong: %q", got)
	}
}

// TestFetchProgress_RepeatedCachedAnnouncedOnce: add materializes the same
// commit at discovery time and again at execute time — the second cache hit
// must not print a second ✓ line.
func TestFetchProgress_RepeatedCachedAnnouncedOnce(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	f := newTestFetchProgress(&buf)
	sink := f.Sink()

	e := progress.Event{Phase: progress.PhaseCached, Repo: "acme/skills", Commit: "a1b2c3d4e5f6a7b8c9d0a1b2c3d4e5f6a7b8c9d0"}
	sink(e)
	sink(e)
	if got := strings.Count(buf.String(), "(cached)"); got != 1 {
		t.Fatalf("cached line printed %d times, want once: %q", got, buf.String())
	}
}

// TestFetchProgress_NoTotalsFallsBackToSpinner: when git reports no totals
// the live line shows the object count, never a percentage.
func TestFetchProgress_NoTotalsFallsBackToSpinner(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	f := newTestFetchProgress(&buf)
	f.Sink()(progress.Event{Phase: progress.PhaseReceiving, Percent: -1, Objects: 1234})

	got := buf.String()
	if strings.Contains(got, "%") {
		t.Fatalf("no-totals frame must not show a percentage: %q", got)
	}
	if !strings.Contains(got, "1234") {
		t.Fatalf("no-totals frame missing the object count: %q", got)
	}
}

// TestFetchProgress_CloseErasesLiveLine: Close must leave the cursor on an
// empty line so the following stdout summary starts clean.
func TestFetchProgress_CloseErasesLiveLine(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	f := newTestFetchProgress(&buf)
	f.Sink()(progress.Event{Phase: progress.PhaseResolving, Repo: "acme/skills"})
	f.Close()
	if !strings.HasSuffix(buf.String(), eraseLine) {
		t.Fatalf("Close did not erase the live line: %q", buf.String())
	}
}

// TestFetchProgress_SanitizesHostileNames: repo/skill/detail/commit strings
// are remote-origin (the commit can come from a hostile manifest) and must
// never carry escape sequences to the terminal (constitution VI).
func TestFetchProgress_SanitizesHostileNames(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	f := newTestFetchProgress(&buf)
	sink := f.Sink()
	sink(progress.Event{Phase: progress.PhaseResolving, Repo: "evil\x1b[2Jrepo"})
	sink(progress.Event{Phase: progress.PhaseFetching, Repo: "acme/skills", Commit: "\x1b[2Jgarbage"})
	if strings.Contains(buf.String(), "\x1b[2J") {
		t.Fatalf("hostile string leaked an escape sequence: %q", buf.String())
	}
}

// TestFetchProgress_NilReceiver: a nil renderer yields a nil sink, so call
// sites can wire unconditionally.
func TestFetchProgress_NilReceiver(t *testing.T) {
	t.Parallel()

	var f *fetchProgress
	if f.Sink() != nil {
		t.Error("nil renderer must produce a nil sink")
	}
	f.Close() // must not panic
}

// TestOutput_FetchProgressGating: the renderer only exists for interactive,
// non-JSON, non-quiet runs whose stderr is a terminal — buffer-backed runs
// (every test, every pipe) get nil and stay silent.
func TestOutput_FetchProgressGating(t *testing.T) {
	t.Parallel()

	var outb, errb bytes.Buffer
	out := NewOutput(&outb, &errb, OutputOptions{Interactive: true})
	if out.fetchProgress() != nil {
		t.Error("buffer-backed output must not get a progress renderer")
	}
}

// ---- install lifecycle progress (spec 014 US3) ------------------------------------

func installEvent(k, total int, name string, phase app.InstallPhase, status app.InstallStatus) app.InstallProgressEvent {
	return app.InstallProgressEvent{
		SkillIndex: k, SkillTotal: total, SkillName: name,
		Source: "acme/skills", Phase: phase, Status: status,
	}
}

// TestInstallLines_VerboseNonTTY (FR-022, clarification #4): with the global
// verbose flag set, a non-TTY run prints one stable line per skill terminal
// state — and never any cursor-control sequence.
func TestInstallLines_VerboseNonTTY(t *testing.T) {
	t.Parallel()

	var outb, errb bytes.Buffer
	out := NewOutput(&outb, &errb, OutputOptions{Verbose: true})
	_, sink, done := out.withInstallProgress(t.Context())
	defer done()
	if sink == nil {
		t.Fatal("verbose non-TTY run returned a nil install sink")
	}

	sink(installEvent(1, 2, "alpha", app.InstallPhaseFetching, app.InstallStatusRunning))
	if errb.Len() != 0 {
		t.Errorf("running event produced output on a non-TTY (must be terminal-state lines only): %q", errb.String())
	}
	sink(installEvent(1, 2, "alpha", app.InstallPhaseComplete, app.InstallStatusInstalled))
	sink(installEvent(2, 2, "beta", app.InstallPhaseComplete, app.InstallStatusFailed))

	got := errb.String()
	for _, want := range []string{"installed alpha (1/2)", "failed beta (2/2)"} {
		if !strings.Contains(got, want) {
			t.Errorf("verbose lines missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "\x1b[") || strings.Contains(got, "\r") {
		t.Errorf("non-TTY output carries cursor-control bytes: %q", got)
	}
	if outb.Len() != 0 {
		t.Errorf("progress lines leaked to stdout: %q", outb.String())
	}
}

// TestInstallLines_SilentWithoutVerbose: non-TTY runs stay quiet by default.
func TestInstallLines_SilentWithoutVerbose(t *testing.T) {
	t.Parallel()

	var outb, errb bytes.Buffer
	out := NewOutput(&outb, &errb, OutputOptions{})
	_, sink, done := out.withInstallProgress(t.Context())
	defer done()
	if sink != nil {
		sink(installEvent(1, 1, "alpha", app.InstallPhaseComplete, app.InstallStatusInstalled))
	}
	if errb.Len() != 0 || outb.Len() != 0 {
		t.Errorf("default non-TTY run produced progress output: stdout=%q stderr=%q", outb.String(), errb.String())
	}
}

// TestInstallLines_JSONAndQuietStaySilent: --json and --quiet suppress even
// verbose progress lines.
func TestInstallLines_JSONAndQuietStaySilent(t *testing.T) {
	t.Parallel()

	for _, opts := range []OutputOptions{
		{Verbose: true, JSON: true},
		{Verbose: true, Quiet: true},
	} {
		var outb, errb bytes.Buffer
		out := NewOutput(&outb, &errb, opts)
		_, sink, done := out.withInstallProgress(t.Context())
		if sink != nil {
			sink(installEvent(1, 1, "alpha", app.InstallPhaseComplete, app.InstallStatusInstalled))
		}
		done()
		if errb.Len() != 0 || outb.Len() != 0 {
			t.Errorf("opts %+v produced progress output: stdout=%q stderr=%q", opts, outb.String(), errb.String())
		}
	}
}

// TestInstallLines_SanitizesNames: hostile skill names are neutralized in the
// verbose lines (FR-028).
func TestInstallLines_SanitizesNames(t *testing.T) {
	t.Parallel()

	var outb, errb bytes.Buffer
	out := NewOutput(&outb, &errb, OutputOptions{Verbose: true})
	_, sink, done := out.withInstallProgress(t.Context())
	defer done()
	sink(installEvent(1, 1, "evil\x1b]0;pwned\x07", app.InstallPhaseComplete, app.InstallStatusInstalled))
	if got := errb.String(); strings.Contains(got, "\x1b]") || strings.Contains(got, "\x07") {
		t.Errorf("escape bytes leaked: %q", got)
	}
}

// TestFetchProgress_InstallLiveLine: on an interactive terminal the live line
// shows [k/N] skill — phase between download events, and terminal events
// clear it.
func TestFetchProgress_InstallLiveLine(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	f := newTestFetchProgress(&buf)
	sink := f.InstallSink()

	sink(installEvent(1, 3, "gke-scaling", app.InstallPhaseVerifying, app.InstallStatusRunning))
	got := buf.String()
	for _, want := range []string{"[1/3]", "gke-scaling", "Verifying integrity"} {
		if !strings.Contains(got, want) {
			t.Errorf("install live line missing %q: %q", want, got)
		}
	}

	buf.Reset()
	sink(installEvent(1, 3, "gke-scaling", app.InstallPhaseComplete, app.InstallStatusInstalled))
	if got := buf.String(); !strings.Contains(got, eraseLine) {
		t.Errorf("terminal event did not clear the live line: %q", got)
	}
}
