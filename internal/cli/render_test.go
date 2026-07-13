package cli

import (
	"fmt"
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
		{
			Name: "tui-design", Version: "1.0.1", Source: "acme/skills", Status: "installed",
			Active: "ok", AgentHealth: []app.AgentHealthEntry{
				{ID: "claude", Health: "ok-symlink"}, {ID: "codex", Health: "ok-symlink"},
			},
		},
		{
			Name: "deploy", Version: "0.9.2", Source: "corp/devops", Status: "missing",
			Active: "ok", AgentHealth: []app.AgentHealthEntry{{ID: "claude", Health: "missing"}},
		},
	}
	got := renderListStyled(skills)
	for _, want := range []string{
		"NAME", "VERSION", "SOURCE", "STATUS", "ACTIVE", "AGENTS", "●", "✗",
		"tui-design", "corp/devops", "claude", "ok-symlink", "missing",
	} {
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
	// The ACTIVE column aligns: both data rows place their active glyph at the
	// header's ACTIVE offset.
	if idx := strings.Index(lines[0], "ACTIVE"); idx <= 0 ||
		!strings.HasPrefix(strings.TrimLeft(lines[1][idx:], " "), "●") {
		t.Errorf("ACTIVE column not aligned:\n%s", got)
	}
}

func TestRenderListStyled_Empty(t *testing.T) {
	t.Parallel()
	if got := renderListStyled(nil); got != noSkillsInstalled {
		t.Errorf("empty styled list = %q", got)
	}
}

func TestRenderListStyled_EmptyAgentHealth(t *testing.T) {
	t.Parallel()
	got := renderListStyled([]app.ListedSkill{
		{Name: "solo", Version: "1.0.0", Source: "acme/skills", Status: "installed", Active: "ok"},
	})
	if !strings.Contains(got, "solo") {
		t.Errorf("styled list missing row for empty-AgentHealth skill:\n%s", got)
	}
}

func TestRenderListTable_ActiveAndAgentHealthColumns(t *testing.T) {
	t.Parallel()
	skills := []app.ListedSkill{
		{
			Name: "tui-design", Version: "1.0.1", Source: "acme/skills", Status: "installed",
			Active: "ok", AgentHealth: []app.AgentHealthEntry{
				{ID: "claude", Health: "ok-symlink"}, {ID: "codex", Health: "missing"},
			},
		},
	}
	got := renderListTable(skills)

	// The four pre-existing columns keep their current NAME/STATUS/VERSION/
	// SOURCE positional order and values (contracts/list-command.md §2) —
	// ACTIVE/AGENTS are appended, not inserted, so this prefix must be
	// unchanged from before the merge.
	lines := strings.Split(got, "\n")
	if len(lines) != 1 {
		t.Fatalf("want 1 line, got %d:\n%s", len(lines), got)
	}
	wantPrefix := fmt.Sprintf("%-24s %-10s %-14s", "tui-design", "installed", "1.0.1")
	if !strings.HasPrefix(lines[0], wantPrefix) {
		t.Errorf("plain list column order changed, want prefix %q:\n%q", wantPrefix, lines[0])
	}
	for _, want := range []string{"acme/skills", "ok", "claude", "ok-symlink", "codex", "missing"} {
		if !strings.Contains(got, want) {
			t.Errorf("plain list missing %q:\n%s", want, got)
		}
	}
}

func TestRenderListTable_EmptyAgentHealth(t *testing.T) {
	t.Parallel()
	got := renderListTable([]app.ListedSkill{
		{Name: "solo", Version: "1.0.0", Source: "acme/skills", Status: "installed", Active: "ok"},
	})
	if !strings.Contains(got, "solo") {
		t.Errorf("plain list missing row for empty-AgentHealth skill:\n%s", got)
	}
}

func TestRenderListTable_Empty(t *testing.T) {
	t.Parallel()
	if got := renderListTable(nil); got != noSkillsInstalled {
		t.Errorf("empty plain list = %q", got)
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
