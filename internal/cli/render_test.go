package cli

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/source"
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

func TestRenderConfigListStyled_Table(t *testing.T) {
	t.Parallel()
	got := renderConfigListStyled("/etc/gskill/config.toml", map[string]string{"cache.dir": "/x", "agents": "claude"})
	lines := strings.Split(got, "\n")
	// Path header (folded in from the retired `config path`), then the table
	// header, then the sorted keys.
	if len(lines) != 4 ||
		!strings.Contains(lines[0], "user config: /etc/gskill/config.toml") ||
		!strings.Contains(lines[1], "KEY") ||
		!strings.Contains(lines[2], "agents") {
		t.Errorf("styled config list wrong shape (path header, table header, sorted keys):\n%s", got)
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

var (
	acmeRepo   = source.Repo{Kind: source.RepoRemote, Key: "github.com/acme/skills", Label: "github.com/acme/skills"}
	devopsRepo = source.Repo{Kind: source.RepoRemote, Key: "github.com/example/devops", Label: "github.com/example/devops"}
)

// groupedPlan is a two-repository plan: acme holds an update, a pin, and an
// up-to-date skill; devops holds one update.
func groupedPlan() app.UpdatePlan {
	return app.UpdatePlan{Items: []app.UpdatePlanItem{
		{
			Name: "code-review", Current: "1.2.0", Policy: "1.2.0", Status: app.StatusPinnedVersion, Pinned: true,
			Reason: `pinned by skills.toml (version = "1.2.0")`, Informational: "2.1.0",
			NextAction: "gskill upgrade code-review", Repo: acmeRepo,
		},
		{Name: "deploy", Current: "0.3.0", Candidate: "0.4.0", Policy: "latest", Status: app.StatusUpdateAvailable, Repo: devopsRepo},
		{Name: "kubernetes", Current: "1.2.0", Candidate: "1.4.1", Policy: "^1.2.0", Status: app.StatusUpdateAvailable, Repo: acmeRepo},
		{Name: "security", Current: "2.3.2", Policy: "^2.0.0", Status: app.StatusUpToDate, Reason: "already the newest revision the policy allows", Repo: acmeRepo},
	}}
}

func plainOutput() *Output {
	var out, errb bytes.Buffer
	return &Output{stdout: &out, stderr: &errb}
}

func TestGroupLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		available, total, failed, notChecked int
		want                                 string
	}{
		{1, 3, 0, 0, "github.com/acme/skills · 1 update available / 3 skills"},
		{2, 1, 0, 0, "github.com/acme/skills · 2 updates available / 1 skill"},
		{0, 2, 1, 0, "github.com/acme/skills · 0 updates available / 2 skills · 1 failed"},
		{0, 2, 0, 2, "github.com/acme/skills · 0 updates available / 2 skills · 2 not checked"},
	}
	for _, tt := range tests {
		if got := groupLine("github.com/acme/skills", tt.available, tt.total, tt.failed, tt.notChecked); got != tt.want {
			t.Errorf("groupLine(%d, %d, %d, %d) = %q, want %q", tt.available, tt.total, tt.failed, tt.notChecked, got, tt.want)
		}
	}
}

func TestResultGroupLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		updated, total, failed int
		dryRun                 bool
		want                   string
	}{
		{1, 3, 0, false, "github.com/acme/skills · 1 updated / 3 skills"},
		{1, 1, 0, true, "github.com/acme/skills · 1 would update / 1 skill"},
		{0, 2, 1, false, "github.com/acme/skills · 2 skills · 1 failed"},
		{0, 1, 0, false, "github.com/acme/skills · 1 skill"},
	}
	for _, tt := range tests {
		if got := resultGroupLine("github.com/acme/skills", tt.updated, tt.total, tt.failed, tt.dryRun); got != tt.want {
			t.Errorf("resultGroupLine(%d, %d, %d, %v) = %q, want %q", tt.updated, tt.total, tt.failed, tt.dryRun, got, tt.want)
		}
	}
}

// The report prints one section per repository: its line, then its own
// aligned table (spec 032 FR-031).
func TestRenderUpdateList_GroupedSections(t *testing.T) {
	t.Parallel()

	got := stripAnsi(renderUpdateList(plainOutput(), groupedPlan(), false))
	want := strings.Join([]string{
		"github.com/acme/skills · 1 update available / 3 skills",
		"NAME        CURRENT  AVAILABLE  POLICY",
		"kubernetes  1.2.0    1.4.1      ^1.2.0",
		"",
		"github.com/example/devops · 1 update available / 1 skill",
		"NAME    CURRENT  AVAILABLE  POLICY",
		"deploy  0.3.0    0.4.0      latest",
		"",
		"2 updates available · 1 up to date · 1 pinned (--all shows them)",
	}, "\n")
	if got != want {
		t.Errorf("renderUpdateList =\n%s\nwant\n%s", got, want)
	}
}

