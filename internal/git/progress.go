package git

import (
	"bytes"
	"context"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/glapsfun/gskill/internal/progress"
)

// Streaming parser for `git fetch --progress` stderr (spec 013). Git redraws
// progress frames in place, separating them with \r, and sideband lines from
// the server arrive prefixed with "remote: ". Unknown lines are ignored —
// progress reporting must never break a fetch — while the raw stream is kept
// intact for the classify() error path.

// fetchProgressRE matches git's percent-style progress frames, e.g.
//
//	Receiving objects:  62% (124/200), 4.10 MiB | 2.30 MiB/s
//	remote: Compressing objects: 100% (2/2), done.
//	Resolving deltas:  10% (5/50)
var fetchProgressRE = regexp.MustCompile(
	`^(Counting|Compressing|Receiving) objects:\s+(\d+)% \((\d+)/(\d+)\)(?:, (.*))?$`)

// deltaProgressRE matches the delta-resolution phase, same shape.
var deltaProgressRE = regexp.MustCompile(
	`^Resolving deltas:\s+(\d+)% \((\d+)/(\d+)\)(?:, (.*))?$`)

// countOnlyRE matches the totals-unknown variant ("Counting objects: 1234").
var countOnlyRE = regexp.MustCompile(
	`^(Counting|Compressing|Receiving) objects:\s+(\d+)\s*$`)

// parseFetchProgress parses one stderr line into a progress event. The second
// return is false for anything that is not a recognized progress frame.
func parseFetchProgress(line string) (progress.Event, bool) {
	line = strings.TrimPrefix(line, "remote: ")
	// The completion frame appends ", done." after the human tail; strip it so
	// the Detail capture holds only the rate/size text.
	line = strings.TrimSuffix(strings.TrimRight(line, " \t"), ", done.")

	if m := fetchProgressRE.FindStringSubmatch(line); m != nil {
		return progress.Event{
			Phase:   objectPhase(m[1]),
			Percent: atoi(m[2]),
			Objects: atoi64(m[3]),
			Total:   atoi64(m[4]),
			Detail:  m[5],
		}, true
	}
	if m := deltaProgressRE.FindStringSubmatch(line); m != nil {
		return progress.Event{
			Phase:   progress.PhaseDeltas,
			Percent: atoi(m[1]),
			Objects: atoi64(m[2]),
			Total:   atoi64(m[3]),
			Detail:  m[4],
		}, true
	}
	if m := countOnlyRE.FindStringSubmatch(line); m != nil {
		return progress.Event{
			Phase:   objectPhase(m[1]),
			Percent: -1, // git reported no totals: renderers fall back to a spinner
			Objects: atoi64(m[2]),
		}, true
	}
	return progress.Event{}, false
}

// objectPhase maps git's object-phase verb onto the event vocabulary:
// counting and compressing are both pre-transfer bookkeeping.
func objectPhase(verb string) progress.Phase {
	if verb == "Receiving" {
		return progress.PhaseReceiving
	}
	return progress.PhaseCounting
}

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }

func atoi64(s string) int64 { n, _ := strconv.ParseInt(s, 10, 64); return n }

// progressWriter is an io.Writer for cmd.Stderr that feeds complete lines
// (terminated by \r or \n) to the parser while capturing the raw stream so
// git failures still classify against full stderr.
type progressWriter struct {
	onEvent progress.Sink
	raw     bytes.Buffer
	partial bytes.Buffer
}

func newProgressWriter(onEvent progress.Sink) *progressWriter {
	return &progressWriter{onEvent: onEvent}
}

// Write implements io.Writer. It never returns an error: progress parsing
// must not be able to fail a fetch.
func (w *progressWriter) Write(p []byte) (int, error) {
	w.raw.Write(p)
	for _, b := range p {
		if b == '\r' || b == '\n' {
			w.flushLine()
			continue
		}
		w.partial.WriteByte(b)
	}
	return len(p), nil
}

// flushLine parses and resets the buffered line.
func (w *progressWriter) flushLine() {
	line := w.partial.String()
	w.partial.Reset()
	if line == "" || w.onEvent == nil {
		return
	}
	if e, ok := parseFetchProgress(line); ok {
		w.onEvent(e)
	}
}

// String returns the raw stderr captured so far (for classify on failure).
func (w *progressWriter) String() string { return w.raw.String() }

// runGitProgress executes git with args like runGit, but streams stderr
// through the progress parser instead of only buffering it. Error handling
// (classification, credential redaction) matches runGit.
func runGitProgress(ctx context.Context, dir string, onEvent progress.Sink, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var out bytes.Buffer
	errw := newProgressWriter(onEvent)
	cmd.Stdout = &out
	cmd.Stderr = errw
	if err := cmd.Run(); err != nil {
		return "", classify(err, errw.String())
	}
	return strings.TrimRight(out.String(), "\n"), nil
}
