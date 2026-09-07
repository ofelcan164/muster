package ui

import (
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ofelcan/muster/internal/model"
)

// Breakpoints are measured in columns of the tab area, which is what an overlay
// actually gets: the terminal minus herdr's sidebar.
//
// Checked against real terminals: a fullscreen 13" laptop gives 143 columns, and
// the same laptop with the window shrunk gives 53. Both cases have to work, and
// the narrow one is not an edge case, it is what you get whenever you split.
const (
	twoColumnMin   = 70
	threeColumnMin = 120
	fourColumnMin  = 200
)

func columnsFor(width int) int {
	switch {
	case width >= fourColumnMin:
		return 4
	case width >= threeColumnMin:
		return 3
	case width >= twoColumnMin:
		return 2
	default:
		return 1
	}
}

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

type Model struct {
	snap *model.Snapshot

	// hits are the clickable regions the view drew, rebuilt on every render.
	// A terminal click gives coordinates and nothing else, so the only way to
	// know what was clicked is to remember where things were put.
	hits []hitRegion

	// hover is the target under the pointer, or -1.
	hover int

	width, height int
	cursor        int
	targets       []target

	// filtering is entered with "/", following herdr's own convention.
	//
	// The plan had any bare letter start filtering, which forced the awkward
	// rule that vim keys only worked while the query was empty and made every
	// other letter unavailable for anything else. An explicit "/" costs one
	// keystroke and gives the whole alphabet back for navigation and actions.
	filtering bool
	filter    string

	// jump is set when the user picked something. main focuses it and exits,
	// which is the whole of jumping: no close call and no delay.
	jump string

	// sort is how the grid is ordered, and moves is the manual arrangement
	// layered on top of it.
	sort  SortMode
	moves []string

	warning string
	quit    bool
}

func New(snap *model.Snapshot, warning string) *Model {
	m := &Model{snap: snap, warning: warning, width: 80, height: 24, hover: -1}
	m.rebuild()
	return m
}

func (m *Model) Init() tea.Cmd { return nil }

// Jump returns the pane the user chose, or "".
func (m *Model) Jump() string { return m.jump }

// visibleRepos applies the filter. Filtering collapses the grid into a flat
// ranked list, because once you are filtering you already know what you want
// and spatial memory is not doing any work.
func (m *Model) visibleRepos() []model.Repo {
	if m.filter == "" {
		return m.snap.Repos
	}
	q := strings.ToLower(m.filter)
	var out []model.Repo
	for _, r := range m.snap.Repos {
		// A repo that matches on its own name or branch is a hit, whether or not
		// anything is running in it. Requiring a matching agent meant searching
		// for a repo with no agents found nothing at all, which is exactly the
		// case you hit when looking for somewhere to start work.
		if repoMatches(r, q) {
			out = append(out, r)
			continue
		}
		var kept []model.Agent
		for _, a := range r.Agents {
			if agentMatches(a, q) {
				kept = append(kept, a)
			}
		}
		if len(kept) > 0 {
			r.Agents = kept
			out = append(out, r)
		}
	}
	return out
}

// repoMatches searches the fields that belong to the repo itself.
func repoMatches(r model.Repo, q string) bool {
	return containsAny(q, r.Display, r.Name, r.Branch)
}

// agentMatches searches the fields that belong to an agent. Together these
// cover agent name, task text, repo and branch, which is what the design says
// the filter should look at.
func agentMatches(a model.Agent, q string) bool {
	return containsAny(q, a.Name, a.Task, a.Question, a.PaneID)
}

func containsAny(q string, fields ...string) bool {
	for _, f := range fields {
		if f != "" && strings.Contains(strings.ToLower(f), q) {
			return true
		}
	}
	return false
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

	repos := m.visibleRepos()
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

func (m *Model) selectedPane() string {
	if m.cursor < 0 || m.cursor >= len(m.targets) {
		return ""
	}
	return m.targets[m.cursor].paneID
}

// ribbonRows is the ranked ribbon, capped by the daemon and never more than
// four. It disappears entirely when nothing needs you, which is the point.
func (m *Model) ribbonRows() []model.Attention {
	return m.snap.Attention
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.rebuild()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.MouseMsg:
		return m.handleMouse(msg)
	}
	return m, nil
}

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
		if idx, ok := m.targetAt(msg.X, msg.Y); ok {
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

// targetIndex finds the cursor position for a drawn item.
func (m *Model) targetIndex(key string) int {
	for i, t := range m.targets {
		if m.keyOf(t) == key {
			return i
		}
	}
	return -1
}

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

// agentByPane finds an agent and its repo, for rendering the selected row.
func (m *Model) agentByPane(paneID string) (model.Repo, model.Agent, bool) {
	for _, r := range m.snap.Repos {
		for _, a := range r.Agents {
			if a.PaneID == paneID {
				return r, a, true
			}
		}
	}
	return model.Repo{}, model.Agent{}, false
}

// sortedRepos returns repos in grid slot order, which is what pins a repo to
// the same cell for good.
func sortedRepos(repos []model.Repo) []model.Repo {
	out := append([]model.Repo(nil), repos...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].GridSlot < out[j].GridSlot })
	return out
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

// TargetPane is the agent a target jumps to, or "" for a bare repo card.
func (m *Model) TargetPane(i int) string {
	if i < 0 || i >= len(m.targets) {
		return ""
	}
	return m.targets[i].paneID
}

// Cursor and Hover are exposed for tests.
func (m *Model) Cursor() int { return m.cursor }
func (m *Model) Hover() int  { return m.hover }
