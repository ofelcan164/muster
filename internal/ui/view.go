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

	var b strings.Builder
	b.WriteString(m.header())
	b.WriteString("\n")

	if ribbon := m.viewRibbon(); ribbon != "" {
		b.WriteString(ribbon)
		b.WriteString("\n")
	}
	b.WriteString(m.viewRepos())

	if m.filtering || m.filter != "" {
		b.WriteString("\n")
		b.WriteString(m.viewFilterBar())
	}
	return b.String()
}

func (m *Model) header() string {
	c := m.snap.Counts
	left := styTitle.Render("MUSTER") + "  " +
		styMeta.Render(fmt.Sprintf("%s · %s", plural(c.Repos, "repo"), plural(c.Agents, "agent")))
	if c.NeedsYou > 0 {
		left += "  " + lipgloss.NewStyle().Foreground(colRed).Render(
			fmt.Sprintf("%d need you", c.NeedsYou))
	}

	right := styHint.Render("prefix+m closes")
	if m.warning != "" {
		right = styWarn.Render("! " + m.warning)
	}

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return left + "\n"
	}
	return left + strings.Repeat(" ", gap) + right + "\n"
}

func (m *Model) viewRibbon() string {
	rows := m.ribbonRows()
	if len(rows) == 0 || m.filter != "" {
		return ""
	}

	var b strings.Builder
	b.WriteString(m.sectionRule("needs you"))
	for i, a := range rows {
		selected := m.cursor < len(m.targets) &&
			m.targets[m.cursor].ribbon && m.targets[m.cursor].paneID == a.PaneID

		repo, _, _ := m.agentByPane(a.PaneID)
		st := statusStyle(a.Status)

		idx := styFaint.Render(fmt.Sprintf("%d", i+1))
		icon := st.Render(statusIcon(a.Status))
		status := st.Bold(true).Render(strings.ToUpper(string(a.Status)))
		age := styMeta.Render(ageText(a.Age, a.AgeKnown))

		var lines []string
		if m.width < twoColumnMin {
			// Narrow. The detail is the most valuable thing in the row, and it
			// is the first casualty of fixed columns, so it gets its own line.
			head := fmt.Sprintf(" %s %s %s %s %s",
				idx, icon,
				repoStyle(repo).Render(truncate(repo.Sigil+" "+repo.Display, 14)),
				styFG.Render(truncate(a.Agent, 14)), status)
			lines = append(lines, fitLine(head, m.width))
			if a.Detail != "" {
				lines = append(lines, fitLine(
					"    "+styDim.Render(truncate(a.Detail, m.width-5)), m.width))
			}
		} else {
			sigil := repoStyle(repo).Render(repo.Sigil + " " + repo.Display)
			name := styFG.Render(truncate(a.Agent, 16))
			head := fmt.Sprintf(" %s %s  %s %s  %s %s  ",
				idx, pad(sigil, 14), icon, pad(name, 16), pad(status, 8), pad(age, 4))
			head += styDim.Render(truncate(a.Detail, max(10, m.width-lipgloss.Width(head)-1)))
			lines = append(lines, fitLine(head, m.width))
		}

		for _, line := range lines {
			switch {
			case selected:
				line = stySel.Render(line)
			case a.Rank <= 2:
				// The top two ranks keep a warm background even unselected, so
				// the thing that most needs you is visible before you read it.
				line = styHot.Render(line)
			}
			b.WriteString(line + "\n")
		}
	}
	return b.String()
}

func (m *Model) viewRepos() string {
	repos := sortedRepos(m.visibleRepos())
	if len(repos) == 0 {
		if m.filter != "" {
			return m.sectionRule("no matches") + styDim.Render("  nothing matches "+m.filter) + "\n"
		}
		return m.sectionRule("repos") + styDim.Render("  no repos discovered yet") + "\n"
	}

	label := "repos"
	if m.filter != "" {
		label = "matches"
	}

	cols := columnsFor(m.width)
	if m.filter != "" {
		cols = 1
	}
	if cols == 1 {
		return m.sectionRule(label) + m.viewList(repos)
	}
	return m.sectionRule(label) + m.viewGrid(repos, cols)
}

// viewGrid lays repos out in fixed cells. A repo keeps its slot whether it has
// five agents or none, so you point instead of read.
func (m *Model) viewGrid(repos []model.Repo, cols int) string {
	cellWidth := (m.width - (cols - 1)) / cols
	if cellWidth < 20 {
		return m.viewList(repos)
	}

	var rows []string
	for i := 0; i < len(repos); i += cols {
		var cells []string
		for c := 0; c < cols; c++ {
			if i+c < len(repos) {
				cells = append(cells, m.renderCard(repos[i+c], cellWidth))
			} else {
				cells = append(cells, lipgloss.NewStyle().Width(cellWidth).Render(""))
			}
		}
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, cells...))
	}
	return strings.Join(rows, "\n") + "\n"
}

// viewList is the narrow layout. Below about seventy columns a grid is a worse
// version of a list, so it becomes one dense column.
func (m *Model) viewList(repos []model.Repo) string {
	var b strings.Builder
	for _, r := range repos {
		b.WriteString(m.renderCard(r, m.width))
	}
	return b.String()
}

func (m *Model) renderCard(r model.Repo, width int) string {
	style := repoStyle(r)
	var b strings.Builder

	// Header: sigil, name, branch. The branch slot is the one that becomes a
	// worktree label when worktrees start mattering.
	head := style.Bold(true).Render(r.Sigil + " " + strings.ToUpper(r.Display))
	if r.Branch != "" {
		head += " " + styFaint.Render(truncate(r.Branch, max(6, width-lipgloss.Width(head)-3)))
	}
	b.WriteString(fitLine(" "+head, width) + "\n")

	if len(r.Agents) == 0 {
		b.WriteString(fitLine(styFaint.Render("   no agents"), width) + "\n")
	}
	for _, a := range r.Agents {
		b.WriteString(m.renderAgent(a, width))
	}

	if len(r.OtherPanes) > 0 {
		labels := make([]string, 0, len(r.OtherPanes))
		for _, p := range r.OtherPanes {
			labels = append(labels, p.Label)
		}
		foot := fmt.Sprintf("   %s · %s", plural(len(r.OtherPanes), "pane"),
			truncate(strings.Join(labels, " · "), max(6, width-18)))
		b.WriteString(fitLine(styFaint.Render(foot), width) + "\n")
	}
	b.WriteString(strings.Repeat(" ", width) + "\n")
	return b.String()
}

func (m *Model) renderAgent(a model.Agent, width int) string {
	selected := m.selectedPane() == a.PaneID
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
		first = stySel.Render(padLine(line, width))
	}
	out := first + "\n"

	// Task line, dimmed and marked when it came from a lower rung of the
	// ladder. Showing doubt beats showing false confidence.
	if task := taskText(a); task != "" {
		style := styDim
		if a.TaskSource == model.TaskFromOrchestratorStale {
			style = styFaint
		}
		out += fitLine(style.Render("     "+truncate(task, max(6, width-6))), width) + "\n"
	}
	return out
}

func (m *Model) viewFilterBar() string {
	shown := 0
	for _, r := range m.visibleRepos() {
		shown += len(r.Agents)
	}
	bar := " " + styMeta.Render("filter ") + styMatch.Render(m.filter) + "▏" +
		styFaint.Render(fmt.Sprintf("  %d of %d · esc clears", shown, m.snap.Counts.Agents))
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
