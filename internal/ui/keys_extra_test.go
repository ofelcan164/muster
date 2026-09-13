package ui

import (
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ofelcan164/muster/internal/model"
)

// An empty workspace tile always focuses its workspace, however many panes it
// holds: there is no longer a choice among them to refuse.
func TestActivatingAnEmptyWorkspaceTile(t *testing.T) {
	withPanes := func(panes int) *model.Snapshot {
		s := &model.Snapshot{
			Workspaces: []model.Workspace{{ID: "w1", Number: 1, Label: "ofelcan"}},
			Repos:      []model.Repo{{Key: "k", Name: "ofelcan", Display: "ofelcan", WorkspaceIDs: []string{"w1"}}},
		}
		for i := 0; i < panes; i++ {
			s.Repos[0].OtherPanes = append(s.Repos[0].OtherPanes,
				model.Pane{PaneID: "w" + string(rune('1'+i)) + ":p1", WorkspaceID: "w1", Label: "bash"})
		}
		return s
	}

	for _, panes := range []int{0, 1, 9} {
		m := withSnapshot(t, withPanes(panes), 100)
		m.cursor = m.targetIndex("ws:w1")
		m.activate()
		if m.Jump() != "ws:w1" {
			t.Errorf("%d panes: jumped to %q, want the workspace", panes, m.Jump())
		}
	}
}

// A notice raised with no orchestrator marked is written to a line the strip
// was not drawing at all.
func TestNoticeShowsWithNoOrchestrator(t *testing.T) {
	m := withSnapshot(t, testSnapshot(), 100)
	m.snap.Orch = model.Orchestrator{Found: false}
	m.notice = "ofelcan has nothing open to jump to"
	if !strings.Contains(plain(m.View()), "nothing open to jump to") {
		t.Errorf("the notice never reached the screen:\n%s", plain(m.View()))
	}
}

// Muster is a popup, so herdr hands it the global keys instead of acting on
// them. M is prefix+shift+m with the prefix left off, and the prefix itself has
// to do nothing for that to hold. m is not a close key: only q and esc are.
func TestGlobalKeysInsideThePopup(t *testing.T) {
	altQ := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q"), Alt: true}

	m := withSnapshot(t, withOrch(), 143)
	m.Update(altQ)
	key(m, "m")
	if m.quit || m.Jump() != "" {
		t.Errorf("prefix then m acted: quit=%v jump=%q, want nothing", m.quit, m.Jump())
	}
	m.Update(altQ)
	key(m, "M")
	if m.Jump() != "w4:p1" {
		t.Errorf("prefix then M jumped to %q, want the orchestrator w4:p1", m.Jump())
	}

	orch := withSnapshot(t, withOrch(), 143)
	key(orch, "M")
	if orch.Jump() != "w4:p1" {
		t.Errorf("M jumped to %q, want the orchestrator w4:p1", orch.Jump())
	}

	none := withSnapshot(t, testSnapshot(), 143)
	key(none, "M")
	if none.quit || none.Jump() != "" {
		t.Errorf("with no orchestrator M should stay open: quit=%v jump=%q", none.quit, none.Jump())
	}
	if !strings.Contains(none.notice, "no orchestrator") {
		t.Errorf("notice = %q, want it to say there is no orchestrator", none.notice)
	}
}

// While typing, m is a letter, and an alt key is a chord rather than text. The
// prefix is alt+q for plenty of people, and it reaches the popup.
func TestTypingKeepsMAndDropsAltKeys(t *testing.T) {
	altQ := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q"), Alt: true}

	m := withSnapshot(t, withOrch(), 143)
	key(m, "slash")
	m.Update(altQ)
	key(m, "m")
	key(m, "M")
	if m.quit || m.filter != "mM" {
		t.Errorf("filter = %q quit=%v, want \"mM\" and still open", m.filter, m.quit)
	}

	c := withSnapshot(t, withOrch(), 143)
	key(c, "i")
	c.Update(altQ)
	key(c, "m")
	if c.compose != "m" {
		t.Errorf("compose = %q, want \"m\" with the alt key dropped", c.compose)
	}
}

