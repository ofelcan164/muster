// Targets are the things the selection can land on. This is where the list of
// them is built, identified and searched.

package ui

import "strconv"

// target is something the selection can land on.
//
// Every grid target is a tile: an agent, or an empty workspace. Both carry a
// workspaceID, which is what keyOf uses to identify an empty tile since it has
// no pane of its own.
type target struct {
	// paneID is the agent to jump to, or "" for an empty workspace tile.
	paneID string
	// workspaceID is the tile's workspace, set on every grid target.
	workspaceID string
	// column and row are the grid cell the target is drawn in, used for
	// left/right and up/down. Rows outside the grid are -1.
	column, row int
	kind        targetKind
}

// targetKind is which block of the screen a target lives in.
//
// The same agent can be in more than one: an agent in the ribbon is also in the
// grid, and the orchestrator is in the grid and on its own strip. So a lookup
// by key has to say which block it means, or a click on a card resolves to the
// ribbon row above it and lights up the wrong thing.
type targetKind int

const (
	kindGrid targetKind = iota
	kindRibbon
	kindStrip
	// kindBanner is the one target that does not point at a pane, and the one
	// the cursor never lands on. It exists so a click has something to resolve
	// against; the keyboard reaches it through its own key instead.
	kindBanner
)

// rebuild recomputes the selectable targets after anything changes.
func (m *Model) rebuild() {
	prev := m.selectedKey()
	m.targets = nil

	// First, because it is the first thing on screen. It is skipped by the
	// walk in verticalOrder, so this only gives the click somewhere to land.
	if m.showBanner() {
		m.targets = append(m.targets,
			target{column: -1, row: -1, kind: kindBanner})
	}

	if !m.filtering && m.filter == "" {
		for _, a := range m.ribbonRows() {
			m.targets = append(m.targets,
				target{paneID: a.PaneID, column: -1, row: -1, kind: kindRibbon})
		}
	}

	// The same ordering the view draws, not the raw snapshot order. These were
	// two separate calls, so pressing s or moving a card with J left the cursor
	// walking one order while the screen showed another, and the column recorded
	// here disagreed with the column the card was painted in.
	tiles := m.orderedTiles(m.visibleTiles())
	cols := columnsFor(m.width)
	if m.filter != "" {
		cols = 1 // filtering always collapses to one list
	}
	for i, t := range tiles {
		col, row := 0, i
		if cols > 1 {
			col, row = i%cols, i/cols
		}
		m.targets = append(m.targets, target{
			paneID: t.Agent.PaneID, workspaceID: t.Workspace.ID,
			column: col, row: row,
		})
	}

	m.appendStripTarget()

	// Keep the cursor on whatever it was pointing at, so filtering and resizing
	// do not move the selection out from under you. Nothing selected stays
	// nothing selected: the overlay opens with no card claimed.
	//
	// A selection whose target has gone also ends up with nothing selected. It
	// used to fall back to index zero, which is the first ribbon row or the
	// banner, so an agent exiting while you had it selected silently moved the
	// selection to the thing that needs you most, and enter went there instead.
	m.cursor = noSelection
	if prev != "" {
		for i, t := range m.targets {
			if m.qualifiedKey(t) == prev {
				m.cursor = i
				break
			}
		}
	}

	// The lane is a grid column, and the grid narrows: one column while
	// filtering, and fewer when the terminal shrinks. A lane left pointing at a
	// column that no longer exists is empty, so up and down stopped working
	// until the cursor next landed on a card.
	if m.laneCol >= cols {
		m.laneCol = 0
	}

	// Hover is an index into the list just rebuilt. Leaving it alone highlighted
	// whatever inherited that index until the mouse next moved.
	m.hover = noSelection

	m.clampCursor()
}

// selectedKey identifies the current selection for restoring it after a
// rebuild, whether it is an agent, an empty workspace tile or the strip.
func (m *Model) selectedKey() string {
	if m.cursor < 0 || m.cursor >= len(m.targets) {
		return ""
	}
	return m.qualifiedKey(m.targets[m.cursor])
}

// qualifiedKey names a target uniquely across the whole screen.
//
// keyOf alone is not enough: the orchestrator holds a grid target and a strip
// target under one pane, and a ribbon agent holds two. Restoring by the bare
// key would land the cursor in whichever block came first, so a refresh could
// move the selection from the card you were on to the ribbon row above it.
func (m *Model) qualifiedKey(t target) string {
	return strconv.Itoa(int(t.kind)) + ":" + m.keyOf(t)
}

func (m *Model) keyOf(t target) string {
	if t.paneID != "" {
		return "pane:" + t.paneID
	}
	return "ws:" + t.workspaceID
}

func (m *Model) selectedPane() string {
	if m.cursor < 0 || m.cursor >= len(m.targets) {
		return ""
	}
	return m.targets[m.cursor].paneID
}

// selectedWorkspace is the workspace the cursor's tile belongs to, or "" when
// nothing is selected. Every grid target carries one, agent tiles included.
func (m *Model) selectedWorkspace() string {
	if m.cursor < 0 || m.cursor >= len(m.targets) {
		return ""
	}
	return m.targets[m.cursor].workspaceID
}

func (m *Model) clampCursor() {
	if len(m.targets) == 0 || m.cursor < 0 {
		m.cursor = noSelection
		return
	}
	if m.cursor >= len(m.targets) {
		m.cursor = len(m.targets) - 1
	}
}

// noSelection is the cursor before anyone has moved it. The overlay opens with
// nothing highlighted, so the first thing that lights up is the thing you
// pointed at or arrowed to, rather than whatever happened to sort first.
const noSelection = -1

// targetIndex finds the cursor position for a drawn item.
//
// Which target a rendered line should point at depends on which block the line
// is in, since one agent can hold a target in several.
func (m *Model) targetIndex(key string) int { return m.findTarget(key, kindGrid) }

// ribbonTargetIndex finds the ribbon row for an item.
func (m *Model) ribbonTargetIndex(key string) int { return m.findTarget(key, kindRibbon) }

// stripTargetIndex finds the orchestrator strip's target, or -1. There is only
// ever one, so it is found by kind rather than by key.
func (m *Model) stripTargetIndex() int {
	for i, t := range m.targets {
		if t.kind == kindStrip {
			return i
		}
	}
	return -1
}

// bannerTargetIndex finds the skill-install offer's target, or -1. There is
// only ever one, found by kind for the same reason the strip is.
func (m *Model) bannerTargetIndex() int {
	for i, t := range m.targets {
		if t.kind == kindBanner {
			return i
		}
	}
	return -1
}

// appendStripTarget puts the orchestrator strip last, after the grid, which is
// where it is drawn. It carries no workspaceID: selecting the strip must not
// also highlight the tile the orchestrator happens to live in.
func (m *Model) appendStripTarget() {
	if m.filter != "" || !m.snap.Orch.Found {
		return
	}
	m.targets = append(m.targets,
		target{paneID: m.snap.Orch.PaneID, column: -1, row: -1, kind: kindStrip})
}

func (m *Model) findTarget(key string, kind targetKind) int {
	for i, t := range m.targets {
		if t.kind == kind && m.keyOf(t) == key {
			return i
		}
	}
	return -1
}

// targetPane is the agent a target jumps to, or "" for an empty workspace
// tile.
func (m *Model) targetPane(i int) string {
	if i < 0 || i >= len(m.targets) {
		return ""
	}
	return m.targets[i].paneID
}

func (m *Model) isKind(i int, kind targetKind) bool {
	return i >= 0 && i < len(m.targets) && m.targets[i].kind == kind
}
