package daemon

import (
	"context"
	"strings"

	"github.com/ofelcan164/muster/internal/model"
)

// The question a blocked agent is waiting on exists only in the pane's visible
// content. herdr does not surface it: state_labels is empty for a blocked
// agent, and `agent explain` returns the detection rule and its evidence rather
// than the prompt.
//
// So Muster reads the pane. That costs around 350ms, which is why it is scoped
// to blocked agents only and cached until the agent stops being blocked. In
// practice that is one read per blocking event, usually with one agent blocked
// at a time, rather than a read per pane per cycle.

// questionLines is how far back to look. The prompt is always near the bottom.
const questionLines = 40

// fetchQuestions reads the pane of every blocked agent whose question is not
// already known, and asks for a reconcile if it learned anything.
//
// It runs in its own goroutine so a slow read never delays the snapshot.
func (d *Daemon) fetchQuestions(ctx context.Context, blocked []string) {
	// A panic in a goroutine takes the whole process with it, and the main loop
	// cannot recover it. The daemon going silently dead is far worse than a
	// missing question, so this failure is contained and logged instead.
	defer func() {
		if r := recover(); r != nil {
			d.logf("fetchQuestions panic: %v", r)
		}
	}()

	learned := false
	for _, paneID := range blocked {
		if ctx.Err() != nil {
			return
		}
		text, err := d.client.PaneRead(paneID, "visible", questionLines)
		if err != nil {
			continue
		}
		q := ExtractQuestion(text)
		if q == "" {
			continue
		}
		d.mu.Lock()
		if d.questions[paneID] != q {
			d.questions[paneID] = q
			learned = true
		}
		d.mu.Unlock()
	}
	if !learned {
		return
	}
	select {
	case d.rescan <- struct{}{}:
	default:
	}
}

// blockedNeedingQuestion lists blocked panes with no question cached yet, and
// drops cached questions for agents that are no longer blocked.
func (d *Daemon) blockedNeedingQuestion(agents map[string]model.Agent) []string {
	d.mu.Lock()
	defer d.mu.Unlock()

	blocked := map[string]bool{}
	var want []string
	for paneID, a := range agents {
		if a.Status != model.StatusBlocked {
			continue
		}
		blocked[paneID] = true
		if d.questions[paneID] == "" {
			want = append(want, paneID)
		}
	}
	// An agent that answered its prompt has no question any more.
	for paneID := range d.questions {
		if !blocked[paneID] {
			delete(d.questions, paneID)
		}
	}
	return want
}

func (d *Daemon) question(paneID string) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.questions[paneID]
}

// ExtractQuestion pulls the prompt an agent is waiting on out of pane content.
//
// Agents render these differently, so this looks for the shape they share
// rather than matching any one tool: a question line sitting just above a list
// of numbered choices. Claude Code produces
//
//	Do you want to create abc.txt?
//	❯ 1. Yes
//	  2. No
//
// Falling back to the last question line keeps something useful for agents that
// do not offer numbered options.
func ExtractQuestion(text string) string {
	lines := strings.Split(text, "\n")

	clean := func(s string) string {
		return strings.TrimSpace(strings.Trim(strings.TrimSpace(s), "│|>❯● "))
	}

	// Preferred: a question immediately above numbered choices.
	for i := len(lines) - 1; i >= 0; i-- {
		if !isChoice(lines[i]) {
			continue
		}
		for j := i - 1; j >= 0 && j >= i-4; j-- {
			if c := clean(lines[j]); strings.HasSuffix(c, "?") {
				return c
			}
		}
	}

	// Fallback: the last question asked.
	for i := len(lines) - 1; i >= 0; i-- {
		if c := clean(lines[i]); strings.HasSuffix(c, "?") && len(c) > 1 {
			return c
		}
	}
	return ""
}

// isChoice reports whether a line looks like "1. Yes" or "❯ 1. Yes".
func isChoice(line string) bool {
	s := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "❯"))
	if len(s) < 2 || s[0] < '1' || s[0] > '9' {
		return false
	}
	rest := s[1:]
	return strings.HasPrefix(rest, ".") || strings.HasPrefix(rest, ")")
}
