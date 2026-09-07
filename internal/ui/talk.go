// Talking back to the orchestrator: the i input, and the t repair key that the
// whole gate-detection story exists to serve.

package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ofelcan/muster/internal/model"
)

// SetPrompter supplies the function that sends a message to an agent. Injected
// the same way the reloader is, so nothing here touches the socket in a test.
func (m *Model) SetPrompter(f func(paneID, text string) error) { m.prompt = f }

// repairTarget is the finished agent the t key would report, or nil.
//
// The selection wins when it is sitting on a gate row, so t always acts on what
// you are looking at. Otherwise it falls back to the only gate row there is,
// which is the common case: t is meant to be pressed the moment you see the
// row, without navigating to it first. With more than one and none selected it
// stays silent, because picking for you would send the wrong report.
func (m *Model) repairTarget() *model.Attention {
	var gates []model.Attention
	for _, a := range m.snap.Attention {
		if a.Reason == model.ReasonGateUntold {
			gates = append(gates, a)
		}
	}
	if len(gates) == 0 {
		return nil
	}
	if sel := m.selectedPane(); sel != "" {
		for i := range gates {
			if gates[i].PaneID == sel {
				return &gates[i]
			}
		}
	}
	if len(gates) == 1 {
		return &gates[0]
	}
	return nil
}

// repairMessage is what the t key sends.
//
// Facts only, in the order the orchestrator needs them: what finished, how long
// ago, and who is waiting. No instruction about what to do next, because the
// orchestrator knows the plan and Muster does not.
func repairMessage(a model.Attention, repo model.Repo) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s/%s has finished", shortRepo(repo), a.Agent)
	if a.AgeKnown {
		fmt.Fprintf(&b, " (%s ago)", ageText(a.Age, true))
	}
	b.WriteString(" and has not been picked up.")
	if len(a.Downstream) > 0 {
		fmt.Fprintf(&b, " Waiting on it: %s.", strings.Join(a.Downstream, ", "))
	}
	b.WriteString(" Reported by Muster.")
	return b.String()
}

// repair sends the orchestrator what it missed.
func (m *Model) repair() (tea.Model, tea.Cmd) {
	a := m.repairTarget()
	if a == nil {
		m.notice = "nothing to report: no open gate is selected"
		return m, nil
	}
	if !m.snap.Orch.Found {
		m.notice = "no orchestrator marked, so there is nobody to tell"
		return m, nil
	}
	m.send(m.snap.Orch.PaneID, repairMessage(*a, m.repoByKey(a.RepoKey)),
		fmt.Sprintf("told the orchestrator about %s/%s",
			shortRepo(m.repoByKey(a.RepoKey)), a.Agent))
	return m, nil
}

// send delivers a message and records what happened, since a full-screen
// overlay has nowhere else to report an error to.
func (m *Model) send(paneID, text, ok string) {
	if m.prompt == nil {
		m.notice = "cannot send from here"
		return
	}
	if err := m.prompt(paneID, text); err != nil {
		m.notice = "send failed: " + err.Error()
		return
	}
	m.notice = ok
}

// handleCompose owns every key while the i input is open, the same way the
// filter does: whatever you type is text, not a command.
func (m *Model) handleCompose(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.composing, m.compose = false, ""
		return m, nil
	case tea.KeyEnter:
		text := strings.TrimSpace(m.compose)
		m.composing, m.compose = false, ""
		if text == "" {
			return m, nil
		}
		if !m.snap.Orch.Found {
			m.notice = "no orchestrator marked, so there is nobody to tell"
			return m, nil
		}
		m.send(m.snap.Orch.PaneID, text, "sent to the orchestrator")
		return m, nil
	case tea.KeyBackspace:
		if m.compose != "" {
			m.compose = m.compose[:len(m.compose)-1]
		}
		return m, nil
	case tea.KeyCtrlC:
		m.quit = true
		return m, tea.Quit
	case tea.KeyRunes:
		m.compose += string(msg.Runes)
		return m, nil
	case tea.KeySpace:
		m.compose += " "
		return m, nil
	}
	return m, nil
}

// composeLine is the input as it is being typed.
func (m *Model) composeLine() string {
	return " " + styTitle.Render("›") + " " +
		styFG.Render(m.compose) + styMatch.Render("▏") +
		styFaint.Render("  enter sends · esc cancels")
}
