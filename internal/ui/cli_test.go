package ui

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ofelcan164/muster/internal/model"
	"github.com/ofelcan164/muster/internal/state"
)

type call struct {
	Method string
	Params map[string]any
}

// fakeHerdr answers every call with an empty result and records it. The
// commands find it through HERDR_SOCKET_PATH, as they would from a plain shell.
// The socket lives under os.MkdirTemp("", "h") because unix socket paths cap
// near 108 bytes and t.TempDir paths carry the test name.
func fakeHerdr(t *testing.T) func() []call {
	t.Helper()
	dir, err := os.MkdirTemp("", "h")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	t.Setenv("HERDR_SOCKET_PATH", sock)

	var mu sync.Mutex
	var calls []call
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			line, err := bufio.NewReader(conn).ReadBytes('\n')
			if err == nil {
				var req struct {
					ID     string         `json:"id"`
					Method string         `json:"method"`
					Params map[string]any `json:"params"`
				}
				if json.Unmarshal(line, &req) == nil {
					mu.Lock()
					calls = append(calls, call{req.Method, req.Params})
					mu.Unlock()
					fmt.Fprintf(conn, `{"id":%q,"result":{}}`+"\n", req.ID)
				}
			}
			conn.Close()
		}
	}()
	return func() []call {
		mu.Lock()
		defer mu.Unlock()
		return append([]call(nil), calls...)
	}
}

// cliSnapshot is testSnapshot with an orchestrator and a landed row: web's
// checkout-ui depends on api, and api#412 landed ten minutes ago.
func cliSnapshot(t *testing.T) *model.Snapshot {
	t.Helper()
	useTempStateDir(t)
	snap := testSnapshot()
	snap.Orch = model.Orchestrator{Found: true, PaneID: "w1:p1", Name: "orchestrator"}
	web := &snap.Repos[2].Agents[0]
	web.DependsOn, web.DependsOnRepo = "api#412", "acme/api"
	web.LandedAt = time.Now().Add(-10 * time.Minute)
	snap.Attention = append(snap.Attention, model.Attention{
		Rank: 2, Reason: model.ReasonLanded, RepoKey: "acme/web", PaneID: "w3:p1",
		Agent: "checkout-ui", Status: model.StatusWorking,
		Dependents: []string{"web/checkout-ui"},
	})
	writeSnapshot(t, snap)
	return snap
}

func TestTellPromptsTheOrchestratorAndRecordsIt(t *testing.T) {
	cliSnapshot(t)
	calls := fakeHerdr(t)
	if err := Tell("pull main and rerun"); err != nil {
		t.Fatal(err)
	}
	got := calls()
	if len(got) != 2 || got[0].Method != "agent.prompt" || got[1].Method != "pane.report_metadata" {
		t.Fatalf("calls %+v, want agent.prompt then pane.report_metadata", got)
	}
	if got[0].Params["target"] != "w1:p1" || got[0].Params["text"] != "pull main and rerun" {
		t.Errorf("prompt params %v", got[0].Params)
	}
	if tokens, _ := got[1].Params["tokens"].(map[string]any); tokens["task"] != "pull main and rerun" {
		t.Errorf("recorded %v, want the message as the task token", got[1].Params)
	}
}

func TestTellWithNoOrchestratorSendsNothing(t *testing.T) {
	useTempStateDir(t)
	writeSnapshot(t, testSnapshot())
	calls := fakeHerdr(t)
	if err := Tell("hello"); err == nil || !strings.Contains(err.Error(), "no orchestrator") {
		t.Errorf("err %v, want no orchestrator", err)
	}
	if n := len(calls()); n != 0 {
		t.Errorf("%d calls with nobody to tell", n)
	}
}

