// Filtering, entered with "/".

package ui

import (
	"sort"
	"strings"

	"github.com/ofelcan164/muster/internal/model"
)

// visibleRepos applies the filter, best match first. Filtering collapses the
// grid into a flat ranked list, because once you are filtering you already know
// what you want and spatial memory is not doing any work.
func (m *Model) visibleRepos() []model.Repo {
	terms := strings.Fields(strings.ToLower(m.filter))
	if len(terms) == 0 {
		return m.snap.Repos
	}
	type scored struct {
		repo  model.Repo
		score int
	}
	var hits []scored
	for _, r := range m.snap.Repos {
		own := []string{r.Display, r.Name, r.Branch}
		// A repo that matches on its own name or branch is a hit, whether or not
		// anything is running in it. Requiring a matching agent meant searching
		// for a repo with no agents found nothing at all, which is exactly the
		// case you hit when looking for somewhere to start work.
		if s, ok := scoreTerms(terms, own); ok {
			hits = append(hits, scored{r, s})
			continue
		}
		var kept []model.Agent
		best := 0
		for _, a := range r.Agents {
			// An agent is searched against its repo's fields as well as its own,
			// so "web auth" finds the auth agent in the web repo. The terms are
			// spread across both and neither field set alone holds them all.
			s, ok := scoreTerms(terms, append([]string{a.Name, a.Task, a.Question, a.PaneID}, own...))
			if !ok {
				continue
			}
			kept = append(kept, a)
			if s > best {
				best = s
			}
		}
		if len(kept) > 0 {
			r.Agents = kept
			hits = append(hits, scored{r, best})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })

	out := make([]model.Repo, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.repo)
	}
	return out
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
