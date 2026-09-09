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
	if got := overlayPaneIn(panes, "w5"); got != "w5:pE" {
		t.Errorf("in w5 the toggle found %q, want the overlay there", got)
	}
	if got := overlayPaneIn(panes, "w9"); got != "" {
		t.Errorf("in w9 the toggle found %q, which is another workspace's overlay", got)
	}
}
