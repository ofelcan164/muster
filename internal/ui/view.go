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
	lines = append(lines, m.headerLines()...)
	lines = append(lines, "")
	lines = append(lines, m.ribbonLines(len(lines))...)
	lines = append(lines, m.gridLines(len(lines))...)

	// The strip and the query line are pinned to the bottom, and everything
	// above them scrolls. The strip is how you reach the orchestrator, and the
	// query line is what you are typing: scrolling either away leaves you blind.
	var strip, bar []string
	if s := m.stripLines(len(lines) + 1); len(s) > 0 {
		strip = append([]string{""}, s...)
	}
	if m.filtering || m.filter != "" {
		bar = []string{"", m.viewFilterBar()}
	}
	if m.height-len(strip)-len(bar) < 1 {
		// Too short for the strip and a line above it. The strip scrolls with
		// the rest rather than covering all of it.
		lines, strip = append(lines, strip...), nil
	}
	pinned := append(strip, bar...)
	return strings.Join(append(m.window(lines, m.height-len(pinned)), pinned...), "\n")
}

// window trims the lines that scroll to h, kept on the selection, with a scroll
// bar down the right edge when they do not fit, and moves the recorded hit
// regions with them. Regions past the end of lines belong to the pinned strip,
// which moves up to sit right under the window.
//
// Without this the overlay handed bubbletea more lines than the terminal has.
// Its renderer keeps the last ones, so the header and the ribbon were what
// scrolled off, and every click then landed on whatever had been that many rows
// lower: on a 24-line terminal showing four repos, nine rows out.
func (m *Model) window(lines []string, h int) []string {
	if h <= 0 || len(lines) <= h {
		return lines
	}
	top := 0
	// The whole selection, not only its first line, or a tall tile scrolls in
	// with its bottom half still under the strip.
	if first, last, ok := m.cursorLines(); ok && last >= h {
		top = min(first, last-h+1)
	}
	top = min(top, len(lines)-h)

	hits := m.hits[:0]
	for _, r := range m.hits {
		if r.y >= len(lines) {
			r.y -= len(lines) - h
		} else if r.y -= top; r.y < 0 || r.y >= h {
			// Scrolled out of sight. Left in, a tile hidden under the strip
			// would take the clicks meant for the strip.
			continue
		}
		hits = append(hits, r)
	}
	m.hits = hits

	// The thumb is as tall as the share of lines on screen, and reaches the
	// bottom exactly when the window does.
	size := max(1, h*h/len(lines))
	pos := top * (h - size) / (len(lines) - h)
	out := make([]string, h)
	for i := range out {
		mark := styFaint.Render("│")
		if i >= pos && i < pos+size {
			mark = styMeta.Render("┃")
		}
		out[i] = pad(ansi.Truncate(lines[top+i], m.width-1, ""), m.width-1) + mark
	}
	return out
}

// cursorLines are the first and last screen lines the selection was drawn on,
// which is what the window scrolls to keep visible. The hit regions already
// record them, so nothing has to measure the layout twice.
func (m *Model) cursorLines() (first, last int, ok bool) {
	if m.cursor < 0 {
		return 0, 0, false
	}
	for _, h := range m.hits {
		if h.target != m.cursor {
			continue
		}
		if !ok || h.y < first {
			first = h.y
		}
		if !ok || h.y > last {
			last = h.y
		}
		ok = true
	}
	return first, last, ok
}

// headerLines is the title row, plus the warning on a line of its own when the
// two do not fit side by side. A warning that the daemon is not answering is
// the one thing on this screen that must not be dropped for want of room: the
// whole view behind it is stale.
func (m *Model) headerLines() []string {
	head := m.header()
	if m.warning == "" || strings.Contains(head, m.warning) {
		return []string{head}
	}
	return []string{head, styWarn.Render(fitLine("! "+m.warning, m.width))}
}

