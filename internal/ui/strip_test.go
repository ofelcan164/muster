package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ofelcan164/muster/internal/model"
)

// withOrch is the test snapshot plus a marked orchestrator, and optionally an
// landed row for the report key to act on.
func withOrch(rows ...model.Attention) *model.Snapshot {
	s := testSnapshot()
	s.Orch = model.Orchestrator{
		Found: true, PaneID: "w4:p1", Name: "orchestrator",
		Status: model.StatusIdle, DetectedBy: "token",
		StatusSince: time.Now().Add(-3 * time.Minute),
		LastMessage: "contracts#412 is merged. api is picking up the schema change.",
		LastSaid:    "Told api to pick up the schema change.",
	}
	s.Attention = append(s.Attention, rows...)
	if len(rows) > 0 {
		// What a landed row stands on: web depends on api, which landed 6m ago.
		web := &s.Repos[2].Agents[0]
		web.DependsOn, web.DependsOnRepo = "api#412", "acme/api"
		web.LandedAt = time.Now().Add(-6 * time.Minute)
	}
	return s
}

func landedRow() model.Attention {
	return model.Attention{
		Rank: 2, Reason: model.ReasonLanded, RepoKey: "acme/web",
		PaneID: "w3:p1", Agent: "checkout-ui", Status: model.StatusIdle,
		Age: 40 * time.Minute, AgeKnown: true,
		Detail:     "api landed · still needed by web, mobile",
		Dependents: []string{"web/checkout-ui", "mobile/checkout"},
	}
}

func withSnapshot(t *testing.T, s *model.Snapshot, width int) *Model {
	t.Helper()
	m := New(s, "")
	m.Update(tea.WindowSizeMsg{Width: width, Height: 40})
	return m
}

func plain(s string) string { return escapes.ReplaceAllString(s, "") }

func TestStripShowsTheOrchestratorAndItsLastMessage(t *testing.T) {
	m := withSnapshot(t, withOrch(), 143)
	out := plain(m.View())

	for _, want := range []string{"ORCHESTRATOR",
		"Told api to pick up", "press i to tell it something"} {
		if !strings.Contains(out, want) {
			t.Errorf("the strip does not mention %q:\n%s", want, out)
		}
	}
}

// Silent empty space here reads as a bug, and guessing which agent is in charge
// is worse than asking.
func TestStripSaysHowToMarkAnOrchestrator(t *testing.T) {
	m := withSnapshot(t, testSnapshot(), 143)
	out := plain(m.View())
	if !strings.Contains(out, "none marked") {
		t.Errorf("with no orchestrator the strip should say so:\n%s", out)
	}
}

