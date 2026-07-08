package cli

import (
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/app"
)

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
