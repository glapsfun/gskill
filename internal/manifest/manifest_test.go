package manifest_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/manifest"
)

// writeRepo lays out a temp repo containing gskill.toml plus any extra files,
// and returns its root. Extra paths are repo-relative.
func writeRepo(t *testing.T, toml string, extra ...string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, manifest.FileName), []byte(toml), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, rel := range extra {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// TestParse_ValidManifest pins the happy path: every declared field lands where
// data-model.md §1 and §2 say it does, with declared list order preserved.
func TestParse_ValidManifest(t *testing.T) {
	t.Parallel()

	root := writeRepo(t, `
[config]
jobs = 4

[skills.code-review]
source = "github:org/skills"
skill  = "cr"
ref    = "v2.1"
agents = ["claude"]
mode   = "symlink"

[skills.code-review.override]
replace = { "SKILL.md" = "gskill/cr/SKILL.md" }
patch   = ["gskill/cr/01.diff", "gskill/cr/02.diff"]
append  = ["gskill/cr/rules.md"]
`, "gskill/cr/SKILL.md", "gskill/cr/01.diff", "gskill/cr/02.diff", "gskill/cr/rules.md")

	m, err := manifest.Load(filepath.Join(root, manifest.FileName))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := m.Validate(root); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	s, ok := m.Skills["code-review"]
	if !ok {
		t.Fatal("skill code-review missing")
	}
	assertSkillFields(t, s)
	assertOverrideFields(t, s)
	if v, ok := m.Config["jobs"]; !ok || v != int64(4) {
		t.Errorf("config jobs = %v (%T)", v, v)
	}
}

func assertSkillFields(t *testing.T, s manifest.Skill) {
	t.Helper()
	if s.Source != "github:org/skills" || s.Skill != "cr" || s.Ref != "v2.1" || s.Mode != "symlink" {
		t.Errorf("field mismatch: %+v", s)
	}
	if len(s.Agents) != 1 || s.Agents[0] != "claude" {
		t.Errorf("agents = %v", s.Agents)
	}
}

func assertOverrideFields(t *testing.T, s manifest.Skill) {
	t.Helper()
	if s.Override == nil {
		t.Fatal("override missing")
	}
	// Declared list order is data (patches apply in sequence), not iteration order.
	if got := s.Override.Patch; len(got) != 2 || got[0] != "gskill/cr/01.diff" || got[1] != "gskill/cr/02.diff" {
		t.Errorf("patch order not preserved: %v", got)
	}
	if got := s.Override.Replace["SKILL.md"]; got != "gskill/cr/SKILL.md" {
		t.Errorf("replace = %q", got)
	}
}

// TestParse_Validation is the V1–V9 table from contracts/manifest.md. Every
// error case must map to exit code 2 (usage), because the manifest is
// user-authored input.
func TestParse_Validation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		rule    string
		toml    string
		extra   []string
		files   map[string]string
		wantErr bool
		errHas  string
		warnHas string
	}{
		{
			name: "V1 parse failure", rule: "V1",
			toml:    "[skills.a\nsource = \"x\"\n",
			wantErr: true, errHas: "parse",
		},
		{
			name: "V2 unknown key in skill table", rule: "V2",
			toml:    "[skills.a]\nsource = \"github:o/r\"\nrefff = \"v1\"\n",
			wantErr: true, errHas: "refff",
		},
		{
			name: "V2 unknown key in override table", rule: "V2",
			toml:    "[skills.a]\nsource = \"github:o/r\"\n[skills.a.override]\napend = [\"x.md\"]\n",
			wantErr: true, errHas: "apend",
		},
		{
			name: "V3 unknown config key warns only", rule: "V3",
			toml:    "[config]\nnot_a_real_key = 1\n",
			wantErr: false, warnHas: "not_a_real_key",
		},
		{
			name: "V4 missing source", rule: "V4",
			toml:    "[skills.a]\nref = \"v1\"\n",
			wantErr: true, errHas: "source",
		},
		{
			name: "V5 escaping path", rule: "V5",
			toml:    "[skills.a]\nsource = \"github:o/r\"\n[skills.a.override]\nappend = [\"../outside.md\"]\n",
			wantErr: true, errHas: "outside",
		},
		{
			name: "V5 absolute path", rule: "V5",
			toml:    "[skills.a]\nsource = \"github:o/r\"\n[skills.a.override]\nappend = [\"/etc/passwd\"]\n",
			wantErr: true, errHas: "passwd",
		},
		{
			name: "V6 missing input file", rule: "V6",
			toml:    "[skills.a]\nsource = \"github:o/r\"\n[skills.a.override]\nappend = [\"gskill/nope.md\"]\n",
			wantErr: true, errHas: "nope.md",
		},
		{
			name: "V7 replace and patch collide", rule: "V7",
			toml: "[skills.a]\nsource = \"github:o/r\"\n[skills.a.override]\n" +
				"replace = { \"SKILL.md\" = \"gskill/s.md\" }\npatch = [\"gskill/p.diff\"]\n",
			extra: []string{"gskill/s.md"},
			files: map[string]string{
				// A real diff header: V7 collides only when the patch truly
				// targets the replaced file, which requires reading the diff.
				"gskill/p.diff": "--- a/SKILL.md\n+++ b/SKILL.md\n@@ -1 +1 @@\n-a\n+b\n",
			},
			wantErr: true, errHas: "SKILL.md",
		},
		{
			name: "V8 bad mode", rule: "V8",
			toml:    "[skills.a]\nsource = \"github:o/r\"\nmode = \"hardlink\"\n",
			wantErr: true, errHas: "hardlink",
		},
		{
			name: "V9 ref and commit both set warns, commit wins", rule: "V9",
			toml:    "[skills.a]\nsource = \"github:o/r\"\nref = \"v1\"\ncommit = \"abc123\"\n",
			wantErr: false, warnHas: "ref",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := writeRepo(t, tt.toml, tt.extra...)
			for rel, content := range tt.files {
				p := filepath.Join(root, filepath.FromSlash(rel))
				if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			m, err := manifest.Load(filepath.Join(root, manifest.FileName))
			if err == nil && m != nil {
				err = m.Validate(root)
			}

			if tt.wantErr {
				assertUsageError(t, tt.rule, tt.errHas, err)
				return
			}
			if err != nil {
				t.Fatalf("%s: unexpected error: %v", tt.rule, err)
			}
			if tt.warnHas != "" {
				joined := strings.Join(m.Warnings, "; ")
				if !strings.Contains(joined, tt.warnHas) {
					t.Errorf("%s: warnings %q must mention %q", tt.rule, joined, tt.warnHas)
				}
			}
		})
	}
}