// The strip is clickable, and clicking it jumps to the orchestrator.
func TestClickingTheStripJumpsToTheOrchestrator(t *testing.T) {
	m := withSnapshot(t, withOrch(), 143)
	m.View()

	ti := m.stripTargetIndex()
	if ti < 0 {
		t.Fatal("no strip target was built")
	}
	var found bool
	for _, h := range m.hits {
		if h.target == ti {
			found = true
			m.Update(tea.MouseMsg{X: h.x0, Y: h.y,
				Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
			break
		}
	}
	if !found {
		t.Fatal("the strip has no click region")
	}
	if got := m.Jump(); got != "w4:p1" {
		t.Errorf("clicking the strip jumped to %q, wanted the orchestrator's pane", got)
	}
}

// The orchestrator is on the strip and in its repo card. Selecting the strip
// must not light the card up as well.
func TestSelectingTheStripDoesNotHighlightACard(t *testing.T) {
	m := withSnapshot(t, withOrch(), 143)
	m.View()
	m.cursor = m.stripTargetIndex()
	if got := m.selectedWorkspace(); got != "" {
		t.Errorf("selecting the strip also selected workspace %q", got)
	}
}

func TestInputSendsToTheOrchestrator(t *testing.T) {
	m := withSnapshot(t, withOrch(), 143)
	var toPane, sent string
	m.SetPrompter(func(pane, text string) error { toPane, sent = pane, text; return nil })

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	if !m.composing {
		t.Fatal("i did not open the input")
	}
	for _, r := range "ship it" {
		if r == ' ' {
			// As bubbletea delivers it: KeySpace carries the space in Runes
			// too, which is why nothing may add one of its own.
			m.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
			continue
		}
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if !strings.Contains(plain(m.View()), "ship it") {
		t.Error("what is being typed is not shown")
	}
	key(m, "enter")

	if m.composing {
		t.Error("the input stayed open after enter")
	}
	if toPane != "w4:p1" || sent != "ship it" {
		t.Errorf("sent %q to %q, wanted %q to the orchestrator", sent, toPane, "ship it")
	}
}

// Typing hjkl into the input has to be text, not navigation.
func TestInputOwnsEveryKeyWhileOpen(t *testing.T) {
	m := withSnapshot(t, withOrch(), 143)
	m.SetPrompter(func(string, string) error { return nil })
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	before := m.cursor
	for _, r := range "hjkl" {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if m.cursor != before {
		t.Errorf("typing moved the cursor from %d to %d", before, m.cursor)
	}
	if m.compose != "hjkl" {
		t.Errorf("input holds %q, wanted %q", m.compose, "hjkl")
	}
}

func TestEscapeCancelsTheInputWithoutSending(t *testing.T) {
	m := withSnapshot(t, withOrch(), 143)
	sends := 0
	m.SetPrompter(func(string, string) error { sends++; return nil })

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if m.composing || m.compose != "" {
		t.Error("escape left the input open")
	}
	if sends != 0 {
		t.Errorf("escape sent %d messages", sends)
	}
}

// The report key is the whole reason landed detection exists. What it sends has
// to carry the facts the orchestrator missed.
func TestReportKeyReportsTheLandedRow(t *testing.T) {
	m := withSnapshot(t, withOrch(landedRow()), 143)
	var toPane, sent string
	m.SetPrompter(func(pane, text string) error { toPane, sent = pane, text; return nil })

	if out := plain(m.View()); !strings.Contains(out, "t tells it api landed") {
		t.Errorf("the strip hint does not say what landed:\n%s", out)
	}
	key(m, "t")

	if toPane != "w4:p1" {
		t.Fatalf("the report went to %q, wanted the orchestrator", toPane)
	}
	for _, want := range []string{"api landed 6m ago", "web/checkout-ui, mobile/checkout still depend on it"} {
		if !strings.Contains(sent, want) {
			t.Errorf("the report does not mention %q:\n%s", want, sent)
		}
	}
	if !strings.Contains(plain(m.View()), "told the orchestrator") {
		t.Error("the strip does not confirm the report was sent")
	}
}

func TestReportKeyStaysSilentWithNothingLanded(t *testing.T) {
	m := withSnapshot(t, withOrch(), 143)
	sends := 0
	m.SetPrompter(func(string, string) error { sends++; return nil })

	key(m, "t")

	if sends != 0 {
		t.Errorf("t sent %d messages with nothing landed", sends)
	}
	if !strings.Contains(plain(m.View()), "nothing to report") {
		t.Error("t said nothing about why it did nothing")
	}
}

// Two landed rows and no selection: picking one would send the wrong report.
func TestReportKeyRefusesToChooseBetweenRows(t *testing.T) {
	second := landedRow()
	second.PaneID, second.Agent, second.RepoKey = "w2:p2", "tests", "acme/api"
	m := withSnapshot(t, withOrch(landedRow(), second), 143)
	sends := 0
	m.SetPrompter(func(string, string) error { sends++; return nil })

	m.cursor = 0 // the blocked row, not either landed row
	key(m, "t")

	if sends != 0 {
		t.Errorf("t picked one of two landed rows and sent %d messages", sends)
	}
}

// With two landed rows, t acts on the one you are looking at.
func TestReportKeyFollowsTheSelection(t *testing.T) {
	second := landedRow()
	second.PaneID, second.Agent, second.RepoKey = "w2:p2", "tests", "acme/api"
	second.Dependents = []string{"api/tests"}
	m := withSnapshot(t, withOrch(landedRow(), second), 143)
	var sent string
	m.SetPrompter(func(_, text string) error { sent = text; return nil })

	for i := 0; i < len(m.targets); i++ {
		if m.isKind(i, kindRibbon) && m.targetPane(i) == "w2:p2" {
			m.cursor = i
			break
		}
	}
	key(m, "t")

	if !strings.Contains(sent, "api/tests") {
		t.Errorf("t reported %q, wanted the selected row", sent)
	}
}

// A send that fails has nowhere else to be reported from.
func TestFailedSendIsReportedOnTheStrip(t *testing.T) {
	m := withSnapshot(t, withOrch(landedRow()), 143)
	m.SetPrompter(func(string, string) error { return errFake })

	key(m, "t")

	if out := plain(m.View()); !strings.Contains(out, "send failed") {
		t.Errorf("a failed send is not shown:\n%s", out)
	}
}

// A wedged socket must not freeze the overlay. The key hands the send off as a
// command and returns at once, so q still works while the socket hangs.
func TestSendDoesNotHoldUpTheKeyboard(t *testing.T) {
	m := withSnapshot(t, withOrch(landedRow()), 143)
	release := make(chan struct{})
	m.SetPrompter(func(string, string) error { <-release; return nil })

	got := make(chan tea.Cmd, 1)
	go func() {
		_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
		got <- cmd
	}()
	var cmd tea.Cmd
	select {
	case cmd = <-got:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("t waited on the prompter instead of handing it off")
	}
	if cmd == nil {
		t.Fatal("t returned no command, so nothing would ever be sent")
	}
	if !strings.Contains(m.notice, "sending") {
		t.Errorf("notice = %q while in flight, want it to say it is sending", m.notice)
	}

	close(release)
	m.Update(cmd())
	if !strings.Contains(m.notice, "told the orchestrator") {
		t.Errorf("notice = %q once the send returned", m.notice)
	}
}

var errFake = fakeErr("socket is gone")

type fakeErr string

func (e fakeErr) Error() string { return string(e) }

// A report of what just happened must not sit there through the next thing you
// do.
func TestNoticeClearsOnTheNextKey(t *testing.T) {
	m := withSnapshot(t, withOrch(landedRow()), 143)
	m.SetPrompter(func(string, string) error { return nil })

	key(m, "t")
	if m.notice == "" {
		t.Fatal("t recorded no notice")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if m.notice != "" {
		t.Errorf("the notice survived the next key: %q", m.notice)
	}
}

// The strip is subject to the same rule as everything else: a line that runs
// past the pane wraps and destroys the layout.
func TestStripNeverExceedsTheWidth(t *testing.T) {
	for _, w := range widths {
		m := withSnapshot(t, withOrch(landedRow()), w)
		m.SetPrompter(func(string, string) error { return nil })
		// Every state the last line can be in.
		for _, step := range []func(){
			func() {},
			func() { key(m, "t") },
			func() {
				m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
				for i := 0; i < 200; i++ {
					m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
				}
			},
		} {
			step()
			for i, line := range strings.Split(m.View(), "\n") {
				if got := visibleWidth(line); got > w {
					t.Fatalf("width %d: line %d is %d wide:\n%q", w, i, got, plain(line))
				}
			}
		}
	}
}

// Marking used to mean finding the orchestrator's pane and running an action on
// it, while the overlay was already showing every agent on the screen.
func TestMarkingTheOrchestratorFromTheOverlay(t *testing.T) {
	m := withSnapshot(t, testSnapshot(), 143)
	var marked []string
	m.SetMarker(func(pane string) error {
		marked = append(marked, pane)
		return nil
	})

	m.cursor = m.targetIndex("pane:w2:p1")
	key(m, "o")
	if len(marked) != 1 || marked[0] != "w2:p1" {
		t.Fatalf("o marked %v, want just w2:p1", marked)
	}
	if m.notice == "" {
		t.Error("the daemon takes a few seconds to catch up, so o should report")
	}

	// The strip says which key does it, so it is discoverable from the screen
	// that tells you nothing is marked.
	if out := plain(m.View()); !strings.Contains(out, "press o") {
		t.Errorf("the strip should name the key:\n%s", out)
	}
}

// A repo card has no agent to rename, and neither does a stopped-process row.
func TestMarkingSomethingThatIsNotAnAgent(t *testing.T) {
	m := withSnapshot(t, testSnapshot(), 143)
	m.SetMarker(func(string) error {
		t.Error("marked something that is not an agent")
		return nil
	})
	m.cursor = m.targetIndex("repo:acme/infra")
	key(m, "o")
	if m.notice == "" {
		t.Error("o on an empty card should say what o is for")
	}
}

// Marking the one already marked is a no-op, not a second rename.
func TestMarkingTheOrchestratorAgain(t *testing.T) {
	s := withOrch()
	// Put the orchestrator on an agent the grid actually shows.
	s.Orch.PaneID = "w1:p1"
	m := withSnapshot(t, s, 143)
	m.SetMarker(func(string) error {
		t.Error("re-marked the current orchestrator")
		return nil
	})
	m.cursor = m.targetIndex("pane:w1:p1")
	key(m, "o")
	if !strings.Contains(m.notice, "already") {
		t.Errorf("notice = %q, want it to say it is already the orchestrator", m.notice)
	}
}

// The strip shows the reply and not the prompt. What you told it is the half
// you already know, and silence still has to say it is silence: empty space
// here reads as a bug.
func TestStripShowsTheReplyAndNotThePrompt(t *testing.T) {
	s := withOrch()
	m := withSnapshot(t, s, 143)
	out := plain(m.View())

	if strings.Contains(out, "contracts#412 is merged") {
		t.Errorf("the prompt is back on the strip:\n%s", out)
	}

	s.Orch.LastSaid = ""
	out = plain(withSnapshot(t, s, 143).View())
	if !strings.Contains(out, "nothing said yet") {
		t.Errorf("an orchestrator that has not replied should say so:\n%s", out)
	}
}

// The age is half the point of the line. A sentence with no clock on it cannot
// be told from one that has been sitting there since this morning.
func TestSaidCarriesItsAge(t *testing.T) {
	s := withOrch()
	s.Orch.SaidAt = time.Now().Add(-90 * time.Second)
	out := plain(withSnapshot(t, s, 143).View())

	if !strings.Contains(out, "1m") {
		t.Errorf("the reply lost its age:\n%s", out)
	}

	// Nothing read yet means no age to show, not a zero-time age of years.
	s.Orch.SaidAt = time.Time{}
	if out := plain(withSnapshot(t, s, 143).View()); strings.Contains(out, "y ") {
		t.Errorf("an unread reply invented an age:\n%s", out)
	}
}

// Marking renames the agent to "orchestrator", so the head line was echoing
// the section rule directly above it. The repo it works in is the fact that
// line did not have.
func TestStripHeadNamesTheRepoNotTheBlock(t *testing.T) {
	s := testSnapshot()
	repo := s.Repos[0]
	s.Orch = model.Orchestrator{
		Found: true, PaneID: repo.Agents[0].PaneID, Name: "orchestrator",
		Status: model.StatusIdle, StatusSince: time.Now().Add(-time.Minute),
	}
	out := plain(withSnapshot(t, s, 143).View())

	if !strings.Contains(out, repo.Sigil+" "+strings.ToUpper(repo.Display)) {
		t.Errorf("the head does not name the orchestrator's repo:\n%s", out)
	}
	if strings.Count(out, "ORCHESTRATOR") != 1 {
		t.Errorf("ORCHESTRATOR appears %d times, want once:\n%s",
			strings.Count(out, "ORCHESTRATOR"), out)
	}

	// A pane in no repo Muster knows about has nothing but its name.
	s.Orch.PaneID, s.Orch.Name = "zz:p9", "lead"
	if out := plain(withSnapshot(t, s, 143).View()); !strings.Contains(out, "LEAD") {
		t.Errorf("an orchestrator outside every repo lost its name:\n%s", out)
	}
}

// A reply longer than the line gets a more link. e or a click on the link
// shows all of it, paragraphs included, and esc folds it back before closing.
func TestSaidExpandsToTheWholeMessage(t *testing.T) {
	s := withOrch()
	tail := "and that is the end of the second paragraph"
	s.Orch.LastSaid = strings.Repeat("api is picking up the schema change. ", 6) + "\n" + tail
	m := withSnapshot(t, s, 80)

	out := plain(m.View())
	if !strings.Contains(out, "e more") || strings.Contains(out, tail) {
		t.Fatalf("a long reply should be cut with a more link:\n%s", out)
	}

	key(m, "e")
	out = plain(m.View())
	if !strings.Contains(out, tail) || !strings.Contains(out, "e less") {
		t.Fatalf("e should show the whole reply:\n%s", out)
	}

	key(m, "esc")
	if m.quit || m.sayMore {
		t.Fatal("esc should fold the reply, not close the overlay")
	}

	m.View()
	var link *hitRegion
	for i := range m.hits {
		if m.hits[i].target == moreTarget {
			link = &m.hits[i]
		}
	}
	if link == nil {
		t.Fatal("the more link has no click region")
	}
	m.Update(tea.MouseMsg{X: link.x0, Y: link.y,
		Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
	if !m.sayMore || m.jump != "" {
		t.Errorf("clicking more should expand, not jump (jump=%q)", m.jump)
	}
}

// grip is where the strip's rule was drawn, the handle a drag resizes it by.
func grip(t *testing.T, m *Model) hitRegion {
	t.Helper()
	m.View()
	for _, h := range m.hits {
		if h.target == gripTarget {
			return h
		}
	}
	t.Fatal("the strip's rule has no drag region")
	return hitRegion{}
}

// Dragging the strip's rule up gives the message more lines, the rule lands on
// the pointer, and the height is saved once the button comes up. A drag never
// jumps or moves the selection.
func TestDraggingTheStripRuleResizesIt(t *testing.T) {
	s := withOrch()
	s.Orch.LastSaid = strings.Repeat("api is picking up the schema change. ", 40)
	m := withSnapshot(t, s, 80)
	saved := -1
	m.SetSayRows(0, func(n int) { saved = n })
	cursor := m.cursor

	g := grip(t, m)
	m.Update(tea.MouseMsg{X: 5, Y: g.y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m.Update(tea.MouseMsg{X: 5, Y: 25, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft})
	m.Update(tea.MouseMsg{X: 5, Y: 20, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft})
	if got := grip(t, m).y; got != 20 {
		t.Errorf("mid-drag the rule is on line %d, want the pointer's 20", got)
	}
	if saved != -1 {
		t.Error("the height was saved before the drag ended")
	}
	m.Update(tea.MouseMsg{X: 5, Y: 20, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})

	// 40 lines, less the blank line, rule, head and footer, less the 20 above
	// the rule.
	if m.sayRows != 17 || saved != 17 {
		t.Errorf("sayRows = %d, saved %d, want 17", m.sayRows, saved)
	}
	if m.jump != "" || m.cursor != cursor {
		t.Errorf("a drag jumped or moved the selection (jump=%q)", m.jump)
	}
	lines := strings.Split(plain(m.View()), "\n")
	if len(lines) != 40 || !strings.Contains(lines[len(lines)-1], "press i") {
		t.Errorf("the strip should end on the last line:\n%s", strings.Join(lines, "\n"))
	}
	if !strings.Contains(lines[len(lines)-2], "e more") {
		t.Errorf("a message longer than the rows should keep its more link:\n%s", strings.Join(lines, "\n"))
	}

	// Down past the bottom folds it back to one line, never less.
	g = grip(t, m)
	m.Update(tea.MouseMsg{X: 5, Y: g.y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m.Update(tea.MouseMsg{X: 5, Y: 60, Action: tea.MouseActionRelease})
	if m.sayRows != 1 || saved != 1 {
		t.Errorf("sayRows = %d, saved %d, want 1", m.sayRows, saved)
	}
}

// However far up the rule is dragged, the header and a line of the grid stay
// on screen above it.
func TestDraggingTheStripLeavesTheHeader(t *testing.T) {
	s := withOrch()
	s.Orch.LastSaid = strings.Repeat("a long reply. ", 400)
	m := withSnapshot(t, s, 80)

	g := grip(t, m)
	m.Update(tea.MouseMsg{X: 5, Y: g.y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m.Update(tea.MouseMsg{X: 5, Y: 0, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})

	lines := strings.Split(plain(m.View()), "\n")
	if len(lines) != 40 {
		t.Fatalf("the view is %d lines, want the terminal's 40", len(lines))
	}
	if !strings.HasPrefix(lines[0], "MUSTER") {
		t.Errorf("the header was pushed off the top: %q", lines[0])
	}
	if got := grip(t, m).y; got != 3 {
		t.Errorf("the rule stopped on line %d, want 3", got)
	}
}

// A dragged height docks the strip to the bottom and holds even for a message
// shorter than it, so the rule does not spring away from the pointer. That is
// also how a height saved in an earlier session opens.
func TestDockedStripHoldsItsHeight(t *testing.T) {
	m := withSnapshot(t, withOrch(), 80)
	m.SetSayRows(4, nil)
	if got := grip(t, m).y; got != 33 {
		t.Errorf("rule on line %d, want 33", got)
	}
	lines := strings.Split(plain(m.View()), "\n")
	if len(lines) != 40 || !strings.Contains(lines[39], "press i") {
		t.Errorf("a docked strip should end on the last line:\n%s", strings.Join(lines, "\n"))
	}
	// And a click on it still jumps: the docking moved its regions with it.
	m.Update(tea.MouseMsg{X: 5, Y: 34, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
	if m.jump != "w4:p1" {
		t.Errorf("clicking the docked strip jumped to %q, want the orchestrator", m.jump)
	}
}

// Hovering the rule lights it and says it drags.
func TestHoveringTheStripRuleSaysItDrags(t *testing.T) {
	m := withSnapshot(t, withOrch(), 80)
	if strings.Contains(plain(m.View()), "drag") {
		t.Fatal("the drag hint shows without the pointer on the rule")
	}
	g := grip(t, m)
	m.Update(tea.MouseMsg{X: 5, Y: g.y, Action: tea.MouseActionMotion})
	if !strings.Contains(plain(m.View()), "↕ drag") {
		t.Error("hovering the rule should say it drags")
	}
}

// herdr drops a release outside the popup, so a drag let go of out there never
// sees its button come up. The next motion arrives with no button held, and
// that ends the drag where it was rather than resizing to follow the pointer.
func TestDragReleasedOutsideThePopupEnds(t *testing.T) {
	m := withSnapshot(t, withOrch(), 80)
	saved := -1
	m.SetSayRows(0, func(n int) { saved = n })

	g := grip(t, m)
	m.Update(tea.MouseMsg{X: 5, Y: g.y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m.Update(tea.MouseMsg{X: 5, Y: 20, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft})
	m.Update(tea.MouseMsg{X: 5, Y: 10, Action: tea.MouseActionMotion, Button: tea.MouseButtonNone})

	if m.resizing || m.sayRows != 17 || saved != 17 {
		t.Errorf("resizing=%v sayRows=%d saved=%d, want the drag ended at 17",
			m.resizing, m.sayRows, saved)
	}
	// And the pointer hovers again, rather than still being the drag's.
	m.Update(tea.MouseMsg{X: 5, Y: 0, Action: tea.MouseActionMotion})
	if m.sayRows != 17 {
		t.Errorf("motion after the drag resized the strip to %d", m.sayRows)
	}
}

// The more link on a docked strip's last row is clickable where it is drawn,
// and expands rather than jumping.
func TestDockedMoreLinkExpands(t *testing.T) {
	s := withOrch()
	s.Orch.LastSaid = strings.Repeat("api is picking up the schema change. ", 40)
	m := withSnapshot(t, s, 80)
	m.SetSayRows(4, nil)

	lines := strings.Split(plain(m.View()), "\n")
	var link *hitRegion
	for i := range m.hits {
		if m.hits[i].target == moreTarget {
			link = &m.hits[i]
		}
	}
	if link == nil {
		t.Fatal("the docked more link has no click region")
	}
	if got := string([]rune(lines[link.y])[link.x0 : link.x1+1]); got != " e more" {
		t.Errorf("the region covers %q, want the link's \" e more\"", got)
	}
	m.Update(tea.MouseMsg{X: link.x0 + 2, Y: link.y, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
	if !m.sayMore || m.jump != "" {
		t.Errorf("clicking the docked more link should expand, not jump (jump=%q)", m.jump)
	}
}
