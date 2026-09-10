package state

import (
	"slices"
	"testing"
)

// Two overlays open at once each load the file when they open, then save
// different fields. Every save has to keep what the other one wrote.
func TestUISaveKeepsWhatAnotherOverlaySaved(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())

	a, b := LoadUI(), LoadUI()
	a.Sort = 2
	if err := a.Save(); err != nil {
		t.Fatal(err)
	}
	b.RepoOrder = []string{"acme/api", "acme/web"}
	if err := b.Save(); err != nil {
		t.Fatal(err)
	}
	// a saves again without ever having seen b's order.
	a.Dismissed = map[string]string{"p1": "done"}
	if err := a.Save(); err != nil {
		t.Fatal(err)
	}

	got := LoadUI()
	if got.Sort != 2 {
		t.Errorf("sort = %d, want a's 2", got.Sort)
	}
	if !slices.Equal(got.RepoOrder, []string{"acme/api", "acme/web"}) {
		t.Errorf("repo order = %v, want b's order", got.RepoOrder)
	}
	if got.Dismissed["p1"] != "done" {
		t.Errorf("dismissed = %v, want a's p1", got.Dismissed)
	}
}
