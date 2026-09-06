package daemon

import (
	"testing"

	"github.com/ofelcan/muster/internal/model"
)

func agents(panes ...string) map[string]model.Agent {
	m := map[string]model.Agent{}
	for _, p := range panes {
		m[p] = model.Agent{PaneID: p}
	}
	return m
}

func TestBackKeyReturnsToThePreviousAgent(t *testing.T) {
	d := newTestDaemon(t)
	live := agents("w1:p1", "w2:p1", "w3:p1")

	d.trackFocus("w1:p1", live)
	if got := d.previousAgent(); got != "" {
		t.Errorf("nowhere to go back to yet, got %q", got)
	}

	d.trackFocus("w2:p1", live)
	if got := d.previousAgent(); got != "w1:p1" {
		t.Errorf("previous = %q, want w1:p1", got)
	}

	d.trackFocus("w3:p1", live)
	if got := d.previousAgent(); got != "w2:p1" {
		t.Errorf("previous = %q, want w2:p1", got)
	}
}

// Focusing the same agent repeatedly must not make "back" point at itself.
func TestRefocusingDoesNotOverwriteHistory(t *testing.T) {
	d := newTestDaemon(t)
	live := agents("w1:p1", "w2:p1")
	d.trackFocus("w1:p1", live)
	d.trackFocus("w2:p1", live)
	d.trackFocus("w2:p1", live)
	d.trackFocus("w2:p1", live)
	if got := d.previousAgent(); got != "w1:p1" {
		t.Errorf("previous = %q, want w1:p1", got)
	}
}

// The overlay itself, shells and log tails are not agents. Landing on one must
// not become the thing the back key returns you to.
func TestNonAgentPanesAreNotTracked(t *testing.T) {
	d := newTestDaemon(t)
	live := agents("w1:p1", "w2:p1")

	d.trackFocus("w1:p1", live)
	d.trackFocus("w4:p2", live) // the Muster overlay
	d.trackFocus("w2:p1", live)

	if got := d.previousAgent(); got != "w1:p1" {
		t.Errorf("previous = %q, want w1:p1; the overlay must be invisible to back", got)
	}
}

// An agent whose pane has closed is not somewhere to go back to.
func TestClosedAgentsDropOutOfHistory(t *testing.T) {
	d := newTestDaemon(t)
	d.trackFocus("w1:p1", agents("w1:p1", "w2:p1"))
	d.trackFocus("w2:p1", agents("w1:p1", "w2:p1"))
	if d.previousAgent() != "w1:p1" {
		t.Fatal("setup")
	}
	// w1 closes.
	d.trackFocus("w3:p1", agents("w2:p1", "w3:p1"))
	if got := d.previousAgent(); got != "w2:p1" {
		t.Errorf("previous = %q, want w2:p1", got)
	}
}

func TestHistorySurvivesARestart(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HERDR_PLUGIN_STATE_DIR", dir)
	live := agents("w1:p1", "w2:p1")

	d1 := New(nil, nil)
	d1.trackFocus("w1:p1", live)
	d1.trackFocus("w2:p1", live)
	if err := d1.persist.Save(); err != nil {
		t.Fatal(err)
	}

	d2 := New(nil, nil)
	if got := d2.previousAgent(); got != "w1:p1" {
		t.Errorf("back key lost its history across a restart, got %q", got)
	}
}
