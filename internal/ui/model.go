package ui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ofelcan164/muster/internal/identity"
	"github.com/ofelcan164/muster/internal/model"
)

// Breakpoints are measured in columns of what the popup gets: the whole tab
// area, which is the terminal minus herdr's sidebar and the popup's border.
//
// Checked against real terminals: a fullscreen 13" laptop gives a 143-column
// tab area, so three columns, and the same laptop with the window shrunk gives
// 53. Both cases have to work, and the narrow one is not an edge case, it is
// what a half-screen window gets.
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

	// skillMissing drives the banner: the reporting skill is what writes every
	// task line on the screen, and without it every card falls back to a
	// terminal title. installSkill puts it there, injected the same way the
	// prompter is so a test never writes into anyone's home directory.
	skillMissing bool
	installSkill func() (string, error)

	// prompt sends a message to an agent. Injected so the model stays testable
	// without a socket, and so a test can never prompt a real agent.
	prompt func(paneID, text string) error

	// mark tags an agent as the orchestrator, injected for the same reason: a
	// test must never rename one of the user's agents.
	mark func(paneID string) error

	// sort is how the grid is ordered.
	sort SortMode

	// saveSort persists the sort mode, injected the same way saveDismissed is.
	saveSort func(SortMode)

	// dismissed is the acknowledged ribbon rows, pane id to the status each was
	// dismissed at, with saveDismissed persisting them.
	dismissed     map[string]string
	saveDismissed func(map[string]string)

	// colors is the repo colours picked with c, repo key to palette slot, laid
	// over every snapshot. saveColors persists them.
	colors     map[string]int
	saveColors func(map[string]int)

	// moveWorkspace asks herdr to move a workspace to a new position, for J and
	// K in herdr sort. Injected the same way mark and prompt are, so a test
	// never moves one of the user's real workspaces.
	moveWorkspace func(workspaceID string, insertIndex int) error

	// reload fetches a fresh snapshot. Injected so the model stays testable
	// without touching the filesystem.
	reload func() *model.Snapshot

	// frame advances the animations. ticking says whether a tick is in flight,
	// so the loop can stop when nothing is moving and start again when
	// something is, without ever running two at once.
	frame   int
	ticking bool

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
// The overlay was first built to be spawned per keypress, so it read the
// snapshot once and never again. It is not spawned per keypress: one process
// runs for as long as the overlay is open, and left open it froze. An agent
// could go from working to blocked while the screen still showed the old state
// and herdr's own sidebar showed the new one. A read costs about 25
// microseconds, so refreshing is cheaper than reasoning about when not to.
const refreshInterval = 700 * time.Millisecond

type refreshMsg struct{}

func refreshTick() tea.Cmd {
	return tea.Tick(refreshInterval, func(time.Time) tea.Msg { return refreshMsg{} })
}

func (m *Model) Init() tea.Cmd { return tea.Batch(refreshTick(), m.startAnimation()) }

// animInterval is the animation frame rate. The refresh tick at 700ms is far
// too slow to spin, and a frame is only a re-render, so the two run separately
// rather than one being sped up to serve both.
const animInterval = 120 * time.Millisecond

type animMsg struct{}

func animTick() tea.Cmd {
	return tea.Tick(animInterval, func(time.Time) tea.Msg { return animMsg{} })
}

// startAnimation begins the frame loop, if anything is moving and one is not
// already running.
func (m *Model) startAnimation() tea.Cmd {
	if m.ticking || !m.animated() {
		return nil
	}
	m.ticking = true
	return animTick()
}

// animated reports whether anything on the screen moves. An overlay showing
// nothing but idle agents redraws never, which is what a screen left open all
// day should cost.
func (m *Model) animated() bool {
	moving := func(s model.Status) bool {
		return s == model.StatusWorking || s == model.StatusBlocked
	}
	for _, r := range m.snap.Repos {
		for _, a := range r.Agents {
			if moving(a.Status) {
				return true
			}
		}
	}
	return m.snap.Orch.Found && moving(m.snap.Orch.Status)
}

// SetSnapshot swaps in fresh data, keeping the selection where it was.
func (m *Model) SetSnapshot(s *model.Snapshot) {
	if s == nil {
		return
	}
	m.snap = s
	m.applyColors()
	m.pruneDismissed()
	m.rebuild()
}

// SetReloader supplies the function the overlay calls to refresh itself.
func (m *Model) SetReloader(f func() *model.Snapshot) { m.reload = f }

