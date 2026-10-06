package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestDoctorJSON_NotesAlwaysPresent(t *testing.T) {
	t.Parallel()
	stdout, stderr, code := runCLI(t, newTestApp(), "-C", initedProject(t), "--json", "doctor")
	if code != 0 {
		t.Fatalf("doctor = %d: %s", code, stderr)
	}
	var res map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stdout), &res); err != nil {
		t.Fatalf("parse: %v\n%s", err, stdout)
	}
	if got := string(res["notes"]); got != "[]" {
		t.Errorf("notes = %s, want []", got)
	}
}

func TestDoctor_PrintsNotesWithoutCountingThemAsWarnings(t *testing.T) {
	t.Parallel()
	dir := initedProject(t)
	src := filepath.Join(t.TempDir(), "demo")
	if err := os.MkdirAll(src, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("---\nname: demo\ndescription: d\n---\n# x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, stderr, code := runCLI(t, newTestApp(), "-C", dir, "add", src, "--agent", "hermes"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}

	stdout, stderr, code := runCLI(t, newTestApp(), "-C", dir, "doctor")
	if code != 0 {
		t.Fatalf("doctor = %d: %s", code, stderr)
	}
	if !strings.Contains(stderr, "note: hermes: Hermes loads project skills only after you trust this project") {
		t.Errorf("doctor did not print the Hermes note:\n%s", stderr)
	}
	if !regexp.MustCompile(`warnings:\s+0`).MatchString(stdout) {
		t.Errorf("notes were counted as warnings:\n%s", stdout)
	}
}
