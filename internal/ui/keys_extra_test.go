package ui

import (
	"strings"
	"testing"

	"github.com/ofelcan/muster/internal/model"
)

// A card with one pane behind it has an obvious answer. A card with nine does
// not, and picking the first of nine shells scattered across nine workspaces
// takes the screen somewhere nobody asked to go.
func TestActivatingABareCard(t *testing.T) {
	bare := func(panes int) *model.Snapshot {
		s := &model.Snapshot{Repos: []model.Repo{{Key: "k", Name: "ofelcan", Display: "ofelcan"}}}
		for i := 0; i < panes; i++ {
			s.Repos[0].OtherPanes = append(s.Repos[0].OtherPanes,
				model.Pane{PaneID: "w" + string(rune('1'+i)) + ":p1", Label: "bash"})
		}
		return s
	}

	one := withSnapshot(t, bare(1), 100)
	one.cursor = 0
	one.activate()
	if one.Jump() != "w1:p1" {
		t.Errorf("one pane is an obvious answer, jumped to %q", one.Jump())
	}

	many := withSnapshot(t, bare(9), 100)
	many.cursor = 0
	many.activate()
	if many.Jump() != "" {
		t.Errorf("nine panes should not pick one, jumped to %q", many.Jump())
	}
	if !strings.Contains(many.notice, "9 panes") {
		t.Errorf("notice = %q, want it to say why it did nothing", many.notice)
	}

	none := withSnapshot(t, bare(0), 100)
	none.cursor = 0
	none.activate()
	if none.Jump() != "" || !strings.Contains(none.notice, "nothing open") {
		t.Errorf("jump=%q notice=%q", none.Jump(), none.notice)
	}
}

// A notice raised with no orchestrator marked is written to a line the strip
// was not drawing at all.
func TestNoticeShowsWithNoOrchestrator(t *testing.T) {
	m := withSnapshot(t, testSnapshot(), 100)
	m.snap.Orch = model.Orchestrator{Found: false}
	m.notice = "ofelcan has nothing open to jump to"
	if !strings.Contains(plain(m.View()), "nothing open to jump to") {
		t.Errorf("the notice never reached the screen:\n%s", plain(m.View()))
	}
}
