package daemon

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"github.com/ofelcan164/muster/internal/herdr"
	"github.com/ofelcan164/muster/internal/identity"
	"github.com/ofelcan164/muster/internal/model"
)

// buildRepos groups agents and non-agent panes under the repo their workspace
// sits in. Every repo here was learned at runtime from a workspace cwd.
func (d *Daemon) buildRepos(snap *herdr.Snapshot, agents map[string]model.Agent, orch model.Orchestrator) []model.Repo {
	wsByID := make(map[string]herdr.Workspace, len(snap.Workspaces))
	for _, w := range snap.Workspaces {
		wsByID[w.WorkspaceID] = w
	}

	// A workspace has no cwd of its own, so take it from its panes. The most
	// common pane cwd is a better answer than the first one, which might be a
	// shell someone has cd'd out of.
	wsCwd := map[string]string{}
	for wsID := range wsByID {
		wsCwd[wsID] = dominantCwd(snap.Panes, wsID)
	}

	// Iterate workspaces in a stable order. Grid slots are handed out on first
	// sight and then pinned forever, so allocating them from Go's randomised
	// map iteration would give a different layout on every fresh install: the
	// exact opposite of the fixed positions the grid exists to provide.
	ordered := make([]herdr.Workspace, 0, len(snap.Workspaces))
	ordered = append(ordered, snap.Workspaces...)
	slices.SortStableFunc(ordered, func(a, b herdr.Workspace) int {
		return cmp.Or(cmp.Compare(a.Number, b.Number), strings.Compare(a.WorkspaceID, b.WorkspaceID))
	})

	byKey := map[string]*model.Repo{}
	for _, ws := range ordered {
		wsID := ws.WorkspaceID
		cwd := wsCwd[wsID]
		if cwd == "" {
			continue
		}
		var hint *herdr.WorkspaceWorktree
		if ws.Worktree != nil {
			hint = ws.Worktree
		}
		info := d.resolver.Resolve(cwd, hint)

		r, ok := byKey[info.Key]
		if !ok {
			colorIdx := identity.Look(info.Key)
			slot := d.persist.AssignSlot(info.Key)
			r = &model.Repo{
				Key:          info.Key,
				Name:         info.Name,
				Root:         info.Root,
				Branch:       info.Branch,
				WorktreePath: info.WorktreePath,
				IsWorktree:   info.IsWorktree,
				IsGit:        info.IsGit,
				ColorIndex:   colorIdx,
				Sigil:        identity.Sigil(slot),
				GridSlot:     slot,
			}
			if !info.IsGit {
				r.ColorIndex = -1
				r.Sigil = identity.NeutralSigil
			}
			byKey[info.Key] = r
		}
		r.WorkspaceIDs = append(r.WorkspaceIDs, wsID)
	}

	repoForWorkspace := map[string]*model.Repo{}
	for _, r := range byKey {
		for _, wsID := range r.WorkspaceIDs {
			repoForWorkspace[wsID] = r
		}
	}

	procs := d.foregroundProcesses(snap, agents)

	// The panes a stop can be recorded against: live, and not running an agent.
	// An agent starting in a pane drops it out of this set, which is what clears
	// a stop left over from before it started.
	tracked := make(map[string]bool, len(snap.Panes))
	for _, p := range snap.Panes {
		if _, isAgent := agents[p.PaneID]; isAgent {
			continue
		}
		tracked[p.PaneID] = true
	}
	d.detectStoppedProcesses(procs, tracked, time.Now())

	for _, p := range snap.Panes {
		r := repoForWorkspace[p.WorkspaceID]
		if r == nil {
			continue
		}
		if isOverlayPane(p) {
			// Muster's own overlay is a pane like any other, so without this it
			// shows up in the footer of whatever workspace you opened it from,
			// and closing it reads as a process that stopped.
			continue
		}
		if a, isAgent := agents[p.PaneID]; isAgent {
			a.IsOrchestrator = orch.Found && orch.PaneID == p.PaneID
			r.Agents = append(r.Agents, a)
			continue
		}
		// Non-agent panes are ignored for triage and shown as a footer, so you
		// know what is running without opening the workspace. The label is the
		// real foreground process where the daemon has one: a dead dev server
		// shows as "shell", which is the information you actually wanted.
		r.OtherPanes = append(r.OtherPanes, model.Pane{
			PaneID:  p.PaneID,
			Label:   paneLabel(p, procs[p.PaneID]),
			Command: procs[p.PaneID],
		})
	}

	out := make([]model.Repo, 0, len(byKey))
	for _, r := range byKey {
		// Every discovered repo gets a card, whether or not anything is running
		// in it. Ordering is what keeps the quiet ones out of the way: the
		// overlay sorts repos with agents ahead of repos without.
		slices.SortStableFunc(r.Agents, func(a, b model.Agent) int { return strings.Compare(a.PaneID, b.PaneID) })
		slices.SortStableFunc(r.OtherPanes, func(a, b model.Pane) int { return strings.Compare(a.PaneID, b.PaneID) })
		slices.Sort(r.WorkspaceIDs)
		// Emit empty arrays rather than nil, which Go would marshal as null.
		// The snapshot is a contract with the overlay, and a client should
		// never have to special-case null where it expects a list.
		if r.Agents == nil {
			r.Agents = []model.Agent{}
		}
		if r.OtherPanes == nil {
			r.OtherPanes = []model.Pane{}
		}
		if r.WorkspaceIDs == nil {
			r.WorkspaceIDs = []string{}
		}
		out = append(out, *r)
	}
	assignDisplayNames(out)

	// Grid slot order, so a repo is in the same cell every time you look. This
	// comes before repoByPane, which points into out and would be left pointing
	// at the wrong repos by a sort that moved them.
	slices.SortStableFunc(out, func(a, b model.Repo) int { return cmp.Compare(a.GridSlot, b.GridSlot) })

	// Sigils are handed out in slot order, so the lowest slot keeps the sigil it
	// has always had and only a repo that would collide with one already on
	// screen moves. Slots are never freed, so without this two cards visible at
	// once shared a mark as soon as nine had ever been allocated.
	taken := make(map[string]bool, len(out))
	for i := range out {
		if !out[i].IsGit {
			continue
		}
		out[i].Sigil = identity.SigilFor(out[i].GridSlot, taken)
		taken[out[i].Sigil] = true
	}

	// Stopped panes are resolved against the finished repo set, so a stop in a
	// workspace that no longer maps to a repo is simply dropped.
	repoByPane := map[string]*model.Repo{}
	for i := range out {
		for _, p := range out[i].OtherPanes {
			repoByPane[p.PaneID] = &out[i]
		}
	}
	d.stopped = d.stoppedRows(snap, repoByPane)
	return out
}

