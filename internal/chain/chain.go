// Package chain stores the usual order between repos.
//
// It is a default, not an edge. The orchestrator reads it back through
// `muster show` or `muster chain get` before it writes depends_on onto the
// agents it dispatches, and records a new one with `muster chain set` when a
// durable order changes. Muster never draws an edge or ranks a row from it:
// "api before web" holds for one feature and can reverse for the next, so edges
// live on agents.
package chain

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Chain is an order over repo names. Stages run in sequence and everything
// within a stage runs in parallel, which covers a linear order and a fan-out
// where several repos follow the same repo.
type Chain struct {
	Stages [][]string `json:"stages"`

	// SetBy and SetAt record provenance so an orchestrator reading the order
	// back can tell how old it is and decide whether it still holds.
	SetBy string    `json:"set_by,omitempty"`
	SetAt time.Time `json:"set_at,omitempty"`
}

// Path is where the order lives, inside Muster's state directory.
func Path(stateDir string) string { return filepath.Join(stateDir, "chain.json") }

// Load reads the stored order. A missing or unreadable file is not an error:
// no order simply means there is nothing to follow.
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

// Save writes the order, replacing whatever was there.
func Save(stateDir string, c *Chain, write func(string, []byte) error) error {
	c.SetAt = time.Now()
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return write(Path(stateDir), append(b, '\n'))
}

func (c *Chain) Empty() bool { return len(c.Stages) == 0 }

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

// Format renders stages back into the spec syntax Parse accepts.
func Format(stages [][]string) string {
	parts := make([]string, 0, len(stages))
	for _, stage := range stages {
		parts = append(parts, strings.Join(stage, ","))
	}
	return strings.Join(parts, " > ")
}
