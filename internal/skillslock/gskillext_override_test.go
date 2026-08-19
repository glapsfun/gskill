package skillslock_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/skillslock"
)

// spec022Lock is a lockfile as spec 022 wrote it: no baseHash, no
// overrideDigest, no override block. It also carries a foreign entry owned by
// another spec-012 tool, which gskill must never touch.
const spec022Lock = `{
  "version": 1,
  "skills": {
    "code-review": {
      "source": "github:org/skills",
      "sourceType": "git",
      "skillPath": "code-review",
      "computedHash": "sha256:aaaa",
      "gskill": {
        "sourceUrl": "https://github.com/org/skills",
        "commit": "abc123",
        "contentHash": "sha256:bbbb"
      }
    },
    "other-tool-skill": {
      "source": "github:someone/else",
      "sourceType": "git",
      "skillPath": "other-tool-skill",
      "computedHash": "sha256:cccc",
      "otherTool": {
        "itsOwnField": "must survive untouched"
      }
    }
  }
}
`

// TestExt_OverrideFieldsRoundTrip pins A2's serialization half: the three new
// fields survive a marshal/unmarshal cycle with their values and, for the
// declaration, its declared list order.
func TestExt_OverrideFieldsRoundTrip(t *testing.T) {
	t.Parallel()

	l, err := skillslock.Unmarshal([]byte(spec022Lock))
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	e, ok := l.Entry("code-review")
	if !ok || e.Ext == nil {
		t.Fatal("code-review entry missing its gskill extension")
	}

	e.Ext.BaseHash = "sha256:1111"
	e.Ext.ContentHash = "sha256:2222"
	e.Ext.OverrideDigest = "sha256:3333"
	e.Ext.Override = &skillslock.OverrideDecl{
		Replace: map[string]string{"SKILL.md": "gskill/cr/SKILL.md"},
		Patch:   []string{"gskill/cr/01.diff", "gskill/cr/02.diff"},
		Append:  []string{"gskill/cr/rules.md"},
	}
	if err := l.SetExt("code-review", e.Ext); err != nil {
		t.Fatalf("SetExt: %v", err)
	}

	data, err := skillslock.Marshal(l)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	round, err := skillslock.Unmarshal(data)
	if err != nil {
		t.Fatalf("re-Unmarshal: %v", err)
	}
	got, _ := round.Entry("code-review")
	assertOverrideRoundTrip(t, got.Ext)
}

func assertOverrideRoundTrip(t *testing.T, ext *skillslock.Ext) {
	t.Helper()
	if ext.BaseHash != "sha256:1111" || ext.OverrideDigest != "sha256:3333" {
		t.Errorf("hashes lost: %+v", ext)
	}
	if ext.Override == nil {
		t.Fatal("override declaration lost")
	}
	if p := ext.Override.Patch; len(p) != 2 || p[0] != "gskill/cr/01.diff" || p[1] != "gskill/cr/02.diff" {
		t.Errorf("declared patch order not preserved: %v", p)
	}
	if ext.Override.Replace["SKILL.md"] != "gskill/cr/SKILL.md" {
		t.Errorf("replace lost: %v", ext.Override.Replace)
	}
}

// TestExt_OmittedWhenNoOverride is assertion A1: an entry with no override
// carries no override keys at all, so spec 022 entries stay valid and their
// serialization does not grow.
func TestExt_OmittedWhenNoOverride(t *testing.T) {
	t.Parallel()

	l, err := skillslock.Unmarshal([]byte(spec022Lock))
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	e, _ := l.Entry("code-review")
	if e.Ext.OverrideDigest != "" {
		t.Errorf("OverrideDigest = %q, want empty for a non-overridden entry", e.Ext.OverrideDigest)
	}
	if e.Ext.Override != nil {
		t.Errorf("Override = %+v, want nil", e.Ext.Override)
	}

	if err := l.SetExt("code-review", e.Ext); err != nil {
		t.Fatalf("SetExt: %v", err)
	}
	data, err := skillslock.Marshal(l)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, key := range []string{"overrideDigest", "\"override\"", "baseHash"} {
		if strings.Contains(string(data), key) {
			t.Errorf("empty override still serialized %s:\n%s", key, data)
		}
	}
}

// TestLock_Spec022ReserializesByteIdentical is assertion A4: reading a spec 022
// lockfile and writing it back without touching anything must be a no-op, which
// is what keeps rewrites minimal-diff (Constitution I).
func TestLock_Spec022ReserializesByteIdentical(t *testing.T) {
	t.Parallel()

	l, err := skillslock.Unmarshal([]byte(spec022Lock))
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	data, err := skillslock.Marshal(l)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(data) != spec022Lock {
		t.Errorf("untouched lock did not re-serialize byte-identically\n--- got ---\n%s\n--- want ---\n%s", data, spec022Lock)
	}
}

// TestLock_ForeignEntryPreserved is assertion A5: a spec-012 entry owned by
// another tool survives a gskill write with its own namespaced block intact.
func TestLock_ForeignEntryPreserved(t *testing.T) {
	t.Parallel()

	l, err := skillslock.Unmarshal([]byte(spec022Lock))
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	e, _ := l.Entry("code-review")
	e.Ext.OverrideDigest = "sha256:3333"
	if err := l.SetExt("code-review", e.Ext); err != nil {
		t.Fatalf("SetExt: %v", err)
	}
	data, err := skillslock.Marshal(l)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var doc struct {
		Skills map[string]json.RawMessage `json:"skills"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	foreign := string(doc.Skills["other-tool-skill"])
	if !strings.Contains(foreign, "must survive untouched") {
		t.Errorf("foreign entry damaged: %s", foreign)
	}
	if strings.Contains(foreign, "gskill") {
		t.Errorf("gskill wrote into a foreign entry: %s", foreign)
	}
}
