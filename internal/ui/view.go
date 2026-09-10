package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/ofelcan164/muster/internal/model"
)

func (m *Model) View() string {
	if m.quit || m.jump != "" {
		// Leave nothing behind on the way out. The jump happens after the
		// program exits, so a final frame would only flash.
		return ""
	}
	m.resetRows()

	var lines []string
	lines = append(lines, strings.TrimRight(m.header(), "\n"), "")
	lines = append(lines, m.ribbonLines(len(lines))...)
	lines = append(lines, m.repoLines(len(lines))...)
	if strip := m.stripLines(len(lines) + 1); len(strip) > 0 {
		lines = append(lines, "")
		lines = append(lines, strip...)
	}

	if m.filtering || m.filter != "" {
		lines = append(lines, "", m.viewFilterBar())
	}
	return strings.Join(lines, "\n")
}

func (m *Model) header() string {
	c := m.snap.Counts
	left := styTitle.Render("MUSTER") + "  " +
		styMeta.Render(fmt.Sprintf("%s · %s", plural(c.Repos, "repo"), plural(c.Agents, "agent")))
	// The ribbon rather than the daemon's count, which does not know what you
	// have dismissed. They are the same number until you dismiss something.
	if n := len(m.ribbonRows()); n > 0 {
		left += "  " + lipgloss.NewStyle().Foreground(colRed).Render(
			fmt.Sprintf("%d need you", n))
	}

	right := styHint.Render("/ search · s sort:" + m.sort.String() + " · prefix+m closes")
	if m.warning != "" {
		right = styWarn.Render("! " + m.warning)
	}

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return left + "\n"
	}
	return left + strings.Repeat(" ", gap) + right + "\n"
}

// ribbonLines renders the ranked ribbon, recording which screen line each row
// lands on so a click can find it. startY is the line this block begins at.
func (m *Model) ribbonLines(startY int) []string {
	rows := m.ribbonRows()
	if len(rows) == 0 || m.filter != "" {
		return nil
	}

	out := []string{m.attentionRule(len(rows))}
	for i, a := range rows {
		ti := m.ribbonTargetIndex("pane:" + a.PaneID)
		selected := m.isActive(ti)

		repo := m.repoByKey(a.RepoKey)
		accent := reasonAccent(a.Reason)

		bar := accentBar(accent)
		idx := lipgloss.NewStyle().Foreground(accent).Bold(true).Render(fmt.Sprintf("%d", i+1))
		label := badge(reasonLabel(a.Reason, a.Status), accent)
		who := repoStyle(repo).Bold(true).Render(repo.Sigil+" "+repo.Display) +
			styFaint.Render("/") + styFG.Bold(true).Render(a.Agent)
		age := styMeta.Render(ageText(a.Age, a.AgeKnown))

		var rowLines []string
		if m.width < twoColumnMin {
			// Narrow. The detail is the most valuable thing in the row and the
			// first casualty of fixed columns, so it gets its own line.
			head := fmt.Sprintf("%s %s %s %s", bar, idx, label, truncate(who, m.width-24))
			rowLines = append(rowLines, fitLine(head, m.width))
			if a.Detail != "" {
				rowLines = append(rowLines, fitLine(
					bar+"     "+styFG.Render(truncate(a.Detail, m.width-7)), m.width))
			}
		} else {
			head := fmt.Sprintf("%s %s %s %s %s  ",
				bar, idx, pad(label, 11), pad(who, 34), pad(age, 4))
			// The detail is the sentence you actually read, so it gets the
			// bright foreground rather than the dim one the grid uses.
			head += styFG.Render(truncate(a.Detail, max(10, m.width-lipgloss.Width(head)-1)))
			rowLines = append(rowLines, fitLine(head, m.width))
		}

		if ti >= 0 {
			for k := range rowLines {
				m.noteRegion(startY+len(out)+k, 0, m.width-1, ti)
			}
		}
		for _, line := range rowLines {
			switch {
			case selected:
				line = paint(line, stySel)
			case a.Rank <= 2:
				// The top two ranks keep a warm background even unselected, so
				// the thing that most needs you reads before you do.
				line = paint(line, styHot)
			default:
				// The rest still get a panel so the ribbon reads as one block
				// rather than trailing off into the background.
				line = paint(line, styPanel)
			}
			out = append(out, line)
		}
	}
	return append(out, "")
}

