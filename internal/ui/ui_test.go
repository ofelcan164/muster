package ui

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/ofelcan164/muster/internal/model"
	"github.com/ofelcan164/muster/internal/state"
)

var escapes = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// lipgloss strips styling when it cannot detect a colour-capable terminal,
// which a test binary never has. Forcing the profile is what lets these tests
// assert on what a real pane would actually show.
func init() { lipgloss.SetColorProfile(termenv.TrueColor) }

func visibleWidth(line string) int {
	return len([]rune(escapes.ReplaceAllString(line, "")))
}

// testSnapshot's four workspaces: w1 contracts (one agent), w2 api (two
// agents), w3 web (one agent), w9 infra (no agent at all, so it draws as an
// empty tile). Each workspace also holds a "vite" pane, which is what exercises
// the shared "N agents · M panes" footer and the empty tile's own line.
func testSnapshot() *model.Snapshot {
	now := time.Now()
	agent := func(pane, ws, name string, st model.Status, task string) model.Agent {
		return model.Agent{
			PaneID: pane, WorkspaceID: ws, Name: name, Status: st, Task: task,
			TaskSource:  model.TaskFromOrchestrator,
			StatusSince: now.Add(-5 * time.Minute), AgeKnown: true,
		}
	}
	repo := func(slot int, key, display, branch, sigil, ws string, agents ...model.Agent) model.Repo {
		return model.Repo{
			Key: key, Name: key, Display: display, Branch: branch,
			Sigil: sigil, ColorIndex: slot % 8, GridSlot: slot,
			IsGit: true, Agents: agents, WorkspaceIDs: []string{ws},
			OtherPanes: []model.Pane{{PaneID: key + ":p9", WorkspaceID: ws, Label: "vite"}},
		}
	}
	return &model.Snapshot{
		GeneratedAt: now,
		Workspaces: []model.Workspace{
			{ID: "w1", Number: 1, Label: "contracts"},
			{ID: "w2", Number: 2, Label: "api"},
			{ID: "w3", Number: 3, Label: "web"},
			{ID: "w9", Number: 4, Label: "infra"},
		},
		Repos: []model.Repo{
			repo(0, "acme/contracts", "contracts", "main", "✦", "w1",
				agent("w1:p1", "w1", "bump-v3", model.StatusIdle, "cut v3 types")),
			repo(1, "acme/api", "api", "feat/billing-migrations", "◆", "w2",
				agent("w2:p1", "w2", "migrations", model.StatusBlocked, "add billing schema migration"),
				agent("w2:p2", "w2", "tests", model.StatusDone, "check the test suite")),
			repo(2, "acme/web", "web", "feat/checkout-ui", "▣", "w3",
				agent("w3:p1", "w3", "checkout-ui", model.StatusWorking, "parked until api lands")),
			repo(3, "acme/infra", "infra", "main", "⬡", "w9"),
		},
		Attention: []model.Attention{
			{Rank: 1, Reason: model.ReasonBlocked, RepoKey: "acme/api", PaneID: "w2:p1",
				Agent: "migrations", Status: model.StatusBlocked, Age: 4 * time.Minute,
				AgeKnown: true, Detail: "Do you want to create abc.txt?"},
			{Rank: 4, Reason: model.ReasonDoneUnseen, RepoKey: "acme/api", PaneID: "w2:p2",
				Agent: "tests", Status: model.StatusDone, Age: 6 * time.Minute,
				AgeKnown: true, Detail: "finished, unseen"},
		},
		Counts: model.Counts{Repos: 4, Workspaces: 4, Agents: 4, NeedsYou: 2},
	}
}

func render(t *testing.T, width int) string {
	t.Helper()
	m := New(testSnapshot(), "")
	out, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 40})
	return out.View()
}

