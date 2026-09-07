// Keyboard handling: the key map, and how the cursor moves through the targets
// rebuild produced.

package ui

import (
	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Filter mode owns every printable key, which is exactly why it has to be a
	// mode: hjkl are literal text while you are typing.
	if m.filtering {
		switch msg.Type {
		case tea.KeyEsc:
			// Leave the typing mode but keep the results, so you can navigate
			// what you just searched for. A second escape clears the query.
			m.filtering = false
			return m, nil
		case tea.KeyEnter:
			return m.activate()
		case tea.KeyBackspace:
			if m.filter != "" {
				m.filter = m.filter[:len(m.filter)-1]
			}
			m.rebuild()
			return m, nil
		case tea.KeyUp:
			m.move(-1)
			return m, nil
		case tea.KeyDown:
			m.move(1)
			return m, nil
		case tea.KeyCtrlC:
			m.quit = true
			return m, tea.Quit
		case tea.KeyRunes, tea.KeySpace:
			m.filter += string(msg.Runes)
			if msg.Type == tea.KeySpace {
				m.filter += " "
			}
			m.rebuild()
			return m, nil
		}
		return m, nil
	}

	switch msg.String() {
	case "ctrl+c", "q":
		m.quit = true
		return m, tea.Quit

	case "esc":
		// Clear the filter first, then close. Escape never does both at once.
		if m.filter != "" {
			m.filter = ""
			m.rebuild()
			return m, nil
		}
		m.quit = true
		return m, tea.Quit

	case "enter":
		return m.activate()

	case "/":
		m.filtering = true
		return m, nil

	case "up", "k":
		m.move(-1)
		return m, nil
	case "down", "j":
		m.move(1)
		return m, nil
	case "left", "h":
		m.moveColumn(-1)
		return m, nil
	case "right", "l":
		m.moveColumn(1)
		return m, nil

	case "s":
		// Cycle the sort. First seen is the default and where it returns to.
		m.sort = m.sort.Next()
		m.rebuild()
		return m, nil

	case "K":
		m.moveSelectedRepo(-1)
		return m, nil
	case "J":
		m.moveSelectedRepo(1)
		return m, nil

	case "g":
		m.cursor = 0
		return m, nil
	case "G":
		m.cursor = len(m.targets) - 1
		m.clampCursor()
		return m, nil
	}

	// Digits jump straight to a ribbon row, so the thing that needs you most is
	// always one keystroke away.
	if len(msg.Runes) == 1 && msg.Runes[0] >= '1' && msg.Runes[0] <= '9' {
		n := int(msg.Runes[0] - '1')
		rows := m.ribbonRows()
		if n < len(rows) {
			m.jump = rows[n].PaneID
			return m, tea.Quit
		}
		return m, nil
	}

	return m, nil
}

func (m *Model) activate() (tea.Model, tea.Cmd) {
	if p := m.selectedPane(); p != "" {
		m.jump = p
		return m, tea.Quit
	}
	return m, nil
}

func (m *Model) move(delta int) {
	if len(m.targets) == 0 {
		return
	}
	m.cursor += delta
	// Wrap, so holding a key never dead-ends.
	if m.cursor < 0 {
		m.cursor = len(m.targets) - 1
	}
	if m.cursor >= len(m.targets) {
		m.cursor = 0
	}
}

// moveColumn steps sideways across the grid, landing on the nearest target in
// the adjacent column rather than counting rows.
func (m *Model) moveColumn(delta int) {
	if len(m.targets) == 0 {
		return
	}
	cur := m.targets[m.cursor]
	if cur.ribbon {
		// Left and right do nothing useful in a one-per-line ribbon, so they
		// drop you into the grid instead.
		for i, t := range m.targets {
			if !t.ribbon {
				m.cursor = i
				return
			}
		}
		return
	}

	cols := columnsFor(m.width)
	if cols <= 1 {
		return
	}
	want := (cur.column + delta + cols) % cols

	// Prefer a target in the wanted column closest to where the cursor already
	// is, which keeps sideways movement feeling like it stays on the same row.
	best, bestDist := -1, 1<<30
	for i, t := range m.targets {
		if t.ribbon || t.column != want {
			continue
		}
		d := i - m.cursor
		if d < 0 {
			d = -d
		}
		if d < bestDist {
			best, bestDist = i, d
		}
	}
	if best >= 0 {
		m.cursor = best
	}
}
