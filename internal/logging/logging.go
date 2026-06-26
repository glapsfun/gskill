// Package logging configures gskill's structured logger. Logs always go to
// stderr (never stdout, which is reserved for primary command output), with the
// level and format taken from configuration.
package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// Format selects the structured-log encoding.
type Format string

// Supported log formats.
const (
	FormatText Format = "text"
	FormatJSON Format = "json"
)

// Options configures a logger built by New.
type Options struct {
	// Level is the minimum level emitted. The zero value is slog.LevelInfo.
	Level slog.Level
	// Format selects text or JSON encoding. The zero value is FormatText.
	Format Format
	// Writer is the log sink. The zero value is os.Stderr.
	Writer io.Writer
}

// New builds an slog.Logger from opts, defaulting the sink to stderr and the
// encoding to text.
func New(opts Options) *slog.Logger {
	w := opts.Writer
	if w == nil {
		w = os.Stderr
	}

	handlerOpts := &slog.HandlerOptions{Level: opts.Level}

	var handler slog.Handler
	if opts.Format == FormatJSON {
		handler = slog.NewJSONHandler(w, handlerOpts)
	} else {
		handler = slog.NewTextHandler(w, handlerOpts)
	}
	return slog.New(handler)
}

// ParseLevel maps a case-insensitive level name ("debug", "info", "warn",
// "error") to an slog.Level, falling back to slog.LevelInfo for empty or
// unknown values.
func ParseLevel(name string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
