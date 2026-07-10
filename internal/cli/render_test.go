package cli

import (
	"regexp"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/tui"
)

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// stripAnsi removes SGR sequences so tests can assert visible layout.
func stripAnsi(s string) string { return ansiRE.ReplaceAllString(s, "") }

func TestRenderListStyled_ColumnsAndGlyphs(t *testing.T) {
	t.Parallel()
	skills := []app.ListedSkill{
		{Name: "tui-design", Version: "1.0.1", Source: "acme/skills", Status: "installed"},
		{Name: "deploy", Version: "0.9.2", Source: "corp/devops", Status: "missing"},
	}
	got := renderListStyled(skills)
	for _, want := range []string{"NAME", "VERSION", "SOURCE", "STATUS", "●", "✗", "tui-design", "corp/devops"} {
		if !strings.Contains(got, want) {
			t.Errorf("styled list missing %q:\n%s", want, got)
		}
	}
	// Columns align: VERSION values start at the header's column offset.
	lines := strings.Split(got, "\n")
	if idx := strings.Index(lines[0], "VERSION"); idx <= 0 ||
		!strings.Contains(lines[1][idx:], "1.0.1") {
		t.Errorf("VERSION column not aligned:\n%s", got)
	}
}

func TestRenderListStyled_Empty(t *testing.T) {
	t.Parallel()
	if got := renderListStyled(nil); got != "No skills installed." {
		t.Errorf("empty styled list = %q", got)
	}
}

func TestRenderInfoStyled_Fields(t *testing.T) {
	t.Parallel()
	got := renderInfoStyled(app.SkillInfo{Name: "x", Version: "1.0.0", Source: "a/b", Commit: "abc", Description: "d", Agents: []string{"claude"}})
	for _, want := range []string{"x", "1.0.0", "a/b", "source", "agents"} {
		if !strings.Contains(got, want) {
			t.Errorf("styled info missing %q:\n%s", want, got)
		}
	}
}

func TestRenderStatusStyled_ColumnsAndGlyphs(t *testing.T) {
	t.Parallel()
	report := app.StatusReport{Skills: []app.SkillStatus{
		{Name: "audience-ingestion", Active: "ok", Agents: []app.AgentStatus{
			{ID: "claude", Health: "ok-symlink"}, {ID: "codex", Health: "ok-symlink"},
		}},
		{Name: "event-ingestion", Active: "ok", Agents: []app.AgentStatus{
			{ID: "claude", Health: "missing"},
		}},
	}}
	got := renderStatusStyled(report)
	for _, want := range []string{"NAME", "ACTIVE", "AGENTS", "●", "✗", "audience-ingestion", "claude", "ok-symlink", "missing"} {
		if !strings.Contains(got, want) {
			t.Errorf("styled status missing %q:\n%s", want, got)
		}
	}
	// The ACTIVE column aligns: both data rows place their active glyph at the
	// header's ACTIVE offset.
	lines := strings.Split(got, "\n")
	if idx := strings.Index(lines[0], "ACTIVE"); idx <= 0 ||
		!strings.HasPrefix(strings.TrimLeft(lines[1][idx:], " "), "●") {
		t.Errorf("ACTIVE column not aligned:\n%s", got)
	}
}

func TestRenderStatusStyled_Empty(t *testing.T) {
	t.Parallel()
	if got := renderStatusStyled(app.StatusReport{}); got != "0 skill(s)" {
		t.Errorf("empty styled status = %q", got)
	}
}

func TestRenderAligned_AnsiAwareWidths(t *testing.T) {
	t.Parallel()
	st := tui.DefaultTheme()
	got := renderAligned(st, []string{"A", "BB"}, [][]string{
		{st.Accent.Render("xxxx"), "y"},
		{"z", st.Success.Render("● w")},
	})
	lines := strings.Split(got, "\n")
	if len(lines) != 3 {
		t.Fatalf("want header + 2 rows, got %d lines:\n%s", len(lines), got)
	}
	// The second column starts at the same visible offset on every line: the
	// first column is 4 wide ("xxxx") plus the 2-space gutter.
	for i, l := range lines {
		stripped := stripAnsi(l)
		if i > 0 && len(stripped) > 6 && stripped[4:6] != "  " {
			t.Errorf("line %d: gutter misplaced in %q", i, stripped)
		}
	}
}

func TestRenderFindStyled_Table(t *testing.T) {
	t.Parallel()
	got := renderFindStyled([]app.SearchHit{
		{ID: "deploy", Source: "acme/skills", RepoPath: "ops", Installed: true},
		{ID: "docs", Source: "acme/skills", RepoPath: ""},
	})
	for _, want := range []string{"ID", "SOURCE", "PATH", "deploy", "● installed", "."} {
		if !strings.Contains(got, want) {
			t.Errorf("styled find missing %q:\n%s", want, got)
		}
	}
}

func TestRenderDiffStyled_Table(t *testing.T) {
	t.Parallel()
	got := renderDiffStyled([]app.DiffEntry{
		{Name: "a", Status: "installed"},
		{Name: "b", Status: "missing"},
	})
	for _, want := range []string{"NAME", "STATUS", "●", "✗"} {
		if !strings.Contains(got, want) {
			t.Errorf("styled diff missing %q:\n%s", want, got)
		}
	}
}

func TestRenderConfigListStyled_Table(t *testing.T) {
	t.Parallel()
	got := renderConfigListStyled(map[string]string{"cache.dir": "/x", "agents": "claude"})
	lines := strings.Split(got, "\n")
	if len(lines) != 3 || !strings.Contains(lines[0], "KEY") || !strings.Contains(lines[1], "agents") {
		t.Errorf("styled config list wrong shape (sorted keys, header):\n%s", got)
	}
}

func TestRenderDoctorStyled_Fields(t *testing.T) {
	t.Parallel()
	got := renderDoctorStyled(app.DoctorReport{GitAvailable: true, DetectedAgents: []string{"claude"}, Warnings: []string{"w"}})
	for _, want := range []string{"git available", "✓", "claude", "warnings", "1"} {
		if !strings.Contains(got, want) {
			t.Errorf("styled doctor missing %q:\n%s", want, got)
		}
	}
}

func TestRenderPlanTextStyled_KeepsText(t *testing.T) {
	t.Parallel()
	plan := app.InstallPlan{Actions: []app.PlannedAction{{Skill: "s", AgentID: "claude", Destination: "/d"}}}
	// Without color the styled rendering must equal the plain one exactly.
	if got, want := renderPlanTextStyled(plan), renderPlanText(plan); got != want {
		t.Errorf("styled plan text diverges without color:\n%q\n%q", got, want)
	}
}
