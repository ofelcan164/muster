// Package triage computes the attention ranking.
//
// The ranks come straight from the plan's triage table. Working agents are
// deliberately absent: an agent doing its job is not something that needs you.
package triage

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ofelcan/muster/internal/chain"
	"github.com/ofelcan/muster/internal/model"
)

// RibbonMax caps the ranked ribbon. The plan holds it to four rows so it stays
// scannable and so it can vanish entirely when nothing needs you.
const RibbonMax = 4

// StaleAfter is how long an idle agent that never reported done has to sit
// before it counts as stalled.
const StaleAfter = 10 * time.Minute

// Age comparisons have to account for ages the daemon never watched
// accumulate. Those are lower bounds, so they sort last in either direction
// rather than being trusted against a known age.
//
// Threshold checks are different and do not need this: a lower bound of ten
// minutes still proves an agent has been idle for at least ten minutes.
func longestFirst(a, b model.Agent, now time.Time) bool {
	if a.AgeKnown != b.AgeKnown {
		return a.AgeKnown
	}
	return a.Age(now) > b.Age(now)
}

func newestFirst(a, b model.Agent, now time.Time) bool {
	if a.AgeKnown != b.AgeKnown {
		return a.AgeKnown
	}
	return a.Age(now) < b.Age(now)
}

// Input is everything the ranking needs, already assembled by the daemon.
type Input struct {
	Now   time.Time
	Repos []model.Repo
	Orch  model.Orchestrator

	// Stopped lists non-agent panes whose process went away.
	Stopped []model.Stopped

	// Chain is the orchestrator-recorded dependency order. When it is empty the
	// gate rule stays silent rather than guessing what downstream means.
	Chain *chain.Chain
	// EverDone reports whether a pane has ever been observed entering "done".
	// The idle-never-done rule turns on this.
	EverDone map[string]bool
}

type agentRef struct {
	repo  model.Repo
	agent model.Agent
}

// Rank builds the ribbon. Rows are ordered by rank, then by the tie-break the
// table specifies for that rank, and the result is capped at RibbonMax.
func Rank(in Input) []model.Attention {
	var all []agentRef
	for _, r := range in.Repos {
		for _, a := range r.Agents {
			if a.IsOrchestrator {
				continue // the orchestrator has its own strip
			}
			all = append(all, agentRef{repo: r, agent: a})
		}
	}

	var rows []model.Attention
	claimed := map[string]bool{} // one row per agent, highest rank wins

	add := func(row model.Attention) {
		if claimed[row.PaneID] {
			return
		}
		claimed[row.PaneID] = true
		rows = append(rows, row)
	}

	// Rank 1: blocked, longest waiting first.
	blocked := filter(all, func(x agentRef) bool { return x.agent.Status == model.StatusBlocked })
	sort.SliceStable(blocked, func(i, j int) bool {
		return longestFirst(blocked[i].agent, blocked[j].agent, in.Now)
	})
	for _, x := range blocked {
		add(model.Attention{
			Rank: 1, Reason: model.ReasonBlocked,
			RepoKey: x.repo.Key, PaneID: x.agent.PaneID, Agent: x.agent.Name,
			Status: x.agent.Status, Age: x.agent.Age(in.Now), AgeKnown: x.agent.AgeKnown,
			Detail: blockedDetail(x.agent),
		})
	}

	// Rank 2: a gate is open and nobody moved through it.
	for _, g := range detectGates(in, all) {
		add(g)
	}

	// Rank 3: something that was running has stopped. A dev server going down
	// is invisible until you open the workspace, which is exactly the kind of
	// thing this screen exists to surface.
	stopped := append([]model.Stopped(nil), in.Stopped...)
	sort.SliceStable(stopped, func(i, j int) bool { return stopped[i].PaneID < stopped[j].PaneID })
	for _, sp := range stopped {
		add(model.Attention{
			Rank: 3, Reason: model.ReasonProcessStopped,
			RepoKey: sp.RepoKey, PaneID: sp.PaneID, Agent: sp.Label,
			Status: model.StatusUnknown,
			Detail: sp.Process + " stopped",
		})
	}

	// Rank 4: done and unseen, newest first. herdr's "done" already means idle
	// after work you have not looked at, so this needs no history of our own.
	done := filter(all, func(x agentRef) bool { return x.agent.Status == model.StatusDone })
	sort.SliceStable(done, func(i, j int) bool {
		return newestFirst(done[i].agent, done[j].agent, in.Now)
	})
	for _, x := range done {
		add(model.Attention{
			Rank: 4, Reason: model.ReasonDoneUnseen,
			RepoKey: x.repo.Key, PaneID: x.agent.PaneID, Agent: x.agent.Name,
			Status: x.agent.Status, Age: x.agent.Age(in.Now), AgeKnown: x.agent.AgeKnown,
			Detail: "finished, unseen",
		})
	}

	// Rank 5: idle, never marked done, quiet long enough to look stalled.
	stale := filter(all, func(x agentRef) bool {
		return x.agent.Status == model.StatusIdle &&
			!in.EverDone[x.agent.PaneID] &&
			x.agent.Age(in.Now) >= StaleAfter
	})
	sort.SliceStable(stale, func(i, j int) bool {
		return longestFirst(stale[i].agent, stale[j].agent, in.Now)
	})
	for _, x := range stale {
		add(model.Attention{
			Rank: 5, Reason: model.ReasonIdleNeverDone,
			RepoKey: x.repo.Key, PaneID: x.agent.PaneID, Agent: x.agent.Name,
			Status: x.agent.Status, Age: x.agent.Age(in.Now), AgeKnown: x.agent.AgeKnown,
			Detail: "idle, never marked done",
		})
	}

	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Rank < rows[j].Rank })
	if len(rows) > RibbonMax {
		rows = rows[:RibbonMax]
	}
	return rows
}

