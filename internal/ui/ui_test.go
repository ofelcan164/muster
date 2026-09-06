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

// The mode model that resolves the plan's second contradiction. Vim keys work
// only while the filter is empty; any other letter enters filter mode, and once
// there hjkl are literal text so no letter is reserved.
func TestLetterStartsFilteringAndVimKeysBecomeText(t *testing.T) {
	m := newSized(143)
	key(m, "m")
	if !m.filtering || m.filter != "m" {
		t.Fatalf("a non-vim letter should start filtering, got filtering=%v filter=%q",
			m.filtering, m.filter)
	}
	key(m, "j")
	key(m, "k")
	if m.filter != "mjk" {
		t.Errorf("hjkl must be literal once filtering, got %q", m.filter)
	}
}

func TestEscapeClearsFilterThenCloses(t *testing.T) {
	m := newSized(143)
	key(m, "m")
	key(m, "i")
	if m.filter != "mi" {
		t.Fatalf("filter = %q", m.filter)
	}
	key(m, "esc")
	if m.filtering || m.filter != "" {
		t.Fatalf("escape should leave filter mode, got filtering=%v filter=%q",
			m.filtering, m.filter)
	}
	if m.quit {
		t.Fatal("escape must not close on the same press that clears the filter")
	}
	key(m, "esc")
	if !m.quit {
		t.Error("a second escape should close")
	}
}

func TestVimKeysWorkOnlyWhenTheFilterIsEmpty(t *testing.T) {
	m := newSized(143)
	start := m.cursor
	key(m, "j")
	if m.cursor == start {
		t.Fatal("with an empty filter, j should move the selection")
	}
	if m.filtering {
		t.Fatal("j must not start filtering while the filter is empty")
	}

	// Now enter filter mode. j is text from here.
	//
	// The cursor index is not a useful thing to assert on inside filter mode,
	// because the target list itself changes as the query narrows. What matters
	// is that the keystroke landed in the query rather than moving anything.
	key(m, "m")
	key(m, "j")
	if m.filter != "mj" {
		t.Errorf("filter = %q, want mj", m.filter)
	}

	// Escape gives the vim keys back.
	key(m, "esc")
	before := m.cursor
	key(m, "j")
	if m.cursor == before {
		t.Error("escape should restore vim navigation")
	}
}

func TestFilterMatchesAgentTaskRepoAndBranch(t *testing.T) {
	for _, q := range []string{"migrations", "billing", "web", "ui"} {
		m := newSized(143)
		for _, r := range q {
			key(m, string(r))
		}
		if len(m.targets) == 0 {
			t.Errorf("filter %q matched nothing", q)
		}
	}
	m := newSized(143)
	for _, r := range "zzzznope" {
		key(m, string(r))
	}
	if len(m.targets) != 0 {
		t.Errorf("filter should have matched nothing, got %d targets", len(m.targets))
	}
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
