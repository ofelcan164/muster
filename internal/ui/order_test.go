package ui

import (
	"time"

	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ofelcan164/muster/internal/model"
	"github.com/ofelcan164/muster/internal/state"
	"github.com/ofelcan164/muster/internal/triage"
)

// tileKey identifies a tile the same way a target does, so the two can be
// compared position by position.
func tileKey(t tile) string {
	if t.isAgent() {
		return "pane:" + t.Agent.PaneID
	}
	return "ws:" + t.Workspace.ID
}

// drawnTileKeys is the tile order the view actually paints.
func drawnTileKeys(m *Model) []string {
	var out []string
	for _, t := range m.orderedTiles(m.visibleTiles()) {
		out = append(out, tileKey(t))
	}
	return out
}

// walkedTileKeys is the order the cursor moves through, which is the order
// the targets were built in.
func walkedTileKeys(m *Model) []string {
	var out []string
	for _, t := range m.targets {
		if t.kind != kindGrid {
			continue
		}
		out = append(out, m.keyOf(t))
	}
	return out
}

func sized(t *testing.T, width int) *Model {
	t.Helper()
	m := New(testSnapshot(), "")
	m.Update(tea.WindowSizeMsg{Width: width, Height: 40})
	return m
}

// The cursor has to walk the grid in the order the grid is drawn. They come
// from two different calls, so any sort made them disagree and j moved
// somewhere other than the tile below.
func TestCursorOrderMatchesDrawnOrder(t *testing.T) {
	for _, mode := range []SortMode{SortFirstSeen, SortAlphabetical, SortAttention, SortHerdr} {
		m := sized(t, 143)
		m.sort = mode
		m.rebuild()

		drawn, walked := drawnTileKeys(m), walkedTileKeys(m)
		if len(drawn) != len(walked) {
			t.Fatalf("sort %v: drew %d tiles, cursor reaches %d", mode, len(drawn), len(walked))
		}
		for i := range drawn {
			if drawn[i] != walked[i] {
				t.Fatalf("sort %v: position %d is drawn as %q but walked as %q\ndrawn:  %v\nwalked: %v",
					mode, i, drawn[i], walked[i], drawn, walked)
			}
		}
	}
}

// The column recorded on a target is what left and right navigate by, so it has
// to be the column the tile is drawn in.
func TestTargetColumnMatchesDrawnColumn(t *testing.T) {
	m := sized(t, 143)
	m.sort = SortAlphabetical
	m.rebuild()

	cols := columnsFor(m.width)
	want := map[string]int{}
	for i, key := range drawnTileKeys(m) {
		want[key] = i % cols
	}
	for _, tg := range m.targets {
		if tg.kind != kindGrid {
			continue
		}
		key := m.keyOf(tg)
		if got := tg.column; got != want[key] {
			t.Errorf("tile %q drawn in column %d, target says %d", key, want[key], got)
		}
	}
}

// The sort mode has to survive the overlay closing. The client is
// short-lived, so a mode held only in the model lasted one keypress.
func TestSortModeRoundTrips(t *testing.T) {
	state.SetDir(t.TempDir())
	t.Cleanup(func() { state.SetDir("") })

	m := sized(t, 143)
	saved := state.LoadUI()
	m.SetSortSaver(func(s SortMode) {
		saved.Sort = int(s)
		if err := saved.Save(); err != nil {
			t.Errorf("save: %v", err)
		}
	})

	key(m, "s")
	key(m, "s")
	want := m.sort
	if want != SortAttention {
		t.Fatalf("two presses of s gave %v, want attention", want)
	}

	next := sized(t, 143)
	next.SetSort(SortMode(state.LoadUI().Sort))
	if got := next.sort; got != want {
		t.Errorf("reopened with sort %v, want %v", got, want)
	}
}

// A mode from a file that no longer means anything must not leave the cycle
// stuck outside its range.
func TestRestoredSortModeIsClamped(t *testing.T) {
	for _, bad := range []SortMode{-1, sortModeCount, 99} {
		m := sized(t, 143)
		m.SetSort(bad)
		if got := m.sort; got != SortFirstSeen {
			t.Errorf("SetSort(%d) kept %v, want first seen", bad, got)
		}
	}
}

// Working agents never reach the ribbon, so the attention sort used to weigh a
// tile with work in it exactly the same as a silent one.
func TestAttentionSortPutsWorkAboveSilence(t *testing.T) {
	m := sized(t, 143)
	m.sort = SortAttention
	m.rebuild()

	got := drawnTileKeys(m)
	pos := map[string]int{}
	for i, k := range got {
		pos[k] = i
	}
	// w2:p1 is blocked, w3:p1 is working, w1:p1 is only idle.
	if !(pos["pane:w2:p1"] < pos["pane:w3:p1"] && pos["pane:w3:p1"] < pos["pane:w1:p1"]) {
		t.Errorf("attention order %v, want w2:p1 before w3:p1 before w1:p1", got)
	}
}

// The ribbon holds four rows, and the attention sort read its ranks from the
// ribbon, so a fifth agent that needed you sorted as if it did not.
func TestAttentionSortRanksPastTheRibbonCap(t *testing.T) {
	snap := testSnapshot()
	api := &snap.Repos[1]
	for _, p := range []string{"w2:p3", "w2:p4", "w2:p5", "w2:p6"} {
		api.Agents = append(api.Agents, model.Agent{
			PaneID: p, WorkspaceID: "w2", Name: p, Status: model.StatusDone,
			StatusSince: time.Now().Add(-time.Minute), AgeKnown: true,
		})
	}
	snap.Attention = triage.Rank(triage.Input{Now: time.Now(), Repos: snap.Repos})

	m := New(snap, "")
	m.Update(tea.WindowSizeMsg{Width: 143, Height: 40})
	m.sort = SortAttention
	m.rebuild()

	pos := map[string]int{}
	for i, k := range drawnTileKeys(m) {
		pos[k] = i
	}
	for _, p := range []string{"w2:p2", "w2:p3", "w2:p4", "w2:p5", "w2:p6"} {
		if pos["pane:"+p] > pos["pane:w3:p1"] {
			t.Errorf("done agent %s sorted below working w3:p1: %v", p, drawnTileKeys(m))
		}
	}

	// Six rows need you: the ribbon draws four, and dismissing one promotes the
	// fifth rather than leaving a gap.
	if got := len(m.ribbonRows()); got != triage.RibbonMax {
		t.Fatalf("ribbon drew %d rows, want %d", got, triage.RibbonMax)
	}
	first := m.ribbonRows()[0]
	m.dismissed = map[string]string{first.PaneID: string(first.Status)}
	if got := m.ribbonRows(); len(got) != triage.RibbonMax || got[0].PaneID == first.PaneID {
		t.Errorf("after dismissing %s the ribbon is %v", first.PaneID, got)
	}
}
