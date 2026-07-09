package tui

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
)

func key(s string) tea.KeyMsg {
	switch s {
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func win(w, h int) tea.WindowSizeMsg { return tea.WindowSizeMsg{Width: w, Height: h} }

// update drives the model with a sequence of messages and returns the resulting
// selectorModel.
func update(t *testing.T, m tea.Model, msgs ...tea.Msg) selectorModel {
	t.Helper()
	for _, msg := range msgs {
		m, _ = m.Update(msg)
	}
	sm, ok := m.(selectorModel)
	if !ok {
		t.Fatalf("model is not selectorModel: %T", m)
	}
	return sm
}

func send(t *testing.T, m tea.Model, keys ...string) selectorModel {
	t.Helper()
	msgs := make([]tea.Msg, len(keys))
	for i, k := range keys {
		msgs[i] = key(k)
	}
	return update(t, m, msgs...)
}

func items() []SkillItem {
	return []SkillItem{
		{ID: "code-review", RepoPath: "skills/code-review", Valid: true},
		{ID: "writing", RepoPath: "skills/writing", Valid: true},
		{ID: "broken", RepoPath: "skills/broken", Valid: false},
	}
}

func manyItems(n int) []SkillItem {
	out := make([]SkillItem, n)
	for i := range out {
		out[i] = SkillItem{
			ID:       fmt.Sprintf("skill-%03d", i),
			RepoPath: fmt.Sprintf("skills/skill-%03d", i),
			Valid:    true,
		}
	}
	return out
}

// countRows counts rendered skill rows (those carrying a checkbox marker).
func countRows(view string) int {
	n := 0
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "[ ]") || strings.Contains(line, "[x]") || strings.Contains(line, "[-]") {
			n++
		}
	}
	return n
}

func downs(n int) []tea.Msg {
	msgs := make([]tea.Msg, n)
	for i := range msgs {
		msgs[i] = key("down")
	}
	return msgs
}

// --- Existing regression coverage (RG-1) ---------------------------------

func TestSelector_ToggleAndConfirm(t *testing.T) {
	t.Parallel()

	// Toggle first item, move down, toggle second, confirm.
	m := send(t, newSelectorModel(items()), " ", "down", " ", "enter")
	if !m.done {
		t.Fatal("expected done after enter")
	}
	got := m.chosenIndices()
	if len(got) != 2 || got[0] != 0 || got[1] != 1 {
		t.Errorf("chosen = %v, want [0 1]", got)
	}
}

func TestSelector_InvalidNotSelectable(t *testing.T) {
	t.Parallel()

	// Move to the invalid item (index 2) and try to toggle it.
	m := send(t, newSelectorModel(items()), "down", "down", " ", "enter")
	if len(m.chosenIndices()) != 0 {
		t.Errorf("invalid item must not be selectable, chose %v", m.chosenIndices())
	}
}

func TestSelector_Cancel(t *testing.T) {
	t.Parallel()

	m := send(t, newSelectorModel(items()), " ", "esc")
	if !m.cancelled {
		t.Error("expected cancelled after esc")
	}
}

func TestSelector_DuplicateBothSelectable(t *testing.T) {
	t.Parallel()

	// Two skills with the same id but different paths — both valid, both shown,
	// both individually selectable (FR-024 interactive branch).
	dups := []SkillItem{
		{ID: "shared", RepoPath: "skills/a/shared", Valid: true},
		{ID: "shared", RepoPath: "skills/b/shared", Valid: true},
	}
	m := send(t, newSelectorModel(dups), " ", "down", " ", "enter")
	if got := m.chosenIndices(); len(got) != 2 {
		t.Errorf("both duplicate paths should be selectable, chose %v", got)
	}
}

// --- US1: viewport & scrolling -------------------------------------------

func TestSelector_ViewportBounded(t *testing.T) {
	t.Parallel()

	m := update(t, newSelectorModel(manyItems(100)), win(80, 24))
	ps := m.pageSize()
	if got := countRows(m.View()); got > ps {
		t.Errorf("rendered %d rows, want <= pageSize %d", got, ps)
	}
	if got := countRows(m.View()); got == 100 {
		t.Errorf("rendered entire list (%d), viewport not bounded", got)
	}
}

