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

// target is something the selection can land on. Ribbon rows and grid agents
// are both targets, so one cursor covers the whole screen.
type target struct {
	paneID string
	// column is the grid column a target sits in, used for left and right.
	// Ribbon rows are column -1.
	column int
	ribbon bool
}

type Model struct {
	snap *model.Snapshot

	width, height int
	cursor        int
	targets       []target

	// filtering is the mode the plan resolves Q16 with: arrows always work, any
	// letter enters filter mode, and vim keys come back when the filter is
	// empty. Without the mode there is no free letter for anything.
	filtering bool
	filter    string

	// jump is set when the user picked something. main focuses it and exits,
	// which is the whole of jumping: no close call and no delay.
	jump string

	warning string
	quit    bool
}

func New(snap *model.Snapshot, warning string) *Model {
	m := &Model{snap: snap, warning: warning, width: 80, height: 24}
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
		var kept []model.Agent
		for _, a := range r.Agents {
			if matchesFilter(r, a, q) {
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

// matchesFilter searches agent name, task text, repo and branch, which is what
// the design says the filter covers.
func matchesFilter(r model.Repo, a model.Agent, q string) bool {
	for _, field := range []string{a.Name, a.Task, a.Question, r.Display, r.Name, r.Branch} {
		if strings.Contains(strings.ToLower(field), q) {
			return true
		}
	}
	return false
}

// rebuild recomputes the selectable targets after anything changes.
func (m *Model) rebuild() {
	prev := m.selectedPane()
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
		for _, a := range r.Agents {
			m.targets = append(m.targets, target{paneID: a.PaneID, column: col})
		}
	}

	// Keep the cursor on whatever it was pointing at, so filtering and resizing
	// do not move the selection out from under you.
	m.cursor = 0
	if prev != "" {
		for i, t := range m.targets {
			if t.paneID == prev {
				m.cursor = i
				break
			}
		}
	}
	m.clampCursor()
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
	}
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Filter mode owns every printable key, which is exactly why it has to be a
	// mode: hjkl are literal text while you are typing.
	if m.filtering {
		switch msg.Type {
		case tea.KeyEsc:
			m.filtering, m.filter = false, ""
			m.rebuild()
			return m, nil
		case tea.KeyEnter:
			return m.activate()
		case tea.KeyBackspace:
			if m.filter != "" {
				m.filter = m.filter[:len(m.filter)-1]
			}
			if m.filter == "" {
				m.filtering = false
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

	// Any other letter starts filtering.
	if len(msg.Runes) == 1 {
		m.filtering = true
		m.filter = string(msg.Runes)
		m.rebuild()
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
