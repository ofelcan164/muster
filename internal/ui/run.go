package ui

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ofelcan164/muster/internal/daemon"
	"github.com/ofelcan164/muster/internal/herdr"
	"github.com/ofelcan164/muster/internal/install"
	"github.com/ofelcan164/muster/internal/model"
	"github.com/ofelcan164/muster/internal/state"
)

// Badge is the line herdr's tab bar shows: how many rows need you, else how
// many agents are working, then the key that opens the overlay.
//
// herdr runs it through sh every few seconds, so it reads files and never the
// socket, and never starts a daemon. A stale snapshot gets the key alone: a
// count nobody is keeping current is worse than no count.
func Badge(letter string) string {
	key := "prefix+" + letter
	snap, err := daemon.ReadSnapshot()
	if err != nil || time.Since(snap.GeneratedAt) >= model.StaleAfter {
		return "◆ " + key
	}
	switch n := len(undismissed(snap.Attention, state.LoadUI().Dismissed)); {
	case n == 1:
		return "◆ 1 needs you · " + key
	case n > 1:
		return fmt.Sprintf("◆ %d need you · %s", n, key)
	case snap.Counts.Working > 0:
		return fmt.Sprintf("◆ %d working · %s", snap.Counts.Working, key)
	}
	return "◆ " + key
}

// Run opens the overlay and returns the pane to jump to, or "".
//
// If musterd is not running the overlay must still open. It starts the daemon,
// waits briefly for a snapshot, and shows a warning if it had to. A dead daemon
// degrades this screen, it never blocks you.
func Run() (string, error) {
	snap, warning := load()
	if snap == nil {
		return "", errors.New("no snapshot and the daemon would not start")
	}

	m := New(snap, warning)
	ui := state.LoadUI()
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
	// The skill is what writes every task line on the screen. Without it the
	// grid is a wall of terminal titles, and the only way to learn that is to
	// know about a command nobody mentioned.
	m.SetSkillPrompt(!install.SkillInstalled(), func() (string, error) {
		res, err := install.Skill()
		if err != nil {
			return "", err
		}
		return res.Path, nil
	})
	// How i and t reach the orchestrator. agent.prompt is the same call the
	// orchestrator's own tooling uses, so a message from Muster is not a
	// special case at the other end.
	m.SetMarker(func(paneID string) error {
		return markOrchestratorPane(paneID, markedOrchestratorPane())
	})
	m.SetPrompter(func(paneID, text string) error {
		if err := herdr.NewClient("").Call("agent.prompt",
			map[string]any{"target": paneID, "text": text}, nil); err != nil {
			return err
		}
		recordTold(paneID, text)
		return nil
	})
	// J and K in herdr sort. The local reorder happens synchronously in the
	// model; this is only the socket call that makes it stick.
	m.SetWorkspaceMover(func(workspaceID string, insertIndex int) error {
		return herdr.NewClient("").Call("workspace.move",
			map[string]any{"workspace_id": workspaceID, "insert_index": insertIndex}, nil)
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
	if err == nil && time.Since(snap.GeneratedAt) < model.StaleAfter {
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
			time.Since(fresh.GeneratedAt) < model.StaleAfter {
			return fresh, "daemon was restarted"
		}
		time.Sleep(25 * time.Millisecond)
	}
	if snap != nil {
		return snap, "daemon not responding, showing a stale view"
	}
	return nil, ""
}

// Jump focuses a pane or a workspace and returns. That is the whole of
// jumping: the popup tears down on process exit and leaves focus where it was
// put, with no close call and no detached helper.
//
// pane.focus, not agent.focus. Since 0.9.0 every attached terminal keeps its
// own view of which tab it shows, and a socket call only moves those views for
// workspace.focus, tab.focus and pane.focus. agent.focus moved the session's
// focus and left the screen where it was, so a jump closed Muster and landed
// nowhere.
//
// A target carrying the "ws:" prefix is an empty workspace tile: it has no
// pane of its own to focus, however many panes or repos it holds, so the
// model hands over its workspace id instead of guessing among them.
func Jump(target string) error {
	if wsID, ok := strings.CutPrefix(target, "ws:"); ok {
		return herdr.NewClient("").Call("workspace.focus", map[string]any{"workspace_id": wsID}, nil)
	}
	return herdr.NewClient("").Call("pane.focus", map[string]any{"pane_id": target}, nil)
}

// OpenPane asks herdr to open the overlay as a popup.
//
// A plugin_action keybinding cannot address a [[panes]] entrypoint directly,
// verified against 0.8.2: invoking the pane id returns plugin_action_not_found.
// So the key binds to an action, and the action shells out to this.
//
// It only opens. Closing used to be this key's job too, which meant finding the
// overlay among the panes of the focused tab, and every way that search went
// wrong left a stray overlay somewhere. A popup takes every key while it is
// open, so the key never reaches here while there is anything to close.
//
// There is no focus param. An overlay needed "focus": true, but a popup is
// modal and herdr never reads the flag for one.
func OpenPane() error {
	err := herdr.NewClient("").Call("plugin.pane.open", map[string]any{
		"plugin_id":  pluginID(),
		"entrypoint": "home",
	}, nil)
	// herdr allows one popup per session. ui_busy means Muster is already open,
	// or another plugin's popup is, and either way there is nothing to do. Keys
	// cannot get here while a popup is up, but `herdr plugin action invoke
	// muster.open` can.
	if herdr.Code(err) == "ui_busy" {
		return nil
	}
	return err
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
		return "", errors.New("no snapshot: is musterd running?")
	}
	if name == "orchestrator" {
		if !snap.Orch.Found {
			return "", errors.New("no orchestrator marked: run the mark-orchestrator action on its pane")
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
	return "", errors.New("no previous agent yet")
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
