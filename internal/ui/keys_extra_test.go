package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ofelcan164/muster/internal/model"
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

// Muster is a popup, so herdr hands it the global keys instead of acting on
// them. m and M are those keys with the prefix left off, and the prefix itself
// has to do nothing for prefix+m to keep closing.
func TestGlobalKeysInsideThePopup(t *testing.T) {
	altQ := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q"), Alt: true}

	m := withSnapshot(t, withOrch(), 143)
	m.Update(altQ)
	if m.quit || m.Jump() != "" {
		t.Fatalf("the prefix on its own acted: quit=%v jump=%q", m.quit, m.Jump())
	}
	key(m, "m")
	if !m.quit {
		t.Error("prefix then m should close, the way it would outside the popup")
	}

	orch := withSnapshot(t, withOrch(), 143)
	key(orch, "M")
	if orch.Jump() != "w4:p1" {
		t.Errorf("M jumped to %q, want the orchestrator w4:p1", orch.Jump())
	}

	none := withSnapshot(t, testSnapshot(), 143)
	key(none, "M")
	if none.quit || none.Jump() != "" {
		t.Errorf("with no orchestrator M should stay open: quit=%v jump=%q", none.quit, none.Jump())
	}
	if !strings.Contains(none.notice, "no orchestrator") {
		t.Errorf("notice = %q, want it to say there is no orchestrator", none.notice)
	}
}

// While typing, m is a letter, and an alt key is a chord rather than text. The
// prefix is alt+q for plenty of people, and it reaches the popup.
func TestTypingKeepsMAndDropsAltKeys(t *testing.T) {
	altQ := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q"), Alt: true}

	m := withSnapshot(t, withOrch(), 143)
	key(m, "slash")
	m.Update(altQ)
	key(m, "m")
	key(m, "M")
	if m.quit || m.filter != "mM" {
		t.Errorf("filter = %q quit=%v, want \"mM\" and still open", m.filter, m.quit)
	}

	c := withSnapshot(t, withOrch(), 143)
	key(c, "i")
	c.Update(altQ)
	key(c, "m")
	if c.compose != "m" {
		t.Errorf("compose = %q, want \"m\" with the alt key dropped", c.compose)
	}
}
