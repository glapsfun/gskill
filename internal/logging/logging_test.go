package logging_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/logging"
)

func TestNew_JSONFormatEmitsParseableLine(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := logging.New(logging.Options{Format: logging.FormatJSON, Writer: &buf})
	logger.Info("hello", "skill", "demo")

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("log line is not valid JSON: %v\nline: %s", err, buf.String())
	}
	if rec["msg"] != "hello" {
		t.Errorf("msg = %v, want %q", rec["msg"], "hello")
	}
	if rec["skill"] != "demo" {
		t.Errorf("skill = %v, want %q", rec["skill"], "demo")
	}
}

func TestNew_LevelFiltersBelowThreshold(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := logging.New(logging.Options{Level: slog.LevelInfo, Writer: &buf})
	logger.Debug("suppressed")

	if buf.Len() != 0 {
		t.Errorf("debug line emitted at info level: %q", buf.String())
	}

	logger.Warn("shown")
	if !strings.Contains(buf.String(), "shown") {
		t.Errorf("warn line not emitted: %q", buf.String())
	}
}

func TestParseLevel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"INFO", slog.LevelInfo},
		{"Warn", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"error", slog.LevelError},
		{"", slog.LevelInfo},
		{"nonsense", slog.LevelInfo},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()

			if got := logging.ParseLevel(tt.in); got != tt.want {
				t.Errorf("ParseLevel(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
