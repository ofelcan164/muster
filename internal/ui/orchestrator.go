package ui

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/ofelcan/muster/internal/herdr"
)

// MarkOrchestrator names a pane's agent as the orchestrator and tags it.
//
// Both, deliberately. The token is authoritative because it survives a rename
// and can carry more than a string later. The name exists so that
// `agent prompt orchestrator "..."` works from any script without anyone having
// to look up a pane id.
func MarkOrchestrator() error {
	pane := contextPaneID()
	if pane == "" {
		return fmt.Errorf("no pane to mark: run this action on the orchestrator's pane")
	}
	c := herdr.NewClient("")

	if err := c.Call("agent.rename", map[string]any{
		"target": pane,
		"name":   "orchestrator",
	}, nil); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	if err := c.Call("pane.report_metadata", map[string]any{
		"pane_id": pane,
		"source":  "muster",
		"tokens":  map[string]string{"role": "orchestrator"},
	}, nil); err != nil {
		return fmt.Errorf("tag: %w", err)
	}
	fmt.Printf("marked %s as the orchestrator\n", pane)
	return nil
}

// contextPaneID finds the pane an action was invoked on. herdr passes the
// invocation context as JSON, and falls back to the pane the command runs in.
func contextPaneID() string {
	if raw := os.Getenv("HERDR_PLUGIN_CONTEXT_JSON"); raw != "" {
		var ctx struct {
			FocusedPaneID string `json:"focused_pane_id"`
			PaneID        string `json:"pane_id"`
		}
		if json.Unmarshal([]byte(raw), &ctx) == nil {
			if ctx.PaneID != "" {
				return ctx.PaneID
			}
			if ctx.FocusedPaneID != "" {
				return ctx.FocusedPaneID
			}
		}
	}
	return os.Getenv("HERDR_PANE_ID")
}
