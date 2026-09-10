// Keyboard handling: the key map, and how the cursor moves through the targets
// rebuild produced.

package ui

import (
	"cmp"
	"fmt"
	"slices"

	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.composing {
		return m.handleCompose(msg)
	}
	// Any key that is not the input itself clears the last notice, so a report
	// of what just happened does not sit there through the next thing you do.
	m.notice = ""

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

	case "i":
		// Talk to the orchestrator. Only worth opening when there is one.
		if m.snap.Orch.Found {
			m.composing, m.compose = true, ""
		} else {
			m.notice = "no orchestrator marked, so there is nobody to tell"
		}
		return m, nil

	case "t":
		// The repair key. This is the whole reason gate detection exists.
		return m.repair()

	case "S":
		// Install the reporting skill. Its own key rather than enter on the
		// banner: enter means go to the thing you picked, everywhere else on
		// this screen, and writing a file into someone's home is not that.
		return m.runSkillInstall()

	case "x":
		// Acknowledge a ribbon row you have already dealt with.
		return m.dismissSelected()

	case "o":
		// Marking used to mean finding the orchestrator's pane and running an
		// action on it. The overlay already lists every agent.
		return m.markSelected()

	case "s":
		// Cycle the sort. First seen is the default and where it returns to.
		m.sort = m.sort.Next()
		m.rebuild()
		if m.saveSort != nil {
			m.saveSort(m.sort)
		}
		return m, nil

	case "K":
		m.moveSelectedRepo(-1)
		return m, nil
	case "J":
		m.moveSelectedRepo(1)
		return m, nil

	case "g", "G":
		// The ends of the screen, which are the ends of the vertical walk.
		if order := m.verticalOrder(); len(order) > 0 {
			m.cursor = order[0]
			if msg.String() == "G" {
				m.cursor = order[len(order)-1]
			}
		}
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
	// Every keystroke of a query rebuilds the targets, and a rebuild with
	// nothing previously selected selects nothing, so enter after typing had no
	// target at all and did nothing. Searching is the one mode with an obvious
	// first result to mean.
	if m.cursor == noSelection && m.filter != "" && len(m.targets) > 0 {
		m.cursor = 0
	}
	p := m.selectedPane()
	if p == "" {
		// A repo card with no agents still has panes behind it: a shell, an
		// editor, whatever is open there. One of those is an obvious answer and
		// keeps every tile openable. Nine of them is not an answer at all:
		// jumping to the first of nine shells spread across nine workspaces
		// takes the screen somewhere nobody asked to go. The t key already
		// refuses to guess between several gates for the same reason.
		r := m.repoByKey(m.selectedRepo())
		switch len(r.OtherPanes) {
		case 0:
			m.notice = shortRepo(r) + " has nothing open to jump to"
		case 1:
			p = r.OtherPanes[0].PaneID
		default:
			m.notice = fmt.Sprintf("%s has no agent, and %s with nothing to choose between them",
				shortRepo(r), plural(len(r.OtherPanes), "pane"))
		}
	}
	if p == "" {
		return m, nil
	}
	m.jump = p
	return m, tea.Quit
}

// runSkillInstall puts the reporting skill in place and takes the offer off the
// screen, which is the whole of dismissing it: the banner reports a condition,
// so fixing the condition is what makes it go.
func (m *Model) runSkillInstall() (tea.Model, tea.Cmd) {
	if !m.showBanner() {
		// No offer on screen means no key. A letter that silently reinstalls a
		// skill already in place is a keystroke nobody can predict.
		return m, nil
	}
	path, err := m.installSkill()
	if err != nil {
		m.notice = "could not install the skill: " + err.Error()
		return m, nil
	}
	m.skillMissing = false
	m.rebuild()
	m.notice = "installed the reporting skill to " + path
	return m, nil
}

// dismissSelected takes the selected ribbon row off the ribbon until that
// agent's status changes again. Rows otherwise leave only when the underlying
// state does, or after the stopped-process TTL, so something you have already
// handled sits in a ribbon that only holds four things.
func (m *Model) dismissSelected() (tea.Model, tea.Cmd) {
	if m.cursor < 0 || m.cursor >= len(m.targets) || m.targets[m.cursor].kind != kindRibbon {
		m.notice = "x dismisses a row that needs you: select one first"
		return m, nil
	}
	pane := m.targets[m.cursor].paneID
	for _, a := range m.ribbonRows() {
		if a.PaneID != pane {
			continue
		}
		if m.dismissed == nil {
			m.dismissed = map[string]string{}
		}
		m.dismissed[pane] = string(a.Status)
		m.pruneDismissed()
		m.rebuild()
		if m.saveDismissed != nil {
			m.saveDismissed(m.dismissed)
		}
		break
	}
	return m, nil
}

