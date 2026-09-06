package ui

import (
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ofelcan/muster/internal/daemon"
	"github.com/ofelcan/muster/internal/herdr"
	"github.com/ofelcan/muster/internal/model"
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
	p := tea.NewProgram(m, tea.WithAltScreen())
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
