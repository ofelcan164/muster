package ui

import (
	"testing"

	"github.com/ofelcan/muster/internal/herdr"
)

// The bug: the toggle closed any overlay it found anywhere, so pressing the key
// somewhere else closed one you could not see and appeared to do nothing.
//
// The scope is the tab rather than the workspace, because only one tab of a
// workspace is on screen: an overlay in the tab next door is exactly as
// invisible as one in another workspace, and scoping to the workspace still
// spent a press closing it.
func TestToggleOnlyFindsTheOverlayInTheFocusedTab(t *testing.T) {
	panes := []herdr.Pane{
		{PaneID: "w5:p1", WorkspaceID: "w5", TabID: "w5:t1"},
		{PaneID: "w5:pE", WorkspaceID: "w5", TabID: "w5:t1", Label: overlayTitle},
		{PaneID: "w9:p1", WorkspaceID: "w9", TabID: "w9:t1"},
	}
	if got := overlayPanesIn(panes, "w5:t1"); len(got) != 1 || got[0] != "w5:pE" {
		t.Errorf("in w5:t1 the toggle found %v, want the overlay there", got)
	}
	if got := overlayPanesIn(panes, "w9:t1"); len(got) != 0 {
		t.Errorf("in w9:t1 the toggle found %v, which is another tab's overlay", got)
	}
	// The reported bug: same workspace, different tab, so it is off screen.
	if got := overlayPanesIn(panes, "w5:t2"); len(got) != 0 {
		t.Errorf("in w5:t2 the toggle found %v, which is in the tab next door", got)
	}
}

// A pane keeps the label after the overlay process in it exits, so the label is
// not proof that a pane is an overlay. Every match has to be offered, or a
// corpse sitting in front of a live overlay means the live one never closes.
func TestEveryLabelledPaneIsACandidate(t *testing.T) {
	panes := []herdr.Pane{
		{PaneID: "w5:pE", WorkspaceID: "w5", TabID: "w5:t1", Label: overlayTitle}, // corpse
		{PaneID: "w5:pG", WorkspaceID: "w5", TabID: "w5:t1", Label: overlayTitle}, // live
	}
	got := overlayPanesIn(panes, "w5:t1")
	if len(got) != 2 || got[0] != "w5:pE" || got[1] != "w5:pG" {
		t.Errorf("got %v, want both panes in order", got)
	}
}
