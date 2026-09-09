package daemon

import (
	"strings"
	"time"

	"github.com/ofelcan164/muster/internal/herdr"
	"github.com/ofelcan164/muster/internal/model"
	"github.com/ofelcan164/muster/internal/state"
)

// stoppedTTL is how long a stopped process stays in the ribbon.
const stoppedTTL = 15 * time.Minute

// shells are the processes that mean "this pane is sitting at a prompt".
// Anything else is treated as work in progress.
var shells = map[string]bool{
	"bash": true, "zsh": true, "fish": true, "sh": true, "dash": true,
	"ksh": true, "tcsh": true, "csh": true, "nu": true, "elvish": true,
	"xonsh": true,
}

func isShell(name string) bool { return name == "" || shells[strings.ToLower(name)] }

// detectStoppedProcesses compares each non-agent pane's foreground process
// against what was there last time.
//
// This replaces scanning pane text for things that look like errors. herdr
// exposes no exit code anywhere, not even on pane_exited, so there is no
// structured failure signal to read. What there is, is the process list: a pane
// that was running vite and is now sitting in bash has stopped, and that is a
// fact rather than a guess about what some output meant.
//
// It cannot tell a crash from a deliberate Ctrl-C. It reports that the thing
// stopped, which is the part you cannot currently see without opening the
// workspace.
func (d *Daemon) detectStoppedProcesses(procs map[string]string, tracked map[string]bool, now time.Time) {
	for paneID, current := range procs {
		previous := d.persist.LastProcess[paneID]
		switch {
		case !isShell(current):
			// Something is running. Any earlier stop is resolved.
			delete(d.persist.Stopped, paneID)
		case !isShell(previous):
			// It was running and now it is not.
			d.persist.Stopped[paneID] = state.StoppedStamp{Process: previous, At: now}
		}
		d.persist.LastProcess[paneID] = current
	}

	// A stop is news for a while and then it is not. Without this, Ctrl-C on a
	// one-off command would sit in the ribbon forever, since nothing else is
	// ever going to run in that pane to clear it.
	for paneID, st := range d.persist.Stopped {
		if now.Sub(st.At) > stoppedTTL {
			delete(d.persist.Stopped, paneID)
		}
	}

	// Panes this no longer tracks: closed, or taken over by an agent. Neither is
	// a stop. A closed pane is something the user deliberately got rid of, and a
	// pane running an agent is reported by the agent's own status, so a stop
	// recorded before it started has to go. Only non-agent panes are read for a
	// foreground process, so nothing above can clear that entry: the row sat for
	// the full TTL, outranking the real state of the agent working underneath.
	for paneID := range d.persist.LastProcess {
		if !tracked[paneID] {
			delete(d.persist.LastProcess, paneID)
			delete(d.persist.Stopped, paneID)
		}
	}
	for paneID := range d.persist.Stopped {
		if !tracked[paneID] {
			delete(d.persist.Stopped, paneID)
		}
	}
}

// stoppedRows turns detected stops into the shape triage ranks.
func (d *Daemon) stoppedRows(snap *herdr.Snapshot, repoForPane map[string]*model.Repo) []model.Stopped {
	if len(d.persist.Stopped) == 0 {
		return nil
	}
	labels := map[string]string{}
	for _, p := range snap.Panes {
		labels[p.PaneID] = p.PaneID
		if l := strings.TrimSpace(p.Label); l != "" {
			labels[p.PaneID] = l
		}
	}
	out := make([]model.Stopped, 0, len(d.persist.Stopped))
	for paneID, st := range d.persist.Stopped {
		r := repoForPane[paneID]
		if r == nil {
			continue
		}
		label := labels[paneID]
		if label == paneID {
			label = st.Process
		}
		out = append(out, model.Stopped{
			PaneID:  paneID,
			RepoKey: r.Key,
			Label:   label,
			Process: st.Process,
		})
	}
	return out
}
