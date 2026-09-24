// Mouse handling, and the hit regions it resolves against.
//
// A terminal click gives coordinates and nothing else, so the only way to know
// what was clicked is to remember where the last render put things. The window
// scrolls, so the regions move with it; see Model.window. herdr
// delivers those coordinates pane-local and 0-based, measured against a real
// mouse; see docs/herdr-api-notes.md.

package ui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// handleMouse makes the overlay clickable. One click jumps, the wheel moves
// through the list, and dragging the orchestrator strip's rule resizes it.
func (m *Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.resizing {
		// The drag owns the pointer until the button comes up, wherever it
		// wanders: letting motion hover tiles would light them mid-drag.
		// Release is taken with any button, because legacy mouse encoding
		// does not say which one came up.
		switch {
		case msg.Action == tea.MouseActionMotion && msg.Button == tea.MouseButtonNone:
			// Moving with no button held: the release happened outside the
			// popup, where herdr drops it. The drag ended wherever it last was.
			m.endDrag()
		case msg.Action == tea.MouseActionMotion:
			m.dragTo(msg.Y)
		case msg.Action == tea.MouseActionRelease:
			m.dragTo(msg.Y)
			m.endDrag()
		}
		return m, nil
	}
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
		if ok {
			m.hover = idx
		} else {
			m.hover = -1
		}
		return m, nil
	}

	if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
		// Clicks act on release, but a drag has to start on the press.
		if idx, ok := m.targetAt(msg.X, msg.Y); ok && idx == gripTarget {
			m.resizing = true
		}
		return m, nil
	}
	if msg.Action != tea.MouseActionRelease || msg.Button != tea.MouseButtonLeft {
		return m, nil
	}
	idx, ok := m.targetAt(msg.X, msg.Y)
	if !ok || idx == gripTarget {
		return m, nil
	}
	if idx == moreTarget {
		m.sayMore = !m.sayMore
		return m, nil
	}
	if m.isKind(idx, kindBanner) {
		// The banner is not a selection, so clicking it must not move the
		// cursor off whatever the keyboard was on.
		return m.runSkillInstall()
	}
	// One click goes. Requiring a click to select and another to commit makes
	// the mouse slower than the keyboard, which defeats the point of having it.
	m.cursor = idx
	return m.activate()
}

// endDrag stops resizing the strip and keeps the height it was left at.
func (m *Model) endDrag() {
	m.resizing = false
	if m.saveSayRows != nil {
		m.saveSayRows(m.sayRows)
	}
}

// hitRegion is a rectangle of the screen belonging to one target. Cards claim
// their whole area rather than a single line, so clicking anywhere on a card
// selects it, including its task line and its footer.
type hitRegion struct {
	y      int
	x0, x1 int // inclusive
	target int
}

// moreTarget is the region of the strip's more/less link. It is no target: the
// cursor never lands on it, and isActive ignores it like any negative index.
const moreTarget = -2

// gripTarget is the region of the strip's rule, which a drag resizes the strip
// by. No target either, for the same reasons.
const gripTarget = -3

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
