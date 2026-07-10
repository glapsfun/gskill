package skillslock_test

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/glapsfun/gskill/internal/skillslock"
)

func fullExt() *skillslock.Ext {
	return &skillslock.Ext{
		SourceURL:     "https://github.com/vercel-labs/agent-skills.git",
		Ref:           "main",
		Commit:        "abc123def456",
		Version:       "1.2.0",
		Agents:        []string{"claude", "codex"},
		InstallMode:   "symlink",
		Scope:         "project",
		StoreHash:     "sha256:1111111111111111111111111111111111111111111111111111111111111111",
		SkillFileHash: "sha256:2222222222222222222222222222222222222222222222222222222222222222",
		InstalledAt:   "2026-07-10T12:00:00Z",
		UpdatedAt:     "2026-07-10T12:30:00Z",
	}
}

func TestExtRoundTripAllFields(t *testing.T) {
	t.Parallel()
	l := mustUnmarshal(t, foreignInput(t))
	want := fullExt()
	if err := l.SetExt("deploy-to-vercel", want); err != nil {
		t.Fatalf("SetExt: %v", err)
	}

	// Round trip through bytes.
	l2 := mustUnmarshal(t, mustMarshal(t, l))
	e, ok := l2.Entry("deploy-to-vercel")
	if !ok {
		t.Fatal("entry lost")
	}
	if e.Ext == nil {
		t.Fatal("Ext lost in round trip")
	}
	if !reflect.DeepEqual(e.Ext, want) {
		t.Errorf("Ext = %+v, want %+v", e.Ext, want)
	}
}

func TestExtAbsentMeansNotInstalled(t *testing.T) {
	t.Parallel()
	l := mustUnmarshal(t, foreignInput(t))
	e, ok := l.Entry("vercel-cli-with-tokens")
	if !ok {
		t.Fatal("entry missing")
	}
	if e.Ext != nil {
		t.Errorf("Ext = %+v, want nil for external-only entry", e.Ext)
	}
}

func TestSetExtPreservesCoreAndForeignFields(t *testing.T) {
	t.Parallel()
	in := foreignInput(t)
	l := mustUnmarshal(t, in)
	if err := l.SetExt("deploy-to-vercel", fullExt()); err != nil {
		t.Fatalf("SetExt: %v", err)
	}
	out := mustMarshal(t, l)

	for _, want := range []string{
		`"source": "vercel-labs/agent-skills"`,
		`"computedHash": "03e0eaaa9bf13ba1e7ffa387f5893de6f324c0868c627001f179395a8feaa7c9"`,
		`"otherTool": {`,
		`"entryUnknown": 42`,
		`"customTopLevel": "keep-me"`,
		`"gskill": {`,
		`"agents": [`,
	} {
		if !bytes.Contains(out, []byte(want)) {
			t.Errorf("output missing %q after SetExt:\n%s", want, out)
		}
	}
}

func TestSetExtUpdateInPlace(t *testing.T) {
	t.Parallel()
	l := mustUnmarshal(t, foreignInput(t))
	if err := l.SetExt("deploy-to-vercel", fullExt()); err != nil {
		t.Fatalf("SetExt: %v", err)
	}
	upd := fullExt()
	upd.Agents = []string{"claude", "codex", "cursor"}
	upd.UpdatedAt = "2026-07-11T00:00:00Z"
	if err := l.SetExt("deploy-to-vercel", upd); err != nil {
		t.Fatalf("SetExt update: %v", err)
	}
	e, _ := l.Entry("deploy-to-vercel")
	if !reflect.DeepEqual(e.Ext, upd) {
		t.Errorf("Ext = %+v, want updated %+v", e.Ext, upd)
	}
	out := mustMarshal(t, l)
	if bytes.Contains(out, []byte("2026-07-10T12:30:00Z")) {
		t.Errorf("stale updatedAt still present:\n%s", out)
	}
}

func TestSetExtUnknownEntryFails(t *testing.T) {
	t.Parallel()
	l := mustUnmarshal(t, foreignInput(t))
	if err := l.SetExt("no-such-skill", fullExt()); err == nil {
		t.Fatal("SetExt on unknown entry should fail")
	}
}
