package ui

import (
	"cmp"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ofelcan164/muster/internal/model"
)

// SortMode is how the grid is ordered.
//
// First-seen is the default and the one the design argues for: a repo keeps
// its cell forever, so you point instead of read. herdr mirrors the sidebar
// instead, and is the only mode J and K do anything in.
type SortMode int

const (
	SortFirstSeen SortMode = iota
	SortAlphabetical
	SortAttention
	// SortHerdr is appended after SortAttention, not alongside the others in
	// whatever order reads best: ui.json stores the sort as a bare int, and a
	// mode inserted earlier in the list would change what every saved file
	// means.
	SortHerdr
	sortModeCount
)

func (s SortMode) String() string {
	switch s {
	case SortAlphabetical:
		return "a-z"
	case SortAttention:
		return "attention"
	case SortHerdr:
		return "herdr"
	default:
		return "first seen"
	}
}

// Next cycles through the sort modes.
func (s SortMode) Next() SortMode { return (s + 1) % sortModeCount }

// orderedTiles applies the current sort to the grid.
func (m *Model) orderedTiles(tiles []tile) []tile {
	// While filtering the order is the match ranking and nothing else. A sort
	// on top of it would push a worse match above a better one, and position in
	// a filtered list carries no meaning to preserve.
	if m.filter != "" {
		return tiles
	}

	out := append([]tile(nil), tiles...)

	switch m.sort {
	case SortAlphabetical:
		slices.SortStableFunc(out, func(a, b tile) int {
			return cmp.Or(
				cmp.Compare(strings.ToLower(a.label()), strings.ToLower(b.label())),
				cmp.Compare(a.Agent.Name, b.Agent.Name))
		})
	case SortAttention:
		rank := m.tileRanks()
		slices.SortStableFunc(out, func(a, b tile) int {
			return cmp.Or(cmp.Compare(tileRank(a, rank), tileRank(b, rank)), cmp.Compare(a.slot(), b.slot()))
		})
	case SortHerdr:
		// Workspace number is exactly the sidebar's own order, and the busy-first
		// rule below does not apply here: an empty tile belongs at its sidebar
		// position, not shuffled to the back, or this mode stops matching the
		// sidebar it exists to mirror.
		slices.SortStableFunc(out, func(a, b tile) int {
			return cmp.Or(cmp.Compare(a.Workspace.Number, b.Workspace.Number), cmp.Compare(a.Agent.PaneID, b.Agent.PaneID))
		})
		return out
	default:
		slices.SortStableFunc(out, func(a, b tile) int { return cmp.Compare(a.slot(), b.slot()) })
	}

	// Agent tiles always come first, whatever the sort. An empty workspace is
	// still worth a tile, but it should never sit between two you are working
	// in. This runs after the sort so it never disturbs the order within each
	// group.
	slices.SortStableFunc(out, func(a, b tile) int {
		return cmp.Compare(busyRank(b), busyRank(a))
	})
	return out
}

func busyRank(t tile) int {
	if t.isAgent() {
		return 1
	}
	return 0
}

// Ranks below these come from the ribbon, which uses 1 to 5. Work sits under
// all of them and above silence: an agent doing its job never reaches the
// ribbon, so without a rank of its own it sorted level with an empty tile.
const (
	rankWorking = 50
	rankQuiet   = 99
)

// tileRanks is the best ribbon rank each agent holds, keyed by pane id.
func (m *Model) tileRanks() map[string]int {
	rank := map[string]int{}
	for _, a := range m.snap.Attention {
		if r, ok := rank[a.PaneID]; !ok || a.Rank < r {
			rank[a.PaneID] = a.Rank
		}
	}
	return rank
}

// tileRank is where a tile falls: needs you, then working, then everything
// else. An empty tile has no agent and so is always quiet.
func tileRank(t tile, rank map[string]int) int {
	if !t.isAgent() {
		return rankQuiet
	}
	if r, ok := rank[t.Agent.PaneID]; ok {
		return r
	}
	if t.Agent.Status == model.StatusWorking {
		return rankWorking
	}
	return rankQuiet
}

// moveSelectedWorkspace moves the selected tile's workspace one place in
// herdr's own order, and only in herdr sort: it is the only mode whose tiles
// are laid out in that order for the move to preserve.
//
// insert_index counts positions in the list as it stood before the move,
// including the workspace being moved at its old spot: moving it to the
// index one place away lands it there directly, but moving it further needs
// the index one past the target, to account for its own removal shifting
// everything after it back by one. Verified against a throwaway workspace
// created and closed for the purpose, never one of the user's own.
func (m *Model) moveSelectedWorkspace(delta int) tea.Cmd {
	if m.sort != SortHerdr {
		m.notice = "J and K move workspaces, and only in herdr sort"
		return nil
	}
	wsID := m.selectedWorkspace()
	if wsID == "" {
		return nil
	}
	at := slices.IndexFunc(m.snap.Workspaces, func(w model.Workspace) bool { return w.ID == wsID })
	if at < 0 {
		return nil
	}
	to := at + delta
	if to < 0 || to >= len(m.snap.Workspaces) {
		return nil
	}
	insertIndex := to
	if to > at {
		insertIndex++
	}

	// Reordered locally so the tiles move before the next snapshot lands: the
	// daemon reconciles on its own cadence, and waiting for it would make the
	// key feel like it had done nothing.
	moved := append([]model.Workspace(nil), m.snap.Workspaces...)
	ws := moved[at]
	moved = slices.Delete(moved, at, at+1)
	moved = slices.Insert(moved, to, ws)
	m.snap.Workspaces = moved
	m.rebuild()

	if m.moveWorkspace == nil {
		return nil
	}
	move := m.moveWorkspace
	return func() tea.Msg {
		if err := move(wsID, insertIndex); err != nil {
			return noticeMsg("could not move: " + err.Error())
		}
		return nil
	}
}
