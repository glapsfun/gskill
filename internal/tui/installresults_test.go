package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/app"
)

func failedResult(name, source, version string, cat app.FailureCategory, msg, hint string) app.LockSkillResult {
	return app.LockSkillResult{
		Name: name, Source: source, Status: app.LockSkillFailed,
		SourceType: "github", SkillPath: "skills/" + name,
		ResolvedVersion: version, Commit: "a92d74f0",
		Agents: []string{"claude", "codex"}, InstallMode: "symlink",
		Phase: app.InstallPhaseVerifying,
		Err:   errors.New(msg),
		Failure: &app.InstallFailure{
			Category: cat, Phase: app.InstallPhaseVerifying,
			Message: msg, Hint: hint,
			Expected: "sha256:03e0eaaa", Actual: "sha256:94ab28c1",
		},
	}
}

func mixedResults() []app.LockSkillResult {
	return []app.LockSkillResult{
		{Name: "ok-one", Status: app.LockSkillInstalled},
		failedResult("gke-scaling", "glapsfun/cloud-native-skills", "v1.4.2",
			app.FailureIntegrity, "computedHash mismatch", "re-run with --force"),
		{Name: "ok-two", Status: app.LockSkillUpToDate},
		failedResult("k8s-debug", "./local-skills", "",
			app.FailureForeignContent, "target contains unmanaged content", ""),
	}
}

// TestInstallResults_TableOnlyUnsuccessful (clarification #2): successful
// entries appear only in the summary; the table lists failures.
func TestInstallResults_TableOnlyUnsuccessful(t *testing.T) {
	t.Parallel()
	m := NewInstallResults(mixedResults()).SetSize(100, 30)
	view := m.View()

	for _, want := range []string{"gke-scaling", "k8s-debug"} {
		if !strings.Contains(view, want) {
			t.Errorf("table missing failed skill %q:\n%s", want, view)
		}
	}
	// Successful entries appear in counters, not rows: their names are absent.
	for _, absent := range []string{"ok-one", "ok-two"} {
		if strings.Contains(view, absent) {
			t.Errorf("table lists successful skill %q (must be counters-only):\n%s", absent, view)
		}
	}
	// Summary counters account for every entry (FR-015/FR-016).
	for _, want := range []string{"4 skills processed", "1 installed", "1 already up to date", "2 failed"} {
		if !strings.Contains(view, want) {
			t.Errorf("summary missing %q:\n%s", want, view)
		}
	}
	// State is never color-only: the textual status appears in each row.
	if !strings.Contains(view, "failed") {
		t.Errorf("rows lack textual status:\n%s", view)
	}
}

// TestInstallResults_AllSuccessNoTable: with nothing unsuccessful there are no
// rows, only the summary.
func TestInstallResults_AllSuccessNoTable(t *testing.T) {
	t.Parallel()
	m := NewInstallResults([]app.LockSkillResult{
		{Name: "a", Status: app.LockSkillInstalled},
		{Name: "b", Status: app.LockSkillUpToDate},
	}).SetSize(100, 30)
	if m.HasRows() {
		t.Error("HasRows() = true for an all-success run")
	}
	view := m.View()
	if !strings.Contains(view, "2 skills processed") {
		t.Errorf("summary missing processed count:\n%s", view)
	}
}

// TestInstallResults_DryRunListsAllEntries (FR-017 exception): a dry run
// tables every entry with its planned-action text.
func TestInstallResults_DryRunListsAllEntries(t *testing.T) {
	t.Parallel()
	m := NewInstallResults([]app.LockSkillResult{
		{Name: "fresh", Status: app.LockSkillPlanned, PlannedAction: app.PlannedWouldInstall},
		{Name: "relink", Status: app.LockSkillPlanned, PlannedAction: app.PlannedWouldRepair},
		{Name: "narrow", Status: app.LockSkillPlanned, PlannedAction: app.PlannedWouldRemoveTarget},
		{Name: "rewrite", Status: app.LockSkillPlanned, PlannedAction: app.PlannedWouldUpdateLock},
		{
			Name: "stuck", Status: app.LockSkillFailed, PlannedAction: app.PlannedBlocked,
			Err: errors.New("mismatch"), Failure: &app.InstallFailure{Category: app.FailureIntegrity, Message: "mismatch"},
		},
	}).SetSize(110, 30)
	view := m.View()
	for _, want := range []string{
		"fresh", "Would install",
		"relink", "Would repair",
		"narrow", "Would remove target",
		"rewrite", "Would update lock",
		"stuck", "Blocked",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("dry-run table missing %q:\n%s", want, view)
		}
	}
}

// TestInstallResults_ScrollAndSelect: the table scrolls with the standard key
// set and the cursor moves.
func TestInstallResults_ScrollAndSelect(t *testing.T) {
	t.Parallel()
	var rs []app.LockSkillResult
	for i := range 40 {
		rs = append(rs, failedResult(
			"skill-"+string(rune('a'+i%26))+string(rune('0'+i/26)),
			"src", "v1", app.FailureIntegrity, "boom", ""))
	}
	m := NewInstallResults(rs).SetSize(100, 20)

	if got := m.Cursor(); got != 0 {
		t.Fatalf("initial cursor = %d, want 0", got)
	}
	m, _ = m.Update(key("down"))
	m, _ = m.Update(key("j"))
	if got := m.Cursor(); got != 2 {
		t.Errorf("cursor after down+j = %d, want 2", got)
	}
	m, _ = m.Update(key("k"))
	if got := m.Cursor(); got != 1 {
		t.Errorf("cursor after k = %d, want 1", got)
	}
	m, _ = m.Update(key("end"))
	if got := m.Cursor(); got != len(rs)-1 {
		t.Errorf("cursor after end = %d, want %d", got, len(rs)-1)
	}
	m, _ = m.Update(key("home"))
	if got := m.Cursor(); got != 0 {
		t.Errorf("cursor after home = %d, want 0", got)
	}
}

