// The orchestrator strip: who is coordinating, what it last said, and the two
// keys that talk back to it.

package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/ofelcan/muster/internal/model"
)

// stripLines renders the orchestrator strip, the last block on the screen.
//
// Three lines when an orchestrator is marked: who it is, what it last said, and
// the keys. One line when none is, saying how to mark one. Never nothing:
// silent empty space here reads as a bug, and guessing which agent is in charge
// from who talks the most is worse than asking.
func (m *Model) stripLines(startY int) []string {
	if m.filter != "" {
		return nil
	}
	o := m.snap.Orch
	if !o.Found {
		return []string{
			m.sectionRule("orchestrator"),
			styFaint.Render(fitLine(
				"  none marked · run the mark-orchestrator action on its pane", m.width)),
		}
	}

	ti := m.stripTargetIndex()
	out := []string{m.sectionRule("orchestrator")}

	name := o.Name
	if name == "" {
		name = "orchestrator"
	}
	st := statusStyle(o.Status)
	head := fmt.Sprintf(" %s %s %s %s",
		styTitle.Render("⌂"),
		styFG.Bold(true).Render(strings.ToUpper(name)),
		st.Render(statusIcon(o.Status)+" "+string(o.Status)),
		styMeta.Render(ageText(orchAge(o), !o.StatusSince.IsZero())))

	// The jump hint is the first thing to go when the pane is narrow. Who the
	// orchestrator is and what state it is in matter more than a key you can
	// also read off the strip's last line.
	hint := styHint.Render("prefix+shift+m jumps ")
	if gap := m.width - lipgloss.Width(head) - lipgloss.Width(hint); gap > 0 {
		head += strings.Repeat(" ", gap) + hint
	}
	out = append(out, fitLine(head, m.width))

	// The last message is the whole reason to look here, so it gets the bright
	// foreground and a whole line to itself.
	msg := o.LastMessage
	if msg == "" {
		msg = "nothing said yet"
		out = append(out, styFaint.Render(fitLine("   "+msg, m.width)))
	} else {
		out = append(out, styFG.Render(fitLine(
			"   \""+truncate(msg, max(10, m.width-6))+"\"", m.width)))
	}

	out = append(out, fitLine(m.stripFooter(), m.width))

	if ti >= 0 {
		// The whole strip is one click region, the way a card is: clicking any
		// part of it jumps to the orchestrator.
		for k := 1; k < len(out); k++ {
			m.noteRegion(startY+k, 0, m.width-1, ti)
		}
		if m.isActive(ti) {
			for k := 1; k < len(out); k++ {
				out[k] = paint(out[k], stySel)
			}
		}
	}
	return out
}

// stripFooter is the last line of the strip: the input while it is open, then
// whatever just happened, then the keys.
func (m *Model) stripFooter() string {
	if m.composing {
		return m.composeLine()
	}
	if m.notice != "" {
		return " " + styWarn.Render("· "+m.notice)
	}
	return styFaint.Render(m.stripHint())
}

// stripHint is the last line of the strip, which changes with what the current
// selection makes possible.
func (m *Model) stripHint() string {
	if repair := m.repairTarget(); repair != nil {
		return fmt.Sprintf(" › i tells it something · t reports %s/%s finished",
			shortRepo(m.repoByKey(repair.RepoKey)), repair.Agent)
	}
	return " › press i to tell it something"
}

// orchAge is how long the orchestrator has held its status, or zero when the
// daemon never watched it get there.
func orchAge(o model.Orchestrator) time.Duration {
	if o.StatusSince.IsZero() {
		return 0
	}
	return time.Since(o.StatusSince)
}

func shortRepo(r model.Repo) string {
	if r.Display != "" {
		return r.Display
	}
	return r.Name
}
