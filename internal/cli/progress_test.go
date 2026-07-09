package cli

import (
	"bytes"
	"strings"
	"testing"

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
	if got := buf.String(); !strings.Contains(got, "Fetching acme/skills @ a1b2c3d\n") {
		t.Fatalf("fetch header not persisted: %q", got)
	}

	sink(progress.Event{Phase: progress.PhaseReceiving, Percent: 62, Objects: 124, Total: 200, Detail: "4.10 MiB | 2.30 MiB/s"})
	got := buf.String()
	if !strings.Contains(got, "62%") || !strings.Contains(got, "4.10 MiB | 2.30 MiB/s") {
		t.Fatalf("live bar missing percent/detail: %q", got)
	}

	sink(progress.Event{Phase: progress.PhaseDone, Repo: "acme/skills"})
	if !strings.HasSuffix(buf.String(), eraseLine) {
		t.Fatalf("done (add path) must erase the live line and persist nothing: %q", buf.String())
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
	if got := buf.String(); !strings.Contains(got, "✓ beta @ 9f8e7d6\n") {
		t.Fatalf("finished skill line missing: %q", got)
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

// TestFetchProgress_SanitizesHostileNames: repo/skill/detail strings are
// remote-origin and must never carry escape sequences to the terminal.
func TestFetchProgress_SanitizesHostileNames(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	f := newTestFetchProgress(&buf)
	f.Sink()(progress.Event{Phase: progress.PhaseResolving, Repo: "evil\x1b[2Jrepo"})
	if strings.Contains(buf.String(), "\x1b[2J") {
		t.Fatalf("hostile repo name leaked an escape sequence: %q", buf.String())
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