// TestInstallResults_DetailViewRoundTrip (FR-019): enter opens the complete
// failure detail; esc returns; q exits from the table.
func TestInstallResults_DetailViewRoundTrip(t *testing.T) {
	t.Parallel()
	m := NewInstallResults(mixedResults()).SetSize(100, 30)

	m, exit := m.Update(key(keyEnter))
	if exit {
		t.Fatal("enter reported exit")
	}
	view := m.View()
	for _, want := range []string{
		"gke-scaling",
		"glapsfun/cloud-native-skills",
		"skills/gke-scaling",
		"v1.4.2",
		"a92d74f0",
		"claude, codex",
		"symlink",
		"Verifying integrity",
		"integrity",
		"computedHash mismatch",
		"sha256:03e0eaaa",
		"sha256:94ab28c1",
		"re-run with --force",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("detail view missing %q:\n%s", want, view)
		}
	}

	m, exit = m.Update(key(keyEsc))
	if exit {
		t.Fatal("esc from detail reported exit instead of returning to the table")
	}
	if !strings.Contains(m.View(), "k8s-debug") {
		t.Errorf("table not restored after esc:\n%s", m.View())
	}

	if _, exit = m.Update(key("q")); !exit {
		t.Error("q at the table did not exit")
	}
	if _, exit = m.Update(key(keyEsc)); !exit {
		t.Error("esc at the table did not exit")
	}
}

// TestInstallResults_EmDashForUnknowns (FR-014): a failure before resolution
// renders — for version, never fabricated data.
func TestInstallResults_EmDashForUnknowns(t *testing.T) {
	t.Parallel()
	r := failedResult("early", "src", "", app.FailureResolution, "no such ref", "")
	r.Commit = ""
	m := NewInstallResults([]app.LockSkillResult{r}).SetSize(100, 30)
	if !strings.Contains(m.View(), "—") {
		t.Errorf("table lacks — placeholder for unknown version:\n%s", m.View())
	}
	m, _ = m.Update(key(keyEnter))
	if !strings.Contains(m.View(), "—") {
		t.Errorf("detail lacks — placeholder:\n%s", m.View())
	}
}

// TestInstallResults_SanitizesUntrustedText (FR-028): hostile names, sources,
// and messages cannot reach the terminal un-neutralized.
func TestInstallResults_SanitizesUntrustedText(t *testing.T) {
	t.Parallel()
	r := failedResult("evil\x1b]0;pwned\x07", "src\x1b[2Jwipe", "v1",
		app.FailureUnknown, "boom \x1b[31mred\x1b[0m", "hint \x1b]8;;http://x\x07link")
	m := NewInstallResults([]app.LockSkillResult{r}).SetSize(100, 30)

	for _, view := range []string{m.View(), func() string { m, _ = m.Update(key(keyEnter)); return m.View() }()} {
		if strings.Contains(view, "\x1b]") || strings.Contains(view, "\x1b[2J") || strings.Contains(view, "\x07") {
			t.Errorf("untrusted escape sequence leaked:\n%q", view)
		}
	}
}

// TestInstallResults_LongMessageTruncatedWithFullDetail (FR-029): table cells
// truncate; the detail view carries the complete text.
func TestInstallResults_LongMessageTruncatedWithFullDetail(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("very-long-reason ", 30)
	r := failedResult("longy", "src", "v1", app.FailureUnknown, long, "")
	m := NewInstallResults([]app.LockSkillResult{r}).SetSize(80, 30)

	for line := range strings.SplitSeq(m.View(), "\n") {
		if len([]rune(line)) > 120 {
			t.Errorf("table line exceeds sane width (%d runes): %q", len([]rune(line)), line)
		}
	}
	m, _ = m.Update(key(keyEnter))
	if !strings.Contains(m.View(), strings.TrimSpace(long)) {
		t.Error("detail view lacks the complete message")
	}
}

// Theme.InstallStatusCell mirrors StatusCell/HealthCell: glyph + text, never
// color alone, unknown statuses render plain.
func TestTheme_InstallStatusCell(t *testing.T) {
	t.Parallel()
	th := DefaultTheme()
	for _, status := range []string{
		"installed", "repaired", "up-to-date", "skipped",
		"failed", "cancelled", "not-attempted", "planned",
	} {
		if got := th.InstallStatusCell(status); !strings.Contains(got, status) {
			t.Errorf("InstallStatusCell(%q) = %q lacks the textual status", status, got)
		}
	}
	if got := th.InstallStatusCell("mystery"); got != "mystery" {
		t.Errorf("unknown status rendered %q, want plain passthrough", got)
	}
}

// TestInstallResults_CtrlCAlwaysExits (review C3): raw mode never raises
// SIGINT, so ctrl+c must exit from both the table and the detail view.
func TestInstallResults_CtrlCAlwaysExits(t *testing.T) {
	t.Parallel()
	m := NewInstallResults(mixedResults()).SetSize(100, 30)
	if _, exit := m.Update(key("ctrl+c")); !exit {
		t.Error("ctrl+c at the table did not exit")
	}
	m, _ = m.Update(key("enter")) // detail view
	if _, exit := m.Update(key("ctrl+c")); !exit {
		t.Error("ctrl+c in the detail view did not exit")
	}
}
