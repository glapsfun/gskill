package agent_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/glapsfun/gskill/internal/agent"
)

func TestHermes_DirsAndIdentity(t *testing.T) {
	t.Parallel()

	a := agent.NewHermes()
	if a.ID() != "hermes" {
		t.Errorf("ID = %q, want hermes", a.ID())
	}
	if a.DisplayName() != "Hermes Agent" {
		t.Errorf("DisplayName = %q, want Hermes Agent", a.DisplayName())
	}
	if !agent.UsesSharedDir(a, "/proj") {
		t.Error("Hermes must read the shared store")
	}
	if got, want := a.GlobalSkillDir("/home"), filepath.Join("/home", ".hermes", "skills"); got != want {
		t.Errorf("GlobalSkillDir = %q, want %q", got, want)
	}
}

func TestHermes_Detect(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		dir  string
		want bool
	}{
		{name: ".hermes directory", dir: ".hermes", want: true},
		{name: "store dir only", dir: ".agents", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, tc.dir), 0o750); err != nil {
				t.Fatal(err)
			}
			got, err := agent.NewHermes().Detect(context.Background(), root)
			if err != nil {
				t.Fatalf("Detect: %v", err)
			}
			if got != tc.want {
				t.Errorf("Detect = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestHermes_ProjectNote(t *testing.T) {
	t.Parallel()

	n, ok := agent.NewHermes().(agent.ProjectNoter)
	if !ok {
		t.Fatal("Hermes does not implement agent.ProjectNoter")
	}
	const want = "hermes: Hermes loads project skills only after you trust this project; run 'hermes skills trust'"
	if got := n.ProjectNote(); got != want {
		t.Errorf("ProjectNote = %q, want %q", got, want)
	}
}
