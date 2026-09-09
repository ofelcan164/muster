package ui

import (
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ofelcan/muster/internal/daemon"
	"github.com/ofelcan/muster/internal/herdr"
	"github.com/ofelcan/muster/internal/model"
	"github.com/ofelcan/muster/internal/state"
)

// staleAfter is when a snapshot stops being trustworthy. The daemon rewrites
// every five seconds, so anything this old means it is not running.
const staleAfter = 30 * time.Second

// Run opens the overlay and returns the pane to jump to, or "".
//
// If musterd is not running the overlay must still open. It starts the daemon,
// waits briefly for a snapshot, and shows a warning if it had to. A dead daemon
// degrades this screen, it never blocks you.
func Run() (string, error) {
	snap, warning := load()
	if snap == nil {
		return "", fmt.Errorf("no snapshot and the daemon would not start")
	}

	m := New(snap, warning)
	// The arrangement made with J and K, restored from the last time the overlay
	// was open. A grid you rearrange and that forgets is worse than one you
	// cannot rearrange at all.
	ui := state.LoadUI()
	m.SetManualOrder(ui.RepoOrder)
	m.SetOrderSaver(func(order []string) {
		ui.RepoOrder = order
		// A failed write costs this session's arrangement and nothing else, and
		// there is nowhere to report it from inside a full-screen overlay.
		_ = ui.Save()
	})
	m.SetSort(SortMode(ui.Sort))
	m.SetSortSaver(func(s SortMode) {
		ui.Sort = int(s)
		_ = ui.Save()
	})
	m.SetDismissed(ui.Dismissed)
	m.SetDismissedSaver(func(d map[string]string) {
		ui.Dismissed = d
		_ = ui.Save()
	})
	// How i and t reach the orchestrator. agent.prompt is the same call the
	// orchestrator's own tooling uses, so a message from Muster is not a
	// special case at the other end.
	m.SetMarker(func(paneID string) error {
		return markOrchestratorPane(paneID, markedOrchestratorPane())
	})
	m.SetPrompter(func(paneID, text string) error {
		return herdr.NewClient("").Call("agent.prompt",
			map[string]any{"target": paneID, "text": text}, nil)
	})
	// Keep it live. The daemon rewrites the snapshot every few seconds, and an
	// overlay left open should follow rather than freeze on whatever was true
	// when it opened.
	m.SetReloader(func() *model.Snapshot {
		fresh, err := daemon.ReadSnapshot()
		if err != nil {
			return nil
		}
		return fresh
	})
	// All-motion rather than cell-motion: cell motion only reports movement
	// while a button is held, which gives drag but never hover.
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseAllMotion())
	if _, err := p.Run(); err != nil {
		return "", err
	}
	return m.Jump(), nil
}

// load reads the snapshot, healing a dead daemon on the way.
func load() (*model.Snapshot, string) {
	snap, err := daemon.ReadSnapshot()
	if err == nil && time.Since(snap.GeneratedAt) < staleAfter {
		return snap, ""
	}

	// Either there is no snapshot or it has gone stale. Start the daemon and
	// give it a moment; it writes its first snapshot about 15ms after starting.
	if _, startErr := daemon.Ensure(); startErr != nil {
		if snap != nil {
			return snap, "daemon not running, showing a stale view"
		}
		return nil, ""
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if fresh, err := daemon.ReadSnapshot(); err == nil &&
			time.Since(fresh.GeneratedAt) < staleAfter {
			return fresh, "daemon was restarted"
		}
		time.Sleep(25 * time.Millisecond)
	}
	if snap != nil {
		return snap, "daemon not responding, showing a stale view"
	}
	return nil, ""
}

// Jump focuses an agent and returns. That is the whole of jumping: the overlay
// tears down on process exit and leaves focus where it was put, with no close
// call and no detached helper.
func Jump(paneID string) error {
	c := herdr.NewClient("")
	return c.Call("agent.focus", map[string]any{"target": paneID}, nil)
}

