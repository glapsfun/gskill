package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/glapsfun/gskill/internal/config"
)

// TestLoad_ProjectLayerPrecedence pins FR-002: project configuration declared
// in the manifest sits above user configuration and below environment and
// flags. Each layer here overrides the same key, so the winner identifies the
// precedence unambiguously.
func TestLoad_ProjectLayerPrecedence(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	userFile := filepath.Join(dir, "user.toml")
	if err := os.WriteFile(userFile, []byte("log_level = \"user\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		sources config.Sources
		want    string
	}{
		{
			name:    "defaults only",
			sources: config.Sources{Environ: []string{}},
			want:    "info",
		},
		{
			name:    "user file beats defaults",
			sources: config.Sources{UserFile: userFile, Environ: []string{}},
			want:    "user",
		},
		{
			name: "project beats user",
			sources: config.Sources{
				UserFile:   userFile,
				ProjectMap: map[string]any{"log_level": "project"},
				Environ:    []string{},
			},
			want: "project",
		},
		{
			name: "environment beats project",
			sources: config.Sources{
				UserFile:   userFile,
				ProjectMap: map[string]any{"log_level": "project"},
				Environ:    []string{"GSKILL_LOG_LEVEL=env"},
			},
			want: "env",
		},
		{
			name: "flags beat everything",
			sources: config.Sources{
				UserFile:   userFile,
				ProjectMap: map[string]any{"log_level": "project"},
				Environ:    []string{"GSKILL_LOG_LEVEL=env"},
				Flags:      map[string]any{"log_level": "flag"},
			},
			want: "flag",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := config.Load(tt.sources)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.LogLevel != tt.want {
				t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, tt.want)
			}
		})
	}
}

// TestLoad_ProjectLayerUnknownKeysAccepted: an unrecognized project key must
// never fail the load. The manifest is committed and shared, so a key a newer
// gskill understands cannot break an older one (spec 022 FR-011).
func TestLoad_ProjectLayerUnknownKeysAccepted(t *testing.T) {
	t.Parallel()

	cfg, err := config.Load(config.Sources{
		ProjectMap: map[string]any{"not_a_real_key": "x", "log_level": "project"},
		Environ:    []string{},
	})
	if err != nil {
		t.Fatalf("unknown project key must not fail the load: %v", err)
	}
	if cfg.LogLevel != "project" {
		t.Errorf("LogLevel = %q, want the known key still applied", cfg.LogLevel)
	}
}
