package ui

import (
	"sort"
	"strings"

	"github.com/ofelcan/muster/internal/model"
)

// SortMode is how the repo grid is ordered.
//
// First-seen is the default and the one the design argues for: a repo keeps its
// cell forever, so you point instead of read. The others exist because a grid
// you cannot rearrange is one bad first session away from being wrong for good.
type SortMode int

const (
	SortFirstSeen SortMode = iota
	SortAlphabetical
	SortAttention
	sortModeCount
)

func (s SortMode) String() string {
	switch s {
	case SortAlphabetical:
		return "a-z"
	case SortAttention:
		return "attention"
	default:
		return "first seen"
	}
}

// Next cycles through the sort modes.
func (s SortMode) Next() SortMode { return (s + 1) % sortModeCount }

// orderedRepos applies the current sort, then the user's manual moves.
func (m *Model) orderedRepos(repos []model.Repo) []model.Repo {
	// While filtering the order is the match ranking and nothing else. A sort or
	// an arrangement made for the full grid on top of it would push a worse
	// match above a better one, and position in a filtered list carries no
	// meaning to preserve.
	if m.filter != "" {
		return repos
	}

	out := append([]model.Repo(nil), repos...)

	switch m.sort {
	case SortAlphabetical:
		sort.SliceStable(out, func(i, j int) bool {
			return strings.ToLower(out[i].Display) < strings.ToLower(out[j].Display)
		})
	case SortAttention:
		// Repos holding something that needs you come first, most urgent first.
		rank := m.repoRanks()
		sort.SliceStable(out, func(i, j int) bool {
			ri, rj := rank[out[i].Key], rank[out[j].Key]
			if ri != rj {
				return ri < rj
			}
			return out[i].GridSlot < out[j].GridSlot
		})
	default:
		sort.SliceStable(out, func(i, j int) bool { return out[i].GridSlot < out[j].GridSlot })
	}

	// Repos with agents always come first, whatever the sort. A quiet repo is
	// still worth a card, but it should never sit between two you are working
	// in. This runs after the sort so it never disturbs the order within each
	// group, and before manual moves so you can still override it.
	sort.SliceStable(out, func(i, j int) bool {
		return len(out[i].Agents) > 0 && len(out[j].Agents) == 0
	})

	return applyMoves(out, m.moves)
}

// Ranks below these come from the ribbon, which uses 1 to 5. Work sits under
// all of them and above silence: an agent doing its job never reaches the
// ribbon, so without a rank of its own a repo with three agents mid-work sorted
// level with an empty one.
const (
	rankWorking = 50
	rankQuiet   = 99
)

// repoRanks is the best ribbon rank each repo holds, or where it falls without
// one: needs you, then working, then everything else.
func (m *Model) repoRanks() map[string]int {
	rank := map[string]int{}
	for _, a := range m.snap.Attention {
		if r, ok := rank[a.RepoKey]; !ok || a.Rank < r {
			rank[a.RepoKey] = a.Rank
		}
	}
	for _, r := range m.snap.Repos {
		if _, ok := rank[r.Key]; ok {
			continue
		}
		rank[r.Key] = rankQuiet
		for _, a := range r.Agents {
			if a.Status == model.StatusWorking {
				rank[r.Key] = rankWorking
				break
			}
		}
	}
	return rank
}

// applyMoves reorders by the manual moves the user has made this session.
// Moves are expressed as a repo key and the position it was dragged to, which
// survives the underlying list changing beneath them.
func applyMoves(repos []model.Repo, moves []string) []model.Repo {
	if len(moves) == 0 {
		return repos
	}
	byKey := map[string]model.Repo{}
	for _, r := range repos {
		byKey[r.Key] = r
	}
	var out []model.Repo
	seen := map[string]bool{}
	for _, key := range moves {
		if r, ok := byKey[key]; ok && !seen[key] {
			out = append(out, r)
			seen[key] = true
		}
	}
	for _, r := range repos {
		if !seen[r.Key] {
			out = append(out, r)
		}
	}
	return out
}

// moveSelectedRepo shifts the selected repo one place in the manual order.
//
// Not while filtering. The order is built from what is visible, so a move made
// against three matches would record an arrangement of three repos and push
// every other repo behind them. Filtering already collapses the grid into a
// ranked list where position carries no meaning, so there is nothing to
// rearrange.
func (m *Model) moveSelectedRepo(delta int) {
	if m.filter != "" {
		return
	}
	key := m.selectedRepo()
	if key == "" {
		return
	}
	order := m.currentOrder()
	at := -1
	for i, k := range order {
		if k == key {
			at = i
			break
		}
	}
	if at < 0 {
		return
	}
	to := at + delta
	if to < 0 || to >= len(order) {
		return
	}
	order[at], order[to] = order[to], order[at]
	m.moves = order
	m.rebuild()
	if m.saveOrder != nil {
		m.saveOrder(order)
	}
}

// currentOrder is the repo keys as they are drawn right now.
func (m *Model) currentOrder() []string {
	repos := m.orderedRepos(m.visibleRepos())
	out := make([]string, 0, len(repos))
	for _, r := range repos {
		out = append(out, r.Key)
	}
	return out
}
