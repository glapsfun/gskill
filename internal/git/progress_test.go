package git

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/progress"
)

func TestParseFetchProgress(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		line string
		want progress.Event
		ok   bool
	}{
		{
			name: "receiving with rate",
			line: "Receiving objects:  62% (124/200), 4.10 MiB | 2.30 MiB/s",
			want: progress.Event{Phase: progress.PhaseReceiving, Percent: 62, Objects: 124, Total: 200, Detail: "4.10 MiB | 2.30 MiB/s"},
			ok:   true,
		},
		{
			name: "receiving done",
			line: "Receiving objects: 100% (200/200), 6.60 MiB | 2.30 MiB/s, done.",
			want: progress.Event{Phase: progress.PhaseReceiving, Percent: 100, Objects: 200, Total: 200, Detail: "6.60 MiB | 2.30 MiB/s"},
			ok:   true,
		},
		{
			name: "remote-prefixed counting",
			line: "remote: Counting objects:  50% (1/2)",
			want: progress.Event{Phase: progress.PhaseCounting, Percent: 50, Objects: 1, Total: 2},
			ok:   true,
		},
		{
			name: "remote-prefixed compressing done",
			line: "remote: Compressing objects: 100% (2/2), done.",
			want: progress.Event{Phase: progress.PhaseCounting, Percent: 100, Objects: 2, Total: 2},
			ok:   true,
		},
		{
			name: "resolving deltas",
			line: "Resolving deltas:  10% (5/50)",
			want: progress.Event{Phase: progress.PhaseDeltas, Percent: 10, Objects: 5, Total: 50},
			ok:   true,
		},
		{
			name: "no totals",
			line: "remote: Counting objects: 1234",
			want: progress.Event{Phase: progress.PhaseCounting, Percent: -1, Objects: 1234},
			ok:   true,
		},
		{
			name: "unrelated line",
			line: "From github.com:acme/skills",
			ok:   false,
		},
		{
			name: "auth noise ignored",
			line: "fatal: Authentication failed for 'https://github.com/x/y'",
			ok:   false,
		},
		{
			name: "empty",
			line: "",
			ok:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := parseFetchProgress(tc.line)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v (line %q)", ok, tc.ok, tc.line)
			}
			if ok && got != tc.want {
				t.Errorf("event = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestProgressWriter_SplitsCRAndLF: git separates in-place progress frames
// with \r; the writer must treat both \r and \n as line terminators, cope
// with frames split across Write calls, and keep non-progress diagnostics
// for the classify error path while excluding the frames themselves — a
// failed fetch must classify against a concise tail, not a screenful of
// \r-separated progress.
func TestProgressWriter_SplitsCRAndLF(t *testing.T) {
	t.Parallel()

	var events []progress.Event
	w := newProgressWriter(func(e progress.Event) { events = append(events, e) })

	chunks := []string{
		"remote: Counting objects:  50% (1/2)\rremote: Counting objects: 100% (2/2), done.\n",
		"Receiving objects:  62% (124/2", // frame split mid-line
		"00), 4.10 MiB | 2.30 MiB/s\r",
		"Receiving objects: 100% (200/200), 6.60 MiB | 2.30 MiB/s, done.\n",
		"Resolving deltas:  10% (5/50)\r",
		"fatal: early EOF\n",
	}
	for _, c := range chunks {
		if _, err := w.Write([]byte(c)); err != nil {
			t.Fatal(err)
		}
	}

	wantPhases := []progress.Phase{
		progress.PhaseCounting, progress.PhaseCounting,
		progress.PhaseReceiving, progress.PhaseReceiving,
		progress.PhaseDeltas,
	}
	if len(events) != len(wantPhases) {
		t.Fatalf("got %d events, want %d: %+v", len(events), len(wantPhases), events)
	}
	for i, want := range wantPhases {
		if events[i].Phase != want {
			t.Errorf("event %d phase = %v, want %v", i, events[i].Phase, want)
		}
	}
	if events[2].Percent != 62 || events[2].Objects != 124 || events[2].Total != 200 {
		t.Errorf("split frame parsed wrong: %+v", events[2])
	}

	raw := w.String()
	if !strings.Contains(raw, "fatal: early EOF") {
		t.Errorf("raw capture lost the diagnostic line:\n%s", raw)
	}
	if strings.Contains(raw, "Receiving objects") || strings.Contains(raw, "\r") {
		t.Errorf("raw capture must exclude progress frames and carriage returns:\n%q", raw)
	}
}

// TestProgressWriter_CapsCapture: a very long non-progress stream is bounded,
// keeping the tail where git prints its diagnostics.
func TestProgressWriter_CapsCapture(t *testing.T) {
	t.Parallel()

	w := newProgressWriter(nil)
	line := strings.Repeat("x", 1024)
	for range 128 { // 128 KiB of noise
		if _, err := w.Write([]byte(line + "\n")); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := w.Write([]byte("fatal: the remote end hung up\n")); err != nil {
		t.Fatal(err)
	}
	raw := w.String()
	if len(raw) > rawCaptureCap+len(line)+64 {
		t.Errorf("capture grew past the cap: %d bytes", len(raw))
	}
	if !strings.Contains(raw, "fatal: the remote end hung up") {
		t.Error("capture lost the trailing diagnostic")
	}
}

// TestProgressWriter_FlushesTrailingPartialLine: a final frame without a
// terminator must still reach the raw capture (classify reads it on failure).
func TestProgressWriter_FlushesTrailingPartialLine(t *testing.T) {
	t.Parallel()

	w := newProgressWriter(nil)
	if _, err := w.Write([]byte("fatal: repository not found")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(w.String(), "repository not found") {
		t.Errorf("raw capture lost the unterminated line: %q", w.String())
	}
}

// TestRunGitProgress_FailureClassifies: the progress-streaming exec path must
// keep the typed-error classification of runGit.
func TestRunGitProgress_FailureClassifies(t *testing.T) {
	t.Parallel()

	_, err := runGitProgress(context.Background(), t.TempDir(), nil,
		"fetch", "--progress", "this-remote-does-not-exist")
	if err == nil {
		t.Fatal("expected an error from fetching a nonexistent remote")
	}
	if !errors.Is(err, errs.ErrSourceUnavailable) {
		t.Fatalf("error %v is not classified as source-unavailable", err)
	}
}
