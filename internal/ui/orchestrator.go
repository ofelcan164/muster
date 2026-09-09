package ui

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/ofelcan164/muster/internal/daemon"
	"github.com/ofelcan164/muster/internal/herdr"
)

// MarkOrchestrator names the pane an action was invoked on as the orchestrator.
func MarkOrchestrator() error {
	pane := contextPaneID()
	if pane == "" {
		return fmt.Errorf("no pane to mark: run this action on the orchestrator's pane")
	}
	if err := markOrchestratorPane(pane, markedOrchestratorPane()); err != nil {
		return err
	}
	fmt.Printf("marked %s as the orchestrator\n", pane)
	return nil
}

// markOrchestratorPane names a pane's agent as the orchestrator and tags it.
// The overlay's o key and the pane action both come through here, so the two
// ways of marking cannot drift apart.
//
// Both a rename and a token, deliberately. The token is authoritative because
// it survives a rename and can carry more than a string later. The name exists
// so that `agent prompt orchestrator "..."` works from any script without
// anyone having to look up a pane id.
//
// previous is the pane that holds the mark now, if any. Its token is cleared
// first: detection returns the first agent carrying the token, so leaving two
// marked would make which one wins depend on the order herdr lists them in.
func markOrchestratorPane(pane, previous string) error {
	c := herdr.NewClient("")

	if previous != "" && previous != pane {
		// Best effort. A stale token that could not be cleared is a problem for
		// the next reconcile, not for this call, and failing here would leave
		// nothing marked at all.
		_ = c.Call("pane.report_metadata", map[string]any{
			"pane_id": previous,
			"source":  "muster",
			"tokens":  map[string]string{"role": ""},
		}, nil)
	}

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
	return nil
}

// markedOrchestratorPane is who holds the mark now, read from the snapshot
// rather than asked of herdr: the daemon has already resolved token before
// name, which is the same answer this would have to work out again.
func markedOrchestratorPane() string {
	snap, err := daemon.ReadSnapshot()
	if err != nil || !snap.Orch.Found {
		return ""
	}
	return snap.Orch.PaneID
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

// recordTold writes what Muster just sent onto the orchestrator's pane as its
// task token, so the strip's "told" line is the message that was actually sent
// rather than a terminal title nobody wrote.
//
// The i and t keys are the only channel that tells the orchestrator anything,
// and the client exits between keypresses, so herdr is where the fact has to
// live. Best effort: the message has already been delivered, and failing here
// would report a send that worked as a send that did not.
func recordTold(pane, text string) {
	_ = herdr.NewClient("").Call("pane.report_metadata", map[string]any{
		"pane_id": pane,
		"source":  "muster",
		"tokens":  map[string]string{"task": truncate(text, 200)},
		// The same day the reporting skill tells every other agent to use, and
		// herdr's ceiling for the field. A message you sent last week outliving
		// the work it was about is the exact thing the skill sets a TTL to
		// stop, and the orchestrator's own pane is not an exception to it.
		"ttl_ms": toldTTLMillis,
	}, nil)
}

// toldTTLMillis is one day, matching the --ttl-ms in the muster-report skill.
const toldTTLMillis = 86_400_000
