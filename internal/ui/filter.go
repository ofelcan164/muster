// Filtering, entered with "/".

package ui

import (
	"cmp"
	"slices"
	"strings"
)

// visibleTiles applies the filter, best match first. Filtering collapses the
// grid into a flat ranked list, because once you are filtering you already know
// what you want and spatial memory is not doing any work.
func (m *Model) visibleTiles() []tile {
	all := buildTiles(m.snap)
	terms := strings.Fields(strings.ToLower(m.filter))
	if len(terms) == 0 {
		return all
	}
	type scored struct {
		tile  tile
		score int
	}
	var hits []scored
	for _, t := range all {
		if s, ok := scoreTerms(terms, tileFields(t)); ok {
			hits = append(hits, scored{t, s})
		}
	}
	slices.SortStableFunc(hits, func(a, b scored) int { return cmp.Compare(b.score, a.score) })

	out := make([]tile, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.tile)
	}
	return out
}

// tileFields is what a tile is searched against: where it is and which
// checkout, never who or what. Every field is one the tile draws, or a
// subsequence hit on a hidden one (the repo's owner) selects a tile with no
// visible reason. An empty tile has no repo of its own, so it offers the repos
// its panes sit in, and the branch only when it draws one.
func tileFields(t tile) []string {
	if t.isAgent() {
		return []string{t.Workspace.Label, paneChip(t.Agent), t.Repo.Display, t.Repo.Branch}
	}
	fields := []string{t.Workspace.Label}
	for _, r := range t.Repos {
		fields = append(fields, r.Display)
	}
	if len(t.Repos) == 1 {
		fields = append(fields, t.Repos[0].Branch)
	}
	return fields
}

// scoreTerms requires every term to hit some field, and totals the best score
// each one got. A multi-word query is several conditions rather than one
// literal string: "web auth" is two things that both have to be true.
func scoreTerms(terms, fields []string) (int, bool) {
	total := 0
	for _, t := range terms {
		best := 0
		for _, f := range fields {
			if s := scoreTerm(t, f); s > best {
				best = s
			}
		}
		if best == 0 {
			return 0, false
		}
		total += best
	}
	return total, true
}

// Match quality, coarse on purpose. Three tiers are enough to float the obvious
// answer to the top of a list of a dozen repos, and every finer rule is one
// more thing to explain when the ordering surprises someone.
const (
	scorePrefix      = 100
	scoreContains    = 60
	scoreSubsequence = 20
)

func scoreTerm(term, field string) int {
	if field == "" {
		return 0
	}
	f := strings.ToLower(field)
	switch {
	case strings.HasPrefix(f, term):
		return scorePrefix
	case strings.Contains(f, term):
		return scoreContains
	case isSubsequence(term, f):
		return scoreSubsequence
	}
	return 0
}

// isSubsequence is what makes "cnt" find "contracts": the letters in order,
// not necessarily together.
func isSubsequence(term, field string) bool {
	t := []rune(term)
	if len(t) == 0 {
		return true
	}
	i := 0
	for _, c := range field {
		if c == t[i] {
			if i++; i == len(t) {
				return true
			}
		}
	}
	return false
}