// assignDisplayNames shortens "owner/name" to "name", keeping the owner only
// where two repos would otherwise show the same label.
//
// The identity key stays the full owner/name, so colour, sigil and grid slot
// never move. This only changes what is drawn, and it matters because on a
// three-wide grid the owner eats width the branch and task line need more.
func assignDisplayNames(repos []model.Repo) {
	counts := map[string]int{}
	for _, r := range repos {
		counts[shortName(r.Name)]++
	}
	for i := range repos {
		if short := shortName(repos[i].Name); counts[short] == 1 {
			repos[i].Display = short
		} else {
			repos[i].Display = repos[i].Name
		}
	}
}

func shortName(name string) string {
	if _, after, ok := strings.Cut(name, "/"); ok && after != "" {
		return after
	}
	return name
}

// overlayTitle is the [[panes]] title from the manifest, which herdr reports as
// the pane label.
const overlayTitle = "Muster"

// isOverlayPane reports whether a pane is one of Muster's own overlays. herdr
// opens Muster as a popup, which is not a pane and so never reaches this, but
// the [[panes]] entrypoint is still there to be opened directly.
func isOverlayPane(p herdr.Pane) bool {
	return strings.TrimSpace(p.Label) == overlayTitle
}

// paneLabel produces something short enough for a one-line footer. A raw
// terminal title is usually "user@host:/long/path", which tells you nothing you
// did not already know and crowds out the panes that matter.
//
// An explicit pane label wins, then the foreground process, then the title.
func paneLabel(p herdr.Pane, proc string) string {
	if s := strings.TrimSpace(p.Label); s != "" {
		return s
	}
	if proc != "" {
		return proc
	}
	for _, s := range []string{p.Title} {
		if s = strings.TrimSpace(s); s != "" {
			return s
		}
	}
	t := strings.TrimSpace(p.TerminalTitleStripped)
	if t == "" {
		return p.PaneID
	}
	// Strip a "user@host:path" prompt title down to the command or the leaf
	// directory.
	if _, after, ok := strings.Cut(t, ":"); ok && strings.Contains(t, "@") {
		t = strings.TrimSpace(after)
		if t == "" || strings.HasPrefix(t, "~") || strings.HasPrefix(t, "/") {
			return "shell"
		}
	}
	if i := strings.IndexAny(t, " \t"); i > 0 {
		t = t[:i]
	}
	return t
}

// dominantCwd returns the most common cwd among a workspace's panes.
func dominantCwd(panes []herdr.Pane, wsID string) string {
	counts := map[string]int{}
	for _, p := range panes {
		if p.WorkspaceID != wsID {
			continue
		}
		// Muster's own overlay is a pane in whichever workspace you opened it
		// from, and its cwd is the plugin checkout. Counting it lets the overlay
		// re-identify the workspace it is drawn over: a one-pane workspace ties
		// 1-1 and the tie breaks on the path, so a repo can change its name,
		// colour and slot for exactly as long as you are looking at it.
		if isOverlayPane(p) {
			continue
		}
		cwd := p.Cwd
		if cwd == "" {
			cwd = p.ForegroundCwd
		}
		if cwd != "" {
			counts[cwd]++
		}
	}
	best, bestN := "", 0
	for cwd, n := range counts {
		// Ties break on the lexicographically smaller path purely so the result
		// is deterministic across reconciles.
		if n > bestN || (n == bestN && cwd < best) {
			best, bestN = cwd, n
		}
	}
	return best
}

// foregroundProcesses reads the running process for each non-agent pane.
//
// pane.process_info costs 0.95 to 4ms per call, measured 2026-09-10, against
// pane.read at 350ms. Cheap on its own, but this is one call per non-agent pane
// and so scales with the session, which is why the reading is reused for
// procInterval rather than taken fresh on every reconcile. Agent panes are
// skipped because their foreground process is always the agent binary, which
// the model already knows.
func (d *Daemon) foregroundProcesses(snap *herdr.Snapshot, agents map[string]model.Agent) map[string]string {
	out := make(map[string]string, len(snap.Panes))
	if d.client == nil {
		return out
	}
	if d.procs != nil && time.Since(d.procsAt) < procInterval {
		return d.procs
	}
	for _, p := range snap.Panes {
		if _, isAgent := agents[p.PaneID]; isAgent {
			continue
		}
		if isOverlayPane(p) {
			continue
		}
		name, err := d.client.PaneForegroundProcess(p.PaneID)
		if err != nil || name == "" {
			continue
		}
		out[p.PaneID] = name
	}
	d.procs, d.procsAt = out, time.Now()
	return out
}
