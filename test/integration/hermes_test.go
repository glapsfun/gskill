package integration_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/app"
)

const hermesNote = "hermes: Hermes loads project skills only after you trust this project; run 'hermes skills trust'"

// hermesProject returns an initialized project with a .hermes marker, so
// hermes is detected alongside claude.
func hermesProject(t *testing.T) string {
	t.Helper()
	proj := newProject(t)
	if err := os.MkdirAll(filepath.Join(proj, ".hermes"), 0o750); err != nil {
		t.Fatal(err)
	}
	initProject(t, proj)
	return proj
}

func doctorNotes(t *testing.T, proj string) []string {
	t.Helper()
	stdout, stderr, code := runGskill(t, proj, "--json", "doctor")
	if code != 0 {
		t.Fatalf("doctor = %d: %s", code, stderr)
	}
	var res struct {
		Notes []string `json:"notes"`
	}
	if err := json.Unmarshal([]byte(stdout), &res); err != nil {
		t.Fatalf("parse doctor --json: %v\n%s", err, stdout)
	}
	return res.Notes
}

func TestHermes_TrustNoteOncePerCommand(t *testing.T) {
	t.Parallel()
	proj := hermesProject(t)

	for _, name := range []string{"alpha", "beta"} {
		_, stderr, code := runGskill(t, proj, "add", localSkillDir(t, name))
		if code != 0 {
			t.Fatalf("add %s: %s", name, stderr)
		}
		if n := strings.Count(stderr, hermesNote); n != 1 {
			t.Errorf("add %s printed the trust note %d times, want once:\n%s", name, n, stderr)
		}
	}

	for _, name := range []string{"alpha", "beta"} {
		if err := os.RemoveAll(filepath.Join(proj, ".agents", "skills", name)); err != nil {
			t.Fatal(err)
		}
	}
	_, stderr, code := runGskill(t, proj, "install")
	if code != 0 {
		t.Fatalf("install: %s", stderr)
	}
	if n := strings.Count(stderr, hermesNote); n != 1 {
		t.Errorf("install restoring two skills printed the trust note %d times, want once:\n%s", n, stderr)
	}

	if notes := doctorNotes(t, proj); len(notes) != 1 || notes[0] != hermesNote {
		t.Errorf("doctor notes = %v, want the trust note", notes)
	}
}

func TestHermes_AgentOnlyAddPrintsTheNote(t *testing.T) {
	t.Parallel()
	proj := hermesProject(t)
	src := localSkillDir(t, "gamma")

	if _, stderr, code := runGskill(t, proj, "add", src, "--agent", "claude"); code != 0 {
		t.Fatalf("add --agent claude: %s", stderr)
	}
	_, stderr, code := runGskill(t, proj, "add", src, "--agent", "hermes")
	if code != 0 {
		t.Fatalf("add --agent hermes: %s", stderr)
	}
	if n := strings.Count(stderr, hermesNote); n != 1 {
		t.Errorf("agent-only add printed the trust note %d times, want once:\n%s", n, stderr)
	}
}

func TestHermes_NoNoteWithoutProjectTarget(t *testing.T) {
	t.Parallel()
	proj := newProject(t)
	initProject(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", localSkillDir(t, "demo"), "--agent", "claude"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}
	if notes := doctorNotes(t, proj); len(notes) != 0 {
		t.Errorf("doctor notes = %v, want none", notes)
	}
}

// Not parallel: sets HOME to redirect the user-global location.
func TestHermes_GlobalInstallHasNoTrustNote(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	proj := newProject(t)
	initProject(t, proj)
	_, stderr, code := runGskill(t, proj, "add", localSkillDir(t, "demo"), "--global", "--agent", "hermes")
	if code != 0 {
		t.Fatalf("add --global --agent hermes: %s", stderr)
	}
	if strings.Contains(stderr, hermesNote) {
		t.Errorf("global install printed the project trust note:\n%s", stderr)
	}
	if _, err := os.Stat(filepath.Join(home, ".hermes", "skills", "demo", "SKILL.md")); err != nil {
		t.Errorf("global copy missing: %v", err)
	}
}

// installJSON runs `install --json` and returns each skill's status plus the
// run's warnings.
func installJSON(t *testing.T, proj string) (map[string]string, []string) {
	t.Helper()
	stdout, stderr, code := runGskill(t, proj, "--json", "install")
	if code != 0 {
		t.Fatalf("install = %d: %s", code, stderr)
	}
	var res struct {
		Skills []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"skills"`
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(stdout), &res); err != nil {
		t.Fatalf("parse install --json: %v\n%s", err, stdout)
	}
	statuses := make(map[string]string, len(res.Skills))
	for _, s := range res.Skills {
		statuses[s.Name] = s.Status
	}
	return statuses, res.Warnings
}

func countLine(lines []string, want string) int {
	n := 0
	for _, l := range lines {
		if l == want {
			n++
		}
	}
	return n
}

// TestHermes_NoteOnUpToDateInstall covers the clone-and-go case: the content
// and links are committed, so install finds the skill up to date, and the
// teammate still has to learn about the trust step (spec 027 FR-011, FR-012).
func TestHermes_NoteOnUpToDateInstall(t *testing.T) {
	t.Parallel()
	proj := hermesProject(t)
	if err := os.MkdirAll(filepath.Join(proj, ".opencode"), 0o750); err != nil {
		t.Fatal(err)
	}
	if _, stderr, code := runGskill(t, proj, "add", mismatchedSkillTree(t, "demo", "other"), "--all"); code != 0 {
		t.Fatalf("add: %s", stderr)
	}

	statuses, warnings := installJSON(t, proj)
	if statuses["demo"] != app.LockSkillUpToDate {
		t.Fatalf("demo status = %q, want %s (the up-to-date path is under test)", statuses["demo"], app.LockSkillUpToDate)
	}
	const openCodeAdvice = `opencode: skill "demo" declares name "other"; OpenCode requires it to match the directory`
	if n := countLine(warnings, hermesNote); n != 1 {
		t.Errorf("trust note appears %d times in %v, want once", n, warnings)
	}
	if n := countLine(warnings, openCodeAdvice); n != 1 {
		t.Errorf("OpenCode advice appears %d times in %v, want once", n, warnings)
	}

	_, stderr, code := runGskill(t, proj, "--dry-run", "install")
	if code != 0 {
		t.Fatalf("install --dry-run = %d: %s", code, stderr)
	}
	if strings.Contains(stderr, "hermes:") || strings.Contains(stderr, "opencode:") {
		t.Errorf("dry run printed advice:\n%s", stderr)
	}
}

// TestHermes_NoteOnPartialRepair covers a repair that relinks only another
// agent: the advice comes from every locked target, not just the repaired one.
func TestHermes_NoteOnPartialRepair(t *testing.T) {
	t.Parallel()
	proj := hermesProject(t)
	if _, stderr, code := runGskill(t, proj, "add", localSkillDir(t, "alpha")); code != 0 {
		t.Fatalf("add: %s", stderr)
	}
	link := filepath.Join(proj, ".claude", "skills", "alpha")
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}

	statuses, warnings := installJSON(t, proj)
	if statuses["alpha"] != app.LockSkillRepaired {
		t.Fatalf("alpha status = %q, want %s (the partial-repair path is under test)", statuses["alpha"], app.LockSkillRepaired)
	}
	if _, err := os.Lstat(link); err != nil {
		t.Errorf("claude link not repaired: %v", err)
	}
	if n := countLine(warnings, hermesNote); n != 1 {
		t.Errorf("trust note appears %d times in %v, want once", n, warnings)
	}
}
