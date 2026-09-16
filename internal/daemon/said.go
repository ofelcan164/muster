package daemon

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ofelcan164/muster/internal/model"
)

// What the orchestrator last said exists only in its pane. The task ladder
// reports what it was told to do, which is the other half of the conversation:
// the prompt without the reply cannot tell you whether it answered, or whether
// it dispatched the thing it just learned about.
//
// So Muster reads the pane, on the same terms as the blocking question. A read
// costs 350ms, so it never happens on the reconcile path, and it only happens
// when the orchestrator has settled: mid-turn the tail of the pane is whatever
// it is halfway through writing.

// saidLines is how far back to look, and saidMax caps what is kept. The strip
// shows one line until e expands it to the whole message, so the cap is only so
// a pasted wall of text cannot land in the snapshot.
const (
	saidLines = 60
	saidMax   = 4000
)

// orchSay is what the orchestrator last said, and when that was read, if it
// came from this pane.
func (d *Daemon) orchSay(paneID string) (string, time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.saidPane != paneID {
		return "", time.Time{}
	}
	return d.said, d.saidAt
}

// wantSaid reports whether the pane is worth reading, and claims the read so
// two reconciles in the same second do not both fire one.
//
// The key is the pane, the status it settled into, and herdr's state_change_seq
// at that moment. The seq is what makes consecutive turns distinct: an
// orchestrator that answers, works, and settles back into idle lands on the
// same pane and the same status as last time, and without the seq the read
// would never fire again. herdr has no "the agent replied" event, so a state
// change is the only signal there is.
func (d *Daemon) wantSaid(key string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.saidKey == key || d.saidPending == key {
		return false
	}
	d.saidPending = key
	return true
}

// fetchSaid reads the orchestrator's pane and keeps what it said, in its own
// goroutine so a slow read never delays the snapshot.
func (d *Daemon) fetchSaid(ctx context.Context, paneID, key string) {
	// A panic in a goroutine takes the whole process with it, and the daemon
	// dying silently is far worse than a missing message.
	defer func() {
		if r := recover(); r != nil {
			d.logf("fetchSaid panic: %v", r)
		}
	}()
	if ctx.Err() != nil {
		return
	}
	text, err := d.client.PaneRead(paneID, "visible", saidLines)
	if err != nil {
		d.mu.Lock()
		d.saidPending = ""
		d.mu.Unlock()
		return
	}
	said := ExtractSaid(text)

	d.mu.Lock()
	// The key advances even when nothing could be extracted, so a pane this
	// cannot read is read once per status rather than on every reconcile. The
	// text itself is only replaced by something, since the previous message is
	// better than a blank strip.
	d.saidKey, d.saidPending = key, ""
	if said != "" {
		d.said, d.saidPane, d.saidAt = said, paneID, time.Now()
	}
	changed := said != ""
	d.mu.Unlock()

	if !changed {
		return
	}
	select {
	case d.rescan <- struct{}{}:
	default:
	}
}

// ExtractSaid pulls the agent's own last message out of pane content.
//
// Claude Code prefixes every line it originates with "●", prose and tool calls
// alike, and the user's own input with "❯". So the last "●" is the last thing
// the agent said or did, which is the glance this is for: did it answer, and
// did it dispatch. Wrapped text continues indented underneath and is joined
// back on, because half a sentence reads as a bug. A blank line followed by more
// indented text is the next paragraph of the same message, kept as a newline
// so the expanded strip can show it.
//
// No other agent marks its own lines, so for them the "●" scan never fires and
// the fallback carries the strip: the last line with words in it that is not
// the input box or the chrome around it. It is a guess, and a guess beats an
// empty strip.
//
// ponytail: the reject list below is one agent's box drawing. An agent whose
// chrome slips through shows a junk line, never a missing one; widen the list
// when one actually does, not before.
func ExtractSaid(text string) string {
	lines := strings.Split(text, "\n")

	for i := len(lines) - 1; i >= 0; i-- {
		s := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(s, "●") {
			continue
		}
		said := strings.TrimSpace(strings.TrimPrefix(s, "●"))
		sep := " "
		for j := i + 1; j < len(lines); j++ {
			t := strings.TrimSpace(lines[j])
			if t == "" {
				sep = "\n"
				continue
			}
			if !strings.HasPrefix(lines[j], "  ") {
				break
			}
			said += sep + t
			sep = " "
		}
		return clip(said, saidMax)
	}

	for i := len(lines) - 1; i >= 0; i-- {
		if s := strings.TrimSpace(lines[i]); isProse(s) {
			return clip(s, saidMax)
		}
	}
	return ""
}

// isProse rejects the chrome an agent draws around its output: the input
// prompt, box rules, and the numbered choices of a permission prompt.
func isProse(s string) bool {
	if s == "" || isChoice(s) {
		return false
	}
	for _, p := range []string{"❯", ">", "│", "|", "─", "╌", "╭", "╰", "┃"} {
		if strings.HasPrefix(s, p) {
			return false
		}
	}
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			return true
		}
	}
	return false
}

func clip(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return strings.TrimSpace(string(r[:max])) + "…"
}

// attachSaid fills in what the orchestrator last said and asks for a read when
// that could have changed. seq is the pane's state_change_seq, which is what
// tells one turn from the next. Working is skipped deliberately: it is
// mid-sentence.
func (d *Daemon) attachSaid(ctx context.Context, orch *model.Orchestrator, seq uint64) {
	if !orch.Found {
		return
	}
	orch.LastSaid, orch.SaidAt = d.orchSay(orch.PaneID)
	if d.client == nil || orch.Status == model.StatusWorking {
		return
	}
	key := fmt.Sprintf("%s|%s|%d", orch.PaneID, orch.Status, seq)
	if d.wantSaid(key) {
		go d.fetchSaid(ctx, orch.PaneID, key)
	}
}
