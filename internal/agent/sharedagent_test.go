package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSharedAgent_ProjectDirIsTheStore(t *testing.T) {
	t.Parallel()

	a := sharedAgent{id: "s", name: "S", globalDir: []string{".s", "skills"}}
	if got, want := a.ProjectSkillDir("/proj"), filepath.Join("/proj", ".agents", "skills"); got != want {
		t.Errorf("ProjectSkillDir = %q, want %q", got, want)
	}
	if got, want := a.GlobalSkillDir("/home"), filepath.Join("/home", ".s", "skills"); got != want {
		t.Errorf("GlobalSkillDir = %q, want %q", got, want)
	}
	if !UsesSharedDir(a, "/proj") {
		t.Error("UsesSharedDir = false, want true")
	}
	if !a.SupportsSymlinks() {
		t.Error("SupportsSymlinks = false, want true")
	}
}

func TestSharedAgent_Detect(t *testing.T) {
	t.Parallel()

	marked := sharedAgent{id: "m", markers: []string{".mark", "mark.json"}}
	cases := []struct {
		name  string
		agent sharedAgent
		dirs  []string
		files []string
		want  bool
	}{
		{name: "no markers never detects", agent: sharedAgent{id: "n"}, dirs: []string{".agents", ".gemini", ".claude"}, want: false},
		{name: "store dir alone is not a marker", agent: marked, dirs: []string{".agents"}, want: false},
		{name: "directory marker", agent: marked, dirs: []string{".mark"}, want: true},
		{name: "file marker", agent: marked, files: []string{"mark.json"}, want: true},
		{name: "nothing present", agent: marked, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			for _, d := range tc.dirs {
				if err := os.MkdirAll(filepath.Join(root, d), 0o750); err != nil {
					t.Fatal(err)
				}
			}
			for _, f := range tc.files {
				if err := os.WriteFile(filepath.Join(root, f), []byte("{}"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			got, err := tc.agent.Detect(context.Background(), root)
			if err != nil {
				t.Fatalf("Detect: %v", err)
			}
			if got != tc.want {
				t.Errorf("Detect = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSharedAgent_ValidateInstallation(t *testing.T) {
	t.Parallel()

	a := sharedAgent{id: "s"}
	dir := t.TempDir()
	if err := a.ValidateInstallation(context.Background(), dir); err == nil {
		t.Error("validation passed without SKILL.md")
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.ValidateInstallation(context.Background(), dir); err != nil {
		t.Errorf("validation failed with SKILL.md present: %v", err)
	}
}