// TogglePane opens the overlay, or closes it if it is already open.
//
// The plan makes prefix+m the one hot key: press it to open, press it again to
// close, and press it to come back. herdr does not toggle on its own, so
// opening twice stacks two overlays on top of each other. Finding an existing
// one by its manifest title is how the overlay locates itself, since the pane
// list reports that title as the label.
func TogglePane() error {
	if pane := findOverlayPane(); pane != "" {
		return herdr.NewClient("").Call("plugin.pane.close",
			map[string]any{"pane_id": pane}, nil)
	}
	return OpenPane()
}

// findOverlayPane returns an open Muster overlay in the focused workspace, or
// "".
//
// The workspace check is the whole point: the toggle closes whatever this
// finds, so an overlay left open in another workspace made prefix+m close that
// one, and from where you were sitting the key did nothing. session.snapshot
// carries the plugin panes and the focused workspace together, so scoping it
// costs no extra call.
func findOverlayPane() string {
	snap, err := herdr.NewClient("").SessionSnapshot()
	if err != nil {
		return ""
	}
	return overlayPaneIn(snap.Panes, snap.FocusedWorkspaceID)
}

func overlayPaneIn(panes []herdr.Pane, workspace string) string {
	for _, p := range panes {
		if p.Label == overlayTitle && p.WorkspaceID == workspace {
			return p.PaneID
		}
	}
	return ""
}

// overlayTitle is the [[panes]] title from the manifest, which is what shows up
// as the pane label.
const overlayTitle = "Muster"

// OpenPane asks herdr to open the overlay pane.
//
// A plugin_action keybinding cannot address a [[panes]] entrypoint directly,
// verified against 0.8.2: invoking the pane id returns plugin_action_not_found.
// So the key binds to an action, and the action shells out to this.
func OpenPane() error {
	c := herdr.NewClient("")
	return c.Call("plugin.pane.open", map[string]any{
		"plugin_id":  pluginID(),
		"entrypoint": "home",
		// focus defaults to false, and an overlay you have to click into is not
		// an overlay.
		"focus": true,
	}, nil)
}

func pluginID() string {
	if id := os.Getenv("HERDR_PLUGIN_ID"); id != "" {
		return id
	}
	return "muster"
}

// ResolveTarget turns a jump alias into a pane id.
//
// The two aliases are the ones with global keys of their own: the orchestrator,
// and whichever agent you were in last. Both come from the snapshot, so this
// stays a file read on a key you press constantly.
func ResolveTarget(name string) (string, error) {
	switch name {
	case "orchestrator", "previous":
	default:
		return name, nil // already a pane id
	}

	snap, err := daemon.ReadSnapshot()
	if err != nil {
		return "", fmt.Errorf("no snapshot: is musterd running?")
	}
	if name == "orchestrator" {
		if !snap.Orch.Found {
			return "", fmt.Errorf("no orchestrator marked: run the mark-orchestrator action on its pane")
		}
		return snap.Orch.PaneID, nil
	}
	return previousAgent(snap)
}

// previousAgent picks the agent to go back to.
//
// It resolves against the live focused pane rather than the snapshot's idea of
// it, because the back key is a toggle you press in quick succession and the
// daemon may not have reconciled since the last press. Without this, pressing
// back twice quickly re-focuses the pane you are already in.
func previousAgent(snap *model.Snapshot) (string, error) {
	focused := liveFocusedPane()
	if focused == "" {
		focused = snap.FocusedPane
	}
	for _, pane := range snap.FocusHistory {
		if pane != focused {
			return pane, nil
		}
	}
	if snap.PreviousAgent != "" && snap.PreviousAgent != focused {
		return snap.PreviousAgent, nil
	}
	return "", fmt.Errorf("no previous agent yet")
}

// liveFocusedPane asks the server where focus actually is. Around 2ms, which is
// affordable on a key press and worth it to make the toggle reliable.
func liveFocusedPane() string {
	snap, err := herdr.NewClient("").SessionSnapshot()
	if err != nil {
		return ""
	}
	return snap.FocusedPaneID
}
