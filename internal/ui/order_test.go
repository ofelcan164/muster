package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ofelcan164/muster/internal/state"
)

// drawnRepoKeys is the repo order the view actually paints.
func drawnRepoKeys(m *Model) []string {
	var out []string
	for _, r := range m.orderedRepos(m.visibleRepos()) {
		out = append(out, r.Key)
	}
	return out
}

// walkedRepoKeys is the repo order the cursor moves through, which is the order
// the targets were built in.
func walkedRepoKeys(m *Model) []string {
	var out []string
	for _, t := range m.targets {
		if t.kind != kindGrid || t.repoKey == "" {
			continue
		}
		if len(out) == 0 || out[len(out)-1] != t.repoKey {
			out = append(out, t.repoKey)
		}
	}
	return out
}

func sized(t *testing.T, width int) *Model {
	t.Helper()
	m := New(testSnapshot(), "")
	m.Update(tea.WindowSizeMsg{Width: width, Height: 40})
	return m
}

// The cursor has to walk the grid in the order the grid is drawn. They came
// from two different calls, so any sort or manual move made them disagree and
// j moved somewhere other than the card below.
func TestCursorOrderMatchesDrawnOrder(t *testing.T) {
	for _, mode := range []SortMode{SortFirstSeen, SortAlphabetical, SortAttention} {
		m := sized(t, 143)
		m.sort = mode
		m.rebuild()

		drawn, walked := drawnRepoKeys(m), walkedRepoKeys(m)
		if len(drawn) != len(walked) {
			t.Fatalf("sort %v: drew %d repos, cursor reaches %d", mode, len(drawn), len(walked))
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
// to be the column the card is drawn in.
func TestTargetColumnMatchesDrawnColumn(t *testing.T) {
	m := sized(t, 143)
	m.sort = SortAlphabetical
	m.rebuild()

	cols := columnsFor(m.width)
	want := map[string]int{}
	for i, key := range drawnRepoKeys(m) {
		want[key] = i % cols
	}
	for _, tg := range m.targets {
		if tg.kind != kindGrid {
			continue
		}
		if got := tg.column; got != want[tg.repoKey] {
			t.Errorf("repo %q drawn in column %d, target says %d", tg.repoKey, want[tg.repoKey], got)
		}
	}
}

// A manual move has to survive the overlay closing. It is stored in the
// overlay's own file rather than the daemon's, which the daemon would overwrite
// on its next reconcile.
func TestManualOrderRoundTrips(t *testing.T) {
	state.SetDir(t.TempDir())
	t.Cleanup(func() { state.SetDir("") })

	m := sized(t, 143)
	saved := state.LoadUI()
	m.SetOrderSaver(func(order []string) {
		saved.RepoOrder = order
		if err := saved.Save(); err != nil {
			t.Errorf("save: %v", err)
		}
	})

	// Select the last repo card and move it up one.
	m.cursor = len(m.targets) - 1
	moved := m.selectedRepo()
	m.moveSelectedRepo(-1)

	if len(saved.RepoOrder) == 0 {
		t.Fatal("moving a repo saved nothing")
	}
	want := drawnRepoKeys(m)

	// A fresh overlay, reading the file back.
	reloaded := state.LoadUI()
	next := sized(t, 143)
	next.SetManualOrder(reloaded.RepoOrder)
	got := drawnRepoKeys(next)

	if len(got) != len(want) {
		t.Fatalf("restored %d repos, arranged %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("moved %q, then restored order %v, wanted %v", moved, got, want)
		}
	}
}

// Rearranging while filtering would record an arrangement of only the matches
// and push every other repo behind them, which now outlives the session.
func TestFilteringBlocksManualMoves(t *testing.T) {
	m := sized(t, 143)
	saved := 0
	m.SetOrderSaver(func([]string) { saved++ })

	m.filter = "api"
	m.rebuild()
	before := drawnRepoKeys(m)
	m.moveSelectedRepo(1)

	if saved != 0 {
		t.Errorf("a move made while filtering saved %d times", saved)
	}
	if got := drawnRepoKeys(m); len(got) != len(before) || (len(got) > 0 && got[0] != before[0]) {
		t.Errorf("filtering changed the order: %v became %v", before, got)
	}
	if len(m.moves) != 0 {
		t.Errorf("filtering recorded an arrangement: %v", m.moves)
	}
}

// The sort mode has to survive the overlay closing too. The client is
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
// repo with work in it exactly the same as a silent one.
func TestAttentionSortPutsWorkAboveSilence(t *testing.T) {
	m := sized(t, 143)
	m.sort = SortAttention
	m.rebuild()

	got := drawnRepoKeys(m)
	pos := map[string]int{}
	for i, k := range got {
		pos[k] = i
	}
	// api holds the blocked agent, web is working, contracts is only idle.
	if !(pos["acme/api"] < pos["acme/web"] && pos["acme/web"] < pos["acme/contracts"]) {
		t.Errorf("attention order %v, want api before web before contracts", got)
	}
}
