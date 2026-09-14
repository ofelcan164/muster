// Package triage computes the attention ranking.
//
// The ranks come straight from the design's triage table. Working agents are
// deliberately absent: an agent doing its job is not something that needs you.
package triage

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ofelcan164/muster/internal/model"
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
func longestFirst(a, b model.Agent, now time.Time) int {
	if a.AgeKnown != b.AgeKnown {
		if a.AgeKnown {
			return -1
		}
		return 1
	}
	return cmp.Compare(b.Age(now), a.Age(now))
}

func newestFirst(a, b model.Agent, now time.Time) int {
	if a.AgeKnown != b.AgeKnown {
		if a.AgeKnown {
			return -1
		}
		return 1
	}
	return cmp.Compare(a.Age(now), b.Age(now))
}

// Input is everything the ranking needs, already assembled by the daemon.
type Input struct {
	Now   time.Time
	Repos []model.Repo
	Orch  model.Orchestrator

	// Stopped lists non-agent panes whose process went away.
	Stopped []model.Stopped

	// EverDone reports whether a pane has ever been observed entering "done".
	// The idle-never-done rule turns on this.
	EverDone map[string]bool

	// EverWorked reports whether a pane was ever seen working. An agent that
	// has never done anything is idle, not stalled.
	EverWorked map[string]bool
}

type agentRef struct {
	repo  model.Repo
	agent model.Agent
}

