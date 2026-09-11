package ui

import (
	"testing"

	"github.com/ofelcan164/muster/internal/herdr"
)

// A repo card with no agents jumps to a shell, and agent.focus cannot focus
// one. The tab it lives in is the fallback, so the lookup has to hold.
func TestTabOfFindsThePanesTab(t *testing.T) {
	panes := []herdr.Pane{
		{PaneID: "w1:p1", TabID: "w1:t1"},
		{PaneID: "w4:p1", TabID: "w4:t2"},
	}
	if got := tabOf(panes, "w4:p1"); got != "w4:t2" {
		t.Errorf("got %q, want w4:t2", got)
	}
	if got := tabOf(panes, "w9:p9"); got != "" {
		t.Errorf("a pane that is gone gave %q, want the empty string", got)
	}
}
