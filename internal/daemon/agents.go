package daemon

import (
	"github.com/ofelcan164/muster/internal/herdr"
	"github.com/ofelcan164/muster/internal/model"
	"github.com/ofelcan164/muster/internal/state"
	"strings"
	"time"
)

// buildAgents converts herdr's agent list into Muster's model and maintains the
// status timing that herdr does not expose.
func (d *Daemon) buildAgents(snap *herdr.Snapshot, now time.Time) map[string]model.Agent {
	out := make(map[string]model.Agent, len(snap.Agents))
	panesByID := make(map[string]herdr.Pane, len(snap.Panes))
	for _, p := range snap.Panes {
		panesByID[p.PaneID] = p
	}

	live := map[string]bool{}
	for _, a := range snap.Agents {
		live[a.PaneID] = true
		status := model.Status(a.AgentStatus)
		since, ageKnown := d.stampStatus(a.PaneID, string(status), now)

		if status == model.StatusWorking {
			// Seeing an agent work is what separates "stalled" from "never
			// used" later on.
			d.persist.EverWorked[a.PaneID] = true
		}
		if status == model.StatusDone {
			// Remember that this pane has passed through done, so the
			// idle-never-done rule can tell a stalled agent from a finished one.
			d.persist.LastDoneSeq[a.PaneID] = a.StateChangeSeq
		}

		pane := panesByID[a.PaneID]
		taskSeen := d.stampTask(a.PaneID, tokenOf(a.Tokens, pane.Tokens, "task"), now)
		task, source := taskFor(a, pane, since, taskSeen)

		out[a.PaneID] = model.Agent{
			PaneID:         a.PaneID,
			WorkspaceID:    a.WorkspaceID,
			TabID:          a.TabID,
			Name:           agentName(a),
			Kind:           a.Agent,
			Status:         status,
			Focused:        a.Focused,
			Task:           task,
			TaskSource:     source,
			BlockedOn:      tokenOf(a.Tokens, pane.Tokens, "blocked_on"),
			Note:           tokenOf(a.Tokens, pane.Tokens, "note"),
			StatusSince:    since,
			AgeKnown:       ageKnown,
			StateChangeSeq: a.StateChangeSeq,
		}
	}

	// Forget timing for panes that are gone, so the state file cannot grow
	// without bound across a long-lived session.
	for pane := range d.persist.StatusSince {
		if !live[pane] {
			delete(d.persist.StatusSince, pane)
		}
	}
	for pane := range d.persist.TaskSeenAt {
		if !live[pane] {
			delete(d.persist.TaskSeenAt, pane)
		}
	}
	for pane := range d.persist.EverWorked {
		if !live[pane] {
			delete(d.persist.EverWorked, pane)
		}
	}
	for pane := range d.persist.LastDoneSeq {
		if !live[pane] {
			delete(d.persist.LastDoneSeq, pane)
		}
	}
	return out
}

// stampStatus returns when the pane entered its current status, recording the
// transition the first time it is seen. The second return reports whether the
// daemon actually watched that transition, which is what makes the age honest.
func (d *Daemon) stampStatus(paneID, status string, now time.Time) (time.Time, bool) {
	prev, ok := d.persist.StatusSince[paneID]
	if ok && prev.Status == status {
		return prev.Since, prev.Observed
	}
	// A status the daemon watched change. Only now is the age real. A pane seen
	// for the very first time gets no such credit.
	observed := ok
	d.persist.StatusSince[paneID] = state.StatusStamp{
		Status: status, Since: now, Observed: observed,
	}
	return now, observed
}

// stampTask returns when the daemon first saw the current task value on a pane,
// recording it the first time that value appears.
func (d *Daemon) stampTask(paneID, task string, now time.Time) time.Time {
	if task == "" {
		delete(d.persist.TaskSeenAt, paneID)
		return time.Time{}
	}
	prev, ok := d.persist.TaskSeenAt[paneID]
	if ok && prev.Value == task {
		return prev.Since
	}
	d.persist.TaskSeenAt[paneID] = state.TaskStamp{Value: task, Since: now}
	return now
}