func TestSelector_ScrollKeepsCursorVisible(t *testing.T) {
	t.Parallel()

	m := update(t, newSelectorModel(manyItems(100)), win(80, 12))
	ps := m.pageSize()
	m = update(t, m, downs(ps+3)...)
	if m.cursor < m.offset || m.cursor >= m.offset+ps {
		t.Errorf("cursor %d not within window [%d,%d)", m.cursor, m.offset, m.offset+ps)
	}
	if m.offset == 0 {
		t.Error("expected viewport to scroll, offset still 0")
	}
}

func TestSelector_ReachAndToggleBelowFold(t *testing.T) {
	t.Parallel()

	m := update(t, newSelectorModel(manyItems(100)), win(80, 12))
	msgs := append(downs(50), key(" "), key("enter"))
	m = update(t, m, msgs...)
	got := m.chosenIndices()
	if len(got) != 1 || got[0] != 50 {
		t.Errorf("chosen = %v, want [50]", got)
	}
}

func TestSelector_ResizePreservesSelection(t *testing.T) {
	t.Parallel()

	m := update(t, newSelectorModel(manyItems(40)), win(80, 20))
	m = update(t, m, append(downs(10), key(" "))...)
	// Shrink the terminal.
	m = update(t, m, win(80, 8))
	ps := m.pageSize()
	if m.cursor < m.offset || m.cursor >= m.offset+ps {
		t.Errorf("after resize cursor %d not visible in [%d,%d)", m.cursor, m.offset, m.offset+ps)
	}
	m = update(t, m, key("enter"))
	if got := m.chosenIndices(); len(got) != 1 || got[0] != 10 {
		t.Errorf("selection lost on resize: %v", got)
	}
}

func TestSelector_ShortTerminal(t *testing.T) {
	t.Parallel()

	m := update(t, newSelectorModel(manyItems(20)), win(80, 3))
	if m.pageSize() < 1 {
		t.Fatalf("pageSize = %d, want >= 1", m.pageSize())
	}
	if countRows(m.View()) < 1 {
		t.Error("no navigable row at height 3")
	}
	m2 := update(t, m, key("down"), key("down"))
	if m2.cursor == 0 {
		t.Error("cursor did not move in short terminal")
	}
}

// --- US2: filtering -------------------------------------------------------

func TestSelector_FilterNarrows(t *testing.T) {
	t.Parallel()

	its := []SkillItem{
		{ID: "alpha", RepoPath: "skills/alpha", Valid: true},
		{ID: "beta", RepoPath: "skills/beta", Valid: true},
		{ID: "alphabet", RepoPath: "skills/alphabet", Valid: true},
	}
	m := update(t, newSelectorModel(its), key("/"), key("a"), key("l"), key("p"))
	if len(m.visible) != 2 {
		t.Fatalf("visible = %v, want 2 (alpha, alphabet)", m.visible)
	}
	if m.visible[0] != 0 || m.visible[1] != 2 {
		t.Errorf("visible = %v, want [0 2]", m.visible)
	}
}

func TestSelector_FilterPreservesSelections(t *testing.T) {
	t.Parallel()

	its := []SkillItem{
		{ID: "alpha", RepoPath: "skills/alpha", Valid: true},
		{ID: "beta", RepoPath: "skills/beta", Valid: true},
	}
	// Toggle alpha (index 0), filter to "beta" (hides alpha), clear, confirm.
	m := update(t, newSelectorModel(its),
		key(" "),
		key("/"), key("b"), key("e"), key("t"), key("a"),
	)
	if len(m.visible) != 1 || m.visible[0] != 1 {
		t.Fatalf("filtered visible = %v, want [1]", m.visible)
	}
	m = update(t, m, key("esc"))   // clear filter
	m = update(t, m, key("enter")) // confirm
	if got := m.chosenIndices(); len(got) != 1 || got[0] != 0 {
		t.Errorf("selection not preserved across filter: %v", got)
	}
}

