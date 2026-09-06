package daemon

import (
	"strings"

	"github.com/ofelcan/muster/internal/herdr"
	"github.com/ofelcan/muster/internal/model"
)

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
func (d *Daemon) detectStoppedProcesses(procs map[string]string, live map[string]bool) {
	for paneID, current := range procs {
		previous := d.persist.LastProcess[paneID]
		switch {
		case !isShell(current):
			// Something is running. Any earlier stop is resolved.
			delete(d.persist.Stopped, paneID)
		case !isShell(previous):
			// It was running and now it is not.
			d.persist.Stopped[paneID] = previous
		}
		d.persist.LastProcess[paneID] = current
	}

	// Panes that are gone entirely are not "stopped", they are closed.
	for paneID := range d.persist.LastProcess {
		if !live[paneID] {
			delete(d.persist.LastProcess, paneID)
			delete(d.persist.Stopped, paneID)
		}
	}
	for paneID := range d.persist.Stopped {
		if !live[paneID] {
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
	for paneID, proc := range d.persist.Stopped {
		r := repoForPane[paneID]
		if r == nil {
			continue
		}
		label := labels[paneID]
		if label == paneID {
			label = proc
		}
		out = append(out, model.Stopped{
			PaneID:  paneID,
			RepoKey: r.Key,
			Label:   label,
			Process: proc,
		})
	}
	return out
}
