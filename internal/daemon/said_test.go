package daemon

import (
	"testing"
	"time"
)

// Built from the same rendering the captured prompt in question_test.go shows:
// the agent's own lines start with "●", the user's with "❯", and wrapped text
// continues indented underneath. Unlike that one this was assembled rather than
// captured, because no orchestrator was blocked at the time to capture.
const claudeCodeTranscript = `
❯ contracts is merged, tell api to pick up the schema change

● I'll let api know and check what it is waiting on.

● Bash(muster chain get)
  ⎿  contracts > api > web,mobile

● Told api to pick up the schema change. web and mobile are still
  parked behind it, so they will move once api reports done.

❯
`

func TestExtractsWhatTheAgentLastSaid(t *testing.T) {
	want := "Told api to pick up the schema change. web and mobile are still parked behind it, so they will move once api reports done."
	if got := ExtractSaid(claudeCodeTranscript); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

// A tool call is the last thing it said as much as prose is: dispatching is
// exactly the thing the strip is meant to let you check at a glance.
func TestATooCallCountsAsSaying(t *testing.T) {
	text := "● I'll check the chain.\n\n● Bash(muster chain get)\n"
	if got := ExtractSaid(text); got != "Bash(muster chain get)" {
		t.Errorf("got %q", got)
	}
}

// Not every agent marks its output. The last real sentence beats a blank strip.
func TestFallsBackToTheLastLineWithWordsInIt(t *testing.T) {
	text := "some output\ndispatched the api work\n╌╌╌╌╌╌╌╌╌╌╌╌\n❯ \n"
	if got := ExtractSaid(text); got != "dispatched the api work" {
		t.Errorf("chrome should not win, got %q", got)
	}
}

func TestNothingSaid(t *testing.T) {
	for _, text := range []string{"", "\n\n", "❯ \n╭────────╮\n│        │\n"} {
		if got := ExtractSaid(text); got != "" {
			t.Errorf("ExtractSaid(%q) = %q, want empty", text, got)
		}
	}
}

// A wall of pasted text must not land in the snapshot whole.
func TestSaidIsCapped(t *testing.T) {
	long := "● "
	for i := 0; i < 500; i++ {
		long += "x"
	}
	got := ExtractSaid(long)
	if len([]rune(got)) > saidMax+1 {
		t.Errorf("kept %d runes, want at most %d plus the ellipsis", len([]rune(got)), saidMax)
	}
}

// A pane.read costs 350ms, so the same pane in the same state is read once.
// A state change is what makes the last message new.
func TestThePaneIsReadOncePerState(t *testing.T) {
	d := newTestDaemon(t)
	if !d.wantSaid("w1:p1|idle|4") {
		t.Fatal("the first read should be wanted")
	}
	if d.wantSaid("w1:p1|idle|4") {
		t.Error("a read already in flight should not fire a second")
	}

	// The read lands.
	d.saidKey, d.saidPending = "w1:p1|idle|4", ""
	if d.wantSaid("w1:p1|idle|4") {
		t.Error("nothing has changed, so nothing needs reading")
	}
	if !d.wantSaid("w1:p1|blocked|5") {
		t.Error("a status change makes the last message new again")
	}
}

// The turn is the unit, not the status. An orchestrator that answers, works,
// and settles back into idle lands on the pane and status it started from, and
// what it said in between is exactly what the strip exists to show.
func TestASecondTurnEndingInTheSameStatusIsReadAgain(t *testing.T) {
	d := newTestDaemon(t)
	d.saidKey = "w1:p1|idle|4"

	// Working is never read: mid-turn the tail of the pane is half a sentence.
	if !d.wantSaid("w1:p1|idle|6") {
		t.Error("a new turn settling back into idle is a new thing said")
	}
}

// Text read from one orchestrator must not be shown under the next one.
func TestSaidBelongsToThePaneItCameFrom(t *testing.T) {
	d := newTestDaemon(t)
	d.said, d.saidPane, d.saidAt = "dispatched api", "w1:p1", time.Now()
	got, at := d.orchSay("w1:p1")
	if got != "dispatched api" {
		t.Errorf("got %q", got)
	}
	if at.IsZero() {
		t.Error("the message came back without the time it was read")
	}
	got, at = d.orchSay("w2:p1")
	if got != "" {
		t.Errorf("another pane's message leaked: %q", got)
	}
	if !at.IsZero() {
		t.Error("another pane's read time leaked")
	}
}