func TestSelector_ToggleWhileFiltered(t *testing.T) {
	t.Parallel()

	its := []SkillItem{
		{ID: "alpha", RepoPath: "skills/alpha", Valid: true},
		{ID: "beta", RepoPath: "skills/beta", Valid: true},
		{ID: "gamma", RepoPath: "skills/gamma", Valid: true},
	}
	// Filter to "beta", commit the filter with esc (query retained), then toggle
	// the matching item in place and confirm — without clearing the filter.
	m := update(t, newSelectorModel(its),
		key("/"), key("b"), key("e"), key("t"), key("a"),
		key("esc"),
	)
	if m.filtering {
		t.Fatal("esc should unfocus the filter input")
	}
	if len(m.visible) != 1 || m.visible[0] != 1 {
		t.Fatalf("filter not retained after esc: visible=%v", m.visible)
	}
	m = update(t, m, key(" "), key("enter"))
	if got := m.chosenIndices(); len(got) != 1 || got[0] != 1 {
		t.Errorf("toggle-while-filtered failed: got %v, want [1]", got)
	}
}

func TestSelector_EscIsTwoStageThenCancels(t *testing.T) {
	t.Parallel()

	its := []SkillItem{
		{ID: "alpha", RepoPath: "p/alpha", Valid: true},
		{ID: "beta", RepoPath: "p/beta", Valid: true},
	}
	m := update(t, newSelectorModel(its), key("/"), key("a"))
	// esc #1: unfocus, keep the query.
	m = update(t, m, key("esc"))
	if m.filtering || m.filter.value != "a" {
		t.Fatalf("first esc: filtering=%v query=%q, want false/\"a\"", m.filtering, m.filter.value)
	}
	if m.cancelled {
		t.Fatal("first esc must not cancel")
	}
	// esc #2: clear the query, restore the full list.
	m = update(t, m, key("esc"))
	if m.filter.value != "" || m.cancelled {
		t.Fatalf("second esc: query=%q cancelled=%v, want \"\"/false", m.filter.value, m.cancelled)
	}
	if len(m.visible) != 2 {
		t.Errorf("cleared filter should restore full list: visible=%v", m.visible)
	}
	// esc #3: cancel.
	m = update(t, m, key("esc"))
	if !m.cancelled {
		t.Error("third esc should cancel")
	}
}

func TestSelector_FilterNoMatches(t *testing.T) {
	t.Parallel()

	its := []SkillItem{{ID: "alpha", RepoPath: "skills/alpha", Valid: true}}
	m := update(t, newSelectorModel(its), key("/"), key("z"), key("z"), key("z"))
	if len(m.visible) != 0 {
		t.Fatalf("visible = %d, want 0", len(m.visible))
	}
	if !strings.Contains(m.View(), "no matches") {
		t.Errorf("view missing 'no matches':\n%s", m.View())
	}
}

func TestSelector_EmptyDiscoveryMessage(t *testing.T) {
	t.Parallel()

	// An empty list (no discovered skills) reads differently from a filter that
	// matches nothing. The picker is guarded upstream for zero skills, but the
	// message must still be accurate if reached.
	m := update(t, newSelectorModel(nil), win(80, 24))
	if !strings.Contains(m.View(), "no skills discovered") {
		t.Errorf("empty list should say 'no skills discovered':\n%s", m.View())
	}
}

func TestSelector_FilterTypingDoesNotNavigateOrCancel(t *testing.T) {
	t.Parallel()

	its := []SkillItem{
		{ID: "jjj", RepoPath: "p/jjj", Valid: true},
		{ID: "kkk", RepoPath: "p/kkk", Valid: true},
		{ID: "qqq", RepoPath: "p/qqq", Valid: true},
	}
	m := update(t, newSelectorModel(its), key("/"), key("j"))
	if m.cancelled {
		t.Error("'j' while filtering should not cancel")
	}
	if m.filter.value != "j" {
		t.Errorf("query = %q, want %q", m.filter.value, "j")
	}
	m = update(t, m, key("q"))
	if m.cancelled {
		t.Error("'q' while filtering must not cancel")
	}
	if m.filter.value != "jq" {
		t.Errorf("query = %q, want %q", m.filter.value, "jq")
	}
}

// --- US3: indicators, help, invalid marking ------------------------------

func TestSelector_PositionAndMoreIndicators(t *testing.T) {
	t.Parallel()

	m := update(t, newSelectorModel(manyItems(50)), win(80, 12))
	v := m.View()
	if !strings.Contains(v, "↓ more") {
		t.Errorf("expected '↓ more' indicator:\n%s", v)
	}
	if !strings.Contains(v, "/50") {
		t.Errorf("expected position '/50' counter:\n%s", v)
	}
	m = update(t, m, downs(20)...)
	if !strings.Contains(m.View(), "↑ more") {
		t.Errorf("expected '↑ more' after scrolling:\n%s", m.View())
	}
}

