package ui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ofelcan164/muster/internal/model"
)

// withOffer is an overlay that has noticed the reporting skill is missing.
func withOffer(t *testing.T, install func() (string, error)) *Model {
	t.Helper()
	m := withSnapshot(t, withOrch(), 100)
	m.SetSkillPrompt(true, install)
	return m
}

func okInstall() (string, error) { return "/home/u/.claude/skills/muster-report/SKILL.md", nil }

// Without the skill nothing writes a task token, so every card falls back to a
// terminal title. The overlay has to say so, because the alternative is knowing
// to run a command nobody mentioned.
func TestTheOfferAppearsOnlyWhenTheSkillIsMissing(t *testing.T) {
	if out := plain(withOffer(t, okInstall).View()); !strings.Contains(out, "reporting skill is not installed") {
		t.Errorf("no offer to install the skill:\n%s", out)
	}

	there := withSnapshot(t, withOrch(), 100)
	there.SetSkillPrompt(false, okInstall)
	if out := plain(there.View()); strings.Contains(out, "reporting skill") {
		t.Errorf("offered to install a skill that is already there:\n%s", out)
	}

	// An overlay with no installer wired in must not offer one.
	none := withSnapshot(t, withOrch(), 100)
	none.SetSkillPrompt(true, nil)
	if out := plain(none.View()); strings.Contains(out, "reporting skill") {
		t.Errorf("offered an install it cannot run:\n%s", out)
	}
}

// It belongs to the orchestrator, so it sits on the orchestrator's strip, and
// it is the sibling of "none marked": both are the coordination layer saying it
// is not set up yet.
func TestTheOfferSitsOnTheStripWithOrWithoutAnOrchestrator(t *testing.T) {
	s := withOrch()
	s.Orch = model.Orchestrator{Found: false}
	m := withSnapshot(t, s, 100)
	m.SetSkillPrompt(true, okInstall)

	out := plain(m.View())
	if !strings.Contains(out, "none marked") || !strings.Contains(out, "reporting skill") {
		t.Errorf("the two setup lines are not together:\n%s", out)
	}
}

// S rather than enter. Enter means go to the thing you picked everywhere else
// on this screen, and writing a file into someone's home is not that.
func TestSInstallsTheSkillAndTheOfferGoes(t *testing.T) {
	ran := false
	m := withOffer(t, func() (string, error) { ran = true; return okInstall() })
	key(m, "S")

	if !ran {
		t.Fatal("S did not install anything")
	}
	if m.Jump() != "" {
		t.Errorf("installing the skill jumped to %q", m.Jump())
	}
	if !strings.Contains(m.notice, "installed the reporting skill") {
		t.Errorf("notice = %q", m.notice)
	}
	if out := plain(m.View()); strings.Contains(out, "not installed") {
		t.Errorf("the offer outlived the install:\n%s", out)
	}
}

// A letter that silently reinstalls a skill already in place is a keystroke
// nobody can predict.
func TestSDoesNothingWithNoOfferOnScreen(t *testing.T) {
	ran := false
	m := withSnapshot(t, withOrch(), 100)
	m.SetSkillPrompt(false, func() (string, error) { ran = true; return okInstall() })
	key(m, "S")
	if ran {
		t.Error("S reinstalled a skill that was already there")
	}
}

// The cursor never lands on it: a selection enter cannot act on is a dead end.
func TestTheOfferIsNotAStopOnTheWalk(t *testing.T) {
	m := withOffer(t, okInstall)
	for i := 0; i < m.TargetCount()+2; i++ {
		m.Update(tea.KeyMsg{Type: tea.KeyDown})
		if m.IsBannerTarget(m.Cursor()) {
			t.Fatalf("the walk landed on the offer at step %d", i)
		}
	}
}

// Clicking it installs, and must not drag the selection off whatever the
// keyboard was on, nor jump to the orchestrator the rest of the strip points at.
func TestClickingTheOfferInstalls(t *testing.T) {
	m := withOffer(t, okInstall)
	_ = m.View()
	m.cursor = 1
	// The index shifts when the offer leaves the target list, so what has to
	// hold is the thing selected, not the number.
	was := m.selectedPane()

	for _, h := range m.Hits() {
		if !m.IsBannerTarget(h.Target) {
			continue
		}
		m.Update(tea.MouseMsg{X: h.X0 + 1, Y: h.Y,
			Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
		if !strings.Contains(m.notice, "installed") {
			t.Errorf("clicking the offer did nothing, notice = %q", m.notice)
		}
		if m.Jump() != "" {
			t.Errorf("clicking the offer jumped to %q", m.Jump())
		}
		if m.selectedPane() != was {
			t.Errorf("clicking the offer moved the selection from %q to %q",
				was, m.selectedPane())
		}
		return
	}
	t.Fatal("the offer drew no clickable region")
}

// A failed install leaves the offer up. Taking it away would say the skill is
// there when it is not.
func TestAFailedInstallKeepsTheOffer(t *testing.T) {
	m := withOffer(t, func() (string, error) { return "", errors.New("read-only home") })
	key(m, "S")

	if !strings.Contains(m.notice, "read-only home") {
		t.Errorf("notice = %q, want the reason it failed", m.notice)
	}
	if out := plain(m.View()); !strings.Contains(out, "not installed") {
		t.Errorf("a failed install took the offer down:\n%s", out)
	}
}
