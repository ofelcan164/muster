// The orchestrator strip: who is coordinating, what it last said, and the two
// keys that talk back to it.

package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/ofelcan164/muster/internal/model"
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
		out := []string{
			m.sectionRule("orchestrator"),
			styFaint.Render(fitLine(
				"  none marked · press o on the agent in charge", m.width)),
		}
		if offer, ok := m.skillOffer(startY + len(out)); ok {
			out = append(out, offer)
		}
		// Without this the strip has nowhere to report from when no
		// orchestrator is marked, and every notice raised in that state is
		// written to a line that is never drawn.
		if m.notice != "" {
			out = append(out, fitLine(" "+styWarn.Render("· "+m.notice), m.width))
		}
		return out
	}

	ti := m.stripTargetIndex()
	out := []string{m.sectionRule("orchestrator")}

	st := statusStyle(o.Status, m.frame)
	head := fmt.Sprintf(" %s %s %s %s",
		styTitle.Render("⌂"),
		m.orchWho(o),
		st.Render(statusIcon(o.Status, m.frame)+" "+string(o.Status)),
		styMeta.Render(ageText(orchAge(o), !o.StatusSince.IsZero())))

	// The jump hint is the first thing to go when the pane is narrow. Who the
	// orchestrator is and what state it is in matter more than a key you can
	// also read off the strip's last line.
	hint := styHint.Render("M jumps ")
	if gap := m.width - lipgloss.Width(head) - lipgloss.Width(hint); gap > 0 {
		head += strings.Repeat(" ", gap) + hint
	}
	out = append(out, fitLine(head, m.width))

	out = append(out, m.saidLine(o))

	// The offer is its own click target, so it is drawn after the lines that
	// belong to the orchestrator and skipped by the region loop below. A click
	// on it must install the skill rather than jump.
	offerAt := -1
	if offer, ok := m.skillOffer(startY + len(out)); ok {
		offerAt = len(out)
		out = append(out, offer)
	}

	out = append(out, fitLine(m.stripFooter(), m.width))

	if ti >= 0 {
		// The rest of the strip is one click region, the way a card is:
		// clicking any part of it jumps to the orchestrator.
		for k := 1; k < len(out); k++ {
			if k == offerAt {
				continue
			}
			m.noteRegion(startY+k, 0, m.width-1, ti)
		}
		if m.isActive(ti) {
			for k := 1; k < len(out); k++ {
				if k == offerAt {
					continue
				}
				out[k] = paint(out[k], stySel)
			}
		}
	}
	return out
}

// skillOffer is the line offering to install the reporting skill, and the
// region that makes it clickable.
//
// It lives on this strip because the skill is the orchestrator's to run, and
// because it is the sibling of "none marked": both are the coordination layer
// telling you it is not set up yet. The consequence is elsewhere, on every card
// whose task line is really just a terminal title, but the fix belongs here.
func (m *Model) skillOffer(y int) (string, bool) {
	if !m.showBanner() {
		return "", false
	}
	line := fitLine(" "+styWarn.Render("⚑")+" "+
		styFG.Render("agents have no task lines")+" "+
		styMeta.Render("· the reporting skill is not installed")+" "+
		styHint.Render("· S installs it"), m.width)

	if ti := m.bannerTargetIndex(); ti >= 0 {
		m.noteRegion(y, 0, m.width-1, ti)
		if m.isActive(ti) {
			line = paint(line, stySel)
		}
	}
	return line, true
}

// orchWho names the orchestrator by the repo it is working in, drawn the way
// that repo's card head is drawn, so the strip and the tile read as the same
// thing and the strip finally says where the orchestrator lives.
//
// Its own name is the fallback rather than the first choice. Marking renames
// the agent to "orchestrator", so the name repeated the section rule directly
// above it and spent a line saying nothing. A pane in no repo Muster knows
// about is the one case where the name is the only fact there is.
func (m *Model) orchWho(o model.Orchestrator) string {
	if r, _, ok := m.agentByPane(o.PaneID); ok {
		return repoStyle(r).Bold(true).
			Render(r.Sigil + " " + strings.ToUpper(shortRepo(r)))
	}
	name := o.Name
	if name == "" {
		name = "orchestrator"
	}
	return styFG.Bold(true).Render(strings.ToUpper(name))
}

// saidLine is the strip's one message line: what the orchestrator last said,
// and how long ago that was.
//
// Only the reply. What it was told is the half you already know, because you
// are the one who typed it, and it is still on the orchestrator's own card and
// in musterd dump for the times you want it. The age is the point of the line
// as much as the text is: a sentence with no clock on it cannot be told from
// one that has been sitting there since this morning.
func (m *Model) saidLine(o model.Orchestrator) string {
	if o.LastSaid == "" {
		return styFaint.Render(fitLine("   ↓ nothing said yet", m.width))
	}

	age := ""
	if !o.SaidAt.IsZero() {
		age = styMeta.Render(ageText(time.Since(o.SaidAt), true) + " ")
	}
	// The arrow and the age both hold their columns, so the sentence gets
	// whatever is left rather than pushing the clock off the end of the line.
	head := styMeta.Render("   ↓ ")
	room := m.width - lipgloss.Width(head) - lipgloss.Width(age) - 1
	line := head + styFG.Render(truncate(o.LastSaid, max(10, room)))
	if gap := m.width - lipgloss.Width(line) - lipgloss.Width(age); gap > 0 {
		line += strings.Repeat(" ", gap) + age
	}
	return fitLine(line, m.width)
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