// markSelected makes the selected agent the orchestrator.
func (m *Model) markSelected() (tea.Model, tea.Cmd) {
	pane := m.selectedPane()
	if _, _, ok := m.agentByPane(pane); !ok {
		// A bare repo card, or a ribbon row for a stopped process, which points
		// at a pane with no agent in it to rename.
		m.notice = "o marks an agent as the orchestrator: select one first"
		return m, nil
	}
	if m.snap.Orch.Found && m.snap.Orch.PaneID == pane {
		m.notice = "already the orchestrator"
		return m, nil
	}
	if m.mark == nil {
		return m, nil
	}
	// Off the Update goroutine for the same reason send is: a wedged socket
	// must not stop the overlay reading keys.
	m.notice = "marking…"
	mark := m.mark
	return m, func() tea.Msg {
		if err := mark(pane); err != nil {
			return noticeMsg("could not mark: " + err.Error())
		}
		// The strip cannot show the mark until the daemon reconciles, a few
		// seconds away, so say it happened rather than leaving the screen
		// looking unchanged.
		return noticeMsg("marked as the orchestrator")
	}
}

// move steps up or down the screen, not along the target list. The grid is
// filled row-major, so the next target is the card to the right: walking the
// list made "down" go sideways.
func (m *Model) move(delta int) {
	order := m.lane()
	if len(order) == 0 {
		return
	}
	// Nothing selected yet: the first move lands on the end you came from.
	at := 0
	if delta < 0 {
		at = len(order) - 1
	}
	for i, t := range order {
		if t == m.cursor {
			// Wrap, so holding a key never dead-ends.
			at = (i + delta%len(order) + len(order)) % len(order)
			break
		}
	}
	m.cursor = order[at]
}

// lane is what up and down actually walk: the ribbon, one grid column, and the
// strip. Wrapping stays inside the column, the way left and right stay inside
// their row. The full-width blocks belong to every column, so wrapping past the
// bottom of a column comes back around through them rather than sideways.
func (m *Model) lane() []int {
	// The ribbon and the strip have no column of their own, so the lane keeps
	// the last one the cursor was in. Without that, walking down through the
	// ribbon would drop you into column zero from wherever you started.
	if m.cursor >= 0 && m.cursor < len(m.targets) && m.targets[m.cursor].kind == kindGrid {
		m.laneCol = m.targets[m.cursor].column
	}
	order := m.verticalOrder()
	out := make([]int, 0, len(order))
	for _, i := range order {
		if t := m.targets[i]; t.kind != kindGrid || t.column == m.laneCol {
			out = append(out, i)
		}
	}
	return out
}

// verticalOrder is the whole screen top to bottom: the ribbon, then each grid
// column from top to bottom, then the strip. Within one card its agents come in
// the order they are drawn, so down steps through a card before leaving it.
func (m *Model) verticalOrder() []int {
	order := make([]int, 0, len(m.targets))
	for i := range m.targets {
		// The banner is clickable but is not a stop on the walk. It runs
		// something rather than going somewhere, so arrowing onto it would
		// offer a selection that enter cannot act on.
		if m.targets[i].kind == kindBanner {
			continue
		}
		order = append(order, i)
	}
	slices.SortStableFunc(order, func(a, b int) int {
		x, y := m.targets[a], m.targets[b]
		if x.kind != y.kind {
			return cmp.Compare(blockRank(x.kind), blockRank(y.kind))
		}
		if x.kind != kindGrid {
			return 0 // stable: keep the drawn order
		}
		return cmp.Compare(x.column, y.column)
	})
	return order
}

// blockRank is where a block sits on screen, which is not the order the kinds
// are declared in.
func blockRank(k targetKind) int {
	switch k {
	case kindRibbon:
		return 0
	case kindGrid:
		return 1
	default:
		return 2
	}
}

// firstGridTarget puts the cursor on the first card, for the keys that mean
// "into the grid" without naming a card.
func (m *Model) firstGridTarget() {
	for i, t := range m.targets {
		if t.kind == kindGrid {
			m.cursor = i
			return
		}
	}
}

// moveColumn steps sideways across the grid, landing on the nearest target in
// the adjacent column rather than counting rows.
func (m *Model) moveColumn(delta int) {
	if len(m.targets) == 0 {
		return
	}
	if m.cursor < 0 {
		m.firstGridTarget()
		return
	}
	cur := m.targets[m.cursor]
	if cur.kind != kindGrid {
		// Left and right do nothing useful in a one-per-line ribbon or on the
		// strip, so they drop you into the grid instead.
		m.firstGridTarget()
		return
	}

	cols := columnsFor(m.width)
	if cols <= 1 {
		return
	}
	want := (cur.column + delta + cols) % cols

	// Prefer the card in the wanted column on the nearest grid row, so sideways
	// movement stays on the row you were already reading.
	best, bestDist := -1, 1<<30
	for i, t := range m.targets {
		if t.kind != kindGrid || t.column != want {
			continue
		}
		d := t.row - cur.row
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
