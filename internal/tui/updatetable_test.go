package tui

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/glapsfun/gskill/internal/errs"
)

// tableFixture is two repositories: acme with an update, a pin, and an
// up-to-date skill, and devops with one update.
func tableFixture() UpdateTable {
	return UpdateTable{Groups: []TableGroup{
		{
			Key: "remote\x00github.com/example/devops", Label: "github.com/example/devops",
			Summary: "github.com/example/devops · 1 update available / 1 skill",
			Rows: []TableRow{
				{Name: "deploy", Current: "0.3.0", Candidate: "0.4.0", Policy: "latest", Status: "update available", Detail: "0.3.0 → 0.4.0 within latest", Eligible: true},
			},
		},
		{
			Key: "remote\x00github.com/acme/skills", Label: "github.com/acme/skills",
			Summary: "github.com/acme/skills · 1 update available / 3 skills",
			Rows: []TableRow{
				{Name: "code-review", Current: "1.2.0", Policy: "1.2.0", Status: "pinned version", Detail: `pinned by skills.toml (version = "1.2.0"). Run 'gskill upgrade code-review' or edit skills.toml to move it.`},
				{Name: "kubernetes", Current: "1.2.0", Candidate: "1.4.1", Policy: "^1.2.0", Status: "update available", Detail: "1.2.0 → 1.4.1 within ^1.2.0", Eligible: true},
				{Name: "security", Current: "2.3.2", Policy: "^2.0.0", Status: "up to date", Detail: "already the newest revision the policy allows"},
			},
		},
	}}
}

// manyTableGroups builds n skills spread over g repositories, all eligible.
func manyTableGroups(n, g int) UpdateTable {
	t := UpdateTable{}
	for i := range g {
		t.Groups = append(t.Groups, TableGroup{
			Key: fmt.Sprintf("remote\x00github.com/org/repo-%02d", i), Label: fmt.Sprintf("github.com/org/repo-%02d", i),
			Summary: fmt.Sprintf("github.com/org/repo-%02d", i),
		})
	}
	for i := range n {
		grp := &t.Groups[i%g]
		grp.Rows = append(grp.Rows, TableRow{
			Name: fmt.Sprintf("skill-%03d", i), Current: "1.0.0", Candidate: "1.1.0", Policy: "^1.0.0",
			Status: "update available", Eligible: true,
		})
	}
	return t
}

// driveTable feeds messages to the table model.
func driveTable(t *testing.T, m tea.Model, msgs ...tea.Msg) updateTableModel {
	t.Helper()
	for _, msg := range msgs {
		m, _ = m.Update(msg)
	}
	tm, ok := m.(updateTableModel)
	if !ok {
		t.Fatalf("model is not updateTableModel: %T", m)
	}
	return tm
}

// pressTable feeds key presses to the table model.
func pressTable(t *testing.T, m tea.Model, keys ...string) updateTableModel {
	t.Helper()
	msgs := make([]tea.Msg, len(keys))
	for i, k := range keys {
		msgs[i] = tableKey(k)
	}
	return driveTable(t, m, msgs...)
}

func tableKey(k string) tea.KeyMsg {
	switch k {
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	default:
		return key(k)
	}
}

// moveTo presses down until the cursor is on the named row (or group key).
func moveTo(t *testing.T, m updateTableModel, target string) updateTableModel {
	t.Helper()
	m = pressTable(t, m, "home")
	for range len(m.lines) + 1 {
		if m.cursorID() == target {
			return m
		}
		m = pressTable(t, m, "down")
	}
	t.Fatalf("cursor never reached %q", target)
	return m
}

func plainView(m updateTableModel) string { return ansi.Strip(m.View()) }

func TestUpdateTable_ShowsEveryRowUnderItsGroup(t *testing.T) {
	t.Parallel()

	m := driveTable(t, newUpdateTableModel(tableFixture()), win(120, 40))
	view := plainView(m)
	devops := strings.Index(view, "github.com/example/devops · 1 update available / 1 skill")
	acme := strings.Index(view, "github.com/acme/skills · 1 update available / 3 skills")
	if devops < 0 || acme < 0 || devops > acme {
		t.Fatalf("headers missing or out of order:\n%s", view)
	}
	last := acme
	for _, name := range []string{"code-review", "kubernetes", "security"} {
		i := strings.Index(view, name)
		if i < last || strings.Count(view, name+" ") != 1 {
			t.Errorf("%s missing, duplicated, or out of order:\n%s", name, view)
		}
		last = i
	}
	if i := strings.Index(view, "deploy "); i < devops || i > acme {
		t.Errorf("deploy is not under its group:\n%s", view)
	}

	m = pressTable(t, moveTo(t, m, "kubernetes"), " ")
	if !strings.Contains(plainView(m), "github.com/acme/skills · 1 update available / 3 skills · 1 selected") {
		t.Errorf("header does not count the selection (FR-009):\n%s", plainView(m))
	}
}

