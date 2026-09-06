package ui

import (
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ofelcan/muster/internal/model"
)

var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func visibleWidth(line string) int {
	return len([]rune(ansi.ReplaceAllString(line, "")))
}

func testSnapshot() *model.Snapshot {
	now := time.Now()
	agent := func(pane, name string, st model.Status, task string) model.Agent {
		return model.Agent{
			PaneID: pane, Name: name, Status: st, Task: task,
			TaskSource:  model.TaskFromOrchestrator,
			StatusSince: now.Add(-5 * time.Minute), AgeKnown: true,
		}
	}
	repo := func(slot int, key, display, branch, sigil string, agents ...model.Agent) model.Repo {
		return model.Repo{
			Key: key, Name: key, Display: display, Branch: branch,
			Sigil: sigil, ColorIndex: slot % 8, GridSlot: slot,
			IsGit: true, Agents: agents,
			OtherPanes: []model.Pane{{PaneID: key + ":p9", Label: "vite"}},
		}
	}
	return &model.Snapshot{
		Schema:      model.SchemaVersion,
		GeneratedAt: now,
		Repos: []model.Repo{
			repo(0, "acme/contracts", "contracts", "main", "✦",
				agent("w1:p1", "bump-v3", model.StatusIdle, "cut v3 types")),
			repo(1, "acme/api", "api", "feat/billing-migrations", "◆",
				agent("w2:p1", "migrations", model.StatusBlocked, "add billing schema migration"),
				agent("w2:p2", "tests", model.StatusDone, "integration suite")),
			repo(2, "acme/web", "web", "feat/checkout-ui", "▣",
				agent("w3:p1", "checkout-ui", model.StatusWorking, "parked until api lands")),
			repo(3, "acme/infra", "infra", "main", "⬡"),
		},
		Attention: []model.Attention{
			{Rank: 1, Reason: model.ReasonBlocked, RepoKey: "acme/api", PaneID: "w2:p1",
				Agent: "migrations", Status: model.StatusBlocked, Age: 4 * time.Minute,
				AgeKnown: true, Detail: "Do you want to create abc.txt?"},
			{Rank: 4, Reason: model.ReasonDoneUnseen, RepoKey: "acme/api", PaneID: "w2:p2",
				Agent: "tests", Status: model.StatusDone, Age: 6 * time.Minute,
				AgeKnown: true, Detail: "finished, unseen"},
		},
		Counts: model.Counts{Repos: 4, Agents: 4, NeedsYou: 2},
	}
}

func render(t *testing.T, width int) string {
	t.Helper()
	m := New(testSnapshot(), "")
	out, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 40})
	return out.View()
}

// The overlay is sized to the tab area. A line that runs past it wraps and
// destroys the layout, so nothing may ever exceed the width.
func TestNoLineExceedsTheWidth(t *testing.T) {
	for _, w := range []int{40, 53, 70, 80, 100, 117, 120, 143, 160, 200, 240} {
		for i, line := range strings.Split(render(t, w), "\n") {
			if got := visibleWidth(line); got > w {
				t.Errorf("width %d: line %d is %d wide: %q",
					w, i, got, ansi.ReplaceAllString(line, ""))
			}
		}
	}
}

// Both of the real measured terminals have to work: 143 fullscreen and 53 once
// the window is shrunk.
func TestBreakpoints(t *testing.T) {
	cases := []struct {
		width, cols int
	}{
		{53, 1}, {69, 1}, {70, 2}, {119, 2}, {120, 3}, {143, 3}, {199, 3}, {200, 4},
	}
	for _, c := range cases {
		if got := columnsFor(c.width); got != c.cols {
			t.Errorf("columnsFor(%d) = %d, want %d", c.width, got, c.cols)
		}
	}
}

func TestNarrowCollapsesToOneColumn(t *testing.T) {
	out := render(t, 53)
	// In one column every repo header starts its own line, so all four appear.
	for _, name := range []string{"CONTRACTS", "API", "WEB", "INFRA"} {
		if !strings.Contains(out, name) {
			t.Errorf("narrow layout dropped %s", name)
		}
	}
}

func TestRibbonShowsTheBlockingQuestion(t *testing.T) {
	for _, w := range []int{53, 143} {
		if !strings.Contains(render(t, w), "Do you want to create abc.txt?") {
			t.Errorf("width %d: ribbon lost the blocking question", w)
		}
	}
}

// An empty ribbon is a signal, not a gap to fill.
func TestRibbonVanishesWhenNothingNeedsYou(t *testing.T) {
	snap := testSnapshot()
	snap.Attention = nil
	snap.Counts.NeedsYou = 0
	m := New(snap, "")
	out, _ := m.Update(tea.WindowSizeMsg{Width: 143, Height: 40})
	if strings.Contains(out.View(), "NEEDS YOU") {
		t.Error("the ribbon should disappear entirely when empty")
	}
}

