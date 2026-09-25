package ui

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ofelcan164/muster/internal/model"
	"github.com/ofelcan164/muster/internal/state"
)

func writeSnapshot(t *testing.T, s *model.Snapshot) {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.WriteAtomic(state.SnapshotPath(), b); err != nil {
		t.Fatal(err)
	}
}

// badgeJSON runs BadgeJSON and fails unless the output is one JSON object with
// exactly the three keys a bar reads.
func badgeJSON(t *testing.T) BadgeOutput {
	t.Helper()
	raw := BadgeJSON()
	if strings.Contains(raw, "\n") {
		t.Fatalf("BadgeJSON spans lines, a bar reads one: %q", raw)
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	var out BadgeOutput
	if err := dec.Decode(&out); err != nil {
		t.Fatalf("BadgeJSON is not the {text, tooltip, class} object: %v\n%s", err, raw)
	}
	return out
}

func useTempStateDir(t *testing.T) {
	t.Helper()
	state.SetDir(t.TempDir())
	t.Cleanup(func() { state.SetDir("") })
}

func TestBadgeJSONStaleWithoutASnapshot(t *testing.T) {
	useTempStateDir(t)
	got := badgeJSON(t)
	if got.Class != "stale" || got.Text != "◆" || got.Tooltip == "" {
		t.Errorf("no snapshot: got %+v, want class stale, no count, and a tooltip saying why", got)
	}
}

// A daemon that stopped writing is stale even with rows in its last snapshot:
// a count nobody keeps current must not reach the bar.
func TestBadgeJSONStaleSnapshotHasNoCount(t *testing.T) {
	useTempStateDir(t)
	snap := testSnapshot()
	snap.GeneratedAt = time.Now().Add(-time.Minute)
	writeSnapshot(t, snap)
	got := badgeJSON(t)
	if got.Class != "stale" || got.Text != "◆" || strings.Contains(got.Tooltip, "BLOCKED") {
		t.Errorf("stale snapshot: got %+v", got)
	}
}

func TestBadgeJSONNeedsYou(t *testing.T) {
	useTempStateDir(t)
	writeSnapshot(t, testSnapshot())
	got := badgeJSON(t)
	want := BadgeOutput{
		Text: "◆ 2 need you",
		Tooltip: "BLOCKED api/migrations · Do you want to create abc.txt?\n" +
			"DONE api/tests · finished, unseen",
		Class: "needs-you",
	}
	if got != want {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

// A dismissed row leaves the count and the tooltip, as it leaves the overlay.
func TestBadgeJSONLeavesOutDismissedRows(t *testing.T) {
	useTempStateDir(t)
	writeSnapshot(t, testSnapshot())
	ui := state.LoadUI()
	ui.Dismissed = map[string]string{"w2:p1": string(model.StatusBlocked)}
	if err := ui.Save(); err != nil {
		t.Fatal(err)
	}
	got := badgeJSON(t)
	if got.Text != "◆ 1 needs you" || got.Tooltip != "DONE api/tests · finished, unseen" {
		t.Errorf("with the blocked row dismissed: got %+v", got)
	}
}

// Landed outranks needs-you for the class wherever the landed row sits: it is
// the one row that means work can move now.
func TestBadgeJSONLanded(t *testing.T) {
	useTempStateDir(t)
	snap := testSnapshot()
	snap.Attention = append(snap.Attention, model.Attention{
		Rank: 2, Reason: model.ReasonLanded, RepoKey: "acme/web", PaneID: "w3:p1",
		Agent: "checkout-ui", Status: model.StatusWorking, Detail: "api#412 landed",
	})
	writeSnapshot(t, snap)
	got := badgeJSON(t)
	if got.Class != "landed" || !strings.Contains(got.Tooltip, "LANDED web/checkout-ui · api#412 landed") {
		t.Errorf("got %+v", got)
	}
}

func TestBadgeJSONWorkingAndIdle(t *testing.T) {
	useTempStateDir(t)
	snap := testSnapshot()
	snap.Attention = nil
	snap.Counts.Working = 3
	writeSnapshot(t, snap)
	if got := badgeJSON(t); got != (BadgeOutput{Text: "◆ 3 working", Class: "working"}) {
		t.Errorf("working: got %+v", got)
	}

	snap.Counts.Working = 0
	writeSnapshot(t, snap)
	if got := badgeJSON(t); got != (BadgeOutput{Text: "◆", Class: "idle"}) {
		t.Errorf("idle: got %+v", got)
	}
}

// The tooltip stops where the overlay's ribbon stops and says how many it left
// out, the count does not stop, and a question spread over several lines stays
// on one tooltip row.
func TestBadgeJSONTooltipIsCappedAndOneRowPerLine(t *testing.T) {
	useTempStateDir(t)
	snap := testSnapshot()
	snap.Attention = nil
	for _, pane := range []string{"a", "b", "c", "d", "e", "f"} {
		snap.Attention = append(snap.Attention, model.Attention{
			Reason: model.ReasonBlocked, RepoKey: "acme/api", PaneID: pane,
			Agent: pane, Status: model.StatusBlocked, Detail: "Allow\n  this?",
		})
	}
	writeSnapshot(t, snap)
	got := badgeJSON(t)
	lines := strings.Split(got.Tooltip, "\n")
	if got.Text != "◆ 6 need you" || len(lines) != 5 {
		t.Fatalf("got text %q and %d tooltip lines, want the full count and 4 rows plus a remainder", got.Text, len(lines))
	}
	if lines[0] != "BLOCKED api/a · Allow this?" || lines[4] != "and 2 more" {
		t.Errorf("first line %q, last line %q", lines[0], lines[4])
	}
}
