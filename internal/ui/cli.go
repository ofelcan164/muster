// The overlay's actions for a caller outside it, such as a desktop bar's
// panel. Each goes through the same code as its key, so the two cannot
// disagree about what a message, a report or a dismissal means.

package ui

import (
	"errors"
	"fmt"
	"time"

	"github.com/ofelcan164/muster/internal/daemon"
	"github.com/ofelcan164/muster/internal/herdr"
	"github.com/ofelcan164/muster/internal/model"
	"github.com/ofelcan164/muster/internal/state"
)

// promptAgent sends text to an agent and records it as what the agent was
// told. agent.prompt is the same call the orchestrator's own tooling uses, so
// a message from Muster is not a special case at the other end.
func promptAgent(paneID, text string) error {
	if err := herdr.NewClient("").Call("agent.prompt",
		map[string]any{"target": paneID, "text": text}, nil); err != nil {
		return err
	}
	recordTold(paneID, text)
	return nil
}

// Tell is i: it sends text to the orchestrator.
func Tell(text string) error {
	pane, err := ResolveTarget("orchestrator")
	if err != nil {
		return err
	}
	return promptAgent(pane, text)
}

// Report is t for the landed row on pane: it tells the orchestrator the work
// landed and who still depends on it. It returns what it told.
func Report(pane string) (string, error) {
	snap, err := daemon.ReadSnapshot()
	if err != nil {
		return "", errors.New("no snapshot: is musterd running?")
	}
	var row *model.Attention
	for i, a := range snap.Attention {
		if a.PaneID == pane && a.Reason == model.ReasonLanded {
			row = &snap.Attention[i]
			break
		}
	}
	if row == nil {
		return "", fmt.Errorf("%s has no landed row to report", pane)
	}
	if !snap.Orch.Found {
		return "", errors.New("no orchestrator marked, so there is nobody to tell")
	}
	text, up := landedReport(snap, *row, time.Now())
	if err := promptAgent(snap.Orch.PaneID, text); err != nil {
		return "", err
	}
	return fmt.Sprintf("told the orchestrator %s landed", shortRepo(up)), nil
}

// Dismiss is x: it takes pane's row off the ribbon until its status changes.
func Dismiss(pane string) error {
	snap, err := daemon.ReadSnapshot()
	if err != nil {
		return errors.New("no snapshot: is musterd running?")
	}
	ui := state.LoadUI()
	d, ok := dismissRow(snap, ui.Dismissed, pane)
	if !ok {
		return fmt.Errorf("%s has no row in the ribbon to dismiss", pane)
	}
	ui.Dismissed = d
	return ui.Save()
}