// Without --all a group with nothing actionable is left out; its skills are
// still counted in the summary.
func TestRenderUpdateList_OmitsGroupsWithNothingToShow(t *testing.T) {
	t.Parallel()

	plan := groupedPlan()
	plan.Items[1].Status, plan.Items[1].Candidate = app.StatusUpToDate, ""
	got := stripAnsi(renderUpdateList(plainOutput(), plan, false))
	if strings.Contains(got, "github.com/example/devops") {
		t.Errorf("group with no actionable row was rendered:\n%s", got)
	}
	if !strings.Contains(got, "1 update available · 2 up to date · 1 pinned") {
		t.Errorf("summary does not count the hidden group:\n%s", got)
	}
}

// With --all every skill shows in its group, each guidance line under its row.
func TestRenderUpdateList_AllKeepsGuidanceUnderItsRow(t *testing.T) {
	t.Parallel()

	lines := strings.Split(stripAnsi(renderUpdateList(plainOutput(), groupedPlan(), true)), "\n")
	want := []string{
		"github.com/acme/skills · 1 update available / 3 skills",
		"NAME         CURRENT  AVAILABLE  POLICY  STATUS",
		"code-review  1.2.0    --         1.2.0   pinned version",
		`  pinned by skills.toml (version = "1.2.0"); newest 2.1.0. Run 'gskill upgrade code-review' or edit skills.toml to move it.`,
		"kubernetes   1.2.0    1.4.1      ^1.2.0  update available",
		"security     2.3.2    --         ^2.0.0  up to date",
		"",
		"github.com/example/devops · 1 update available / 1 skill",
	}
	if len(lines) < len(want) {
		t.Fatalf("too few lines:\n%s", strings.Join(lines, "\n"))
	}
	for i, w := range want {
		if lines[i] != w {
			t.Errorf("line %d = %q, want %q", i, lines[i], w)
		}
	}
}

func groupedResult(dryRun bool) app.UpdateResult {
	res := app.UpdateResult{DryRun: dryRun, Skills: []app.UpdateSkillResult{
		{
			Name: "code-review", From: "1.2.0", To: "1.2.0", Outcome: app.UpdateOutcomeNoChange, Status: app.StatusPinnedVersion,
			Reason: `pinned by skills.toml (version = "1.2.0")`, Informational: "2.1.0", NextAction: "gskill upgrade code-review", Repo: acmeRepo,
		},
		{Name: "deploy", From: "0.3.0", To: "0.3.0", Outcome: app.UpdateOutcomeFailed, Reason: "discovery failed: unreachable", Repo: devopsRepo},
		{Name: "kubernetes", From: "1.2.0", To: "1.4.1", Outcome: app.UpdateOutcomeUpdated, Repo: acmeRepo},
	}}
	if dryRun {
		res.Skills[2].Outcome = app.UpdateOutcomeWould
	}
	res.Updated, res.Failed, res.NoChange, res.Pinned = 1, 1, 1, 1
	return res
}

// Results print per repository, each group's detail lines after its table,
// then the run summary (spec 032 FR-014, FR-031).
func TestRenderUpdateResult_GroupedSections(t *testing.T) {
	t.Parallel()

	got := stripAnsi(renderUpdateResult(plainOutput(), groupedResult(false)))
	want := strings.Join([]string{
		"github.com/acme/skills · 1 updated / 2 skills",
		"NAME         FROM   TO     RESULT",
		"code-review  1.2.0  1.2.0  pinned version",
		"kubernetes   1.2.0  1.4.1  updated",
		"",
		`  code-review: pinned by skills.toml (version = "1.2.0"); newest 2.1.0. Run 'gskill upgrade code-review' or edit skills.toml to move it.`,
		"",
		"github.com/example/devops · 1 skill · 1 failed",
		"NAME    FROM   TO     RESULT",
		"deploy  0.3.0  0.3.0  failed",
		"",
		"  deploy: discovery failed: unreachable",
		"",
		"Updated 1 skill · 1 pinned · 1 skill failed",
	}, "\n")
	if got != want {
		t.Errorf("renderUpdateResult =\n%s\nwant\n%s", got, want)
	}
}

func TestRenderUpdateResult_DryRunKeepsItsNotice(t *testing.T) {
	t.Parallel()

	got := stripAnsi(renderUpdateResult(plainOutput(), groupedResult(true)))
	if !strings.Contains(got, "github.com/acme/skills · 1 would update / 2 skills") {
		t.Errorf("dry-run group line missing:\n%s", got)
	}
	if !strings.HasSuffix(got, "\nDry run: no changes made.") {
		t.Errorf("dry-run notice missing:\n%s", got)
	}
}

func TestRenderUpdateList_EmptyPlan(t *testing.T) {
	t.Parallel()

	if got := renderUpdateList(plainOutput(), app.UpdatePlan{}, true); got != noSkillsDeclared {
		t.Errorf("empty plan = %q, want %q", got, noSkillsDeclared)
	}
}
