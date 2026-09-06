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

	return applyMoves(out, m.moves)
}

// repoRanks is the best ribbon rank each repo holds, or a large number.
func (m *Model) repoRanks() map[string]int {
	rank := map[string]int{}
	for _, a := range m.snap.Attention {
		if r, ok := rank[a.RepoKey]; !ok || a.Rank < r {
			rank[a.RepoKey] = a.Rank
		}
	}
	for _, r := range m.snap.Repos {
		if _, ok := rank[r.Key]; !ok {
			rank[r.Key] = 99
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
func (m *Model) moveSelectedRepo(delta int) {
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
