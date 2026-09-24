package state

import "testing"

// Two overlays open at once each load the file when they open, then save
// different fields. Every save has to keep what the other one wrote.
func TestUISaveKeepsWhatAnotherOverlaySaved(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())

	a, b := LoadUI(), LoadUI()
	a.Sort = 2
	if err := a.Save(); err != nil {
		t.Fatal(err)
	}
	b.Dismissed = map[string]string{"p1": "done"}
	b.Colors = map[string]int{"acme/api": 7}
	b.SayRows = 5
	if err := b.Save(); err != nil {
		t.Fatal(err)
	}
	// a saves again without ever having seen b's dismissal.
	a.Sort = 3
	if err := a.Save(); err != nil {
		t.Fatal(err)
	}

	got := LoadUI()
	if got.Sort != 3 {
		t.Errorf("sort = %d, want a's 3", got.Sort)
	}
	if got.Dismissed["p1"] != "done" {
		t.Errorf("dismissed = %v, want b's p1", got.Dismissed)
	}
	if got.Colors["acme/api"] != 7 {
		t.Errorf("colors = %v, want b's acme/api", got.Colors)
	}
	if got.SayRows != 5 {
		t.Errorf("say rows = %d, want b's 5", got.SayRows)
	}
}