func TestSelector_HelpAndInvalidMarking(t *testing.T) {
	t.Parallel()

	its := []SkillItem{
		{ID: "ok", RepoPath: "p/ok", Valid: true},
		{ID: "bad", RepoPath: "p/bad", Valid: false},
	}
	m := update(t, newSelectorModel(its), win(80, 24))
	v := m.View()
	if !strings.Contains(v, "[✗]") || !strings.Contains(v, "invalid") {
		t.Errorf("invalid item not marked:\n%s", v)
	}
	for _, ctrl := range []string{"toggle", "confirm", "filter"} {
		if !strings.Contains(v, ctrl) {
			t.Errorf("help footer missing %q:\n%s", ctrl, v)
		}
	}
	m = update(t, m, key("down"), key(" "), key("enter"))
	if len(m.chosenIndices()) != 0 {
		t.Errorf("invalid item selectable: %v", m.chosenIndices())
	}
}

func TestSelector_SpaceTogglesWhileFilterFocused(t *testing.T) {
	t.Parallel()

	its := []SkillItem{
		{ID: "alpha", RepoPath: "skills/alpha", Valid: true},
		{ID: "beta", RepoPath: "skills/beta", Valid: true},
		{ID: "gamma", RepoPath: "skills/gamma", Valid: true},
	}
	// Filter to "beta" and — with the filter input still focused — toggle with
	// space, exactly as the keyboard contract promises ("space toggle" on every
	// step, no esc needed first).
	m := update(t, newSelectorModel(its),
		key("/"), key("b"), key("e"), key("t"),
		key(" "),
	)
	if m.filter.value != "bet" {
		t.Errorf("space leaked into the filter query: %q", m.filter.value)
	}
	m = update(t, m, key("enter"))
	if got := m.chosenIndices(); len(got) != 1 || got[0] != 1 {
		t.Errorf("space while filtering chose %v, want [1] (beta)", got)
	}
}

func TestSelector_PositionShowsSelectedCount(t *testing.T) {
	t.Parallel()

	its := []SkillItem{
		{ID: "alpha", RepoPath: "skills/alpha", Valid: true},
		{ID: "beta", RepoPath: "skills/beta", Valid: true},
		{ID: "gamma", RepoPath: "skills/gamma", Valid: true},
	}
	// Toggle one item, then filter it out of view: the footer must still say
	// how many are selected, so a filtered list never hides the running count.
	m := update(t, newSelectorModel(its), win(80, 24), key(" "))
	if v := m.View(); !strings.Contains(v, "1 selected") {
		t.Errorf("footer missing selected count after toggle:\n%s", v)
	}
	m = update(t, m, key("/"), key("g"), key("a"), key("m"))
	if v := m.View(); !strings.Contains(v, "1 selected") {
		t.Errorf("footer lost selected count while filtered:\n%s", v)
	}
}

func TestSelector_AlignedColumnsAndCheckboxes(t *testing.T) {
	t.Parallel()
	its := []SkillItem{
		{ID: "a", RepoPath: "skills/a", Description: "Alpha skill", Valid: true},
		{ID: "long-name", RepoPath: "skills/b", Description: "Beta", Valid: true},
		{ID: "broken", RepoPath: "skills/c", Valid: false, InvalidReason: "no description"},
	}
	m := update(t, newSelectorModel(its), win(80, 24), key(" "))
	v := m.View()
	if !strings.Contains(v, "[✓]") {
		t.Errorf("chosen row must render a ✓ checkbox:\n%s", v)
	}
	if !strings.Contains(v, "[✗]") {
		t.Errorf("invalid row must render ✗:\n%s", v)
	}
	// Column alignment: the description column starts at the same offset on
	// every row (name column padded to the longest name).
	var offsets []int
	for _, line := range strings.Split(v, "\n") {
		if i := strings.Index(line, "Alpha skill"); i >= 0 {
			offsets = append(offsets, utf8.RuneCountInString(line[:i]))
		}
		if i := strings.Index(line, "Beta"); i >= 0 {
			offsets = append(offsets, utf8.RuneCountInString(line[:i]))
		}
	}
	if len(offsets) == 2 && offsets[0] != offsets[1] {
		t.Errorf("description column not aligned: %v\n%s", offsets, v)
	}
}