// report sends exactly what t sends for the same row.
func TestReportSendsWhatTheTKeySends(t *testing.T) {
	snap := cliSnapshot(t)
	calls := fakeHerdr(t)
	told, err := Report("w3:p1")
	if err != nil {
		t.Fatal(err)
	}
	if told != "told the orchestrator api landed" {
		t.Errorf("Report said %q", told)
	}
	got := calls()
	if len(got) == 0 || got[0].Method != "agent.prompt" || got[0].Params["target"] != "w1:p1" {
		t.Fatalf("calls %+v, want a prompt to the orchestrator", got)
	}
	want, _ := landedReport(snap, snap.Attention[2], time.Now())
	if got[0].Params["text"] != want {
		t.Errorf("sent %q, the t key sends %q", got[0].Params["text"], want)
	}
	if !strings.HasPrefix(want, "api landed 10m ago, and web/checkout-ui still depends on it") {
		t.Errorf("report message %q", want)
	}
}

func TestReportNeedsALandedRow(t *testing.T) {
	cliSnapshot(t)
	calls := fakeHerdr(t)
	// w2:p1 is in the ribbon, but blocked, not landed.
	if _, err := Report("w2:p1"); err == nil {
		t.Error("reported a row that is not landed")
	}
	if n := len(calls()); n != 0 {
		t.Errorf("%d calls for a row with nothing to report", n)
	}
}

// dismiss writes ui.json the way x does, so the overlay and the badge both
// drop the row, and it refuses a pane with no row rather than writing junk.
func TestDismissWritesWhatXWrites(t *testing.T) {
	cliSnapshot(t)
	if err := Dismiss("w2:p1"); err != nil {
		t.Fatal(err)
	}
	if got := state.LoadUI().Dismissed; got["w2:p1"] != string(model.StatusBlocked) || len(got) != 1 {
		t.Errorf("ui.json dismissed %v, want w2:p1 at blocked", got)
	}
	if got := Badge("m"); got != "◆ 2 need you · prefix+m" {
		t.Errorf("badge after dismissing one of three: %q", got)
	}
	if err := Dismiss("w9:nowhere"); err == nil {
		t.Error("dismissed a pane with no ribbon row")
	}
}

// Dismissing from outside must not wipe what an open overlay saved: ui.json
// is merged under its lock, not overwritten.
func TestDismissKeepsTheOverlaysOtherSettings(t *testing.T) {
	cliSnapshot(t)
	ui := state.LoadUI()
	ui.Sort, ui.Colors = 2, map[string]int{"acme/api": 5}
	if err := ui.Save(); err != nil {
		t.Fatal(err)
	}
	if err := Dismiss("w2:p2"); err != nil {
		t.Fatal(err)
	}
	got := state.LoadUI()
	if got.Sort != 2 || got.Colors["acme/api"] != 5 || got.Dismissed["w2:p2"] != string(model.StatusDone) {
		t.Errorf("ui.json after dismiss: %+v", got)
	}
}

// A named pane wins over the pane the command runs in.
func TestMarkOrchestratorNamedPane(t *testing.T) {
	cliSnapshot(t)
	t.Setenv("HERDR_PANE_ID", "w2:p1")
	t.Setenv("HERDR_PLUGIN_CONTEXT_JSON", "")
	calls := fakeHerdr(t)
	if err := MarkOrchestrator("w3:p1"); err != nil {
		t.Fatal(err)
	}
	var renamed, tagged, cleared string
	for _, c := range calls() {
		switch c.Method {
		case "agent.rename":
			renamed, _ = c.Params["target"].(string)
		case "pane.report_metadata":
			tokens, _ := c.Params["tokens"].(map[string]any)
			pane, _ := c.Params["pane_id"].(string)
			if tokens["role"] == "orchestrator" {
				tagged = pane
			} else {
				cleared = pane
			}
		}
	}
	if renamed != "w3:p1" || tagged != "w3:p1" {
		t.Errorf("renamed %q and tagged %q, want the named pane w3:p1", renamed, tagged)
	}
	if cleared != "w1:p1" {
		t.Errorf("cleared %q, want the old orchestrator w1:p1", cleared)
	}
}
