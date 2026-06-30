package installer

import (
	"context"
	"testing"
)

// stubAgent is a minimal agent.Agent whose symlink capability is configurable.
type stubAgent struct{ symlinks bool }

func (stubAgent) ID() string                                         { return "stub" }
func (stubAgent) DisplayName() string                                { return "Stub" }
func (stubAgent) Detect(context.Context, string) (bool, error)       { return false, nil }
func (stubAgent) ProjectSkillDir(root string) string                 { return root }
func (stubAgent) GlobalSkillDir(home string) string                  { return home }
func (a stubAgent) SupportsSymlinks() bool                           { return a.symlinks }
func (stubAgent) ValidateInstallation(context.Context, string) error { return nil }

func TestAgentActivation(t *testing.T) {
	t.Parallel()
	yes := stubAgent{symlinks: true}
	no := stubAgent{symlinks: false}

	cases := []struct {
		name    string
		pref    string
		support stubAgent
		want    activation
	}{
		{"auto on symlink-capable", PrefAuto, yes, activateAuto},
		{"empty defaults to auto", "", yes, activateAuto},
		{"symlink is strict", PrefSymlink, yes, activateSymlinkStrict},
		{"copy forces copy", PrefCopy, yes, activateCopy},
		{"unsupported agent copies under auto", PrefAuto, no, activateCopy},
		{"unsupported agent copies even under symlink", PrefSymlink, no, activateCopy},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := agentActivation(tc.pref, tc.support); got != tc.want {
				t.Errorf("agentActivation(%q, supports=%v) = %d, want %d", tc.pref, tc.support.symlinks, got, tc.want)
			}
		})
	}
}
