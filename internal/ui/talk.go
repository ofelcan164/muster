// Talking back to the orchestrator: the i input, and the t report key that the
// whole landed-detection story exists to serve.

package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ofelcan164/muster/internal/model"
)

// SetPrompter supplies the function that sends a message to an agent. Injected
// the same way the reloader is, so nothing here touches the socket in a test.
func (m *Model) SetPrompter(f func(paneID, text string) error) { m.prompt = f }

// SetMarker supplies the function that marks an agent as the orchestrator.
func (m *Model) SetMarker(f func(paneID string) error) { m.mark = f }

// reportTarget is the landed row the t key would report, or nil.
//
// The selection wins when it is sitting on a landed row, so t always acts on what
// you are looking at. Otherwise it falls back to the only landed row there is,
// which is the common case: t is meant to be pressed the moment you see the
// row, without navigating to it first. With more than one and none selected it
// stays silent, because picking for you would send the wrong report.
func (m *Model) reportTarget() *model.Attention {
	var rows []model.Attention
	for _, a := range m.snap.Attention {
		if a.Reason == model.ReasonLanded {
			rows = append(rows, a)
		}
	}
	if len(rows) == 0 {
		return nil
	}
	if sel := m.selectedPane(); sel != "" {
		for i := range rows {
			if rows[i].PaneID == sel {
				return &rows[i]
			}
		}
	}
	if len(rows) == 1 {
		return &rows[0]
	}
	return nil
}

// reportMessage is what the t key sends.
//
// Facts only, in the order the orchestrator needs them: what landed, how long
// ago, and who still depends on it. No instruction about what to do next,
// because the orchestrator knows the plan and Muster does not.
func reportMessage(a model.Attention, up model.Repo, landed time.Duration) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s landed", shortRepo(up))
	if landed > 0 {
		fmt.Fprintf(&b, " %s ago", ageText(landed, true))
	}
	verb := "depend"
	if len(a.Dependents) == 1 {
		verb = "depends"
	}
	fmt.Fprintf(&b, ", and %s still %s on it. Reported by Muster.", strings.Join(a.Dependents, ", "), verb)
	return b.String()
}

// report sends the orchestrator what it missed.
func (m *Model) report() (tea.Model, tea.Cmd) {
	a := m.reportTarget()
	if a == nil {
		m.notice = "nothing to report: no landed row is selected"
		return m, nil
	}
	if !m.snap.Orch.Found {
		m.notice = "no orchestrator marked, so there is nobody to tell"
		return m, nil
	}
	text, up := landedReport(m.snap, *a, time.Now())
	return m, m.send(m.snap.Orch.PaneID, text,
		fmt.Sprintf("told the orchestrator %s landed", shortRepo(up)))
}

// landedReport is what t sends for a landed row, and the repo that landed.
// The row lands on a dependent agent, which is what knows the dependency.
func landedReport(snap *model.Snapshot, a model.Attention, now time.Time) (string, model.Repo) {
	_, dep, _ := agentInSnap(snap, a.PaneID)
	up := repoInSnap(snap, dep.DependsOnRepo)
	var landed time.Duration
	if !dep.LandedAt.IsZero() {
		landed = now.Sub(dep.LandedAt)
	}
	return reportMessage(a, up, landed), up
}

// noticeMsg is what a socket call running off the Update goroutine reports
// back, to be shown as the notice.
type noticeMsg string

// send delivers a message and records what happened, since a full-screen
// overlay has nowhere else to report an error to.
//
// The socket call runs as a command rather than here. Update is the goroutine
// that reads the keyboard, so a wedged socket held there froze the overlay for
// seconds with every key ignored, q included.
func (m *Model) send(paneID, text, ok string) tea.Cmd {
	if m.prompt == nil {
		m.notice = "cannot send from here"
		return nil
	}
	m.notice = "sending…"
	prompt := m.prompt
	return func() tea.Msg {
		if err := prompt(paneID, text); err != nil {
			return noticeMsg("send failed: " + err.Error())
		}
		return noticeMsg(ok)
	}
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
		return m, m.send(m.snap.Orch.PaneID, text, "sent to the orchestrator")
	case tea.KeyBackspace:
		m.compose = dropLastRune(m.compose)
		return m, nil
	case tea.KeyCtrlC:
		m.quit = true
		return m, tea.Quit
	case tea.KeyRunes, tea.KeySpace:
		// Same as the filter: an alt key, herdr's prefix among them, is not
		// text, and KeySpace already carries its space in Runes.
		if !msg.Alt {
			m.compose += string(msg.Runes)
		}
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