func (m *Model) repoLines(startY int) []string {
	repos := m.orderedRepos(m.visibleRepos())
	if len(repos) == 0 {
		if m.filter != "" {
			return []string{m.sectionRule("no matches"),
				styDim.Render("  nothing matches " + m.filter)}
		}
		return []string{m.sectionRule("repos"), styDim.Render("  no repos discovered yet")}
	}

	label := "repos"
	if m.filter != "" {
		label = "matches"
	}
	out := []string{m.sectionRule(label)}

	cols := columnsFor(m.width)
	if m.filter != "" {
		cols = 1
	}
	cellWidth := m.width
	if cols > 1 {
		cellWidth = (m.width - (cols - 1)) / cols
		if cellWidth < 20 {
			cols, cellWidth = 1, m.width
		}
	}

	for i := 0; i < len(repos); i += cols {
		var cells [][]string
		height := 0
		for c := 0; c < cols && i+c < len(repos); c++ {
			// Each column starts after the cells before it, plus one space of
			// separator per gap. Passing the offset is what makes every column
			// clickable rather than only the first.
			x0 := c * (cellWidth + 1)
			lines := m.cardLines(repos[i+c], cellWidth, startY+len(out), x0)
			cells = append(cells, lines)
			if len(lines) > height {
				height = len(lines)
			}
		}
		// Pad every cell to the tallest, so columns stay aligned.
		for c := range cells {
			for len(cells[c]) < height {
				cells[c] = append(cells[c], strings.Repeat(" ", cellWidth))
			}
		}
		for row := 0; row < height; row++ {
			var parts []string
			for c := 0; c < cols; c++ {
				if c < len(cells) {
					parts = append(parts, cells[c][row])
				} else {
					parts = append(parts, strings.Repeat(" ", cellWidth))
				}
			}
			out = append(out, fitLine(strings.Join(parts, " "), m.width))
		}
	}
	return out
}

// cardLines renders one repo card.
//
// Every line is built alongside the target it belongs to, then styled from that
// target's state. Doing it in one pass is what keeps the highlight and the
// click region identical: they are derived from the same mapping, so a card
// cannot end up clickable in places it does not light up.
func (m *Model) cardLines(r model.Repo, width, startY, x0 int) []string {
	style := repoStyle(r)

	type row struct {
		text   string
		target int
	}
	var rows []row

	head := style.Bold(true).Render(r.Sigil + " " + strings.ToUpper(r.Display))
	if r.Branch != "" {
		head += " " + styFaint.Render(truncate(r.Branch, max(6, width-lipgloss.Width(head)-3)))
	}
	// The header belongs to the repo target when nothing is running here, and
	// otherwise to the first agent, so clicking the title does something
	// sensible either way.
	headTarget := m.targetIndex("repo:" + r.Key)
	if len(r.Agents) > 0 {
		headTarget = m.targetIndex("pane:" + r.Agents[0].PaneID)
	}
	rows = append(rows, row{" " + head, headTarget})

	if len(r.Agents) == 0 {
		rows = append(rows, row{styFaint.Render("   no agents"), headTarget})
	}
	for _, a := range r.Agents {
		ti := m.targetIndex("pane:" + a.PaneID)
		for _, line := range m.agentLines(a, width) {
			rows = append(rows, row{line, ti})
		}
	}

	if len(r.OtherPanes) > 0 {
		labels := make([]string, 0, len(r.OtherPanes))
		for _, p := range r.OtherPanes {
			labels = append(labels, p.Label)
		}
		foot := fmt.Sprintf("   %s · %s", plural(len(r.OtherPanes), "pane"),
			truncate(strings.Join(labels, " · "), max(6, width-18)))
		rows = append(rows, row{styFaint.Render(foot), headTarget})
	}
	// A blank tail line, still part of the card so the hover block reads as one
	// shape rather than stopping mid-card.
	rows = append(rows, row{"", headTarget})

	out := make([]string, 0, len(rows))
	for i, rw := range rows {
		m.claim(startY+i, x0, width, rw.target)
		line := fitLine(rw.text, width)
		if m.isActive(rw.target) {
			line = paint(line, stySel)
		}
		out = append(out, line)
	}
	return out
}

// claim marks a whole cell-width line as belonging to a target.
func (m *Model) claim(y, x0, width, target int) {
	if target >= 0 {
		m.noteRegion(y, x0, x0+width-1, target)
	}
}

// isActive reports whether a target is selected or hovered, which render the
// same way: the pointer should light up exactly what a click would take.
func (m *Model) isActive(target int) bool {
	if target < 0 {
		return false
	}
	return target == m.cursor || target == m.hover
}