// The popup is a fixed size. A line that runs past it wraps and destroys the
// layout, so nothing may ever exceed the width.
func TestNoLineExceedsTheWidth(t *testing.T) {
	for _, w := range []int{40, 53, 70, 80, 100, 117, 120, 143, 160, 200, 240} {
		for i, line := range strings.Split(render(t, w), "\n") {
			if got := visibleWidth(line); got > w {
				t.Errorf("width %d: line %d is %d wide: %q",
					w, i, got, escapes.ReplaceAllString(line, ""))
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
	// In one column every tile starts its own line, so all four repos and the
	// empty workspace's own label appear.
	for _, name := range []string{"contracts", "api", "web", "infra"} {
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
	// Run whatever the key handed off and feed the result back, the way the
	// program would, so a send or a mark lands its notice before the test looks.
	if _, cmd := m.Update(msg); cmd != nil {
		m.Update(cmd())
	}
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

// Down wraps within its own column, never sideways into the next one: that is
// what left and right are for.
func TestSelectionWrapsInsideItsColumn(t *testing.T) {
	m := newSized(143)
	if len(m.targets) == 0 {
		t.Fatal("no targets")
	}
	// Start inside the grid: the lane is a column, which is what wrapping has
	// to respect.
	m.cursor = m.targetIndex("pane:w3:p1") // the only card in the last column
	start := m.cursor
	n := len(m.lane())
	cols := map[int]bool{}
	for i := 0; i < n; i++ {
		key(m, "j")
		if tg := m.targets[m.cursor]; tg.kind == kindGrid {
			cols[tg.column] = true
		}
	}
	if m.cursor != start {
		t.Errorf("a full lap should come back to %d, got %d", start, m.cursor)
	}
	if len(cols) > 1 {
		t.Errorf("down wandered across columns %v", cols)
	}
}

// The overlay opens with nothing highlighted. A card lit up before you touched
// anything reads as a claim about which one matters, which it is not.
func TestNothingIsSelectedUntilYouMove(t *testing.T) {
	m := newSized(143)
	if m.cursor != noSelection {
		t.Fatalf("opened with target %d selected", m.cursor)
	}
	order := m.lane()
	key(m, "k")
	if want := order[len(order)-1]; m.cursor != want {
		t.Errorf("the first up should land on the bottom target %d, got %d", want, m.cursor)
	}
	m.cursor = noSelection
	key(m, "j")
	if want := order[0]; m.cursor != want {
		t.Errorf("the first down should land on the top target %d, got %d", want, m.cursor)
	}
}

// Down means the card below. The grid is filled row-major, so stepping to the
// next target in the list moved sideways instead.
func TestDownMovesDownTheColumn(t *testing.T) {
	m := newSized(143)
	if columnsFor(m.width) < 2 {
		t.Skip("one column: down and next are the same move")
	}
	start := -1
	for i, tg := range m.targets {
		if tg.kind == kindGrid && tg.column == 0 && tg.row == 0 {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatal("no card in the top-left cell")
	}
	m.cursor = start
	// One tile is one cell now, so a single down is enough to leave it.
	key(m, "j")
	if got := m.targets[m.cursor]; got.column != 0 || got.row != 1 {
		t.Errorf("down from the top-left card landed in column %d row %d, want 0,1",
			got.column, got.row)
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
// filter only kept repos with matching agents. Now it is an empty workspace
// tile the same search has to find.
func TestSearchFindsWorkspacesWithNoAgent(t *testing.T) {
	m := newSized(143)
	key(m, "slash")
	for _, r := range "infra" {
		key(m, string(r))
	}
	tiles := m.visibleTiles()
	if len(tiles) != 1 || tiles[0].isAgent() || tiles[0].Workspace.Label != "infra" {
		t.Fatalf("searching for an agent-less workspace found %d tiles: %+v", len(tiles), tiles)
	}
	if len(m.targets) == 0 {
		t.Error("the matched workspace should be selectable")
	}
}

func TestSearchMatchesBranchAndTask(t *testing.T) {
	for _, q := range []string{"billing", "checkout", "migration"} {
		m := newSized(143)
		key(m, "slash")
		for _, r := range q {
			key(m, string(r))
		}
		if len(m.visibleTiles()) == 0 {
			t.Errorf("search %q matched nothing", q)
		}
	}
	m := newSized(143)
	key(m, "slash")
	for _, r := range "zzzznope" {
		key(m, string(r))
	}
	if len(m.visibleTiles()) != 0 {
		t.Error("expected no matches")
	}
}

// The bug: with two agents across four repos, only the agents were reachable,
// so the arrow keys appeared to move between two things and stop.
func TestEveryWorkspaceIsReachable(t *testing.T) {
	m := newSized(143)
	seen := map[string]bool{}
	for i := 0; i < len(m.targets); i++ {
		if m.targets[i].kind == kindGrid {
			seen[m.targets[i].workspaceID] = true
		}
	}
	for _, w := range m.snap.Workspaces {
		if !seen[w.ID] {
			t.Errorf("workspace %s cannot be reached by the cursor", w.ID)
		}
	}
}

// An empty workspace tile always focuses its workspace, however many panes or
// repos it holds: there is no longer a choice to refuse between them.
func TestEmptyWorkspaceTileFocusesItsWorkspace(t *testing.T) {
	m := newSized(143)
	for i := range m.targets {
		if m.targets[i].kind != kindGrid || m.targets[i].paneID != "" {
			continue
		}
		wsID := m.targets[i].workspaceID
		m.cursor = i
		key(m, "enter")
		if want := "ws:" + wsID; m.Jump() != want {
			t.Errorf("enter on workspace %s jumped to %q, want %q", wsID, m.Jump(), want)
		}
		return
	}
	t.Fatal("no empty workspace target found")
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
	tiles := m.orderedTiles(m.visibleTiles())
	// Agents-first is the outer key, so alphabetical holds within each group
	// rather than across the whole list.
	for i := 1; i < len(tiles); i++ {
		if tiles[i-1].isAgent() != tiles[i].isAgent() {
			continue // group boundary
		}
		if tiles[i-1].label() > tiles[i].label() {
			t.Errorf("not alphabetical within its group: %s before %s",
				tiles[i-1].label(), tiles[i].label())
		}
	}
}

func TestMouseClickSelectsThenJumps(t *testing.T) {
	m := newSized(143)
	m.View() // populate row positions
	// One click goes. Requiring two would make the mouse slower than the
	// keyboard, which defeats the point of having it.
	var row, col, want int
	found := false
	for _, h := range m.hits {
		if m.targetPane(h.target) != "" && h.target != m.cursor {
			row, col, want, found = h.y, h.x0, h.target, true
			break
		}
	}
	if !found {
		t.Skip("no clickable unselected agent row")
	}
	m.Update(tea.MouseMsg{X: col, Y: row, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
	if m.Jump() != m.targetPane(want) {
		t.Errorf("a single click should jump to %q, got %q", m.targetPane(want), m.Jump())
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

// The whole card should be clickable, not just its first line. Clicking a
// task line or a footer is still clicking that card.
func TestWholeCardIsClickable(t *testing.T) {
	m := newSized(143)
	m.View()

	byTarget := map[int]int{}
	for _, h := range m.hits {
		// Ribbon rows are legitimately one line; this is about grid cards.
		if m.isKind(h.target, kindRibbon) {
			continue
		}
		byTarget[h.target]++
	}
	if len(byTarget) == 0 {
		t.Fatal("no grid card was made clickable")
	}
	for target, lines := range byTarget {
		if lines < 2 {
			t.Errorf("target %d claims only %d line, expected its whole card", target, lines)
		}
	}
}

// Every column has to be clickable, not only the first.
func TestAllGridColumnsAreClickable(t *testing.T) {
	m := newSized(143)
	m.View()

	cols := map[int]bool{}
	for _, h := range m.hits {
		cols[h.x0] = true
	}
	if len(cols) < 2 {
		t.Errorf("hits landed in %d column offsets, expected several: %v", len(cols), cols)
	}
}

func TestHoverHighlightsWhatAClickWouldTake(t *testing.T) {
	m := newSized(143)
	m.View()
	hits := m.hits
	if len(hits) == 0 {
		t.Skip("nothing clickable")
	}
	var h hitRegion
	for _, c := range hits {
		if c.target != m.cursor {
			h = c
			break
		}
	}
	m.Update(tea.MouseMsg{X: h.x0, Y: h.y, Action: tea.MouseActionMotion})
	if m.hover != h.target {
		t.Errorf("hover = %d, want %d", m.hover, h.target)
	}
	// Moving off everything clears it.
	m.Update(tea.MouseMsg{X: 0, Y: 9999, Action: tea.MouseActionMotion})
	if m.hover != -1 {
		t.Errorf("hover should clear off-target, got %d", m.hover)
	}
}

// Empty workspace tiles are still shown, just never above one you are working
// in.
func TestAgentTilesSortFirst(t *testing.T) {
	m := newSized(143)
	tiles := m.orderedTiles(m.visibleTiles())
	if len(tiles) < 2 {
		t.Skip("need several tiles")
	}
	seenEmpty := false
	for _, tl := range tiles {
		if !tl.isAgent() {
			seenEmpty = true
			continue
		}
		if seenEmpty {
			t.Errorf("agent tile %s sorts after an empty workspace tile", tl.Agent.Name)
		}
	}
	// And nothing was dropped.
	if want := len(buildTiles(m.snap)); len(tiles) != want {
		t.Errorf("showing %d of %d tiles; empty workspaces should still appear", len(tiles), want)
	}
}

// The rule holds under every sort mode, not just the default.
func TestAgentsFirstHoldsAcrossSortModes(t *testing.T) {
	for _, mode := range []SortMode{SortFirstSeen, SortAlphabetical, SortAttention} {
		m := newSized(143)
		m.sort = mode
		seenEmpty := false
		for _, tl := range m.orderedTiles(m.visibleTiles()) {
			if !tl.isAgent() {
				seenEmpty = true
			} else if seenEmpty {
				t.Errorf("sort %v put an agent tile after an empty one", mode)
				break
			}
		}
	}
}

// The highlight and the click region are derived from the same mapping, so
// anywhere a card is clickable it must also light up. They disagreed before:
// the whole card was clickable but only its header was styled.
func TestHoverCoversEveryClickableLineOfACard(t *testing.T) {
	m := newSized(143)
	m.View()

	// Pick a target and hover its first line.
	hits := m.hits
	if len(hits) == 0 {
		t.Skip("nothing clickable")
	}
	target := -1
	for _, h := range hits {
		if !m.isKind(h.target, kindRibbon) {
			target = h.target
			break
		}
	}
	if target < 0 {
		t.Skip("no grid target")
	}
	var claimed []hitRegion
	for _, h := range hits {
		if h.target == target {
			claimed = append(claimed, h)
		}
	}
	if len(claimed) < 2 {
		t.Skip("target claims a single line")
	}
	m.Update(tea.MouseMsg{X: claimed[0].x0, Y: claimed[0].y, Action: tea.MouseActionMotion})

	lines := strings.Split(m.View(), "\n")
	styled := 0
	for _, h := range claimed {
		if h.y < len(lines) && strings.Contains(lines[h.y], "\x1b[") {
			styled++
		}
	}
	if styled != len(claimed) {
		t.Errorf("hover styled %d of %d claimed lines; the highlight must cover the whole click area",
			styled, len(claimed))
	}
}

// Click regions must line up with what was drawn. A section rule that secretly
// counted as two lines shifted every region below it, so clicks landed on the
// wrong card entirely.
func TestClickRegionsLineUpWithWhatWasDrawn(t *testing.T) {
	m := newSized(143)
	out := m.View()
	lines := strings.Split(out, "\n")

	for _, h := range m.hits {
		if h.y >= len(lines) {
			t.Errorf("a click region at y=%d is past the end of a %d line render",
				h.y, len(lines))
			continue
		}
		if m.isKind(h.target, kindRibbon) {
			continue
		}
		// A grid target's own text must actually appear on the line it claims.
		if pane := m.targetPane(h.target); pane != "" {
			continue // agent rows vary; the header check below is the tight one
		}
	}

	// The empty workspace tile's own text must sit on the first line it claims.
	for _, tl := range m.orderedTiles(m.visibleTiles()) {
		if tl.isAgent() {
			continue
		}
		ti := m.targetIndex("ws:" + tl.Workspace.ID)
		if ti < 0 {
			continue
		}
		var first = -1
		for _, h := range m.hits {
			if h.target == ti && (first < 0 || h.y < first) {
				first = h.y
			}
		}
		if first < 0 || first >= len(lines) {
			continue
		}
		plain := escapes.ReplaceAllString(lines[first], "")
		if !strings.Contains(strings.ToLower(plain), strings.ToLower(tl.Workspace.Label)) {
			t.Errorf("workspace %s claims line %d but that line reads %q",
				tl.Workspace.Label, first, strings.TrimSpace(plain))
		}
		return
	}
}

// A ribbon row is keyed on why it is there, not on the agent's status, so the
// badge and the accent have to come from the reason.
func TestRibbonBadgesReadFromTheReason(t *testing.T) {
	cases := []struct {
		reason model.Reason
		want   string
	}{
		{model.ReasonBlocked, "BLOCKED"},
		{model.ReasonGateUntold, "GATE"},
		{model.ReasonProcessStopped, "STOPPED"},
		{model.ReasonDoneUnseen, "DONE"},
		{model.ReasonIdleNeverDone, "STALE"},
	}
	for _, c := range cases {
		if got := reasonLabel(c.reason, model.StatusUnknown); got != c.want {
			t.Errorf("reasonLabel(%s) = %q, want %q", c.reason, got, c.want)
		}
	}
}

func TestReasonAccentsAreDistinct(t *testing.T) {
	seen := map[string]model.Reason{}
	for _, r := range []model.Reason{
		model.ReasonBlocked, model.ReasonGateUntold,
		model.ReasonProcessStopped, model.ReasonDoneUnseen,
	} {
		c := string(reasonAccent(r))
		if prev, dup := seen[c]; dup {
			t.Errorf("%s and %s share accent %s", prev, r, c)
		}
		seen[c] = r
	}
}

// A stopped-process row points at a non-agent pane, so resolving its repo
// through the agent list left it with a blank sigil.
func TestRibbonResolvesRepoForNonAgentRows(t *testing.T) {
	snap := testSnapshot()
	snap.Attention = []model.Attention{{
		Rank: 3, Reason: model.ReasonProcessStopped,
		RepoKey: "acme/web", PaneID: "acme/web:p9",
		Agent: "dev", Status: model.StatusUnknown, Detail: "vite stopped",
	}}
	m := New(snap, "")
	out, _ := m.Update(tea.WindowSizeMsg{Width: 143, Height: 40})
	plain := escapes.ReplaceAllString(out.View(), "")
	if !strings.Contains(plain, "web/dev") {
		t.Errorf("ribbon lost the repo for a non-agent row:\n%s", plain)
	}
	if !strings.Contains(plain, "▣") {
		t.Error("the repo sigil is missing from the row")
	}
}

// The section head carries the count and the colour of the worst row, so the
// ribbon signals severity before any of it is read.
func TestAttentionRuleShowsTheCount(t *testing.T) {
	m := newSized(143)
	plain := escapes.ReplaceAllString(m.View(), "")
	if !strings.Contains(plain, "NEEDS YOU 2") {
		t.Errorf("expected a counted heading, got:\n%s", strings.SplitN(plain, "\n", 4)[2])
	}
}

// An overlay left open must follow the daemon rather than freeze on whatever
// was true when it opened. This showed up as the grid claiming an agent was
// working while herdr's sidebar showed it blocked.
func TestOverlayRefreshesFromTheSnapshot(t *testing.T) {
	m := newSized(143)
	before := escapes.ReplaceAllString(m.View(), "")
	if strings.Contains(before, "something new") {
		t.Fatal("fixture already contains the marker")
	}

	updated := testSnapshot()
	updated.Repos[0].Display = "something new"
	m.SetReloader(func() *model.Snapshot { return updated })

	m.Update(refreshMsg{})
	after := escapes.ReplaceAllString(m.View(), "")
	if !strings.Contains(after, "something new") {
		t.Error("the overlay did not pick up the new snapshot")
	}
}

// A refresh must not move the selection out from under you.
func TestRefreshKeepsTheSelection(t *testing.T) {
	m := newSized(143)
	for i := 0; i < 3; i++ {
		key(m, "j")
	}
	want := m.selectedKey()

	m.SetReloader(func() *model.Snapshot { return testSnapshot() })
	m.Update(refreshMsg{})

	if got := m.selectedKey(); got != want {
		t.Errorf("selection moved across a refresh: %q became %q", want, got)
	}
}

// A failed read must leave the last good view up rather than blanking it.
func TestRefreshSurvivesAMissingSnapshot(t *testing.T) {
	m := newSized(143)
	m.SetReloader(func() *model.Snapshot { return nil })
	m.Update(refreshMsg{})
	if m.snap == nil {
		t.Fatal("a failed reload wiped the snapshot")
	}
	if out := m.View(); !strings.Contains(out, "MUSTER") {
		t.Error("the view did not survive a failed reload")
	}
}

// A hovered card must highlight as one block: every inner reset in the line has
// to re-arm the background, or the card lights up in stripes.
func TestPaintSurvivesInnerResets(t *testing.T) {
	line := paint(styFaint.Render("no agents")+"   ", stySel)
	for _, seg := range strings.Split(line, reset)[1:] {
		if seg != "" && !strings.HasPrefix(seg, "\x1b[") {
			t.Fatalf("background dropped after a reset: %q", line)
		}
	}
}

// The bug: every keystroke of a query rebuilds the targets and a rebuild with
// nothing selected selects nothing, so enter after searching did nothing.
func TestEnterAfterSearchJumpsToTheFirstMatch(t *testing.T) {
	m := newSized(143)
	key(m, "slash")
	for _, r := range "checkout" {
		key(m, string(r))
	}
	key(m, "enter")
	if m.Jump() != "w3:p1" {
		t.Errorf("enter after searching jumped to %q, want w3:p1", m.Jump())
	}
}

// Enter with nothing selected and no query stays inert: the overlay opens with
// nothing claimed and enter must not invent a selection.
func TestEnterWithoutSelectionOrSearchDoesNothing(t *testing.T) {
	m := newSized(143)
	key(m, "enter")
	if m.Jump() != "" {
		t.Errorf("enter with no selection jumped to %q", m.Jump())
	}
}

// Plain substring matching over each field independently missed two things a
// search is expected to do.
func TestSearchMatchesFuzzily(t *testing.T) {
	// A subsequence: the letters in order, not together.
	m := newSized(143)
	key(m, "slash")
	for _, r := range "cnt" {
		key(m, string(r))
	}
	tiles := m.visibleTiles()
	if len(tiles) == 0 || tiles[0].label() != "contracts" {
		t.Errorf("cnt should find contracts first, got %v", labelsOf(tiles))
	}

	// Two terms spread across a repo and one of its agents. Neither field set
	// holds both, so a single literal query could never match this.
	m = newSized(143)
	key(m, "slash")
	for _, r := range "web checkout" {
		key(m, string(r))
	}
	tiles = m.visibleTiles()
	if len(tiles) != 1 || tiles[0].Repo.Key != "acme/web" || tiles[0].Agent.Name != "checkout-ui" {
		t.Fatalf("web checkout matched %v, want just the web agent", labelsOf(tiles))
	}
}

// Every term has to hit something. Otherwise a second word only ever widens the
// search, which is the opposite of what typing more means.
func TestEverySearchTermMustMatch(t *testing.T) {
	m := newSized(143)
	key(m, "slash")
	for _, r := range "web zzzznope" {
		key(m, string(r))
	}
	if got := m.visibleTiles(); len(got) != 0 {
		t.Errorf("a term that matches nothing should empty the results, got %v", labelsOf(got))
	}
}

// The best match sorts first, and neither the grid's sort nor the rule that
// puts agent tiles first gets to reorder it underneath. infra matches on its
// own name and has no agent, so both of those would bury it.
func TestSearchRanksTheBestMatchFirst(t *testing.T) {
	for _, mode := range []SortMode{SortFirstSeen, SortAlphabetical, SortAttention} {
		m := newSized(143)
		m.sort = mode
		key(m, "slash")
		for _, r := range "in" {
			key(m, string(r))
		}
		got := m.orderedTiles(m.visibleTiles())
		if len(got) == 0 || got[0].label() != "infra" {
			t.Errorf("sort %v: prefix match should rank first, got %v", mode, labelsOf(got))
		}
	}
}

func labelsOf(tiles []tile) []string {
	out := make([]string, 0, len(tiles))
	for _, t := range tiles {
		out = append(out, t.label())
	}
	return out
}

// A row you have dealt with used to sit in the ribbon until the underlying
// state changed, or for a quarter of an hour in the case of a stopped process,
// taking one of only four slots.
func TestDismissHidesARibbonRowUntilTheStatusChanges(t *testing.T) {
	state.SetDir(t.TempDir())
	t.Cleanup(func() { state.SetDir("") })

	m := newSized(143)
	saved := state.LoadUI()
	m.SetDismissedSaver(func(d map[string]string) {
		saved.Dismissed = d
		if err := saved.Save(); err != nil {
			t.Errorf("save: %v", err)
		}
	})

	before := len(m.ribbonRows())
	m.cursor = m.ribbonTargetIndex("pane:w2:p1")
	key(m, "x")

	if got := len(m.ribbonRows()); got != before-1 {
		t.Fatalf("dismissing left %d rows, want %d", got, before-1)
	}
	for _, a := range m.ribbonRows() {
		if a.PaneID == "w2:p1" {
			t.Error("the dismissed row is still in the ribbon")
		}
	}
	// The header counts what the ribbon shows, not what the daemon counted.
	if out := m.View(); strings.Contains(out, "2 need you") {
		t.Error("header still counts the dismissed row")
	}

	// A fresh overlay reads it back: still dismissed.
	next := newSized(143)
	next.SetDismissed(state.LoadUI().Dismissed)
	for _, a := range next.ribbonRows() {
		if a.PaneID == "w2:p1" {
			t.Error("dismissal did not survive the overlay closing")
		}
	}

	// The agent moves on. That makes it news again.
	snap := testSnapshot()
	snap.Attention[0].Status = model.StatusDone
	next.SetSnapshot(snap)
	found := false
	for _, a := range next.ribbonRows() {
		found = found || a.PaneID == "w2:p1"
	}
	if !found {
		t.Error("a status change should bring the row back")
	}
}

// Nothing else on the screen is dismissible, and pressing x on a card should
// say so rather than silently doing nothing.
func TestDismissOnlyAppliesToRibbonRows(t *testing.T) {
	m := newSized(143)
	m.cursor = m.targetIndex("pane:w2:p1")
	key(m, "x")
	if len(m.dismissed) != 0 {
		t.Errorf("x on a grid card dismissed %v", m.dismissed)
	}
	if m.notice == "" {
		t.Error("x on a grid card should say what x is for")
	}
}

// Entries would otherwise pile up in the file forever, one per row ever
// dismissed. A row that has left the ribbon is news again if it comes back.
func TestDismissalsAreDroppedOnceTheRowIsGone(t *testing.T) {
	m := newSized(143)
	m.cursor = m.ribbonTargetIndex("pane:w2:p1")
	key(m, "x")
	if len(m.dismissed) != 1 {
		t.Fatalf("expected one dismissal, got %v", m.dismissed)
	}

	snap := testSnapshot()
	snap.Attention = snap.Attention[1:] // the row resolved itself
	m.SetSnapshot(snap)
	if len(m.dismissed) != 0 {
		t.Errorf("stale dismissal kept: %v", m.dismissed)
	}
}

// Working and blocked read as static text otherwise, which is the wrong thing
// for the two states where something is happening or something is waiting.
func TestWorkingSpinsAndBlockedPulses(t *testing.T) {
	m := newSized(143)
	first := plain(m.View())

	// A working agent's icon changes from frame to frame.
	spun := false
	for i := 0; i < len(spinner); i++ {
		m.Update(animMsg{})
		if strings.Contains(plain(m.View()), spinner[m.frame%len(spinner)]) {
			spun = true
		}
	}
	if !spun {
		t.Error("the working icon never moved")
	}

	// Blocked keeps its shape and changes colour, so the pulse is not a shape
	// the eye has to re-read.
	m.frame = 0
	rest := m.View()
	m.frame = pulseFrames
	if pulsed := m.View(); pulsed == rest {
		t.Error("blocked did not pulse")
	}
	if !strings.Contains(plain(m.View()), "▲") {
		t.Error("the pulse should change the colour, not the icon")
	}

	// Frame zero is the resting frame: a still screen looks the way it always
	// did.
	m.frame = 0
	if plain(m.View()) != first {
		t.Error("frame zero should render exactly as an unanimated screen")
	}
}

// An overlay left open on a screen of idle agents should cost nothing.
func TestAnimationStopsWhenNothingMoves(t *testing.T) {
	m := newSized(143)
	if !m.animated() {
		t.Fatal("the test snapshot has a working agent and a blocked one")
	}
	if cmd := m.startAnimation(); cmd == nil {
		t.Error("something is moving, so the frame loop should start")
	}
	if cmd := m.startAnimation(); cmd != nil {
		t.Error("a second loop started while one was already running")
	}

	// Everything settles.
	snap := testSnapshot()
	for i := range snap.Repos {
		for j := range snap.Repos[i].Agents {
			snap.Repos[i].Agents[j].Status = model.StatusIdle
		}
	}
	m.SetSnapshot(snap)
	if m.animated() {
		t.Fatal("nothing is working or blocked any more")
	}
	if _, cmd := m.Update(animMsg{}); cmd != nil {
		t.Error("the frame loop should stop when the screen goes still")
	}
	if m.ticking {
		t.Error("ticking should be cleared so the loop can start again later")
	}

	// And it starts again when something picks up.
	m.SetSnapshot(testSnapshot())
	if cmd := m.startAnimation(); cmd == nil {
		t.Error("an agent starting work should restart the frame loop")
	}
}

// The badge counts what the header counts, so a dismissed row drops out of
// both, and a snapshot the daemon stopped writing gets the key and no number.
func TestBadge(t *testing.T) {
	state.SetDir(t.TempDir())
	t.Cleanup(func() { state.SetDir("") })
	write := func(s *model.Snapshot) {
		t.Helper()
		b, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		if err := state.WriteAtomic(state.SnapshotPath(), b); err != nil {
			t.Fatal(err)
		}
	}
	check := func(letter, want string) {
		t.Helper()
		if got := Badge(letter); got != want {
			t.Errorf("Badge(%q) = %q, want %q", letter, got, want)
		}
	}

	check("m", "◆ prefix+m")

	snap := testSnapshot()
	write(snap)
	check("m", "◆ 2 need you · prefix+m")

	saved := state.LoadUI()
	saved.Dismissed = map[string]string{"w2:p2": string(model.StatusDone)}
	if err := saved.Save(); err != nil {
		t.Fatal(err)
	}
	check("g", "◆ 1 needs you · prefix+g")

	snap.Attention = nil
	snap.Counts.Working = 1
	write(snap)
	check("m", "◆ 1 working · prefix+m")

	snap.GeneratedAt = time.Now().Add(-time.Minute)
	write(snap)
	check("m", "◆ prefix+m")
}