// Every other test renders at height 40, which no real popup gets. A 24-line
// terminal showing four repos produced 33 lines, and bubbletea keeps the last
// ones, so the header and the ribbon scrolled off and every click landed nine
// rows away from what it hit.
func TestViewNeverExceedsTheHeight(t *testing.T) {
	for _, h := range []int{10, 18, 24, 30} {
		m := New(testSnapshot(), "")
		out, _ := m.Update(tea.WindowSizeMsg{Width: 53, Height: h})
		if n := len(strings.Split(out.View(), "\n")); n > h {
			t.Errorf("height %d: rendered %d lines", h, n)
		}
	}
}

// The window scrolls to whatever is selected, or the selection is unreachable
// on a short terminal.
func TestShortTerminalScrollsToTheSelection(t *testing.T) {
	m := New(testSnapshot(), "")
	m.Update(tea.WindowSizeMsg{Width: 53, Height: 12})
	for range 12 {
		key(m, "down")
	}
	// The hit regions, and so the cursor's line, come from a render.
	m.View()
	y, ok := m.cursorLine()
	if !ok {
		t.Fatal("the selection was not drawn at all")
	}
	if y < 0 || y >= 12 {
		t.Errorf("the selection is drawn on line %d of a 12 line screen", y)
	}
}

// A click has to land on what it hit, which on a scrolled screen means the hit
// regions move with the window.
func TestClicksLandAfterScrolling(t *testing.T) {
	m := New(testSnapshot(), "")
	m.Update(tea.WindowSizeMsg{Width: 53, Height: 12})
	for range 12 {
		key(m, "down")
	}
	lines := strings.Split(m.View(), "\n")
	y, ok := m.cursorLine()
	if !ok {
		t.Fatal("nothing selected")
	}
	want := m.selectedPane()
	idx, hit := m.targetAt(2, y)
	if !hit {
		t.Fatalf("line %d holds no target, though the cursor is drawn there:\n%s",
			y, plain(strings.Join(lines, "\n")))
	}
	if got := m.targets[idx].paneID; got != want {
		t.Errorf("clicking the selected line hit %q, want %q", got, want)
	}
}

// An agent that exits while you have it selected leaves nothing selected.
// Falling back to index zero meant the ribbon's top row, so enter went to the
// thing that needs you most rather than nowhere.
func TestSelectionDoesNotFallOntoTheRibbon(t *testing.T) {
	m := newSized(143)
	for range 3 {
		key(m, "down")
	}
	if m.selectedPane() == "" {
		t.Fatal("nothing selected to begin with")
	}

	snap := testSnapshot()
	snap.Repos[0].Agents = nil
	snap.Repos[1].Agents = nil
	snap.Repos[2].Agents = nil
	m.SetSnapshot(snap)

	if m.cursor != noSelection {
		t.Errorf("cursor = %d after its target vanished, want nothing selected (%q)",
			m.cursor, m.selectedPane())
	}
	key(m, "enter")
	if m.Jump() != "" {
		t.Errorf("enter jumped to %q with nothing selected", m.Jump())
	}
}

// J and K move workspaces only in herdr sort. Every other mode leaves the
// order untouched and says why, since repo_order is gone: there is nothing
// left for them to do outside it.
func TestJKDoNothingOutsideHerdrSort(t *testing.T) {
	m := newSized(143)
	before := append([]model.Workspace(nil), m.snap.Workspaces...)
	m.firstGridTarget()
	key(m, "J")
	if !slices.Equal(m.snap.Workspaces, before) {
		t.Errorf("J moved a workspace outside herdr sort: %v", m.snap.Workspaces)
	}
	if !strings.Contains(m.notice, "herdr sort") {
		t.Errorf("notice = %q, want it to say J and K need herdr sort", m.notice)
	}
}