// agentLines renders one agent. Selection styling is applied by the caller, so
// that a whole card highlights as a block rather than a single row.
func (m *Model) agentLines(a model.Agent, width int) []string {
	st := statusStyle(a.Status, m.frame)

	marker := " "
	if a.IsOrchestrator {
		marker = "⌂"
	}
	line := fmt.Sprintf("  %s%s %s %s",
		marker, st.Render(statusIcon(a.Status, m.frame)),
		styFG.Render(truncate(a.Name, 14)),
		styMeta.Render(ageText(a.Age(time.Now()), a.AgeKnown)))

	out := []string{line}

	// Task line, dimmed and marked when it came from a lower rung of the
	// ladder. Showing doubt beats showing false confidence.
	if task := taskText(a); task != "" {
		style := styDim
		if a.TaskSource == model.TaskFromOrchestratorStale {
			style = styFaint
		}
		out = append(out, style.Render("     "+truncate(task, max(6, width-6))))
	}
	return out
}

func (m *Model) viewFilterBar() string {
	repos := m.visibleRepos()
	agents := 0
	for _, r := range repos {
		agents += len(r.Agents)
	}
	cursor := ""
	if m.filtering {
		cursor = "▏"
	}
	hint := "  esc clears"
	if m.filtering {
		hint = "  esc keeps results · enter jumps"
	}
	// Count repos as well as agents. A search that matches a repo with nothing
	// running in it is a hit, and reporting "0 of 2" for it reads as a miss.
	bar := " " + styMeta.Render("search ") + styMatch.Render(m.filter) + cursor +
		styFaint.Render(fmt.Sprintf("  %s · %s%s",
			plural(len(repos), "repo"), plural(agents, "agent"), hint))
	return padLine(bar, m.width)
}

// attentionRule heads the ribbon. It carries a count and the colour of the most
// urgent row, so the section itself signals how bad things are before you read
// a single line of it.
func (m *Model) attentionRule(n int) string {
	accent := colDim
	if len(m.snap.Attention) > 0 {
		accent = reasonAccent(m.snap.Attention[0].Reason)
	}
	head := lipgloss.NewStyle().Background(accent).Foreground(colBG).Bold(true).
		Render(fmt.Sprintf(" NEEDS YOU %d ", n))
	rule := m.width - lipgloss.Width(head) - 1
	if rule < 0 {
		rule = 0
	}
	return head + lipgloss.NewStyle().Foreground(accent).Render(strings.Repeat("─", rule))
}

// sectionRule is exactly one line. It must not append a newline of its own:
// rendering is line-based now, and a rule that secretly counted as two lines
// shifted every click region below it by one per section.
func (m *Model) sectionRule(label string) string {
	text := " " + strings.ToUpper(label) + " "
	rule := m.width - lipgloss.Width(text) - 1
	if rule < 0 {
		rule = 0
	}
	return stySection.Render(text) + styFaint.Render(strings.Repeat("─", rule))
}

// taskText prefers the blocking question, which says what the agent needs
// rather than what it was doing.
func taskText(a model.Agent) string {
	if a.Question != "" {
		return a.Question
	}
	switch a.TaskSource {
	case model.TaskFromOrchestratorStale:
		return a.Task + " (stale)"
	case model.TaskFromNone:
		return ""
	default:
		return a.Task
	}
}

// ageText renders a duration, or a dash when the daemon never watched this
// status begin and so has no honest number.
func ageText(d time.Duration, known bool) string {
	if !known {
		return "-"
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// truncate cuts to a display width, counting cells rather than bytes so sigils
// and box drawing do not corrupt the layout. ansi.Truncate is what lipgloss
// measures with, and unlike the loop it replaced it does not rescan the string
// per rune dropped.
func truncate(s string, width int) string {
	s = strings.TrimSpace(s)
	if width <= 0 {
		return ""
	}
	return ansi.Truncate(s, width, "…")
}

// pad right-pads to a display width without truncating styled content.
func pad(s string, width int) string {
	if gap := width - lipgloss.Width(s); gap > 0 {
		return s + strings.Repeat(" ", gap)
	}
	return s
}

// padLine pads to exactly width, and trims anything longer. Nothing may
// overflow: an overlay is sized to the tab area, and a line that runs past it
// wraps and destroys the layout.
func padLine(s string, width int) string { return fitLine(s, width) }

// fitLine pads or trims a line to exactly the given width, so grid cells always
// join cleanly no matter what is in them.
func fitLine(s string, width int) string {
	if lipgloss.Width(s) > width {
		return truncate(s, width)
	}
	return pad(s, width)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
