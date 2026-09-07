// Targets are the things the selection can land on. This is where the list of
// them is built, identified and searched.

package ui

import "strconv"

// target is something the selection can land on.
//
// A repo with no agents is still a target. Selection used to cover agents only,
// which meant that with two agents across seven repos, five cards could not be
// reached at all and the arrow keys looked broken.
type target struct {
	// paneID is the agent to jump to, or "" for a repo card with no agents.
	paneID string
	// repoKey identifies the card the target sits in.
	repoKey string
	// column is the grid column, used for left and right. Rows outside the grid
	// are -1.
	column int
	kind   targetKind
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
)

// rebuild recomputes the selectable targets after anything changes.
func (m *Model) rebuild() {
	prev := m.selectedKey()
	m.targets = nil

	if !m.filtering && m.filter == "" {
		for _, a := range m.ribbonRows() {
			m.targets = append(m.targets, target{paneID: a.PaneID, column: -1, kind: kindRibbon})
		}
	}

	// The same ordering the view draws, not the raw snapshot order. These were
	// two separate calls, so pressing s or moving a card with J left the cursor
	// walking one order while the screen showed another, and the column recorded
	// here disagreed with the column the card was painted in.
	repos := m.orderedRepos(m.visibleRepos())
	cols := columnsFor(m.width)
	if m.filter != "" {
		cols = 1 // filtering always collapses to one list
	}
	for i, r := range repos {
		col := 0
		if cols > 1 {
			col = i % cols
		}
		if len(r.Agents) == 0 {
			// The card itself. Selecting it does not jump anywhere, but it keeps
			// every repo reachable and the grid navigable.
			m.targets = append(m.targets, target{repoKey: r.Key, column: col})
			continue
		}
		for _, a := range r.Agents {
			m.targets = append(m.targets, target{paneID: a.PaneID, repoKey: r.Key, column: col})
		}
	}

	m.appendStripTarget()

	// Keep the cursor on whatever it was pointing at, so filtering and resizing
	// do not move the selection out from under you.
	m.cursor = 0
	if prev != "" {
		for i, t := range m.targets {
			if m.qualifiedKey(t) == prev {
				m.cursor = i
				break
			}
		}
	}
	m.clampCursor()
}

// selectedKey identifies the current selection for restoring it after a
// rebuild, whether it is an agent, a bare repo card or the strip.
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
	return "repo:" + t.repoKey
}

// selectedRepo is the repo the cursor is in, for highlighting the whole card.
func (m *Model) selectedRepo() string {
	if m.cursor < 0 || m.cursor >= len(m.targets) {
		return ""
	}
	return m.targets[m.cursor].repoKey
}

func (m *Model) selectedPane() string {
	if m.cursor < 0 || m.cursor >= len(m.targets) {
		return ""
	}
	return m.targets[m.cursor].paneID
}

func (m *Model) clampCursor() {
	if len(m.targets) == 0 {
		m.cursor = 0
		return
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.targets) {
		m.cursor = len(m.targets) - 1
	}
}

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

// appendStripTarget puts the orchestrator strip last, after the grid, which is
// where it is drawn. It carries no repoKey: selecting the strip must not also
// highlight the card the orchestrator happens to live in.
func (m *Model) appendStripTarget() {
	if m.filter != "" || !m.snap.Orch.Found {
		return
	}
	m.targets = append(m.targets,
		target{paneID: m.snap.Orch.PaneID, column: -1, kind: kindStrip})
}

func (m *Model) findTarget(key string, kind targetKind) int {
	for i, t := range m.targets {
		if t.kind == kind && m.keyOf(t) == key {
			return i
		}
	}
	return -1
}

// TargetCount and ReachableRepos exist for tests and diagnostics: they report
// what the cursor can actually get to.
func (m *Model) TargetCount() int { return len(m.targets) }

func (m *Model) ReachableRepos() int {
	seen := map[string]bool{}
	for _, t := range m.targets {
		if t.repoKey != "" {
			seen[t.repoKey] = true
		}
	}
	return len(seen)
}

// TargetPane is the agent a target jumps to, or "" for a bare repo card.
func (m *Model) TargetPane(i int) string {
	if i < 0 || i >= len(m.targets) {
		return ""
	}
	return m.targets[i].paneID
}

// IsRibbonTarget reports whether a target is a ribbon row, for tests.
func (m *Model) IsRibbonTarget(i int) bool { return m.isKind(i, kindRibbon) }

// IsStripTarget reports whether a target is the orchestrator strip, for tests.
func (m *Model) IsStripTarget(i int) bool { return m.isKind(i, kindStrip) }

func (m *Model) isKind(i int, kind targetKind) bool {
	return i >= 0 && i < len(m.targets) && m.targets[i].kind == kind
}

// Cursor is the keyboard selection, exposed for tests.
func (m *Model) Cursor() int { return m.cursor }