// detectGates finds the failure the plan calls the most valuable thing Muster
// can show: an agent finished, the orchestrator has not learned of it, and work
// downstream is sitting idle waiting on a gate that already opened.
//
// Three facts, all of which herdr already has:
//   - an agent is done
//   - the orchestrator has been idle since before that agent finished, so it
//     cannot have reacted to it
//   - some other repo has an agent idle for longer than the finished one, so it
//     is waiting rather than merely between tasks
//
// Without a marked orchestrator the middle fact is unavailable and the rule
// stays silent rather than guessing.
func detectGates(in Input, all []agentRef) []model.Attention {
	if !in.Orch.Found || in.Orch.Status != model.StatusIdle || in.Orch.StatusSince.IsZero() {
		return nil
	}
	// Without a recorded chain there is no way to know which idle agent is
	// waiting on which finished one. Guessing produces a repair button that
	// fires on unrelated repos, and a ribbon row you learn to distrust is worse
	// than an empty ribbon.
	if in.Chain == nil || in.Chain.Empty() {
		return nil
	}

	var out []model.Attention
	for _, x := range all {
		if x.agent.Status != model.StatusDone {
			continue
		}
		finishedAt := x.agent.StatusSince
		if finishedAt.IsZero() || !in.Orch.StatusSince.Before(finishedAt) {
			// The orchestrator has changed state since this agent finished, so
			// it has plausibly already seen it.
			continue
		}

		var waiting []string
		for _, other := range all {
			if other.repo.Key == x.repo.Key || other.agent.PaneID == x.agent.PaneID {
				continue
			}
			if other.agent.Status != model.StatusIdle {
				continue
			}
			// The chain is what makes this a gate rather than a coincidence.
			if !in.Chain.DependsOn(other.repo.Name, x.repo.Name) {
				continue
			}
			if other.agent.Age(in.Now) > x.agent.Age(in.Now) {
				waiting = append(waiting, other.repo.Name+"/"+other.agent.Name)
			}
		}
		if len(waiting) == 0 {
			continue
		}
		sort.Strings(waiting)
		out = append(out, model.Attention{
			Rank: 2, Reason: model.ReasonGateUntold,
			RepoKey: x.repo.Key, PaneID: x.agent.PaneID, Agent: x.agent.Name,
			Status: x.agent.Status, Age: x.agent.Age(in.Now), AgeKnown: x.agent.AgeKnown,
			Detail:     fmt.Sprintf("orchestrator not told · %s idle", strings.Join(shortNames(waiting), " + ")),
			Downstream: waiting,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Age > out[j].Age })
	return out
}

func shortNames(full []string) []string {
	out := make([]string, 0, len(full))
	seen := map[string]bool{}
	for _, f := range full {
		repo := f
		if i := strings.LastIndex(f, "/"); i > 0 {
			repo = f[:i]
		}
		if i := strings.LastIndex(repo, "/"); i >= 0 {
			repo = repo[i+1:]
		}
		if !seen[repo] {
			seen[repo] = true
			out = append(out, repo)
		}
	}
	return out
}

// blockedDetail prefers the agent's own state label, which is where herdr keeps
// the question an agent is blocked on.
func blockedDetail(a model.Agent) string {
	if a.Task != "" {
		return truncate(a.Task, 80)
	}
	return "waiting on you"
}

func filter(in []agentRef, keep func(agentRef) bool) []agentRef {
	var out []agentRef
	for _, x := range in {
		if keep(x) {
			out = append(out, x)
		}
	}
	return out
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
