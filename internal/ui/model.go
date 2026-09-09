package ui

import (
	"time"

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

	// laneCol is the grid column up and down stay in, remembered across the
	// full-width blocks that have no column of their own.
	laneCol int

	targets []target

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

	// composing is the i input, open on the orchestrator strip. It is a mode
	// for the same reason filtering is: every printable key is text while it is
	// open.
	composing bool
	compose   string

	// notice is what just happened, shown on the strip. A full-screen overlay
	// has nowhere else to report that a send worked or failed.
	notice string

	// prompt sends a message to an agent. Injected so the model stays testable
	// without a socket, and so a test can never prompt a real agent.
	prompt func(paneID, text string) error

	// sort is how the grid is ordered, and moves is the manual arrangement
	// layered on top of it.
	sort  SortMode
	moves []string

	// saveSort persists the sort mode, injected the same way saveOrder is.
	saveSort func(SortMode)

	// saveOrder persists the manual arrangement. Injected the same way reload
	// is, so the model never touches the filesystem itself.
	saveOrder func([]string)

	// reload fetches a fresh snapshot. Injected so the model stays testable
	// without touching the filesystem.
	reload func() *model.Snapshot

	warning string
	quit    bool
}

func New(snap *model.Snapshot, warning string) *Model {
	m := &Model{snap: snap, warning: warning, width: 80, height: 24,
		hover: noSelection, cursor: noSelection}
	m.rebuild()
	return m
}

// refreshInterval is how often the overlay re-reads the snapshot.
//
// The overlay is meant to be short-lived, spawned per keypress, so it used to
// read once and never again. Left open, it froze: an agent could go from
// working to blocked and the screen would still show the old state while
// herdr's own sidebar showed the new one. A read costs about 25 microseconds,
// so refreshing is cheaper than reasoning about when not to.
const refreshInterval = 700 * time.Millisecond

type refreshMsg struct{}

func refreshTick() tea.Cmd {
	return tea.Tick(refreshInterval, func(time.Time) tea.Msg { return refreshMsg{} })
}

func (m *Model) Init() tea.Cmd { return refreshTick() }

// SetSnapshot swaps in fresh data, keeping the selection where it was.
func (m *Model) SetSnapshot(s *model.Snapshot) {
	if s == nil {
		return
	}
	m.snap = s
	m.rebuild()
}

// SetReloader supplies the function the overlay calls to refresh itself.
func (m *Model) SetReloader(f func() *model.Snapshot) { m.reload = f }

// SetManualOrder restores an arrangement made in an earlier session.
func (m *Model) SetManualOrder(order []string) {
	m.moves = order
	m.rebuild()
}

// SetOrderSaver supplies the function that persists the manual arrangement.
func (m *Model) SetOrderSaver(f func([]string)) { m.saveOrder = f }

// SetSort restores the sort mode chosen in an earlier session. A value from a
// file that no longer means anything falls back to the default rather than
// leaving the cycle stuck outside its range.
func (m *Model) SetSort(s SortMode) {
	if s < 0 || s >= sortModeCount {
		s = SortFirstSeen
	}
	m.sort = s
	m.rebuild()
}

// SetSortSaver supplies the function that persists the sort mode.
func (m *Model) SetSortSaver(f func(SortMode)) { m.saveSort = f }

// Sort is the current sort mode, exposed for tests.
func (m *Model) Sort() SortMode { return m.sort }

// ManualOrder is the current arrangement, exposed for tests.
func (m *Model) ManualOrder() []string { return m.moves }

// Jump returns the pane the user chose, or "".
func (m *Model) Jump() string { return m.jump }

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

	case refreshMsg:
		if m.reload != nil {
			m.SetSnapshot(m.reload())
		}
		return m, refreshTick()
	}
	return m, nil
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

// repoByKey finds a repo by its identity key.
//
// Ribbon rows resolve their repo this way rather than through the pane, because
// a stopped-process row points at a non-agent pane and so has no agent to look
// up. That left those rows with a blank sigil.
func (m *Model) repoByKey(key string) model.Repo {
	for _, r := range m.snap.Repos {
		if r.Key == key {
			return r
		}
	}
	return model.Repo{ColorIndex: -1, Sigil: "·"}
}