// Rank builds the ribbon. Rows are ordered by rank, then by the tie-break the
// table specifies for that rank, and the result is capped at RibbonMax.
func Rank(in Input) []model.Attention {
	// The orchestrator has its own strip, so it stays out of the informational
	// ranks. It does not stay out of the ribbon entirely: an orchestrator
	// sitting at a permission prompt is the single most important thing on the
	// screen, and excluding it wholesale meant a blocked orchestrator produced
	// an empty ribbon while herdr's own sidebar showed it as blocked.
	var all []agentRef
	for _, r := range in.Repos {
		for _, a := range r.Agents {
			if a.IsOrchestrator && !needsYouRegardless(a) {
				continue
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
	slices.SortStableFunc(blocked, func(a, b agentRef) int {
		return longestFirst(a.agent, b.agent, in.Now)
	})
	for _, x := range blocked {
		add(model.Attention{
			Rank: 1, Reason: model.ReasonBlocked,
			RepoKey: x.repo.Key, PaneID: x.agent.PaneID, Agent: x.agent.Name,
			Status: x.agent.Status, Age: x.agent.Age(in.Now), AgeKnown: x.agent.AgeKnown,
			Detail: blockedDetail(x.agent),
		})
	}

	// Rank 2: upstream work landed and nobody moved what was parked on it. One
	// row names every agent parked on the same upstream, so the rest are
	// claimed with it rather than each taking a row further down.
	gates, parked := detectGates(in, all)
	for _, g := range gates {
		add(g)
	}
	for _, pane := range parked {
		claimed[pane] = true
	}

	// Rank 3: something that was running has stopped. A dev server going down
	// is invisible until you open the workspace, which is exactly the kind of
	// thing this screen exists to surface.
	stopped := append([]model.Stopped(nil), in.Stopped...)
	slices.SortStableFunc(stopped, func(a, b model.Stopped) int { return cmp.Compare(a.PaneID, b.PaneID) })
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
	slices.SortStableFunc(done, func(a, b agentRef) int {
		return newestFirst(a.agent, b.agent, in.Now)
	})
	for _, x := range done {
		add(model.Attention{
			Rank: 4, Reason: model.ReasonDoneUnseen,
			RepoKey: x.repo.Key, PaneID: x.agent.PaneID, Agent: x.agent.Name,
			Status: x.agent.Status, Age: x.agent.Age(in.Now), AgeKnown: x.agent.AgeKnown,
			Detail: doneDetail(in.Repos, x.agent),
		})
	}

	// Rank 5: an agent that did some work, never reported done, and has been
	// quiet long enough to look stalled.
	//
	// The EverWorked condition is what stops this firing on a shell you opened
	// and never used. Without it every idle agent qualifies forever, which made
	// this by far the noisiest rule in the ribbon.
	//
	// A parked agent is idle by design, waiting on work in another repo, so
	// blocked_on keeps it out.
	stale := filter(all, func(x agentRef) bool {
		return x.agent.Status == model.StatusIdle &&
			x.agent.BlockedOn == "" &&
			in.EverWorked[x.agent.PaneID] &&
			!in.EverDone[x.agent.PaneID] &&
			x.agent.Age(in.Now) >= StaleAfter
	})
	slices.SortStableFunc(stale, func(a, b agentRef) int {
		return longestFirst(a.agent, b.agent, in.Now)
	})
	for _, x := range stale {
		add(model.Attention{
			Rank: 5, Reason: model.ReasonIdleNeverDone,
			RepoKey: x.repo.Key, PaneID: x.agent.PaneID, Agent: x.agent.Name,
			Status: x.agent.Status, Age: x.agent.Age(in.Now), AgeKnown: x.agent.AgeKnown,
			Detail: "idle, never marked done",
		})
	}

	slices.SortStableFunc(rows, func(a, b model.Attention) int { return cmp.Compare(a.Rank, b.Rank) })
	if len(rows) > RibbonMax {
		rows = rows[:RibbonMax]
	}
	return rows
}

// needsYouRegardless reports statuses that surface even for the orchestrator.
// Being blocked is a request for you specifically; the strip cannot convey that
// with the urgency the ribbon can.
func needsYouRegardless(a model.Agent) bool {
	return a.Status == model.StatusBlocked
}

// detectGates finds upstream work that landed while the agents parked on it sat
// still. It returns one row per upstream, and every pane those rows name.
//
// Three facts, recorded by the orchestrator or measured by the daemon:
//   - an agent's blocked_on names a repo and its landed token matches, so the
//     work it waits on is on main (LandedAt)
//   - the orchestrator has ended a turn since then, so it has had its chance to
//     move the parked work on
//   - the parked agent is idle or done in a status it entered before the
//     landing, so nobody moved it
//
// None of that needs a timer. Without a marked orchestrator the middle fact is
// unavailable and the rule stays silent rather than guessing.
func detectGates(in Input, all []agentRef) ([]model.Attention, []string) {
	// Idle or done, both of which mean the orchestrator is not working. done is
	// herdr's word for an agent that finished its turn while you were looking at
	// another pane, which is the usual state of an orchestrator you walked away
	// from, so testing for idle alone missed the case this rule exists for.
	resting := in.Orch.Status == model.StatusIdle || in.Orch.Status == model.StatusDone
	if !in.Orch.Found || !resting || in.Orch.StatusSince.IsZero() {
		return nil, nil
	}

	var order []string
	groups := map[string][]agentRef{}
	for _, x := range all {
		a := x.agent
		if a.After == "" || a.LandedAt.IsZero() {
			continue
		}
		// An equal time counts as a turn ended: the landed token and the end of
		// the turn that wrote it can arrive in the same reconcile.
		if in.Orch.StatusSince.Before(a.LandedAt) {
			continue
		}
		// For the parked agent, equal counts as moved: changing state in the same
		// reconcile as the landing is most likely the orchestrator dispatching it.
		if (a.Status != model.StatusIdle && a.Status != model.StatusDone) || !a.StatusSince.Before(a.LandedAt) {
			continue
		}
		if _, ok := groups[a.After]; !ok {
			order = append(order, a.After)
		}
		groups[a.After] = append(groups[a.After], x)
	}

	var out []model.Attention
	var parked []string
	for _, up := range order {
		xs := groups[up]
		slices.SortStableFunc(xs, func(a, b agentRef) int { return longestFirst(a.agent, b.agent, in.Now) })
		var who, repos []string
		for _, x := range xs {
			label := cmp.Or(x.repo.Display, x.repo.Name)
			who = append(who, label+"/"+x.agent.Name)
			if !slices.Contains(repos, label) {
				repos = append(repos, label)
			}
			parked = append(parked, x.agent.PaneID)
		}
		// The row lands on the agent parked longest, since that is where the
		// rebase happens. The upstream agent may be long gone by now.
		lead := xs[0]
		out = append(out, model.Attention{
			Rank: 2, Reason: model.ReasonGateOpen,
			RepoKey: lead.repo.Key, PaneID: lead.agent.PaneID, Agent: lead.agent.Name,
			Status: lead.agent.Status, Age: lead.agent.Age(in.Now), AgeKnown: lead.agent.AgeKnown,
			Detail:     fmt.Sprintf("%s landed · %s still parked on it", repoLabel(in.Repos, up), strings.Join(repos, ", ")),
			Downstream: who,
		})
	}
	slices.SortStableFunc(out, func(a, b model.Attention) int { return cmp.Compare(b.Age, a.Age) })
	return out, parked
}

// doneDetail is what a finished agent's row says. A parked agent finishing its
// turn has written code that cannot land yet, which is different news from
// finished work, and calling both "finished" is how the parked one got missed.
func doneDetail(repos []model.Repo, a model.Agent) string {
	switch {
	case a.BlockedOn == "":
		return "finished, unseen"
	case !a.LandedAt.IsZero():
		return "ready, " + afterName(repos, a) + " landed"
	default:
		return "ready, after " + afterName(repos, a)
	}
}

// afterName is what a parked agent waits on: the repo's name when blocked_on
// resolved to one, else the text as it was written.
func afterName(repos []model.Repo, a model.Agent) string {
	if a.After == "" {
		return a.BlockedOn
	}
	return repoLabel(repos, a.After)
}

func repoLabel(repos []model.Repo, key string) string {
	for _, r := range repos {
		if r.Key == key {
			return cmp.Or(r.Display, r.Name, r.Key)
		}
	}
	return key
}

// blockedDetail says what the agent is actually waiting for. The question read
// from the pane beats the task line: "Do you want to create abc.txt?" tells you
// whether you can answer it from here, where the task line only tells you what
// the agent was working on.
func blockedDetail(a model.Agent) string {
	if a.Question != "" {
		return truncate(a.Question, 80)
	}
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

// truncate cuts to n characters. Runes, not bytes: slicing bytes can cut a
// multi-byte character in half, and json.Marshal turns the invalid tail into a
// replacement character in the ribbon.
func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