func TestUpdateTable_IneligibleRowsCannotBeSelected(t *testing.T) {
	t.Parallel()

	m := driveTable(t, newUpdateTableModel(tableFixture()), win(160, 40))
	m = pressTable(t, moveTo(t, m, "code-review"), " ", "x")
	if len(m.chosen) != 0 {
		t.Errorf("pinned row was selected: %v", m.chosen)
	}
	if !strings.Contains(plainView(m), "gskill upgrade code-review") {
		t.Errorf("detail line does not show the pin's guidance:\n%s", plainView(m))
	}
}

func TestUpdateTable_ReviewListsTheSelectionGrouped(t *testing.T) {
	t.Parallel()

	m := driveTable(t, newUpdateTableModel(tableFixture()), win(120, 40))
	m = pressTable(t, moveTo(t, m, "kubernetes"), " ")
	m = pressTable(t, moveTo(t, m, "deploy"), " ", "enter")
	if !m.reviewing {
		t.Fatal("enter with a selection did not open the review")
	}
	view := plainView(m)
	for _, want := range []string{"Review 2 updates", "github.com/example/devops", "deploy", "0.3.0 → 0.4.0", "github.com/acme/skills", "kubernetes", "1.2.0 → 1.4.1", "enter apply"} {
		if !strings.Contains(view, want) {
			t.Errorf("review missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "Dry run") {
		t.Errorf("review shows a dry-run notice outside a dry run:\n%s", view)
	}
	if strings.Contains(view, "code-review") {
		t.Errorf("review lists an unselected skill:\n%s", view)
	}

	m = pressTable(t, m, "enter")
	res := m.result()
	if !m.done || res.Cancelled || res.Interrupted {
		t.Fatalf("review enter = %+v, want a confirmed selection", res)
	}
	if want := []string{"deploy", "kubernetes"}; strings.Join(res.Names, ",") != strings.Join(want, ",") {
		t.Errorf("names = %v, want %v (input order)", res.Names, want)
	}
}

func TestUpdateTable_CancelAndInterrupt(t *testing.T) {
	t.Parallel()

	selected := func() updateTableModel {
		m := newUpdateTableModel(tableFixture())
		return pressTable(t, moveTo(t, m, "deploy"), " ")
	}
	for _, k := range []string{"esc", "q"} {
		if res := pressTable(t, selected(), k).result(); !res.Cancelled || len(res.Names) != 0 {
			t.Errorf("table %s = %+v, want cancelled with no names", k, res)
		}
		if res := pressTable(t, selected(), "enter", k).result(); !res.Cancelled || len(res.Names) != 0 {
			t.Errorf("review %s = %+v, want cancelled with no names", k, res)
		}
	}
	if res := pressTable(t, selected(), "ctrl+c").result(); !res.Interrupted || len(res.Names) != 0 {
		t.Errorf("table ctrl+c = %+v, want interrupted", res)
	}
	if res := pressTable(t, selected(), "enter", "ctrl+c").result(); !res.Interrupted || len(res.Names) != 0 {
		t.Errorf("review ctrl+c = %+v, want interrupted", res)
	}
}

func TestUpdateTable_EnterWithNothingSelectedFinishes(t *testing.T) {
	t.Parallel()

	m := pressTable(t, newUpdateTableModel(tableFixture()), "enter")
	res := m.result()
	if m.reviewing || !m.done || res.Cancelled || res.Interrupted || len(res.Names) != 0 {
		t.Errorf("empty enter = reviewing %v, %+v; want done with no names", m.reviewing, res)
	}
}

func TestUpdateTable_NoEligibleRowsStillOpens(t *testing.T) {
	t.Parallel()

	fx := tableFixture()
	fx.Groups = fx.Groups[1:]
	fx.Groups[0].Rows[1].Eligible = false
	view := plainView(driveTable(t, newUpdateTableModel(fx), win(120, 40)))
	if !strings.Contains(view, "No updates available. Every skill is shown with its status.") {
		t.Errorf("title does not say nothing can be updated (FR-019):\n%s", view)
	}
	for _, name := range []string{"code-review", "kubernetes", "security"} {
		if !strings.Contains(view, name) {
			t.Errorf("row %s missing:\n%s", name, view)
		}
	}
}

func TestUpdateTable_DryRunReviewNotice(t *testing.T) {
	t.Parallel()

	fx := tableFixture()
	fx.DryRun = true
	m := pressTable(t, moveTo(t, newUpdateTableModel(fx), "deploy"), " ", "enter")
	if !strings.Contains(plainView(m), "Dry run: nothing will be written.") {
		t.Errorf("dry-run review notice missing:\n%s", plainView(m))
	}
}

func TestUpdateTable_BannerSelectionAndCursorCarryOver(t *testing.T) {
	t.Parallel()

	fx := tableFixture()
	fx.Banner = "Updated 1 skill"
	fx.Selected = []string{"kubernetes", "code-review"}
	fx.Cursor = "kubernetes"
	m := newUpdateTableModel(fx)
	if !strings.Contains(plainView(m), "Updated 1 skill") {
		t.Errorf("banner missing:\n%s", plainView(m))
	}
	if !m.chosen["kubernetes"] || m.chosen["code-review"] || len(m.chosen) != 1 {
		t.Errorf("chosen = %v, want only the eligible kubernetes", m.chosen)
	}
	if m.cursorID() != "kubernetes" {
		t.Errorf("cursor = %q, want kubernetes", m.cursorID())
	}

	fx.Cursor = "gone"
	if got := newUpdateTableModel(fx).cursor; got != 0 {
		t.Errorf("unknown cursor started at line %d, want 0", got)
	}
	if res := pressTable(t, newUpdateTableModel(fx), "q").result(); res.Cursor == "" {
		t.Error("result does not report the cursor position")
	}
}

func TestUpdateTable_ShowsInRepositoryPath(t *testing.T) {
	t.Parallel()

	fx := tableFixture()
	fx.Groups[1].Rows[1].Path = "tools/lint"
	view := plainView(driveTable(t, newUpdateTableModel(fx), win(120, 40)))
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "kubernetes") && !strings.Contains(line, "tools/lint") {
			t.Errorf("row does not show its path (FR-008): %q", line)
		}
	}
}

