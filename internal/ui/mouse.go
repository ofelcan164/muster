// Mouse handling, and the hit regions it resolves against.
//
// A terminal click gives coordinates and nothing else, so the only way to know
// what was clicked is to remember where the last render put things. herdr
// delivers those coordinates pane-local and 0-based, measured against a real
// mouse; see docs/herdr-api-notes.md.

package ui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// handleMouse makes the overlay clickable. Click to select, click the selection
// again to jump, and scroll to move through the list.
func (m *Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		m.move(-1)
		return m, nil
	case tea.MouseButtonWheelDown:
		m.move(1)
		return m, nil
	}
	// Motion gives the hover highlight, so the card under the pointer lights up
	// the same way the keyboard selection does.
	if msg.Action == tea.MouseActionMotion {
		idx, ok := m.targetAt(msg.X, msg.Y)
		m.logMouse(msg, idx, ok)
		if ok {
			m.hover = idx
		} else {
			m.hover = -1
		}
		return m, nil
	}

	if msg.Action != tea.MouseActionRelease || msg.Button != tea.MouseButtonLeft {
		return m, nil
	}
	idx, ok := m.targetAt(msg.X, msg.Y)
	m.logMouse(msg, idx, ok)
	if !ok {
		return m, nil
	}
	// One click goes. Requiring a click to select and another to commit makes
	// the mouse slower than the keyboard, which defeats the point of having it.
	m.cursor = idx
	return m.activate()
}

// hitRegion is a rectangle of the screen belonging to one target. Cards claim
// their whole area rather than a single line, so clicking anywhere on a card
// selects it, including its task line and its footer.
type hitRegion struct {
	y      int
	x0, x1 int // inclusive
	target int
}

// noteRegion records that a target occupies part of a screen line.
func (m *Model) noteRegion(y, x0, x1, targetIndex int) {
	m.hits = append(m.hits, hitRegion{y: y, x0: x0, x1: x1, target: targetIndex})
}

func (m *Model) resetRows() { m.hits = m.hits[:0] }

// targetAt finds the target under a screen position.
func (m *Model) targetAt(x, y int) (int, bool) {
	for _, h := range m.hits {
		if h.y == y && x >= h.x0 && x <= h.x1 {
			return h.target, true
		}
	}
	return -1, false
}

// Hit is a clickable region, exposed for tests.
type Hit struct {
	Y, X0, X1, Target int
}

// Hits reports the regions the last render made clickable.
func (m *Model) Hits() []Hit {
	out := make([]Hit, 0, len(m.hits))
	for _, h := range m.hits {
		out = append(out, Hit{Y: h.y, X0: h.x0, X1: h.x1, Target: h.target})
	}
	return out
}

// Hover is the target under the pointer, exposed for tests.
func (m *Model) Hover() int { return m.hover }
