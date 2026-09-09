package ui

import (
	"testing"

	"github.com/ofelcan/muster/internal/herdr"
)

// The bug: the toggle closed any overlay it found anywhere, so pressing the key
// in one workspace closed an overlay open in another and appeared to do nothing.
func TestToggleOnlyFindsTheOverlayInTheFocusedWorkspace(t *testing.T) {
	panes := []herdr.Pane{
		{PaneID: "w5:p1", WorkspaceID: "w5"},
		{PaneID: "w5:pE", WorkspaceID: "w5", Label: overlayTitle},
		{PaneID: "w9:p1", WorkspaceID: "w9"},
	}
	if got := overlayPanesIn(panes, "w5"); len(got) != 1 || got[0] != "w5:pE" {
		t.Errorf("in w5 the toggle found %v, want the overlay there", got)
	}
	if got := overlayPanesIn(panes, "w9"); len(got) != 0 {
		t.Errorf("in w9 the toggle found %v, which is another workspace's overlay", got)
	}
}

// A pane keeps the label after the overlay process in it exits, so the label is
// not proof that a pane is an overlay. Every match has to be offered, or a
// corpse sitting in front of a live overlay means the live one never closes.
func TestEveryLabelledPaneIsACandidate(t *testing.T) {
	panes := []herdr.Pane{
		{PaneID: "w5:pE", WorkspaceID: "w5", Label: overlayTitle}, // the corpse
		{PaneID: "w5:pG", WorkspaceID: "w5", Label: overlayTitle}, // the live one
	}
	got := overlayPanesIn(panes, "w5")
	if len(got) != 2 || got[0] != "w5:pE" || got[1] != "w5:pG" {
		t.Errorf("got %v, want both panes in order", got)
	}
}
