package manifest_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/manifest"
)

const setKeyFixture = `# Project skills.

[config]
jobs = 4

# code-review does the reviews
[skills.code-review]
source  = "github:org/skills"   # the upstream
version = "^1.0.0" # tracked range
# keep me: a comment inside the block
agents  = ["claude"]

[skills.code-review.override]
append = ["gskill/cr/rules.md"]

[skills.docs-writer]
source = "github:org/docs"
ref = "v1"
`

func writeFixture(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), manifest.FileName)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func diffLines(a, b string) int {
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	if len(al) != len(bl) {
		return -1
	}
	n := 0
	for i := range al {
		if al[i] != bl[i] {
			n++
		}
	}
	return n
}

func TestSetKey_ReplacesValueInPlace(t *testing.T) {
	t.Parallel()
	path := writeFixture(t, setKeyFixture)
	if err := manifest.SetKey(path, "code-review", "version", "^2.0.0"); err != nil {
		t.Fatalf("SetKey: %v", err)
	}
	got, _ := os.ReadFile(path) //nolint:gosec // test path
	if n := diffLines(setKeyFixture, string(got)); n != 1 {
		t.Fatalf("want exactly one changed line, got %d:\n%s", n, got)
	}
	if !strings.Contains(string(got), `version = "^2.0.0" # tracked range`) {
		t.Errorf("alignment or inline comment lost:\n%s", got)
	}
	for _, keep := range []string{"# keep me: a comment inside the block", `source  = "github:org/skills"   # the upstream`, "[skills.docs-writer]", `ref = "v1"`, "jobs = 4"} {
		if !strings.Contains(string(got), keep) {
			t.Errorf("lost %q:\n%s", keep, got)
		}
	}
	m, err := manifest.Load(path)
	if err != nil || m.Skills["code-review"].Version != "^2.0.0" {
		t.Fatalf("reparsed version = %q, err %v", m.Skills["code-review"].Version, err)
	}
}

func TestSetKey_InsertsMissingKeyAfterSource(t *testing.T) {
	t.Parallel()
	path := writeFixture(t, setKeyFixture)
	if err := manifest.SetKey(path, "docs-writer", "commit", "abc123"); err != nil {
		t.Fatalf("SetKey: %v", err)
	}
	got, _ := os.ReadFile(path) //nolint:gosec // test path
	want := "[skills.docs-writer]\nsource = \"github:org/docs\"\ncommit = \"abc123\"\nref = \"v1\"\n"
	if !strings.Contains(string(got), want) {
		t.Errorf("key not inserted after source:\n%s", got)
	}
	if !strings.Contains(string(got), `version = "^1.0.0" # tracked range`) {
		t.Errorf("other block touched:\n%s", got)
	}
}

func TestSetKey_MissingBlockIsAnError(t *testing.T) {
	t.Parallel()
	path := writeFixture(t, setKeyFixture)
	err := manifest.SetKey(path, "nope", "version", "1.0.0")
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("err = %v, want an error naming the skill", err)
	}
}

func TestSetKey_QuotesSpecialCharacters(t *testing.T) {
	t.Parallel()
	path := writeFixture(t, setKeyFixture)
	if err := manifest.SetKey(path, "docs-writer", "ref", `weird"tag`); err != nil {
		t.Fatalf("SetKey: %v", err)
	}
	m, err := manifest.Load(path)
	if err != nil || m.Skills["docs-writer"].Ref != `weird"tag` {
		t.Fatalf("reparsed ref = %q, err %v", m.Skills["docs-writer"].Ref, err)
	}
}
