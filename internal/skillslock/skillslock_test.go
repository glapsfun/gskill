package skillslock_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/skillslock"
)

// minimalV1 is the spec's compatibility example (specs/012-skills-lock-compat).
const minimalV1 = `{
  "version": 1,
  "skills": {
    "deploy-to-vercel": {
      "source": "vercel-labs/agent-skills",
      "sourceType": "github",
      "skillPath": "skills/deploy-to-vercel/SKILL.md",
      "computedHash": "03e0eaaa9bf13ba1e7ffa387f5893de6f324c0868c627001f179395a8feaa7c9"
    },
    "vercel-cli-with-tokens": {
      "source": "vercel-labs/agent-skills",
      "sourceType": "github",
      "skillPath": "skills/vercel-cli-with-tokens/SKILL.md",
      "computedHash": "eb16b20dcbe6ce51e0372083d624e0847af1b09487ec99249227d5a8eddebfc0"
    },
    "web-design-guidelines": {
      "source": "vercel-labs/agent-skills",
      "sourceType": "github",
      "skillPath": "skills/web-design-guidelines/SKILL.md",
      "computedHash": "f3bc47f890f42a44db1007ab390709ec368e4b8c089baee6b0007182236ac474"
    }
  }
}
`

func TestUnmarshalMinimalV1(t *testing.T) {
	t.Parallel()
	l, err := skillslock.Unmarshal([]byte(minimalV1))
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got := l.Version(); got != 1 {
		t.Errorf("Version() = %d, want 1", got)
	}
	names := l.Names()
	want := []string{"deploy-to-vercel", "vercel-cli-with-tokens", "web-design-guidelines"}
	if len(names) != len(want) {
		t.Fatalf("Names() = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("Names()[%d] = %q, want %q (file order preserved)", i, names[i], want[i])
		}
	}
	e, ok := l.Entry("deploy-to-vercel")
	if !ok {
		t.Fatal("Entry(deploy-to-vercel) not found")
	}
	if e.Source != "vercel-labs/agent-skills" {
		t.Errorf("Source = %q", e.Source)
	}
	if e.SourceType != "github" {
		t.Errorf("SourceType = %q", e.SourceType)
	}
	if e.SkillPath != "skills/deploy-to-vercel/SKILL.md" {
		t.Errorf("SkillPath = %q", e.SkillPath)
	}
	if e.ComputedHash != "03e0eaaa9bf13ba1e7ffa387f5893de6f324c0868c627001f179395a8feaa7c9" {
		t.Errorf("ComputedHash = %q", e.ComputedHash)
	}
	if e.Ext != nil {
		t.Errorf("Ext = %+v, want nil (external-only entry)", e.Ext)
	}
	if err := l.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}
}

func TestUnmarshalRejectsInvalidJSON(t *testing.T) {
	t.Parallel()
	_, err := skillslock.Unmarshal([]byte("{\n  \"version\": 1,\n  oops\n}"))
	if !errors.Is(err, skillslock.ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
	if !strings.Contains(err.Error(), "offset") && !strings.Contains(err.Error(), "line") {
		t.Errorf("err %q lacks position info", err)
	}
}

func TestUnmarshalRejectsUnsupportedVersion(t *testing.T) {
	t.Parallel()
	_, err := skillslock.Unmarshal([]byte(`{"version": 2, "skills": {}}`))
	if !errors.Is(err, skillslock.ErrUnsupportedSchema) {
		t.Fatalf("err = %v, want ErrUnsupportedSchema", err)
	}
	if !strings.Contains(err.Error(), "2") {
		t.Errorf("err %q should name the offending version", err)
	}
}

func TestUnmarshalRejectsMissingVersion(t *testing.T) {
	t.Parallel()
	_, err := skillslock.Unmarshal([]byte(`{"skills": {}}`))
	if !errors.Is(err, skillslock.ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
}

func TestUnmarshalRejectsMissingSkills(t *testing.T) {
	t.Parallel()
	_, err := skillslock.Unmarshal([]byte(`{"version": 1}`))
	if !errors.Is(err, skillslock.ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
	if !strings.Contains(err.Error(), "skills") {
		t.Errorf("err %q should name the missing key", err)
	}
}

func entryJSON(source, sourceType, skillPath, hash string) string {
	b := &strings.Builder{}
	b.WriteString("{")
	first := true
	put := func(k, v string) {
		if v == "" {
			return
		}
		if !first {
			b.WriteString(",")
		}
		first = false
		b.WriteString(`"` + k + `": "` + v + `"`)
	}
	put("source", source)
	put("sourceType", sourceType)
	put("skillPath", skillPath)
	put("computedHash", hash)
	b.WriteString("}")
	return b.String()
}

func TestValidateRejectsBadEntries(t *testing.T) {
	t.Parallel()
	const okHash = "03e0eaaa9bf13ba1e7ffa387f5893de6f324c0868c627001f179395a8feaa7c9"
	tests := []struct {
		name    string
		entry   string
		wantSub string
	}{
		{"missing source", entryJSON("", "github", "skills/x/SKILL.md", okHash), "source"},
		{"missing sourceType", entryJSON("o/r", "", "skills/x/SKILL.md", okHash), "sourceType"},
		{"missing skillPath", entryJSON("o/r", "github", "", okHash), "skillPath"},
		{"missing computedHash", entryJSON("o/r", "github", "skills/x/SKILL.md", ""), "computedHash"},
		{"traversal skillPath", entryJSON("o/r", "github", "../../etc/passwd", okHash), "skillPath"},
		{"absolute skillPath", entryJSON("o/r", "github", "/etc/passwd", okHash), "skillPath"},
		{"sneaky traversal", entryJSON("o/r", "github", "skills/../../x/SKILL.md", okHash), "skillPath"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc := `{"version": 1, "skills": {"bad-skill": ` + tt.entry + `}}`
			l, err := skillslock.Unmarshal([]byte(doc))
			if err != nil {
				t.Fatalf("Unmarshal (structural) should pass, got %v", err)
			}
			vErr := l.Validate()
			if !errors.Is(vErr, skillslock.ErrInvalid) {
				t.Fatalf("Validate() = %v, want ErrInvalid", vErr)
			}
			if !strings.Contains(vErr.Error(), "bad-skill") {
				t.Errorf("Validate() %q should name the entry", vErr)
			}
			if !strings.Contains(vErr.Error(), tt.wantSub) {
				t.Errorf("Validate() %q should name field %q", vErr, tt.wantSub)
			}
		})
	}
}

func TestLoadSaveRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := dir + "/" + skillslock.FileName
	l, err := skillslock.Unmarshal([]byte(minimalV1))
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if err := skillslock.Save(path, l); err != nil {
		t.Fatalf("Save: %v", err)
	}
	l2, err := skillslock.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := len(l2.Names()), 3; got != want {
		t.Errorf("entries after round trip = %d, want %d", got, want)
	}
}
