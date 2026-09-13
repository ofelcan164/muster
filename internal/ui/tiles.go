// Tiles are what the grid draws: one per agent, and one per workspace holding
// no agent at all. buildTiles is the one place that groups repos and panes by
// workspace; targets.go and view.go both walk its output through the same
// ordering, rather than the snapshot directly, so they can never disagree
// about what is on screen. See the comment on rebuild in targets.go.

package ui

import (
	"cmp"
	"slices"

	"github.com/ofelcan164/muster/internal/model"
)

// tile is one cell the grid can draw: either one agent, or an empty
// workspace. Agent.PaneID is "" for an empty tile and set for an agent one,
// which is what isAgent tests.
type tile struct {
	Agent     model.Agent
	Repo      model.Repo // the agent's own repo; zero value for an empty tile
	Workspace model.Workspace

	// Repos is the repos this workspace's panes sit in, sorted by grid slot.
	// Only an empty tile draws it, for its sigil line.
	Repos []model.Repo
	// Panes is this workspace's non-agent panes, across every repo they sit
	// in, sorted by pane id. Read by both kinds: the empty tile's own line,
	// and the "N agents · M panes" footer every agent tile in the workspace
	// shares.
	Panes []model.Pane
	// Agents is how many agents this workspace holds, for that same footer.
	// Zero for an empty tile.
	Agents int
}

func (t tile) isAgent() bool { return t.Agent.PaneID != "" }

// slot orders a tile for first-seen: an agent tile by its own repo's grid
// slot, an empty workspace by the slot of the first repo its panes sit in.
// A workspace touching no repo at all (no panes ever resolved) sorts last.
func (t tile) slot() int {
	if t.isAgent() {
		return t.Repo.GridSlot
	}
	if len(t.Repos) > 0 {
		return t.Repos[0].GridSlot
	}
	return 1 << 30
}

// label orders a-z: an agent tile by its repo's display name, an empty
// workspace by its own label.
func (t tile) label() string {
	if t.isAgent() {
		return t.Repo.Display
	}
	return t.Workspace.Label
}

type agentInRepo struct {
	repoKey string
	agent   model.Agent
}

// buildTiles groups every repo's agents and non-agent panes by workspace,
// then produces one tile per agent and one per workspace holding none.
func buildTiles(snap *model.Snapshot) []tile {
	byKey := make(map[string]model.Repo, len(snap.Repos))
	repos := map[string][]model.Repo{}
	agents := map[string][]agentInRepo{}
	panes := map[string][]model.Pane{}

	for _, r := range snap.Repos {
		byKey[r.Key] = r
		for _, wsID := range r.WorkspaceIDs {
			repos[wsID] = append(repos[wsID], r)
		}
		for _, a := range r.Agents {
			agents[a.WorkspaceID] = append(agents[a.WorkspaceID], agentInRepo{r.Key, a})
		}
		for _, p := range r.OtherPanes {
			panes[p.WorkspaceID] = append(panes[p.WorkspaceID], p)
		}
	}
	for wsID, rs := range repos {
		slices.SortStableFunc(rs, func(a, b model.Repo) int { return cmp.Compare(a.GridSlot, b.GridSlot) })
		repos[wsID] = rs
	}
	for wsID, ps := range panes {
		slices.SortStableFunc(ps, func(a, b model.Pane) int { return cmp.Compare(a.PaneID, b.PaneID) })
		panes[wsID] = ps
	}

	var out []tile
	for _, ws := range snap.Workspaces {
		as := agents[ws.ID]
		slices.SortStableFunc(as, func(a, b agentInRepo) int {
			return cmp.Compare(a.agent.PaneID, b.agent.PaneID)
		})
		if len(as) == 0 {
			out = append(out, tile{
				Workspace: ws,
				Repos:     repos[ws.ID],
				Panes:     panes[ws.ID],
			})
			continue
		}
		for _, x := range as {
			out = append(out, tile{
				Agent:     x.agent,
				Repo:      byKey[x.repoKey],
				Workspace: ws,
				Panes:     panes[ws.ID],
				Agents:    len(as),
			})
		}
	}
	return out
}