func TestUpdateTable_BoundedWindowScrolls(t *testing.T) {
	t.Parallel()

	m := driveTable(t, newUpdateTableModel(manyTableGroups(40, 1)), win(80, 12))
	view := m.View()
	if n := strings.Count(view, "\n"); n > 12 {
		t.Errorf("view has %d lines, want at most 12:\n%s", n, view)
	}
	if !strings.Contains(view, "↓ more") {
		t.Errorf("no more-below marker:\n%s", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if w := ansi.StringWidth(line); w > 80 {
			t.Errorf("line is %d wide, want at most 80: %q", w, line)
		}
	}
	for range 20 {
		m = pressTable(t, m, "down")
	}
	if !strings.Contains(plainView(m), m.cursorID()) {
		t.Errorf("cursor row %q scrolled off screen:\n%s", m.cursorID(), plainView(m))
	}
}

func TestSelectGroupedUpdates_RefusesWithoutTTY(t *testing.T) {
	t.Parallel()

	_, err := SelectGroupedUpdates(UpdateTable{}, false)
	if !errors.Is(err, errs.ErrUsage) {
		t.Errorf("err = %v, want a usage error", err)
	}
}

// toggleFixture is a group with two eligible skills and a pin, a second
// group with one eligible skill, and a third with only an up-to-date skill.
func toggleFixture() UpdateTable {
	return UpdateTable{Groups: []TableGroup{
		{Key: "g-a", Label: "github.com/acme/a", Summary: "github.com/acme/a", Rows: []TableRow{
			{Name: "a1", Current: "1.0.0", Candidate: "1.1.0", Eligible: true},
			{Name: "a2", Current: "1.0.0", Candidate: "1.1.0", Eligible: true},
			{Name: "p1", Current: "1.0.0", Status: "pinned version"},
		}},
		{Key: "g-b", Label: "github.com/acme/b", Summary: "github.com/acme/b", Rows: []TableRow{
			{Name: "b1", Current: "1.0.0", Candidate: "1.1.0", Eligible: true},
		}},
		{Key: "g-u", Label: "github.com/acme/u", Summary: "github.com/acme/u", Rows: []TableRow{
			{Name: "u1", Current: "1.0.0", Status: "up to date"},
		}},
	}}
}

func chosenNames(m updateTableModel) string {
	var names []string
	for _, g := range m.groups {
		for _, r := range g.Rows {
			if m.chosen[r.Name] {
				names = append(names, r.Name)
			}
		}
	}
	return strings.Join(names, ",")
}

func TestUpdateTable_GroupToggle(t *testing.T) {
	t.Parallel()

	m := moveTo(t, newUpdateTableModel(toggleFixture()), "g-a")
	m = pressTable(t, m, " ")
	if got := chosenNames(m); got != "a1,a2" {
		t.Errorf("group toggle chose %q, want a1,a2 and never the pin (US2 scenario 1)", got)
	}
	if got := chosenNames(pressTable(t, m, " ")); got != "" {
		t.Errorf("second group toggle left %q, want everything cleared (scenario 2)", got)
	}

	partial := pressTable(t, moveTo(t, newUpdateTableModel(toggleFixture()), "a1"), " ")
	partial = pressTable(t, moveTo(t, partial, "g-a"), "x")
	if got := chosenNames(partial); got != "a1,a2" {
		t.Errorf("partial group toggle chose %q, want a1,a2 (scenario 3)", got)
	}
}

func TestUpdateTable_SelectAll(t *testing.T) {
	t.Parallel()

	m := pressTable(t, newUpdateTableModel(toggleFixture()), "a")
	if got := chosenNames(m); got != "a1,a2,b1" {
		t.Errorf("select-all chose %q, want every eligible skill (scenario 4)", got)
	}
	if got := chosenNames(pressTable(t, m, "a")); got != "" {
		t.Errorf("second select-all left %q, want everything cleared", got)
	}
}

func TestUpdateTable_GroupWithNothingToUpdate(t *testing.T) {
	t.Parallel()

	m := pressTable(t, moveTo(t, driveTable(t, newUpdateTableModel(toggleFixture()), win(120, 40)), "g-u"), " ")
	if len(m.chosen) != 0 {
		t.Errorf("toggling a group without updates chose %v", m.chosen)
	}
	if !strings.Contains(plainView(m), "github.com/acme/u has no updates available") {
		t.Errorf("no message for an empty group toggle (scenario 5):\n%s", plainView(m))
	}
}

func TestUpdateTable_DeselectAfterGroupToggle(t *testing.T) {
	t.Parallel()

	m := pressTable(t, moveTo(t, newUpdateTableModel(toggleFixture()), "g-a"), " ")
	m = pressTable(t, moveTo(t, m, "a2"), " ", "enter")
	view := plainView(m)
	if !strings.Contains(view, "a1") || strings.Contains(view, "a2") {
		t.Errorf("review after deselecting a2 (scenario 6):\n%s", view)
	}
	if res := pressTable(t, m, "enter").result(); strings.Join(res.Names, ",") != "a1" {
		t.Errorf("names = %v, want [a1]", res.Names)
	}
}

func TestUpdateTable_HelpListsSelectAll(t *testing.T) {
	t.Parallel()

	if view := plainView(driveTable(t, newUpdateTableModel(toggleFixture()), win(160, 40))); !strings.Contains(view, "a all") {
		t.Errorf("footer does not list the select-all key (FR-018):\n%s", view)
	}
}

func typeFilter(t *testing.T, m updateTableModel, q string) updateTableModel {
	t.Helper()
	m = pressTable(t, m, "/")
	for _, r := range q {
		m = pressTable(t, m, string(r))
	}
	return m
}

func TestUpdateTable_FilterKeepsHiddenSelection(t *testing.T) {
	t.Parallel()

	m := driveTable(t, newUpdateTableModel(toggleFixture()), win(160, 40))
	m = pressTable(t, m, "a") // a1, a2, b1
	m = typeFilter(t, m, "b1")
	if got := chosenNames(m); got != "a1,a2,b1" {
		t.Errorf("filtering changed the selection to %q", got)
	}
	view := plainView(m)
	if strings.Contains(view, "a1 ") || !strings.Contains(view, "b1") {
		t.Errorf("filter did not narrow the rows:\n%s", view)
	}
	if !strings.Contains(view, "3 selected (2 hidden)") {
		t.Errorf("footer does not count the hidden selection (FR-017):\n%s", view)
	}
}

func TestUpdateTable_FilterMatchesLabelsAndPaths(t *testing.T) {
	t.Parallel()

	fx := toggleFixture()
	fx.Groups[1].Rows[0].Path = "tools/build"
	m := typeFilter(t, driveTable(t, newUpdateTableModel(fx), win(160, 40)), "acme/a")
	view := plainView(m)
	for _, name := range []string{"a1", "a2", "p1"} {
		if !strings.Contains(view, name) {
			t.Errorf("label match does not show %s (US4 scenario 2):\n%s", name, view)
		}
	}
	if strings.Contains(view, "b1") {
		t.Errorf("label match shows another group:\n%s", view)
	}
	if view := plainView(typeFilter(t, driveTable(t, newUpdateTableModel(fx), win(160, 40)), "tools/b")); !strings.Contains(view, "b1") || strings.Contains(view, "a1 ") {
		t.Errorf("path match does not narrow to b1:\n%s", view)
	}
}

func TestUpdateTable_TogglesRespectTheFilter(t *testing.T) {
	t.Parallel()

	m := typeFilter(t, newUpdateTableModel(toggleFixture()), "a1")
	m = pressTable(t, m, "esc") // leave the input, keep the filter
	m = pressTable(t, m, "a")
	if got := chosenNames(m); got != "a1" {
		t.Errorf("select-all under a filter chose %q, want only the visible a1 (FR-012)", got)
	}
	m = pressTable(t, moveTo(t, m, "g-a"), " ")
	if got := chosenNames(m); got != "" {
		t.Errorf("group toggle under a filter left %q, want the visible a1 cleared", got)
	}
	m = pressTable(t, m, " ", "esc", "enter")
	if !m.reviewing || !strings.Contains(plainView(m), "a1") || strings.Contains(plainView(m), "a2") {
		t.Errorf("review does not show the final selection:\n%s", plainView(m))
	}
}

func TestUpdateTable_CollapseKeepsSelection(t *testing.T) {
	t.Parallel()

	m := driveTable(t, newUpdateTableModel(toggleFixture()), win(160, 40))
	m = pressTable(t, moveTo(t, m, "a2"), " ", "left")
	if m.cursorID() != "g-a" {
		t.Errorf("collapse from a row left the cursor on %q, want its header", m.cursorID())
	}
	view := plainView(m)
	if strings.Contains(view, "a1 ") || !strings.Contains(view, "▸ github.com/acme/a") || !strings.Contains(view, "· 1 selected") {
		t.Errorf("collapsed group should show only its header with counts (US4 scenario 4):\n%s", view)
	}
	if got := chosenNames(m); got != "a2" {
		t.Errorf("collapse changed the selection to %q", got)
	}
	if got := chosenNames(pressTable(t, m, " ")); got != "a1,a2" {
		t.Errorf("header toggle on a collapsed group chose %q, want a1,a2", got)
	}
	m = pressTable(t, m, "right")
	if !strings.Contains(plainView(m), "a1") {
		t.Errorf("expand did not show the rows:\n%s", plainView(m))
	}
	if !pressTable(t, m, "h").collapsedAt("g-a") {
		t.Error("h does not collapse")
	}
	if pressTable(t, pressTable(t, m, "h"), "l").collapsedAt("g-a") {
		t.Error("l does not expand")
	}
}

func TestUpdateTable_HiddenByCollapseIsCounted(t *testing.T) {
	t.Parallel()

	m := pressTable(t, moveTo(t, driveTable(t, newUpdateTableModel(toggleFixture()), win(160, 40)), "g-a"), " ", "left")
	if !strings.Contains(plainView(m), "2 selected (2 hidden)") {
		t.Errorf("footer does not count selection hidden by a collapse:\n%s", plainView(m))
	}
}

func TestUpdateTable_NarrowTerminal(t *testing.T) {
	t.Parallel()

	fx := tableFixture()
	fx.Groups[1].Rows[1].Path = "tools/kubernetes"
	fx.Groups[1].Rows[1].Policy = "branch:release/2026-long"
	m := driveTable(t, newUpdateTableModel(fx), win(60, 40))
	view := m.View()
	for _, line := range strings.Split(view, "\n") {
		if w := ansi.StringWidth(line); w > 60 {
			t.Errorf("line is %d wide at 60 columns: %q", w, line)
		}
	}
	plain := ansi.Strip(view)
	for _, line := range strings.Split(plain, "\n") {
		if strings.Contains(line, "kubernetes  ") {
			if !strings.Contains(line, "update available") {
				t.Errorf("status dropped at 60 columns (US4 scenario 5): %q", line)
			}
			if strings.Contains(line, "tools/kubernetes") || strings.Contains(line, "branch:release") {
				t.Errorf("path and policy should drop first at 60 columns: %q", line)
			}
		}
	}
}

func TestUpdateTable_StickyGroupHeader(t *testing.T) {
	t.Parallel()

	m := driveTable(t, newUpdateTableModel(manyTableGroups(40, 1)), win(80, 14))
	for range 25 {
		m = pressTable(t, m, "down")
	}
	lines := strings.Split(plainView(m), "\n")
	first := ""
	for _, l := range lines[1:] {
		if strings.TrimSpace(l) != "" {
			first = l
			break
		}
	}
	if !strings.Contains(first, "github.com/org/repo-00") {
		t.Errorf("first window line = %q, want the group header kept in place (US4 scenario 6)", first)
	}
	if !strings.Contains(plainView(m), m.cursorID()) {
		t.Errorf("cursor row %q is not on screen", m.cursorID())
	}
}

func TestUpdateTable_EscIsStaged(t *testing.T) {
	t.Parallel()

	m := typeFilter(t, newUpdateTableModel(toggleFixture()), "b")
	m = pressTable(t, m, "esc")
	if m.filtering || m.filter.value != "b" || m.cancelled {
		t.Fatalf("first esc: filtering %v value %q cancelled %v; want the input left, the filter kept", m.filtering, m.filter.value, m.cancelled)
	}
	m = pressTable(t, m, "esc")
	if m.filter.value != "" || m.cancelled {
		t.Fatalf("second esc: value %q cancelled %v; want the filter cleared", m.filter.value, m.cancelled)
	}
	if !pressTable(t, m, "esc").cancelled {
		t.Error("third esc did not cancel")
	}
}

func TestUpdateTable_CursorOnAGroupKey(t *testing.T) {
	t.Parallel()

	fx := toggleFixture()
	fx.Cursor = "g-b"
	if m := newUpdateTableModel(fx); m.cursorID() != "g-b" {
		t.Errorf("cursor = %q, want the g-b header", m.cursorID())
	}
}

func TestUpdateTable_HelpListsFoldAndFilter(t *testing.T) {
	t.Parallel()

	view := plainView(driveTable(t, newUpdateTableModel(toggleFixture()), win(200, 40)))
	for _, want := range []string{"←/→ fold", "/ filter"} {
		if !strings.Contains(view, want) {
			t.Errorf("footer does not list %q (FR-018):\n%s", want, view)
		}
	}
}

// The budgets are wall clock, so the test runs alone and checks the fastest
// of several attempts: machine load only adds time, while a real slowdown is
// slow in every attempt.
//
//nolint:paralleltest // wall-clock budgets; parallel tests in this package would skew them
func TestUpdateTable_ResponsiveAt250SkillsAcross30Repos(t *testing.T) {
	const attempts = 5
	keys := []string{"down", " ", "home", "a", "/", "s", "k", "i", "esc", "left", "right"}
	var opens []time.Duration
	perKey := make([][]time.Duration, len(keys))
	for range attempts {
		start := time.Now()
		m := driveTable(t, newUpdateTableModel(manyTableGroups(250, 30)), win(80, 24))
		_ = m.View()
		opens = append(opens, time.Since(start))
		for i, k := range keys {
			start := time.Now()
			m = pressTable(t, m, k)
			_ = m.View()
			perKey[i] = append(perKey[i], time.Since(start))
		}
	}
	if d := slices.Min(opens); d > 500*time.Millisecond {
		t.Errorf("table took %v to open at 250 skills, SC-001 requires < 500ms", d)
	}
	for i, k := range keys {
		if d := slices.Min(perKey[i]); d > 50*time.Millisecond {
			t.Errorf("key %q took %v at 250 skills, SC-001 requires < 50ms", k, d)
		}
	}
}
