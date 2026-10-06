package agent_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/agent"
)

func TestOpenCode_DirsAndIdentity(t *testing.T) {
	t.Parallel()

	a := agent.NewOpenCode()
	if a.ID() != "opencode" {
		t.Errorf("ID = %q, want opencode", a.ID())
	}
	if a.DisplayName() != "OpenCode" {
		t.Errorf("DisplayName = %q, want OpenCode", a.DisplayName())
	}
	if !agent.UsesSharedDir(a, "/proj") {
		t.Error("OpenCode must read the shared store")
	}
	if got, want := a.GlobalSkillDir("/home"), filepath.Join("/home", ".config", "opencode", "skills"); got != want {
		t.Errorf("GlobalSkillDir = %q, want %q", got, want)
	}
}

func TestOpenCode_Detect(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		dir  string
		file string
		want bool
	}{
		{name: ".opencode directory", dir: ".opencode", want: true},
		{name: "opencode.json", file: "opencode.json", want: true},
		{name: "opencode.jsonc", file: "opencode.jsonc", want: true},
		{name: "store dir only", dir: ".agents", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			if tc.dir != "" {
				if err := os.MkdirAll(filepath.Join(root, tc.dir), 0o750); err != nil {
					t.Fatal(err)
				}
			}
			if tc.file != "" {
				if err := os.WriteFile(filepath.Join(root, tc.file), []byte("{}"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			got, err := agent.NewOpenCode().Detect(context.Background(), root)
			if err != nil {
				t.Fatalf("Detect: %v", err)
			}
			if got != tc.want {
				t.Errorf("Detect = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestOpenCode_Advise(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("a", 65)
	cases := []struct {
		name     string
		dir      string
		skillMD  string
		contains string
	}{
		{name: "valid", dir: "demo", skillMD: "---\nname: demo\ndescription: d\n---\n"},
		{name: "missing name", dir: "demo", skillMD: "---\ndescription: d\n---\n", contains: "has no 'name'"},
		{name: "name differs from directory", dir: "demo", skillMD: "---\nname: other\ndescription: d\n---\n", contains: `declares name "other"`},
		{name: "name too long", dir: long, skillMD: "---\nname: " + long + "\ndescription: d\n---\n", contains: "exceeds 64 characters"},
	}
	adv, ok := agent.NewOpenCode().(agent.Advisor)
	if !ok {
		t.Fatal("OpenCode does not implement agent.Advisor")
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := filepath.Join(t.TempDir(), tc.dir)
			if err := os.MkdirAll(dir, 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(tc.skillMD), 0o600); err != nil {
				t.Fatal(err)
			}

			got := adv.Advise(tc.dir, dir)
			if tc.contains == "" {
				if len(got) != 0 {
					t.Errorf("Advise = %v, want none", got)
				}
				return
			}
			if len(got) != 1 || !strings.Contains(got[0], tc.contains) || !strings.HasPrefix(got[0], "opencode: ") {
				t.Errorf("Advise = %v, want one opencode line containing %q", got, tc.contains)
			}
		})
	}
}
