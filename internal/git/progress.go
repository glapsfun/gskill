package git

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/glapsfun/gskill/internal/progress"
)

// Streaming parser for `git fetch --progress` stderr. Git redraws progress
// frames in place, separating them with \r, and sideband lines from the
// server arrive prefixed with "remote: ". Unknown lines are ignored —
// progress reporting must never break a fetch — and recognized progress
// frames are kept out of the raw capture so a failure still classifies
// against a concise stderr tail instead of a screenful of \r-separated
// frames.

// frameProgressRE matches git's percent-style progress frames, e.g.
//
//	Receiving objects:  62% (124/200), 4.10 MiB | 2.30 MiB/s
//	remote: Compressing objects: 100% (2/2), done.
//	Resolving deltas:  10% (5/50)
var frameProgressRE = regexp.MustCompile(
	`^((?:Counting|Compressing|Receiving) objects|Resolving deltas):\s+(\d+)% \((\d+)/(\d+)\)(?:, (.*))?$`)

// countOnlyRE matches the totals-unknown variant ("Counting objects: 1234").
var countOnlyRE = regexp.MustCompile(
	`^((?:Counting|Compressing|Receiving) objects):\s+(\d+)\s*$`)

// parseFetchProgress parses one stderr line into a progress event. The second
// return is false for anything that is not a recognized progress frame.
func parseFetchProgress(line string) (progress.Event, bool) {
	line = strings.TrimPrefix(line, "remote: ")
	// The completion frame appends ", done." after the human tail; strip it so
	// the Detail capture holds only the rate/size text.
	line = strings.TrimSuffix(strings.TrimRight(line, " \t"), ", done.")

	if m := frameProgressRE.FindStringSubmatch(line); m != nil {
		return progress.Event{
			Phase:   framePhase(m[1]),
			Percent: atoi(m[2]),
			Objects: atoi64(m[3]),
			Total:   atoi64(m[4]),
			Detail:  m[5],
		}, true
	}
	if m := countOnlyRE.FindStringSubmatch(line); m != nil {
		return progress.Event{
			Phase:   framePhase(m[1]),
			Percent: -1, // git reported no totals: renderers fall back to a spinner
			Objects: atoi64(m[2]),
		}, true
	}
	return progress.Event{}, false
}

// framePhase maps a frame's leading label onto the event vocabulary:
// counting and compressing are both pre-transfer bookkeeping.
func framePhase(label string) progress.Phase {
	switch label {
	case "Receiving objects":
		return progress.PhaseReceiving
	case "Resolving deltas":
		return progress.PhaseDeltas
	default:
		return progress.PhaseCounting
	}
}

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }

func atoi64(s string) int64 { n, _ := strconv.ParseInt(s, 10, 64); return n }

// rawCaptureCap bounds the retained non-progress stderr; classification only
// needs the tail, where git prints its fatal diagnostics.
const rawCaptureCap = 64 * 1024

// progressWriter is an io.Writer for cmd.Stderr that feeds complete lines
// (terminated by \r or \n) to the parser. Non-progress lines are retained
// (capped, newline-joined) so git failures still classify against their
// diagnostics; progress frames never enter the capture.
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
	for _, b := range p {
		if b == '\r' || b == '\n' {
			w.flushLine()
			continue
		}
		w.partial.WriteByte(b)
	}
	return len(p), nil
}

// flushLine parses and resets the buffered line; unrecognized lines go to the
// raw capture.
func (w *progressWriter) flushLine() {
	line := w.partial.String()
	w.partial.Reset()
	if line == "" {
		return
	}
	if e, ok := parseFetchProgress(line); ok {
		if w.onEvent != nil {
			w.onEvent(e)
		}
		return
	}
	w.capture(line + "\n")
}

// capture appends text to the bounded raw buffer, dropping the oldest half
// when the cap is exceeded (diagnostics live at the end of the stream).
func (w *progressWriter) capture(text string) {
	w.raw.WriteString(text)
	if w.raw.Len() > rawCaptureCap {
		tail := w.raw.Bytes()[w.raw.Len()-rawCaptureCap/2:]
		trimmed := append([]byte(nil), tail...)
		w.raw.Reset()
		w.raw.Write(trimmed)
	}
}

// String returns the captured non-progress stderr, including any pending
// unterminated line (a final "fatal: …" often has no trailing newline).
func (w *progressWriter) String() string {
	return w.raw.String() + w.partial.String()
}

// repoLocationEnvPrefixes are the environment variables git uses to resolve
// a repository's location, overriding the dir git would otherwise discover
// from cmd.Dir. git sets several of these for hook subprocesses (and an
// enclosing process may set them for other reasons); left unfiltered, they
// silently redirect gskill's own git commands at an unrelated repository
// instead of dir. Auth/behavior/config variables (GIT_SSH_COMMAND,
// GIT_ASKPASS, GIT_TERMINAL_PROMPT, GIT_CONFIG_COUNT/KEY_*/VALUE_*, ...) are
// deliberately left untouched even though git also sets some of these for
// hook subprocesses — a user's git auth and config-injection setup (e.g.
// GIT_CONFIG_KEY_0=http.extraheader for a private source) must keep working
// through gskill's fetches; only the narrower repo-location class is
// filtered here. See internal/testutil.GitEnv for the test-only counterpart,
// which can afford to strip all GIT_* vars since fixtures never need auth.
var repoLocationEnvPrefixes = []string{
	"GIT_DIR=", "GIT_WORK_TREE=", "GIT_INDEX_FILE=", "GIT_COMMON_DIR=",
	"GIT_OBJECT_DIRECTORY=", "GIT_ALTERNATE_OBJECT_DIRECTORIES=",
	"GIT_CEILING_DIRECTORIES=",
}

// sanitizedEnv returns os.Environ() with repoLocationEnvPrefixes stripped, so
// dir is always the sole authority for which repository a git call targets.
func sanitizedEnv() []string {
	env := os.Environ()
	filtered := env[:0]
	for _, kv := range env {
		blocked := false
		for _, prefix := range repoLocationEnvPrefixes {
			if strings.HasPrefix(kv, prefix) {
				blocked = true
				break
			}
		}
		if !blocked {
			filtered = append(filtered, kv)
		}
	}
	return filtered
}

// runGitProgress executes git with args, streaming stderr through the
// progress parser. Error handling (classification, credential redaction)
// matches the historical exec path; LC_ALL=C pins git's output to the
// English the parser and classifier both match on.
func runGitProgress(ctx context.Context, dir string, onEvent progress.Sink, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(sanitizedEnv(), "LC_ALL=C")
	var out bytes.Buffer
	errw := newProgressWriter(onEvent)
	cmd.Stdout = &out
	cmd.Stderr = errw
	if err := cmd.Run(); err != nil {
		return "", classify(err, errw.String())
	}
	return strings.TrimRight(out.String(), "\n"), nil
}
