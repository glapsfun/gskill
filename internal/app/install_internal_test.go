package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/glapsfun/gskill/internal/installer"
	"github.com/glapsfun/gskill/internal/skillslock"
)

// TestRemoveTargets_RollbackNeverDeletesTheActiveEntry covers spec 027 FR-010:
// a shared target records the active entry itself, so an atomic-install
// rollback must skip it rather than delete the committed content.
func TestRemoveTargets_RollbackNeverDeletesTheActiveEntry(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	activeRel := filepath.Join(".agents", "skills", "demo")
	claudeRel := filepath.Join(".claude", "skills", "demo")
	for _, rel := range []string{activeRel, claudeRel} {
		if err := os.MkdirAll(filepath.Join(root, rel), 0o750); err != nil {
			t.Fatal(err)
		}
	}

	newHealthApp().removeTargets(root, installer.ScopeProject, installer.Result{
		ActivePath: filepath.ToSlash(activeRel),
		Targets: map[string]string{
			"sharedfake": filepath.ToSlash(activeRel),
			"claude":     filepath.ToSlash(claudeRel),
		},
	})

	if _, err := os.Stat(filepath.Join(root, activeRel)); err != nil {
		t.Errorf("rollback deleted the active entry: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, claudeRel)); !os.IsNotExist(err) {
		t.Errorf("rollback left the claude target behind: %v", err)
	}
}

// sharedOnlyRecord is a lock record whose every target is a shared-location
// agent, recorded with the stand-in representative mode.
func sharedOnlyRecord(mode string, ids ...string) skillslock.Record {
	modes := make(map[string]string, len(ids))
	targets := make(map[string]string, len(ids))
	for _, id := range ids {
		modes[id] = string(installer.ModeShared)
		targets[id] = ".agents/skills/demo"
	}
	return skillslock.Record{Installation: skillslock.Installation{
		Mode: mode, Agents: ids, Modes: modes, Targets: targets,
	}}
}

func TestOnlySharedTargets(t *testing.T) {
	t.Parallel()

	mixed := sharedOnlyRecord(string(installer.ModeSymlink), "opencode")
	mixed.Installation.Agents = append(mixed.Installation.Agents, "claude")
	mixed.Installation.Modes["claude"] = string(installer.ModeCopy)
	unrecorded := sharedOnlyRecord(string(installer.ModeSymlink), "opencode")
	unrecorded.Installation.Agents = append(unrecorded.Installation.Agents, "hermes")

	cases := []struct {
		name string
		rec  skillslock.Record
		want bool
	}{
		{"no agents", skillslock.Record{}, false},
		{"all shared", sharedOnlyRecord(string(installer.ModeSymlink), "opencode", "hermes"), true},
		{"shared and copy", mixed, false},
		{"agent without a recorded mode", unrecorded, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := onlySharedTargets(tc.rec); got != tc.want {
				t.Errorf("onlySharedTargets = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAgentAddModePref(t *testing.T) {
	t.Parallel()

	mixed := sharedOnlyRecord(string(installer.ModeSymlink), "opencode")
	mixed.Installation.Agents = append(mixed.Installation.Agents, "claude")
	mixed.Installation.Modes["claude"] = string(installer.ModeSymlink)
	claudeOnly := skillslock.Record{Installation: skillslock.Installation{
		Mode: string(installer.ModeSymlink), Agents: []string{"claude"},
		Modes: map[string]string{"claude": string(installer.ModeSymlink)},
	}}

	cases := []struct {
		name     string
		rec      skillslock.Record
		declared string
		want     string
	}{
		{"shared-only stand-in symlink keeps auto's fallback", sharedOnlyRecord(string(installer.ModeSymlink), "opencode"), "", installer.DefaultModePref},
		{"shared-only declared auto keeps auto's fallback", sharedOnlyRecord(string(installer.ModeSymlink), "opencode"), installer.PrefAuto, installer.DefaultModePref},
		{"shared-only declared symlink stays strict", sharedOnlyRecord(string(installer.ModeSymlink), "opencode"), installer.PrefSymlink, installer.PrefSymlink},
		{"shared-only explicit copy is passed through", sharedOnlyRecord(string(installer.ModeCopy), "opencode"), installer.PrefCopy, ""},
		{"mixed skill uses its recorded mode", mixed, "", ""},
		{"linking-only skill uses its recorded mode", claudeOnly, installer.PrefSymlink, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := agentAddModePref(tc.rec, tc.declared); got != tc.want {
				t.Errorf("agentAddModePref(declared %q) = %q, want %q", tc.declared, got, tc.want)
			}
		})
	}
}

func TestMergeAgentInstall_AdoptsFirstLinkingAgentsMode(t *testing.T) {
	t.Parallel()

	mixed := sharedOnlyRecord(string(installer.ModeSymlink), "opencode")
	mixed.Installation.Agents = append(mixed.Installation.Agents, "claude")
	mixed.Installation.Modes["claude"] = string(installer.ModeSymlink)

	cases := []struct {
		name   string
		rec    skillslock.Record
		result installer.Result
		want   string
	}{
		{
			name: "first linking agent replaces the stand-in",
			rec:  sharedOnlyRecord(string(installer.ModeSymlink), "opencode"),
			result: installer.Result{
				Mode: installer.ModeCopy, Agents: []string{"claude"},
				Modes: map[string]string{"claude": string(installer.ModeCopy)},
			},
			want: string(installer.ModeCopy),
		},
		{
			name: "an existing linking agent's mode is kept",
			rec:  mixed,
			result: installer.Result{
				Mode: installer.ModeCopy, Agents: []string{"codex"},
				Modes: map[string]string{"codex": string(installer.ModeCopy)},
			},
			want: string(installer.ModeSymlink),
		},
		{
			// add --agent hermes --copy declares mode = "copy" in skills.toml;
			// keeping the stand-in symlink would read as a placement change on
			// every later install.
			name: "a shared agent added with --copy records copy",
			rec:  sharedOnlyRecord(string(installer.ModeSymlink), "opencode"),
			result: installer.Result{
				Mode: installer.ModeCopy, Agents: []string{"hermes"},
				Modes: map[string]string{"hermes": string(installer.ModeShared)},
			},
			want: string(installer.ModeCopy),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := tc.rec
			mergeAgentInstall(&rec, tc.result)
			if rec.Installation.Mode != tc.want {
				t.Errorf("Installation.Mode = %q, want %q", rec.Installation.Mode, tc.want)
			}
		})
	}
}
