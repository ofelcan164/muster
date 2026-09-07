// Filtering, entered with "/".

package ui

import (
	"strings"

	"github.com/ofelcan/muster/internal/model"
)

// visibleRepos applies the filter. Filtering collapses the grid into a flat
// ranked list, because once you are filtering you already know what you want
// and spatial memory is not doing any work.
func (m *Model) visibleRepos() []model.Repo {
	if m.filter == "" {
		return m.snap.Repos
	}
	q := strings.ToLower(m.filter)
	var out []model.Repo
	for _, r := range m.snap.Repos {
		// A repo that matches on its own name or branch is a hit, whether or not
		// anything is running in it. Requiring a matching agent meant searching
		// for a repo with no agents found nothing at all, which is exactly the
		// case you hit when looking for somewhere to start work.
		if repoMatches(r, q) {
			out = append(out, r)
			continue
		}
		var kept []model.Agent
		for _, a := range r.Agents {
			if agentMatches(a, q) {
				kept = append(kept, a)
			}
		}
		if len(kept) > 0 {
			r.Agents = kept
			out = append(out, r)
		}
	}
	return out
}

// repoMatches searches the fields that belong to the repo itself.
func repoMatches(r model.Repo, q string) bool {
	return containsAny(q, r.Display, r.Name, r.Branch)
}

// agentMatches searches the fields that belong to an agent. Together these
// cover agent name, task text, repo and branch, which is what the design says
// the filter should look at.
func agentMatches(a model.Agent, q string) bool {
	return containsAny(q, a.Name, a.Task, a.Question, a.PaneID)
}

func containsAny(q string, fields ...string) bool {
	for _, f := range fields {
		if f != "" && strings.Contains(strings.ToLower(f), q) {
			return true
		}
	}
	return false
}