// In herdr sort, J calls workspace.move and reorders the local snapshot
// before the next one lands, so the tiles move at once, and the cursor
// follows the tile rather than the position it moved from.
func TestJKMoveWorkspacesInHerdrSort(t *testing.T) {
	m := newSized(143)
	m.sort = SortHerdr
	m.rebuild()

	var movedID string
	var insertIndex int
	m.SetWorkspaceMover(func(id string, i int) error {
		movedID, insertIndex = id, i
		return nil
	})

	// w1 is workspace number 1, first in herdr sort. Moving it down one swaps
	// it with w2.
	m.cursor = m.targetIndex("pane:w1:p1")
	key(m, "J")

	if m.snap.Workspaces[0].ID != "w2" || m.snap.Workspaces[1].ID != "w1" {
		t.Fatalf("w1 did not move down one place: %v", m.snap.Workspaces)
	}
	if movedID != "w1" || insertIndex != 2 {
		t.Errorf("workspace.move called with (%q, %d), want (w1, 2)", movedID, insertIndex)
	}
	if m.selectedPane() != "w1:p1" {
		t.Errorf("cursor left the moved tile, now on %q", m.selectedPane())
	}
}

// herdr sort mirrors the sidebar exactly, so unlike every other mode it must
// not pull an empty workspace behind a busy one that sits later in the
// sidebar.
func TestHerdrSortSkipsBusyFirst(t *testing.T) {
	snap := &model.Snapshot{
		Workspaces: []model.Workspace{
			{ID: "empty", Number: 1, Label: "notes"},
			{ID: "busy", Number: 2, Label: "hub"},
		},
		Repos: []model.Repo{{
			Key: "acme/hub", Name: "acme/hub", Display: "hub", WorkspaceIDs: []string{"busy"},
			Agents: []model.Agent{{PaneID: "busy:p1", WorkspaceID: "busy", Name: "claude",
				Status: model.StatusIdle, AgeKnown: true}},
		}},
	}
	m := New(snap, "")
	m.Update(tea.WindowSizeMsg{Width: 143, Height: 40})
	m.sort = SortHerdr

	tiles := m.orderedTiles(m.visibleTiles())
	if len(tiles) != 2 || tiles[0].Workspace.ID != "empty" {
		t.Fatalf("herdr sort must keep sidebar order even for an empty workspace ahead of a busy one: %+v",
			labelsOf(tiles))
	}
}

// Filtering hides the ribbon and the strip. A key that acts on either is a key
// that acts on something nobody can see.
func TestFilteringDisablesTheKeysItHides(t *testing.T) {
	m := withSnapshot(t, withOrch(), 143)
	key(m, "slash")
	key(m, "w")
	key(m, "esc") // leave typing, keep the query

	key(m, "1")
	if m.Jump() != "" {
		t.Errorf("a digit jumped to %q while the ribbon was hidden", m.Jump())
	}
	key(m, "i")
	if m.composing {
		t.Error("i opened an input on a strip that is not drawn")
	}
	key(m, "q")
	if !m.quit {
		t.Error("q did not close: it was swallowed by the hidden input")
	}
}

// Backspace removes a character, not a byte. Cutting a byte left half of one
// behind, and that went on to be searched with and sent to an agent.
func TestBackspaceRemovesWholeCharacters(t *testing.T) {
	m := withSnapshot(t, withOrch(), 143)
	key(m, "slash")
	key(m, "é")
	key(m, "backspace")
	if m.filter != "" {
		t.Errorf("filter = %q after backspacing over one character", m.filter)
	}

	c := withSnapshot(t, withOrch(), 143)
	key(c, "i")
	key(c, "é")
	c.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if c.compose != "" {
		t.Errorf("compose = %q after backspacing over one character", c.compose)
	}
}

// The lane is a grid column, and a filter collapses the grid to one. A lane
// left pointing at column two is empty, so up and down stopped working.
func TestNarrowingTheGridResetsTheLane(t *testing.T) {
	m := newSized(143)
	key(m, "down")
	key(m, "right")
	if m.laneCol == 0 {
		t.Skip("nothing in a second column to select")
	}
	key(m, "slash")
	key(m, "z") // matches nothing
	key(m, "backspace")
	key(m, "esc")
	key(m, "esc")

	key(m, "down")
	if m.cursor == noSelection {
		t.Error("down does nothing: the lane points at a column that is gone")
	}
}