// SetWorkspaceMover supplies the function J and K call in herdr sort.
func (m *Model) SetWorkspaceMover(f func(workspaceID string, insertIndex int) error) {
	m.moveWorkspace = f
}

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

// SetDismissed restores the ribbon rows acknowledged in an earlier session.
func (m *Model) SetDismissed(d map[string]string) {
	m.dismissed = d
	m.pruneDismissed()
	m.rebuild()
}

// SetDismissedSaver supplies the function that persists them.
func (m *Model) SetDismissedSaver(f func(map[string]string)) { m.saveDismissed = f }

// SetColors restores the repo colours picked in an earlier session, and
// supplies the function that persists new ones.
func (m *Model) SetColors(c map[string]int, save func(map[string]int)) {
	m.colors, m.saveColors = c, save
	m.applyColors()
}

// applyColors lays the picked colours over the snapshot's hashed ones. Every
// draw reads the colour off the repo, so this is the one place it changes.
func (m *Model) applyColors() {
	for i, r := range m.snap.Repos {
		if c, ok := m.colors[r.Key]; ok && r.IsGit && c >= 0 && c < len(identity.Palette) {
			m.snap.Repos[i].ColorIndex = c
		}
	}
}

// SetSkillPrompt says whether the reporting skill is missing, and supplies the
// function that installs it. Both at once, because offering an install with no
// way to run it is worse than not offering one.
func (m *Model) SetSkillPrompt(missing bool, f func() (string, error)) {
	m.skillMissing, m.installSkill = missing, f
	m.rebuild()
}

// showBanner reports whether the offer is on screen. It goes while filtering
// for the same reason the ribbon does: a query collapses the screen to its
// results, and a standing offer is not one of them.
func (m *Model) showBanner() bool {
	return m.skillMissing && m.installSkill != nil && !m.filtering && m.filter == ""
}

// Jump returns the pane the user chose, or "".
func (m *Model) Jump() string { return m.jump }

// ribbonRows is the ranked ribbon, capped by the daemon and never more than
// four, less anything you have already acknowledged. It disappears entirely
// when nothing needs you, which is the point.
//
// ponytail: the cap is applied by the daemon before this filters, so dismissing
// a row leaves a gap rather than promoting the fifth thing that needs you. Move
// the cap to the client if that gap ever matters.
func (m *Model) ribbonRows() []model.Attention {
	return undismissed(m.snap.Attention, m.dismissed)
}

// undismissed is the attention rows less any dismissed at their current status.
// The tab bar badge counts with it too, so the badge and the header agree.
func undismissed(rows []model.Attention, dismissed map[string]string) []model.Attention {
	if len(dismissed) == 0 {
		return rows
	}
	out := make([]model.Attention, 0, len(rows))
	for _, a := range rows {
		if dismissed[a.PaneID] == string(a.Status) {
			continue
		}
		out = append(out, a)
	}
	return out
}

// pruneDismissed drops entries whose row has left the ribbon entirely. The
// state that made it news is gone, so if it comes back it is news again, and
// nothing else would ever clear an entry out of the file.
func (m *Model) pruneDismissed() {
	if len(m.dismissed) == 0 {
		return
	}
	shown := make(map[string]bool, len(m.snap.Attention))
	for _, a := range m.snap.Attention {
		shown[a.PaneID] = true
	}
	for pane := range m.dismissed {
		if !shown[pane] {
			delete(m.dismissed, pane)
		}
	}
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

	case noticeMsg:
		m.notice = string(msg)
		return m, nil

	case refreshMsg:
		if m.reload != nil {
			m.SetSnapshot(m.reload())
		}
		// An agent that has just started working restarts the frame loop, which
		// stopped itself when the screen went still.
		return m, tea.Batch(refreshTick(), m.startAnimation())

	case animMsg:
		m.frame++
		if !m.animated() {
			m.ticking = false
			return m, nil
		}
		return m, animTick()
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

// workspaceOf finds the workspace an agent's pane belongs to, for the
// ribbon's workspace tag. A stopped-process row points at a non-agent pane
// and so has no agent to look the workspace up through; it gets the zero
// value, which draws nothing.
func (m *Model) workspaceOf(paneID string) model.Workspace {
	for _, r := range m.snap.Repos {
		for _, a := range r.Agents {
			if a.PaneID == paneID {
				return m.workspaceByID(a.WorkspaceID)
			}
		}
	}
	return model.Workspace{}
}

// workspaceByID finds a workspace by id.
func (m *Model) workspaceByID(id string) model.Workspace {
	for _, w := range m.snap.Workspaces {
		if w.ID == id {
			return w
		}
	}
	return model.Workspace{}
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