func key(m *Model, s string) {
	var msg tea.KeyMsg
	switch s {
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	case "backspace":
		msg = tea.KeyMsg{Type: tea.KeyBackspace}
	case "slash":
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
	m.Update(msg)
}

func newSized(w int) *Model {
	m := New(testSnapshot(), "")
	m.Update(tea.WindowSizeMsg{Width: w, Height: 40})
	return m
}

func TestDigitJumpsToRibbonRow(t *testing.T) {
	m := newSized(143)
	key(m, "2")
	if m.Jump() != "w2:p2" {
		t.Errorf("2 should jump to the second ribbon row, got %q", m.Jump())
	}
}

func TestDigitBeyondTheRibbonDoesNothing(t *testing.T) {
	m := newSized(143)
	key(m, "9")
	if m.Jump() != "" {
		t.Errorf("there is no ninth row, got %q", m.Jump())
	}
	if m.filtering {
		t.Error("a digit must not start filtering")
	}
}

func TestEnterJumpsToTheSelection(t *testing.T) {
	m := newSized(143)
	key(m, "down")
	want := m.selectedPane()
	key(m, "enter")
	if m.Jump() != want {
		t.Errorf("enter jumped to %q, want %q", m.Jump(), want)
	}
}

func TestSelectionWraps(t *testing.T) {
	m := newSized(143)
	n := len(m.targets)
	if n == 0 {
		t.Fatal("no targets")
	}
	for i := 0; i < n; i++ {
		key(m, "j")
	}
	if m.cursor != 0 {
		t.Errorf("selection should wrap to the top, got %d", m.cursor)
	}
}

// Filtering must not drop the selection somewhere unrelated.
func TestSelectionSurvivesFiltering(t *testing.T) {
	m := newSized(143)
	for m.selectedPane() != "w2:p1" {
		key(m, "j")
	}
	for _, r := range "migr" {
		key(m, string(r))
	}
	if got := m.selectedPane(); got != "w2:p1" {
		t.Errorf("selection moved to %q while filtering", got)
	}
}

func TestBackspaceLeavesFilterModeWhenEmpty(t *testing.T) {
	m := newSized(143)
	key(m, "a")
	key(m, "backspace")
	if m.filtering || m.filter != "" {
		t.Errorf("filtering=%v filter=%q", m.filtering, m.filter)
	}
}

func TestWarningIsShown(t *testing.T) {
	m := New(testSnapshot(), "daemon was restarted")
	out, _ := m.Update(tea.WindowSizeMsg{Width: 143, Height: 40})
	if !strings.Contains(out.View(), "daemon was restarted") {
		t.Error("a degraded daemon should be visible in the header")
	}
}

// Search is entered with "/", following herdr's own convention. That leaves
// every letter free for navigation and actions, which the plan's any-letter
// rule did not.
func TestSlashStartsSearch(t *testing.T) {
	m := newSized(143)
	if m.filtering {
		t.Fatal("should not start in search mode")
	}
	key(m, "slash")
	if !m.filtering {
		t.Fatal("/ should enter search mode")
	}
	for _, r := range "api" {
		key(m, string(r))
	}
	if m.filter != "api" {
		t.Errorf("filter = %q, want api", m.filter)
	}
}

func TestVimKeysNavigateAndNeverType(t *testing.T) {
	m := newSized(143)
	start := m.cursor
	key(m, "j")
	if m.cursor == start {
		t.Error("j should move the selection")
	}
	if m.filtering || m.filter != "" {
		t.Errorf("j must not type: filtering=%v filter=%q", m.filtering, m.filter)
	}
	for _, k := range []string{"h", "k", "l"} {
		key(m, k)
		if m.filter != "" {
			t.Errorf("%s typed into the filter", k)
		}
	}
}

// Once searching, hjkl are literal text again.
func TestVimKeysAreTextWhileSearching(t *testing.T) {
	m := newSized(143)
	key(m, "slash")
	for _, r := range "hjkl" {
		key(m, string(r))
	}
	if m.filter != "hjkl" {
		t.Errorf("filter = %q, want hjkl", m.filter)
	}
}

// Escape leaves the typing mode but keeps the results, so you can navigate what
// you just searched for. A second escape clears.
func TestEscapeLeavesSearchModeThenClears(t *testing.T) {
	m := newSized(143)
	key(m, "slash")
	for _, r := range "api" {
		key(m, string(r))
	}
	key(m, "esc")
	if m.filtering {
		t.Error("escape should leave typing mode")
	}
	if m.filter != "api" {
		t.Errorf("escape should keep the results, filter = %q", m.filter)
	}
	key(m, "esc")
	if m.filter != "" {
		t.Errorf("a second escape should clear, filter = %q", m.filter)
	}
	if m.quit {
		t.Error("clearing must not also close")
	}
	key(m, "esc")
	if !m.quit {
		t.Error("a third escape should close")
	}
}

// The bug: searching for a repo that has no agents found nothing, because the
// filter only kept repos with matching agents.
func TestSearchFindsReposWithNoAgents(t *testing.T) {
	m := newSized(143)
	key(m, "slash")
	for _, r := range "infra" {
		key(m, string(r))
	}
	repos := m.visibleRepos()
	if len(repos) != 1 || repos[0].Display != "infra" {
		t.Fatalf("searching for an agent-less repo found %d repos: %+v", len(repos), repos)
	}
	if len(m.targets) == 0 {
		t.Error("the matched repo should be selectable")
	}
}

func TestSearchMatchesBranchAndTask(t *testing.T) {
	for _, q := range []string{"billing", "checkout", "migration"} {
		m := newSized(143)
		key(m, "slash")
		for _, r := range q {
			key(m, string(r))
		}
		if len(m.visibleRepos()) == 0 {
			t.Errorf("search %q matched nothing", q)
		}
	}
	m := newSized(143)
	key(m, "slash")
	for _, r := range "zzzznope" {
		key(m, string(r))
	}
	if len(m.visibleRepos()) != 0 {
		t.Error("expected no matches")
	}
}

// The bug: with two agents across four repos, only the agents were reachable,
// so the arrow keys appeared to move between two things and stop.
func TestEveryRepoIsReachable(t *testing.T) {
	m := newSized(143)
	seen := map[string]bool{}
	for i := 0; i < len(m.targets); i++ {
		seen[m.targets[i].repoKey] = true
	}
	for _, r := range m.snap.Repos {
		if !seen[r.Key] {
			t.Errorf("repo %s cannot be reached by the cursor", r.Key)
		}
	}
}

func TestAgentlessRepoIsSelectableButDoesNotJump(t *testing.T) {
	m := newSized(143)
	for i := range m.targets {
		if m.targets[i].paneID == "" {
			m.cursor = i
			key(m, "enter")
			if m.Jump() != "" {
				t.Error("a repo card with no agents has nowhere to jump to")
			}
			return
		}
	}
	t.Fatal("no agent-less repo target found")
}

func TestSortCycles(t *testing.T) {
	m := newSized(143)
	if m.sort != SortFirstSeen {
		t.Fatal("first seen should be the default")
	}
	key(m, "s")
	if m.sort == SortFirstSeen {
		t.Error("s should change the sort")
	}
	// One press already happened, so this completes exactly one full cycle.
	for i := 1; i < int(sortModeCount); i++ {
		key(m, "s")
	}
	if m.sort != SortFirstSeen {
		t.Errorf("sort should cycle back round, got %v", m.sort)
	}
}

func TestAlphabeticalSort(t *testing.T) {
	m := newSized(143)
	m.sort = SortAlphabetical
	repos := m.orderedRepos(m.visibleRepos())
	for i := 1; i < len(repos); i++ {
		if repos[i-1].Display > repos[i].Display {
			t.Errorf("not alphabetical: %s before %s", repos[i-1].Display, repos[i].Display)
		}
	}
}

func TestManualReorderMovesARepo(t *testing.T) {
	m := newSized(143)
	before := m.currentOrder()
	if len(before) < 2 {
		t.Skip("need two repos")
	}
	// Select the second repo's first target, then move it up.
	for i, tg := range m.targets {
		if tg.repoKey == before[1] {
			m.cursor = i
			break
		}
	}
	key(m, "K")
	after := m.currentOrder()
	if after[0] != before[1] {
		t.Errorf("expected %s to move to the front, order is %v", before[1], after)
	}
}

func TestMouseClickSelectsThenJumps(t *testing.T) {
	m := newSized(143)
	m.View() // populate row positions
	// Pick a row that is not already selected, so the first click has to move
	// the cursor rather than counting as a click on the selection.
	var row, want int
	found := false
	for y, idx := range m.rowOf {
		if m.targets[idx].paneID != "" && idx != m.cursor {
			row, want, found = y, idx, true
			break
		}
	}
	if !found {
		t.Skip("no clickable unselected agent row")
	}
	click := tea.MouseMsg{Y: row, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft}
	m.Update(click)
	if m.cursor != want {
		t.Fatalf("click selected %d, want %d", m.cursor, want)
	}
	if m.Jump() != "" {
		t.Fatal("the first click should select, not jump")
	}
	m.Update(click)
	if m.Jump() != m.targets[want].paneID {
		t.Errorf("clicking the selection should jump, got %q", m.Jump())
	}
}

func TestMouseWheelMovesSelection(t *testing.T) {
	m := newSized(143)
	start := m.cursor
	m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
	if m.cursor == start {
		t.Error("wheel down should move the selection")
	}
}
