package ui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// withBanner is an overlay that has noticed the reporting skill is missing.
func withBanner(t *testing.T, install func() (string, error)) *Model {
	t.Helper()
	m := withSnapshot(t, withOrch(), 100)
	m.SetSkillPrompt(true, install)
	return m
}

func ok() (string, error) { return "/home/u/.claude/skills/muster-report/SKILL.md", nil }

// Without the skill nothing writes a task token, so every card falls back to a
// terminal title. The overlay has to say so, because the alternative is knowing
// to run a command nobody mentioned.
func TestBannerOffersTheSkillOnlyWhenItIsMissing(t *testing.T) {
	if out := plain(withBanner(t, ok).View()); !strings.Contains(out, "reporting skill is not installed") {
		t.Errorf("no offer to install the skill:\n%s", out)
	}

	m := withSnapshot(t, withOrch(), 100)
	m.SetSkillPrompt(false, ok)
	if out := plain(m.View()); strings.Contains(out, "reporting skill") {
		t.Errorf("offered to install a skill that is already there:\n%s", out)
	}

	// An overlay with no installer wired in must not offer one.
	none := withSnapshot(t, withOrch(), 100)
	none.SetSkillPrompt(true, nil)
	if out := plain(none.View()); strings.Contains(out, "reporting skill") {
		t.Errorf("offered an install it cannot run:\n%s", out)
	}
}

// It is the first thing on screen, so it is the first thing the keyboard
// reaches: the offer is worthless if you have to know it is selectable.
func TestBannerIsTheFirstTarget(t *testing.T) {
	m := withBanner(t, ok)
	if !m.IsBannerTarget(0) {
		t.Fatal("the banner is not the first target")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if !m.IsBannerTarget(m.Cursor()) {
		t.Errorf("the first move down landed on target %d, not the banner", m.Cursor())
	}
}

// The banner reports a condition, so fixing the condition is what dismisses it.
func TestEnterInstallsTheSkillAndTheBannerGoes(t *testing.T) {
	ran := false
	m := withBanner(t, func() (string, error) { ran = true; return ok() })
	m.cursor = 0
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !ran {
		t.Fatal("enter on the banner did not install anything")
	}
	if m.Jump() != "" {
		t.Errorf("installing the skill jumped to %q", m.Jump())
	}
	if !strings.Contains(m.notice, "installed the reporting skill") {
		t.Errorf("notice = %q", m.notice)
	}
	if out := plain(m.View()); strings.Contains(out, "not installed") {
		t.Errorf("the banner outlived the install:\n%s", out)
	}
}

// A click has to land on it the same way a click lands on a card.
func TestClickingTheBannerInstalls(t *testing.T) {
	m := withBanner(t, ok)
	_ = m.View()

	for _, h := range m.Hits() {
		if !m.IsBannerTarget(h.Target) {
			continue
		}
		m.Update(tea.MouseMsg{X: h.X0 + 1, Y: h.Y,
			Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
		if !strings.Contains(m.notice, "installed") {
			t.Errorf("clicking the banner did nothing, notice = %q", m.notice)
		}
		return
	}
	t.Fatal("the banner drew no clickable region")
}

// A failed install leaves the offer up. Taking it away would say the skill is
// there when it is not.
func TestAFailedInstallKeepsTheOffer(t *testing.T) {
	m := withBanner(t, func() (string, error) { return "", errors.New("read-only home") })
	m.cursor = 0
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !strings.Contains(m.notice, "read-only home") {
		t.Errorf("notice = %q, want the reason it failed", m.notice)
	}
	if out := plain(m.View()); !strings.Contains(out, "not installed") {
		t.Errorf("a failed install took the offer down:\n%s", out)
	}
}

// A query collapses the screen to its results, and a standing offer is not one.
func TestBannerStandsDownWhileFiltering(t *testing.T) {
	m := withBanner(t, ok)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})

	if out := plain(m.View()); strings.Contains(out, "reporting skill") {
		t.Errorf("the banner survived a search:\n%s", out)
	}
	if m.IsBannerTarget(0) {
		t.Error("the banner is still a target while filtering")
	}
}