// taskFor walks the fallback ladder. Tier 2 exists because a task line that was
// true forty minutes ago and is now wrong is worse than no line at all: you
// would trust it. Marking it stale costs nothing.
//
// taskSeen is when the daemon first observed the current task value, since
// herdr does not timestamp metadata tokens. The daemon watching for the value
// to change is the only source of that fact, which means a line written while
// the daemon was down looks stale rather than fresh. That is the safer
// direction to be wrong in: it shows doubt about a line that might be current,
// instead of confidence in one that is not.
func taskFor(a herdr.Agent, p herdr.Pane, statusSince, taskSeen time.Time) (string, model.TaskSource) {
	if t := tokenOf(a.Tokens, p.Tokens, "task"); t != "" {
		if !taskSeen.IsZero() && taskSeen.Before(statusSince) {
			return t, model.TaskFromOrchestratorStale
		}
		return t, model.TaskFromOrchestrator
	}
	if t := tokenOf(a.Tokens, p.Tokens, "self_task"); t != "" {
		return t, model.TaskFromSelfReport
	}
	if a.Title != "" {
		return a.Title, model.TaskFromTerminalTitle
	}
	if a.TerminalTitleStripped != "" {
		return a.TerminalTitleStripped, model.TaskFromTerminalTitle
	}
	if p.TerminalTitleStripped != "" {
		return p.TerminalTitleStripped, model.TaskFromTerminalTitle
	}
	return "", model.TaskFromNone
}

// tokenOf prefers the agent's tokens and falls back to the pane's, since
// report-metadata can target either.
func tokenOf(agentTokens, paneTokens map[string]string, key string) string {
	if v, ok := agentTokens[key]; ok && v != "" {
		return v
	}
	if v, ok := paneTokens[key]; ok && v != "" {
		return v
	}
	return ""
}

func agentName(a herdr.Agent) string {
	if a.Name != "" {
		return a.Name
	}
	// No name set. Use the whole pane id rather than just its pane half: two
	// agents in different workspaces are both "p1", and a name you cannot tell
	// apart is worse than an ugly one.
	return a.PaneID
}

// findOrchestrator resolves the orchestrator by token first and name second.
// The token wins because it survives a rename and can carry more than a string
// later. If neither is set the strip is absent rather than guessed at.
func (d *Daemon) findOrchestrator(snap *herdr.Snapshot, agents map[string]model.Agent, now time.Time) model.Orchestrator {
	panesByID := make(map[string]herdr.Pane, len(snap.Panes))
	for _, p := range snap.Panes {
		panesByID[p.PaneID] = p
	}

	var byName *herdr.Agent
	for i := range snap.Agents {
		a := &snap.Agents[i]
		if strings.EqualFold(tokenOf(a.Tokens, panesByID[a.PaneID].Tokens, "role"), "orchestrator") {
			return d.orchFrom(a, agents, "token")
		}
		if strings.EqualFold(a.Name, "orchestrator") && byName == nil {
			byName = a
		}
	}
	if byName != nil {
		return d.orchFrom(byName, agents, "name")
	}
	return model.Orchestrator{Found: false}
}

func (d *Daemon) orchFrom(a *herdr.Agent, agents map[string]model.Agent, how string) model.Orchestrator {
	o := model.Orchestrator{
		Found:      true,
		PaneID:     a.PaneID,
		Name:       agentName(*a),
		Status:     model.Status(a.AgentStatus),
		DetectedBy: how,
	}
	if m, ok := agents[a.PaneID]; ok {
		o.StatusSince = m.StatusSince
		// Only the top of the task ladder. The lower rungs are the pane's
		// terminal title, which nobody said to the orchestrator: under a label
		// reading "told" it is a sentence that never changes and was never
		// true. An empty line saying so is the honest version.
		if m.TaskSource == model.TaskFromOrchestrator ||
			m.TaskSource == model.TaskFromOrchestratorStale {
			o.LastMessage = m.Task
		}
	}
	return o
}
