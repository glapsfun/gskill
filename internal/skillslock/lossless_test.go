package skillslock_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/skillslock"
)

// foreignInput is already in canonical form (2-space indent, trailing newline)
// and carries data gskill does not understand: an unknown top-level field, an
// unknown entry field, and another tool's extension block. A parse+marshal
// round trip must reproduce it byte-for-byte.
func foreignInput(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "foreign.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return data
}

func mustUnmarshal(t *testing.T, data []byte) *skillslock.Lock {
	t.Helper()
	l, err := skillslock.Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	return l
}

func mustMarshal(t *testing.T, l *skillslock.Lock) []byte {
	t.Helper()
	out, err := skillslock.Marshal(l)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	return out
}

func TestRoundTripByteIdentical(t *testing.T) {
	t.Parallel()
	in := foreignInput(t)
	out := mustMarshal(t, mustUnmarshal(t, in))
	if !bytes.Equal(in, out) {
		t.Errorf("round trip not byte-identical:\n--- in ---\n%s\n--- out ---\n%s", in, out)
	}
}

func TestSetEntryTouchesOnlyThatEntry(t *testing.T) {
	t.Parallel()
	in := foreignInput(t)
	l := mustUnmarshal(t, in)

	e, ok := l.Entry("deploy-to-vercel")
	if !ok {
		t.Fatal("missing fixture entry deploy-to-vercel")
	}
	e.ComputedHash = strings.Repeat("ab", 32)
	l.SetEntry("deploy-to-vercel", e)
	out := mustMarshal(t, l)

	// Foreign data survives verbatim.
	for _, want := range []string{
		`"customTopLevel": "keep-me"`,
		`"otherTool": {`,
		`"flavor": "grape"`,
		`"entryUnknown": 42`,
	} {
		if !bytes.Contains(out, []byte(want)) {
			t.Errorf("output lost foreign data %q:\n%s", want, out)
		}
	}
	// The untouched entry's lines are unchanged.
	if !bytes.Contains(out, []byte(`"eb16b20dcbe6ce51e0372083d624e0847af1b09487ec99249227d5a8eddebfc0"`)) {
		t.Errorf("untouched entry changed:\n%s", out)
	}
	// The updated hash is present, the old one gone.
	if !bytes.Contains(out, []byte(strings.Repeat("ab", 32))) {
		t.Errorf("updated hash missing:\n%s", out)
	}
	if bytes.Contains(out, []byte("03e0eaaa9bf13ba1e7ffa387f5893de6f324c0868c627001f179395a8feaa7c9")) {
		t.Errorf("stale hash still present:\n%s", out)
	}
}

func TestAddEntriesAppendSortedAtEnd(t *testing.T) {
	t.Parallel()
	l := mustUnmarshal(t, foreignInput(t))
	add := skillslock.Entry{
		Source:       "acme/skills",
		SourceType:   "github",
		SkillPath:    "skills/x/SKILL.md",
		ComputedHash: strings.Repeat("cd", 32),
	}
	l.SetEntry("zeta-skill", add)
	l.SetEntry("alpha-skill", add)

	names := l.Names()
	n := len(names)
	if n < 4 {
		t.Fatalf("Names() = %v, want originals + 2", names)
	}
	// Original order untouched, new names sorted among themselves at the end.
	if names[0] != "deploy-to-vercel" || names[1] != "vercel-cli-with-tokens" {
		t.Errorf("original order disturbed: %v", names)
	}
	if names[n-2] != "alpha-skill" || names[n-1] != "zeta-skill" {
		t.Errorf("new entries not appended sorted: %v", names)
	}
}

func TestRemoveEntryRemovesOnlyThatEntry(t *testing.T) {
	t.Parallel()
	in := foreignInput(t)
	l := mustUnmarshal(t, in)
	if !l.Remove("deploy-to-vercel") {
		t.Fatal("Remove returned false")
	}
	if l.Remove("deploy-to-vercel") {
		t.Error("second Remove should return false")
	}
	out := mustMarshal(t, l)
	if bytes.Contains(out, []byte("deploy-to-vercel")) {
		t.Errorf("removed entry still present:\n%s", out)
	}
	// The removed entry's own fields (including its foreign ones) go with it;
	// everything outside the entry survives.
	if bytes.Contains(out, []byte("entryUnknown")) {
		t.Errorf("removed entry's fields should be gone:\n%s", out)
	}
	for _, want := range []string{
		`"customTopLevel": "keep-me"`,
		"vercel-cli-with-tokens",
		`"trailingUnknown"`,
	} {
		if !bytes.Contains(out, []byte(want)) {
			t.Errorf("removal lost unrelated data %q:\n%s", want, out)
		}
	}
}

func TestMarshalFormat(t *testing.T) {
	t.Parallel()
	l := skillslock.New()
	l.SetEntry("amp-skill", skillslock.Entry{
		Source:       "acme/a&b",
		SourceType:   "github",
		SkillPath:    "skills/amp/SKILL.md",
		ComputedHash: strings.Repeat("ef", 32),
	})
	out := mustMarshal(t, l)

	if !bytes.HasSuffix(out, []byte("}\n")) {
		t.Errorf("output must end with trailing newline:\n%q", out)
	}
	if bytes.Contains(out, []byte("\\u0026")) {
		t.Errorf("HTML escaping must be disabled:\n%s", out)
	}
	if !bytes.Contains(out, []byte("a&b")) {
		t.Errorf("raw ampersand expected (no HTML escaping):\n%s", out)
	}
	if !bytes.Contains(out, []byte("\n  \"skills\"")) {
		t.Errorf("2-space indent expected:\n%s", out)
	}
	// Deterministic: marshaling twice is identical.
	if !bytes.Equal(out, mustMarshal(t, l)) {
		t.Error("Marshal is not deterministic")
	}
}
