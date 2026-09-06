package daemon

import "github.com/ofelcan/muster/internal/model"

// trackFocus maintains the agent focus history behind the back key.
//
// Only agent panes count. Focusing a shell, a log tail or the Muster overlay
// itself must not become the thing "back" returns you to, or the key would send
// you somewhere you were never working.
func (d *Daemon) trackFocus(focusedPane string, agents map[string]model.Agent) {
	if focusedPane == "" {
		return
	}
	if _, isAgent := agents[focusedPane]; !isAgent {
		return
	}
	if len(d.persist.FocusHistory) > 0 && d.persist.FocusHistory[0] == focusedPane {
		return // already current
	}

	history := append([]string{focusedPane}, d.persist.FocusHistory...)

	// Drop repeats and anything that is no longer an agent, then keep the two
	// entries the back key needs: where you are, and where you were.
	seen := map[string]bool{}
	var kept []string
	for _, pane := range history {
		if seen[pane] {
			continue
		}
		if _, ok := agents[pane]; !ok {
			continue
		}
		seen[pane] = true
		kept = append(kept, pane)
		if len(kept) == 2 {
			break
		}
	}
	d.persist.FocusHistory = kept
}

// previousAgent is the agent to return to, or "" when there is nowhere to go
// back to yet.
func (d *Daemon) previousAgent() string {
	if len(d.persist.FocusHistory) < 2 {
		return ""
	}
	return d.persist.FocusHistory[1]
}