func (m *Model) header() string {
	c := m.snap.Counts
	left := styTitle.Render("MUSTER") + "  " +
		styMeta.Render(fmt.Sprintf("%s · %s", plural(c.Workspaces, "workspace"), plural(c.Agents, "agent")))
	// The ribbon rather than the daemon's count, which does not know what you
	// have dismissed. They are the same number until you dismiss something.
	if n := len(m.ribbonRows()); n > 0 {
		left += "  " + lipgloss.NewStyle().Foreground(colRed).Render(
			fmt.Sprintf("%d need you", n))
	}

	right := styHint.Render("/ search · s sort:" + m.sort.String() + " · q closes")
	if m.warning != "" {
		right = styWarn.Render("! " + m.warning)
	}

	// One column short of the edge, which is where the scroll bar draws.
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 1
	if gap < 1 {
		// Too narrow for both. headerLines puts a warning on its own line; the
		// key hints are what get dropped.
		return fitLine(left, m.width)
	}
	return left + strings.Repeat(" ", gap) + right
}

// ribbonLines renders the ranked ribbon, recording which screen line each row
// lands on so a click can find it. startY is the line this block begins at.
func (m *Model) ribbonLines(startY int) []string {
	rows := m.ribbonRows()
	if len(rows) == 0 || m.filter != "" {
		return nil
	}

	// The count is everything that needs you, as the tab bar badge says, not
	// just the rows that fit.
	out := []string{m.attentionRule(len(undismissed(m.snap.Attention, m.dismissed)))}
	for i, a := range rows {
		ti := m.ribbonTargetIndex("pane:" + a.PaneID)
		selected := m.isActive(ti)

		repo := m.repoByKey(a.RepoKey)
		ws := m.workspaceOf(a.PaneID)
		accent := reasonAccent(a.Reason)

		bar := accentBar(accent)
		idx := lipgloss.NewStyle().Foreground(accent).Bold(true).Render(fmt.Sprintf("%d", i+1))
		label := badge(reasonLabel(a.Reason, a.Status), accent)
		// The workspace comes first, faint, the way every tile in the grid leads
		// with it: the ribbon and the grid should read as the same map.
		wsTag := ""
		if ws.Label != "" {
			wsTag = styFaint.Render(fmt.Sprintf("%d %s", ws.Number, ws.Label)) + " "
		}
		who := wsTag + repoStyle(repo).Bold(true).Render(repo.Sigil+" "+repo.Display) +
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
				bar, idx, pad(label, 11), pad(who, 42), pad(age, 4))
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

// gridLines renders the grid: one tile per agent, one dim tile per workspace
// holding none.
func (m *Model) gridLines(startY int) []string {
	tiles := m.orderedTiles(m.visibleTiles())
	label := "agents & workspaces"
	if m.filter != "" {
		label = "matches"
	}
	if len(tiles) == 0 {
		if m.filter != "" {
			return []string{m.sectionRule("no matches"),
				styDim.Render("  nothing matches " + m.filter)}
		}
		return []string{m.sectionRule(label), styDim.Render("  no workspaces discovered yet")}
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

	blank := strings.Repeat(" ", cellWidth)
	for i := 0; i < len(tiles); i += cols {
		row := tiles[i:min(i+cols, len(tiles))]
		// Every tile stretches to the tallest in its row, so none has dead space
		// under it that neither lights up nor clicks. The blank line between rows
		// belongs to no tile, like the space between columns.
		texts := make([][]string, len(row))
		height := 0
		for c, t := range row {
			texts[c] = m.tileText(t, cellWidth-barWidth)
			height = max(height, len(texts[c]))
		}
		if i > 0 {
			out = append(out, "")
		}
		cells := make([][]string, len(row))
		for c, t := range row {
			// Each column starts after the cells before it, plus one space of
			// separator per gap. Passing the offset is what makes every column
			// clickable rather than only the first.
			cells[c] = m.tileLines(t, texts[c], height, cellWidth, startY+len(out), c*(cellWidth+1))
		}
		for r := 0; r < height; r++ {
			parts := make([]string, cols)
			for c := range parts {
				parts[c] = blank
				if c < len(cells) {
					parts[c] = cells[c][r]
				}
			}
			out = append(out, fitLine(strings.Join(parts, " "), m.width))
		}
	}
	return out
}

// tileLines renders one tile, agent or empty workspace, and claims its lines
// for the one target the whole tile shares.
//
// Every line is built alongside the target it belongs to, then styled from
// that target's state. Doing it in one pass is what keeps the highlight and
// the click region identical: they are derived from the same mapping, so a
// tile cannot end up clickable in places it does not light up.
// barWidth is the repo bar plus its trailing space: the two columns every
// tile line spends before its content starts.
const barWidth = 2

// text is the tile's content from tileText, drawn height lines tall.
func (m *Model) tileLines(t tile, text []string, height, width, startY, x0 int) []string {
	key, bar := "ws:"+t.Workspace.ID, styFaint.Render("▌")
	if t.isAgent() {
		key, bar = "pane:"+t.Agent.PaneID, repoStyle(t.Repo).Render("▌")
	}
	ti := m.targetIndex(key)

	text = append(text, make([]string, max(0, height-len(text)))...)
	out := make([]string, len(text))
	for i, s := range text {
		m.claim(startY+i, x0, width, ti)
		line := fitLine(bar+" "+s, width)
		if m.isActive(ti) {
			line = paint(line, stySel)
		}
		out[i] = line
	}
	return out
}

// tileText is a tile's content lines, agent or empty workspace, before the bar.
func (m *Model) tileText(t tile, width int) []string {
	if t.isAgent() {
		return m.agentTileLines(t, width)
	}
	return m.emptyTileLines(t, width)
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

// agentTileLines renders one agent tile in zones: line 1 is where (workspace
// and pane), line 2 is which checkout (repo and branch), line 3 is who
// (status, agent and kind), line 4 is what it says (question or task); then
// the dependency lines and the shared panes footer. Every
// field keeps its row, so the eye learns positions instead of parsing slashes
// and dots.
func (m *Model) agentTileLines(t tile, width int) []string {
	a := t.Agent

	// Where. The snapshot carries no pane or tab labels, so the pane renders
	// as its short id: the chip says where enter lands.
	chip := styFaint.Render("[" + shortPane(a.PaneID) + "]")
	line1 := workspaceNumCol(t.Workspace, m.snap.FocusedWorkspace) + " " +
		styDim.Render(truncate(t.Workspace.Label, max(4, width-lipgloss.Width(chip)-4))) + " " + chip
	lines := []string{line1}

	// Which checkout: the repo in its own colour, the branch faint behind it.
	line2 := repoStyle(t.Repo).Bold(true).Render(t.Repo.Sigil + " " + truncate(t.Repo.Display, max(4, width-8)))
	if t.Repo.Branch != "" {
		line2 += " " + styFaint.Render("· "+truncate(t.Repo.Branch, max(4, width-lipgloss.Width(line2)-2)))
	}
	lines = append(lines, line2)

	// Who: orchestrator mark, status icon, name and kind, with the age
	// right-aligned. What it waits on has its own line below the task.
	marker := " "
	if a.IsOrchestrator {
		marker = "⌂"
	}
	st := statusStyle(a.Status, m.frame)
	left := fmt.Sprintf("%s%s %s%s", marker, st.Render(statusIcon(a.Status, m.frame)),
		styFG.Bold(true).Render(truncate(a.Name, 14)), kindChip(a.Kind))
	right := styMeta.Render(ageText(a.Age(time.Now()), a.AgeKnown))
	line3 := left
	if gap := width - lipgloss.Width(left) - lipgloss.Width(right) - 1; gap > 0 {
		line3 += strings.Repeat(" ", gap) + right
	} else {
		line3 += " " + right
	}
	lines = append(lines, line3)

	if line, ok := m.tileTaskLine(a, width); ok {
		lines = append(lines, line)
	}
	lines = append(lines, m.tileEdgeLines(t, width)...)
	if len(t.Panes) > 0 {
		lines = append(lines, styFaint.Render("    "+tilePanesFooter(t)))
	}
	return lines
}

// shortPane trims "w1:p1" to "p1" for the tile's pane chip.
func shortPane(id string) string {
	if _, after, ok := strings.Cut(id, ":"); ok && after != "" {
		return after
	}
	return id
}

// kindChip is the agent's kind in faint brackets, or nothing when herdr sent
// none, which older snapshots and bare panes do.
func kindChip(kind string) string {
	if kind == "" {
		return ""
	}
	return " " + styFaint.Render("["+truncate(kind, 12)+"]")
}

// tileTaskLine is the tile's says line: the blocking question in bright
// foreground with a "? " prefix when there is one, the task otherwise, dimmed
// or marked when it came from a lower rung of the fallback ladder. Showing
// doubt beats showing false confidence.
func (m *Model) tileTaskLine(a model.Agent, width int) (string, bool) {
	task := taskText(a)
	if task == "" {
		return "", false
	}
	style := styDim
	if a.Question != "" {
		style = styFG
		task = "? " + task
	} else if a.TaskSource == model.TaskFromOrchestratorStale {
		style = styFaint
	}
	return style.Render("    " + truncate(task, max(6, width-4))), true
}

// tileEdgeLines are the dependency lines: what this agent is parked on, and
// which repos have agents parked on this one. Each sits dim until the tile at
// its other end is selected or under the pointer, so picking a tile lights up
// whatever it is tied to. Neither says "blocked", which means an agent waiting
// on you.
func (m *Model) tileEdgeLines(t tile, width int) []string {
	var lines []string
	a := t.Agent
	if a.BlockedOn != "" {
		lit := a.After != "" && m.litBy(func(r model.Repo, _ model.Agent) bool { return r.Key == a.After })
		sty := edgeStyle(lit)
		up := sty.Render(a.BlockedOn)
		if a.After != "" {
			up = repoMark(m.repoByKey(a.After), lit)
		}
		when := "can't land yet"
		if !a.LandedAt.IsZero() {
			when = "landed " + ageText(time.Since(a.LandedAt), true) + " ago"
		}
		lines = append(lines, "    "+truncate(sty.Render("⧗ after ")+up+sty.Render(" · "+when), width-4))
	}

	var holders []model.Repo
	for _, r := range m.snap.Repos {
		for _, x := range r.Agents {
			if x.After == t.Repo.Key {
				holders = append(holders, r)
				break
			}
		}
	}
	if len(holders) > 0 {
		lit := m.litBy(func(_ model.Repo, x model.Agent) bool { return x.After == t.Repo.Key })
		sty := edgeStyle(lit)
		marks := make([]string, len(holders))
		for i, r := range holders {
			marks[i] = repoMark(r, lit)
		}
		lines = append(lines, "    "+truncate(sty.Render("▸ holds ")+strings.Join(marks, sty.Render(", ")), width-4))
	}
	return lines
}

// litBy reports whether the selected or hovered agent satisfies match, which is
// how an edge line knows the tile at its other end is the one you are on.
func (m *Model) litBy(match func(model.Repo, model.Agent) bool) bool {
	for _, i := range []int{m.cursor, m.hover} {
		if pane := m.targetPane(i); pane != "" {
			if r, a, ok := m.agentByPane(pane); ok && match(r, a) {
				return true
			}
		}
	}
	return false
}

func edgeStyle(lit bool) lipgloss.Style {
	if lit {
		return styFG
	}
	return styDim
}

// repoMark is a repo as an edge line names it: its sigil and name in its own
// colour, bold while the line is lit.
func repoMark(r model.Repo, lit bool) string {
	return repoStyle(r).Bold(lit).Render(r.Sigil + " " + shortRepo(r))
}

// tilePanesFooter is the line every agent tile in a workspace shares once it
// also holds non-agent panes: how many agents and panes, and the pane labels.
func tilePanesFooter(t tile) string {
	labels := make([]string, 0, len(t.Panes))
	for _, p := range t.Panes {
		labels = append(labels, p.Label)
	}
	return fmt.Sprintf("%s · %s · %s", plural(t.Agents, "agent"), plural(len(t.Panes), "pane"), strings.Join(labels, " · "))
}

// emptyTileLines renders a workspace holding no agent: the number and label,
// dim and lowercase, then one faint line with the sigils of the repos its
// panes sit in and the pane labels. One line shorter than an agent tile, and
// with no bold and no colour except the sigils.
func (m *Model) emptyTileLines(t tile, width int) []string {
	num := workspaceNumCol(t.Workspace, m.snap.FocusedWorkspace)
	line1 := fmt.Sprintf("%s   %s", num, styDim.Render(strings.ToLower(t.Workspace.Label)))

	sigils := emptyTileSigils(t)
	detail := styFaint.Render(emptyTileDetail(t))
	line2 := "    " + detail
	if sigils != "" {
		line2 = "    " + sigils + " " + detail
	}
	return []string{line1, line2}
}

// emptyTileDetail is an empty tile's second line: the one repo its panes sit
// in and its branch, or every repo's name when there is more than one, then
// the pane labels.
func emptyTileDetail(t tile) string {
	var parts []string
	switch len(t.Repos) {
	case 0:
	case 1:
		r := t.Repos[0]
		s := r.Display
		if r.Branch != "" {
			s += " " + r.Branch
		}
		parts = append(parts, s)
	default:
		for _, r := range t.Repos {
			parts = append(parts, r.Display)
		}
	}
	for _, p := range t.Panes {
		parts = append(parts, p.Label)
	}
	return strings.Join(parts, " · ")
}

// emptyTileSigils is each repo an empty workspace's panes sit in, marked in
// its own colour, run together with no separator.
func emptyTileSigils(t tile) string {
	var b strings.Builder
	for _, r := range t.Repos {
		b.WriteString(repoStyle(r).Render(r.Sigil))
	}
	return b.String()
}

// workspaceNumCol is the two-column workspace number every tile leads with,
// marked with ▸ when it is the workspace you are in.
func workspaceNumCol(ws model.Workspace, focused string) string {
	s := fmt.Sprintf("%d", ws.Number)
	if ws.ID != "" && ws.ID == focused {
		s = "▸" + s
	} else {
		s = " " + s
	}
	return pad(s, 2)
}

func (m *Model) viewFilterBar() string {
	tiles := m.visibleTiles()
	agents, workspaces := 0, map[string]bool{}
	for _, t := range tiles {
		workspaces[t.Workspace.ID] = true
		if t.isAgent() {
			agents++
		}
	}
	cursor := ""
	if m.filtering {
		cursor = "▏"
	}
	hint := "  esc clears"
	if m.filtering {
		hint = "  esc keeps results · enter jumps"
	}
	// Count workspaces as well as agents. A search that matches an empty
	// workspace is a hit, and reporting "0 of 2" for it reads as a miss.
	bar := " " + styMeta.Render("search ") + styMatch.Render(m.filter) + cursor +
		styFaint.Render(fmt.Sprintf("  %s · %s%s",
			plural(len(workspaces), "workspace"), plural(agents, "agent"), hint))
	return fitLine(bar, m.width)
}

// attentionRule heads the ribbon. It carries a count and the colour of the most
// urgent row, so the section itself signals how bad things are before you read
// a single line of it.
func (m *Model) attentionRule(n int) string {
	// The rows on screen, not the daemon's list: taking the colour from a row
	// you have dismissed kept the header red for something you had dealt with.
	accent := colDim
	if rows := m.ribbonRows(); len(rows) > 0 {
		accent = reasonAccent(rows[0].Reason)
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
		// In front: a tile cuts a long task line at its width, and a marker at
		// the end was the first thing to go.
		return "(stale) " + a.Task
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

// fitLine pads or trims a line to exactly the given width, so grid cells always
// join cleanly no matter what is in them.
func fitLine(s string, width int) string {
	if lipgloss.Width(s) > width {
		return truncate(s, width)
	}
	return pad(s, width)
}
