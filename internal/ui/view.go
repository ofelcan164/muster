package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/ofelcan/muster/internal/model"
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

	if m.filtering || m.filter != "" {
		lines = append(lines, "", m.viewFilterBar())
	}
	return strings.Join(lines, "\n")
}

func (m *Model) header() string {
	c := m.snap.Counts
	left := styTitle.Render("MUSTER") + "  " +
		styMeta.Render(fmt.Sprintf("%s · %s", plural(c.Repos, "repo"), plural(c.Agents, "agent")))
	if c.NeedsYou > 0 {
		left += "  " + lipgloss.NewStyle().Foreground(colRed).Render(
			fmt.Sprintf("%d need you", c.NeedsYou))
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

	out := []string{m.sectionRule("needs you")}
	for i, a := range rows {
		selected := m.cursor < len(m.targets) &&
			m.targets[m.cursor].ribbon && m.targets[m.cursor].paneID == a.PaneID

		repo, _, _ := m.agentByPane(a.PaneID)
		st := statusStyle(a.Status)

		idx := styFaint.Render(fmt.Sprintf("%d", i+1))
		icon := st.Render(statusIcon(a.Status))
		status := st.Bold(true).Render(strings.ToUpper(string(a.Status)))
		age := styMeta.Render(ageText(a.Age, a.AgeKnown))

		var rowLines []string
		if m.width < twoColumnMin {
			// Narrow. The detail is the most valuable thing in the row and the
			// first casualty of fixed columns, so it gets its own line.
			head := fmt.Sprintf(" %s %s %s %s %s",
				idx, icon,
				repoStyle(repo).Render(truncate(repo.Sigil+" "+repo.Display, 14)),
				styFG.Render(truncate(a.Agent, 14)), status)
			rowLines = append(rowLines, fitLine(head, m.width))
			if a.Detail != "" {
				rowLines = append(rowLines, fitLine(
					"    "+styDim.Render(truncate(a.Detail, m.width-5)), m.width))
			}
		} else {
			sigil := repoStyle(repo).Render(repo.Sigil + " " + repo.Display)
			name := styFG.Render(truncate(a.Agent, 16))
			head := fmt.Sprintf(" %s %s  %s %s  %s %s  ",
				idx, pad(sigil, 14), icon, pad(name, 16), pad(status, 8), pad(age, 4))
			head += styDim.Render(truncate(a.Detail, max(10, m.width-lipgloss.Width(head)-1)))
			rowLines = append(rowLines, fitLine(head, m.width))
		}

		if ti := m.targetIndex("pane:" + a.PaneID); ti >= 0 {
			for k := range rowLines {
				m.noteRegion(startY+len(out)+k, 0, m.width-1, ti)
			}
		}
		for _, line := range rowLines {
			switch {
			case selected:
				line = stySel.Render(line)
			case a.Rank <= 2:
				// The top two ranks keep a warm background even unselected, so
				// the thing that most needs you reads before you do.
				line = styHot.Render(line)
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

// cardLines renders one repo card. Row positions are only recorded for the
// first column, because a click resolves by line and multi-column rows would
// otherwise overwrite each other. Keyboard reaches every column regardless.
func (m *Model) cardLines(r model.Repo, width, startY, x0 int) []string {
	style := repoStyle(r)
	var out []string

	head := style.Bold(true).Render(r.Sigil + " " + strings.ToUpper(r.Display))
	if r.Branch != "" {
		head += " " + styFaint.Render(truncate(r.Branch, max(6, width-lipgloss.Width(head)-3)))
	}
	// The card header belongs to the repo target when the repo has no agents,
	// and otherwise to its first agent, so clicking the title does something
	// sensible either way.
	headTarget := m.targetIndex("repo:" + r.Key)
	if len(r.Agents) > 0 {
		headTarget = m.targetIndex("pane:" + r.Agents[0].PaneID)
	}
	headLine := fitLine(" "+head, width)
	if m.isActive(headTarget) {
		headLine = stySel.Render(fitLine(" "+head, width))
	}
	m.claim(startY+len(out), x0, width, headTarget)
	out = append(out, headLine)

	if len(r.Agents) == 0 {
		m.claim(startY+len(out), x0, width, headTarget)
		out = append(out, fitLine(styFaint.Render("   no agents"), width))
	}
	for _, a := range r.Agents {
		ti := m.targetIndex("pane:" + a.PaneID)
		lines := m.agentLines(a, width, ti)
		for k := range lines {
			m.claim(startY+len(out)+k, x0, width, ti)
		}
		out = append(out, lines...)
	}

	if len(r.OtherPanes) > 0 {
		labels := make([]string, 0, len(r.OtherPanes))
		for _, p := range r.OtherPanes {
			labels = append(labels, p.Label)
		}
		foot := fmt.Sprintf("   %s · %s", plural(len(r.OtherPanes), "pane"),
			truncate(strings.Join(labels, " · "), max(6, width-18)))
		m.claim(startY+len(out), x0, width, headTarget)
		out = append(out, fitLine(styFaint.Render(foot), width))
	}
	m.claim(startY+len(out), x0, width, headTarget)
	return append(out, strings.Repeat(" ", width))
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

func (m *Model) agentLines(a model.Agent, width, target int) []string {
	selected := m.isActive(target)
	st := statusStyle(a.Status)

	marker := " "
	if a.IsOrchestrator {
		marker = "⌂"
	}
	line := fmt.Sprintf("  %s%s %s %s",
		marker, st.Render(statusIcon(a.Status)),
		styFG.Render(truncate(a.Name, 14)),
		styMeta.Render(ageText(a.Age(time.Now()), a.AgeKnown)))

	first := fitLine(line, width)
	if selected {
		first = stySel.Render(fitLine(line, width))
	}
	out := []string{first}

	// Task line, dimmed and marked when it came from a lower rung of the
	// ladder. Showing doubt beats showing false confidence.
	if task := taskText(a); task != "" {
		style := styDim
		if a.TaskSource == model.TaskFromOrchestratorStale {
			style = styFaint
		}
		out = append(out, fitLine(style.Render("     "+truncate(task, max(6, width-6))), width))
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

func (m *Model) sectionRule(label string) string {
	text := " " + strings.ToUpper(label) + " "
	rule := m.width - lipgloss.Width(text) - 1
	if rule < 0 {
		rule = 0
	}
	return stySection.Render(text) + styFaint.Render(strings.Repeat("─", rule)) + "\n"
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

// truncate cuts to a display width, counting characters rather than bytes so
// sigils and box drawing do not corrupt the layout.
func truncate(s string, width int) string {
	s = strings.TrimSpace(s)
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	runes := []rune(s)
	for len(runes) > 0 && lipgloss.Width(string(runes))+1 > width {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "…"
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