// assertUsageError pins that every manifest validation failure carries exit
// code 2 and names the offending value.
func assertUsageError(t *testing.T, rule, want string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: want error, got nil", rule)
	}
	if !errors.Is(err, errs.ErrUsage) {
		t.Errorf("%s: want exit code 2 (ErrUsage), got %v", rule, err)
	}
	if want != "" && !strings.Contains(err.Error(), want) {
		t.Errorf("%s: error %q must name %q", rule, err, want)
	}
}

// TestParse_CommitBeatsRef pins the V9 resolution rule, not just its warning.
func TestParse_CommitBeatsRef(t *testing.T) {
	t.Parallel()

	root := writeRepo(t, "[skills.a]\nsource = \"github:o/r\"\nref = \"v1\"\ncommit = \"abc123\"\n")
	m, err := manifest.Load(filepath.Join(root, manifest.FileName))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := m.Skills["a"].Pin(); got != "abc123" {
		t.Errorf("Pin() = %q, want the commit to win over ref", got)
	}
}

// TestLoad_Absent reports a missing manifest as absent, never as an error:
// a pre-023 project has none until a mutating command generates one (FR-015).
func TestLoad_Absent(t *testing.T) {
	t.Parallel()

	m, err := manifest.Load(filepath.Join(t.TempDir(), manifest.FileName))
	if err != nil {
		t.Fatalf("absent manifest must not error: %v", err)
	}
	if m != nil {
		t.Errorf("absent manifest must yield nil, got %+v", m)
	}
}
