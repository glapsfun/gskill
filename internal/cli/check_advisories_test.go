package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func checkAdvisories(t *testing.T, dir string) []string {
	t.Helper()
	stdout, stderr, code := runCLI(t, newTestApp(), "-C", dir, "--json", "project", "check")
	if code != 0 {
		t.Fatalf("check = %d, want 0: %s", code, stderr)
	}
	var res map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stdout), &res); err != nil {
		t.Fatalf("parse: %v\n%s", err, stdout)
	}
	raw, ok := res["advisories"]
	if !ok {
		t.Fatalf("advisories key missing:\n%s", stdout)
	}
	var adv []string
	if string(raw) == "null" {
		t.Fatalf("advisories is null, want an array:\n%s", stdout)
	}
	if err := json.Unmarshal(raw, &adv); err != nil {
		t.Fatalf("advisories: %v", err)
	}
	return adv
}

func TestCheckJSON_AdvisoriesAlwaysPresent(t *testing.T) {
	t.Parallel()
	if adv := checkAdvisories(t, initedProject(t)); len(adv) != 0 {
		t.Errorf("advisories = %v, want []", adv)
	}
}

func TestCheckJSON_ListsOpenCodeAdvisory(t *testing.T) {
	t.Parallel()
	dir := initedProject(t)
	src := t.TempDir()
	skill := filepath.Join(src, "skills", "demo")
	if err := os.MkdirAll(skill, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("---\nname: other\ndescription: d\n---\n# x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, stderr, code := runCLI(t, newTestApp(), "-C", dir, "add", src, "--all", "--agent", "opencode"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}
	if adv := checkAdvisories(t, dir); len(adv) != 1 {
		t.Errorf("advisories = %v, want the one OpenCode naming advisory", adv)
	}
}
