// Package chain stores the dependency order between repos.
//
// The chain is owned by the orchestrator, not by the user. It is written
// through `muster chain set` during workflow setup, persists across sessions,
// and can be read back with `muster chain get` so a new orchestrator can
// confirm or replace what a previous one recorded. Muster only stores and
// serves it; nothing here infers a dependency.
//
// Without a chain the gate rule stays silent rather than guessing, so Muster is
// fully useful before anyone sets one.
package chain

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Chain is a dependency order over repo names.
//
// Stages run in sequence and everything within a stage runs in parallel, which
// covers both shapes that actually occur: a linear chain, and a fan-out where
// several repos wait on the same upstream.
type Chain struct {
	Stages [][]string `json:"stages"`

	// Independent repos take part in no ordering at all. Naming them is what
	// stops the gate rule treating a quiet unrelated repo as a blocked
	// downstream.
	Independent []string `json:"independent"`

	// SetBy and SetAt record provenance so an orchestrator reading the chain
	// back can tell how old it is and decide whether to confirm it.
	SetBy string    `json:"set_by,omitempty"`
	SetAt time.Time `json:"set_at,omitempty"`
}

// Path is where the chain lives, inside Muster's state directory.
func Path(stateDir string) string { return filepath.Join(stateDir, "chain.json") }

// Load reads the stored chain. A missing or unreadable file is not an error:
// no chain simply means the gate rule stays quiet.
func Load(stateDir string) *Chain {
	b, err := os.ReadFile(Path(stateDir))
	if err != nil {
		return &Chain{}
	}
	var c Chain
	if json.Unmarshal(b, &c) != nil {
		return &Chain{}
	}
	return &c
}

// Save writes the chain, replacing whatever was there. The orchestrator is the
// single source of truth, so this overwrites rather than merges.
func Save(stateDir string, c *Chain, write func(string, []byte) error) error {
	c.SetAt = time.Now()
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return write(Path(stateDir), append(b, '\n'))
}

func (c *Chain) Empty() bool { return len(c.Stages) == 0 && len(c.Independent) == 0 }

// stageOf returns the index of the stage containing a repo, or -1.
func (c *Chain) stageOf(repo string) int {
	for i, stage := range c.Stages {
		for _, name := range stage {
			if matches(name, repo) {
				return i
			}
		}
	}
	return -1
}

// IsIndependent reports whether a repo takes part in no ordering.
func (c *Chain) IsIndependent(repo string) bool {
	for _, name := range c.Independent {
		if matches(name, repo) {
			return true
		}
	}
	return false
}

// DependsOn reports whether downstream sits after upstream in the order.
//
// Only strictly later stages count. Repos sharing a stage run in parallel and
// so cannot be waiting on each other.
func (c *Chain) DependsOn(downstream, upstream string) bool {
	return c.Lookup()(downstream, upstream)
}

// Lookup returns DependsOn with each repo resolved to its stage only once.
//
// Answering a single pair means scanning the stages and lowercasing every name
// in them, which is nothing; a caller comparing every agent against every other
// pays it thousands of times. The returned function is not safe for concurrent
// use, and holds whatever the chain said when it was made.
func (c *Chain) Lookup() func(downstream, upstream string) bool {
	type place struct {
		stage int
		indep bool
	}
	seen := map[string]place{}
	at := func(repo string) place {
		p, ok := seen[repo]
		if !ok {
			p = place{c.stageOf(repo), c.IsIndependent(repo)}
			seen[repo] = p
		}
		return p
	}
	return func(downstream, upstream string) bool {
		d, u := at(downstream), at(upstream)
		if d.indep || u.indep || d.stage < 0 || u.stage < 0 {
			return false
		}
		return d.stage > u.stage
	}
}

// matches compares a configured name against a discovered repo, tolerating the
// owner prefix on either side. The orchestrator should not have to know whether
// Muster resolved a repo to "api" or "acme/api".
func matches(configured, repo string) bool {
	configured, repo = strings.ToLower(configured), strings.ToLower(repo)
	if configured == repo {
		return true
	}
	// Tolerance only goes one way: when both sides name an owner, they are
	// saying which "web" they mean, and comparing the short names anyway made a
	// chain written for acme/web treat otherherd/web as downstream of it.
	if strings.Contains(configured, "/") && strings.Contains(repo, "/") {
		return false
	}
	return short(configured) == short(repo)
}

func short(name string) string {
	if _, after, ok := strings.Cut(name, "/"); ok && after != "" {
		return after
	}
	return name
}

// Parse turns "contracts > api > web,mobile" into stages. Commas separate repos
// that run in parallel; ">" separates things that must happen in order.
func Parse(spec string) [][]string {
	var stages [][]string
	for _, part := range strings.Split(spec, ">") {
		var stage []string
		for _, name := range strings.Split(part, ",") {
			if n := strings.TrimSpace(name); n != "" {
				stage = append(stage, n)
			}
		}
		if len(stage) > 0 {
			stages = append(stages, stage)
		}
	}
	return stages
}

// ParseList splits a comma-separated list of repo names.
func ParseList(spec string) []string {
	var out []string
	for _, name := range strings.Split(spec, ",") {
		if n := strings.TrimSpace(name); n != "" {
			out = append(out, n)
		}
	}
	return out
}

// Format renders stages back into the spec syntax Parse accepts.
func Format(stages [][]string) string {
	parts := make([]string, 0, len(stages))
	for _, stage := range stages {
		parts = append(parts, strings.Join(stage, ","))
	}
	return strings.Join(parts, " > ")
}
