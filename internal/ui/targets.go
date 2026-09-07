// Targets are the things the selection can land on. This is where the list of
// them is built, identified and searched.

package ui

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
	// column is the grid column, used for left and right. Ribbon rows are -1.
	column int
	ribbon bool
}

// rebuild recomputes the selectable targets after anything changes.
func (m *Model) rebuild() {
	prev := m.selectedKey()
	m.targets = nil

	if !m.filtering && m.filter == "" {
		for _, a := range m.ribbonRows() {
			m.targets = append(m.targets, target{paneID: a.PaneID, column: -1, ribbon: true})
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

	// Keep the cursor on whatever it was pointing at, so filtering and resizing
	// do not move the selection out from under you.
	m.cursor = 0
	if prev != "" {
		for i, t := range m.targets {
			if m.keyOf(t) == prev {
				m.cursor = i
				break
			}
		}
	}
	m.clampCursor()
}

// selectedKey identifies the current selection for restoring it after a
// rebuild, whether it is an agent or a bare repo card.
func (m *Model) selectedKey() string {
	if m.cursor < 0 || m.cursor >= len(m.targets) {
		return ""
	}
	t := m.targets[m.cursor]
	if t.paneID != "" {
		return "pane:" + t.paneID
	}
	return "repo:" + t.repoKey
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
// An agent in the ribbon is also in the grid, so it has two targets. Which one
// a rendered line should point at depends on where the line is: a grid card must
// not resolve to the ribbon row above it, or clicking the card would move the
// cursor into the ribbon and light up the wrong thing.
func (m *Model) targetIndex(key string) int { return m.findTarget(key, false) }

// ribbonTargetIndex finds the ribbon row for an item.
func (m *Model) ribbonTargetIndex(key string) int { return m.findTarget(key, true) }

func (m *Model) findTarget(key string, ribbon bool) int {
	for i, t := range m.targets {
		if t.ribbon == ribbon && m.keyOf(t) == key {
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
func (m *Model) IsRibbonTarget(i int) bool {
	return i >= 0 && i < len(m.targets) && m.targets[i].ribbon
}

// Cursor is the keyboard selection, exposed for tests.
func (m *Model) Cursor() int { return m.cursor }