func TestSelector_OverlongIDTruncatedKeepsAlignment(t *testing.T) {
	t.Parallel()
	its := []SkillItem{
		{ID: strings.Repeat("x", 30), RepoPath: "skills/x", Description: "Long skill", Valid: true},
		{ID: "short", RepoPath: "skills/s", Description: "Tiny", Valid: true},
	}
	m := update(t, newSelectorModel(its), win(120, 24))
	v := m.View()
	if strings.Contains(v, strings.Repeat("x", 30)) {
		t.Errorf("an over-long ID must be truncated to the name column:\n%s", v)
	}
	var offsets []int
	for _, line := range strings.Split(v, "\n") {
		for _, desc := range []string{"Long skill", "Tiny"} {
			if i := strings.Index(line, desc); i >= 0 {
				offsets = append(offsets, utf8.RuneCountInString(line[:i]))
			}
		}
	}
	if len(offsets) != 2 || offsets[0] != offsets[1] {
		t.Errorf("description column not aligned with an over-long ID: %v\n%s", offsets, v)
	}
}

func TestSelector_InvalidRowShowsDescription(t *testing.T) {
	t.Parallel()
	its := []SkillItem{
		{ID: "bad", RepoPath: "p/bad", Description: "half-written helper", Valid: false},
	}
	m := update(t, newSelectorModel(its), win(120, 24))
	if v := m.View(); !strings.Contains(v, "half-written helper") {
		t.Errorf("invalid rows must keep their description (FR-009):\n%s", v)
	}
}

// --- Prefix-first filtering ------------------------------------------------

// TestSelector_FilterPrefixWins: when any skill name starts with the query,
// only prefix matches show — substring hits in other fields are suppressed,
// so typing "s" narrows to s-* names instead of everything containing an s.
func TestSelector_FilterPrefixWins(t *testing.T) {
	t.Parallel()

	its := []SkillItem{
		{ID: "s3-sync", RepoPath: "skills/s3-sync", Description: "Syncs buckets", Valid: true},
		{ID: "ssh-agent", RepoPath: "skills/ssh-agent", Description: "Manages keys", Valid: true},
		{ID: "deploy", RepoPath: "skills/deploy", Description: "ships services", Valid: true},
	}
	m := update(t, newSelectorModel(its), key("/"), key("s"))
	if len(m.visible) != 2 || m.visible[0] != 0 || m.visible[1] != 1 {
		t.Fatalf("query 's': visible = %v, want [0 1] (prefix matches only)", m.visible)
	}
	m = update(t, m, key("s"))
	if len(m.visible) != 1 || m.visible[0] != 1 {
		t.Fatalf("query 'ss': visible = %v, want [1] (ssh-agent)", m.visible)
	}
}

// TestSelector_FilterPrefixBeatsSubstringInName: a name merely containing the
// query does not dilute the prefix matches.
func TestSelector_FilterPrefixBeatsSubstringInName(t *testing.T) {
	t.Parallel()

	its := []SkillItem{
		{ID: "py-lint", RepoPath: "skills/py-lint", Valid: true},
		{ID: "numpy-helper", RepoPath: "skills/numpy-helper", Valid: true},
	}
	m := update(t, newSelectorModel(its), key("/"), key("p"), key("y"))
	if len(m.visible) != 1 || m.visible[0] != 0 {
		t.Fatalf("query 'py': visible = %v, want [0] (py-lint only)", m.visible)
	}
}

// TestSelector_FilterFallsBackToSubstring: a query no name starts with still
// searches name/path/description as before (FR-010 description search).
func TestSelector_FilterFallsBackToSubstring(t *testing.T) {
	t.Parallel()

	its := []SkillItem{
		{ID: "alpha", RepoPath: "skills/alpha", Description: "reviews pull requests", Valid: true},
		{ID: "beta", RepoPath: "skills/beta", Description: "debugs kubernetes pods", Valid: true},
	}
	m := update(t, newSelectorModel(its), key("/"))
	for _, r := range "kubernetes" {
		m = update(t, m, key(string(r)))
	}
	if len(m.visible) != 1 || m.visible[0] != 1 {
		t.Fatalf("query 'kubernetes': visible = %v, want [1] (description fallback)", m.visible)
	}
}
