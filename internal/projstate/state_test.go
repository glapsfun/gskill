package projstate_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/projstate"
)

func TestLoadOrInit_CreatesStateWithProjectID(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	st, err := projstate.LoadOrInit(root)
	if err != nil {
		t.Fatalf("LoadOrInit: %v", err)
	}
	if st.ProjectID == "" {
		t.Fatal("ProjectID empty after init")
	}
	if !strings.HasPrefix(st.ProjectID, "p-") {
		t.Errorf("ProjectID = %q, want p- prefix", st.ProjectID)
	}
	if err := st.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".gskill", "state.json")); err != nil {
		t.Errorf("state.json not written: %v", err)
	}
}

func TestLoadOrInit_StableIDAcrossLoads(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	st1, err := projstate.LoadOrInit(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := st1.Save(); err != nil {
		t.Fatal(err)
	}

	st2, err := projstate.LoadOrInit(root)
	if err != nil {
		t.Fatal(err)
	}
	if st2.ProjectID != st1.ProjectID {
		t.Errorf("ProjectID changed across loads: %q vs %q", st2.ProjectID, st1.ProjectID)
	}
}

func TestLoadOrInit_DeletedStateRegeneratesFreshID(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	st1, err := projstate.LoadOrInit(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := st1.Save(); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, ".gskill")); err != nil {
		t.Fatal(err)
	}

	st2, err := projstate.LoadOrInit(root)
	if err != nil {
		t.Fatal(err)
	}
	if st2.ProjectID == st1.ProjectID {
		t.Error("deleted state regenerated the same ID; want a fresh one")
	}
	if len(st2.Skills) != 0 {
		t.Errorf("fresh state has skills: %+v", st2.Skills)
	}
}

func TestState_SkillRoundTripDeterministic(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	st, err := projstate.LoadOrInit(root)
	if err != nil {
		t.Fatal(err)
	}
	st.SetSkill("zeta", projstate.SkillState{
		StoreHash:    "sha256:bbb",
		StoreScope:   "global",
		ActiveTarget: ".agents/skills/zeta",
		ActiveMode:   "symlink",
		Agents: map[string]projstate.AgentState{
			"codex":  {Target: ".codex/skills/zeta", Mode: "symlink"},
			"claude": {Target: ".claude/skills/zeta", Mode: "symlink"},
		},
	})
	st.SetSkill("alpha", projstate.SkillState{
		StoreHash:    "sha256:aaa",
		StoreScope:   "global",
		ActiveTarget: ".agents/skills/alpha",
		ActiveMode:   "copy",
	})
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(projstate.Path(root))
	if err != nil {
		t.Fatal(err)
	}
	// Re-save: byte identical (deterministic serialization).
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(projstate.Path(root))
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Error("state serialization is not deterministic")
	}

	loaded, err := projstate.LoadOrInit(root)
	if err != nil {
		t.Fatal(err)
	}
	sk, ok := loaded.Skill("zeta")
	if !ok {
		t.Fatal("skill zeta lost in round trip")
	}
	if sk.StoreHash != "sha256:bbb" || sk.Agents["claude"].Target != ".claude/skills/zeta" {
		t.Errorf("round trip = %+v", sk)
	}
}

func TestState_RemoveSkill(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	st, err := projstate.LoadOrInit(root)
	if err != nil {
		t.Fatal(err)
	}
	st.SetSkill("gone", projstate.SkillState{StoreHash: "sha256:x"})
	st.RemoveSkill("gone")
	if _, ok := st.Skill("gone"); ok {
		t.Error("RemoveSkill left the entry")
	}
}

func TestLoadOrInit_CorruptStateFails(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".gskill"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projstate.Path(root), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := projstate.LoadOrInit(root); err == nil {
		t.Error("LoadOrInit accepted corrupt state.json")
	}
}
