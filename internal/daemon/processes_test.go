package daemon

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/ofelcan/muster/internal/herdr"
	"github.com/ofelcan/muster/internal/model"
	"github.com/ofelcan/muster/internal/state"
)

var now = time.Now()

func TestDetectsAProcessStopping(t *testing.T) {
	d := newTestDaemon(t)
	live := map[string]bool{"w1:p1": true}

	d.detectStoppedProcesses(map[string]string{"w1:p1": "vite"}, live, now)
	if len(d.persist.Stopped) != 0 {
		t.Fatal("a running process is not a stop")
	}

	d.detectStoppedProcesses(map[string]string{"w1:p1": "bash"}, live, now)
	if got := d.persist.Stopped["w1:p1"].Process; got != "vite" {
		t.Fatalf("expected vite recorded as stopped, got %q", got)
	}

	// Still at a prompt. The stop stands rather than being re-reported.
	d.detectStoppedProcesses(map[string]string{"w1:p1": "bash"}, live, now)
	if d.persist.Stopped["w1:p1"].Process != "vite" {
		t.Error("stop should persist while the pane sits idle")
	}

	// Started again. Resolved.
	d.detectStoppedProcesses(map[string]string{"w1:p1": "vite"}, live, now)
	if _, still := d.persist.Stopped["w1:p1"]; still {
		t.Error("restarting the process should clear the stop")
	}
}

func TestShellToShellIsNotAStop(t *testing.T) {
	d := newTestDaemon(t)
	live := map[string]bool{"w1:p1": true}
	d.detectStoppedProcesses(map[string]string{"w1:p1": "bash"}, live, now)
	d.detectStoppedProcesses(map[string]string{"w1:p1": "zsh"}, live, now)
	if len(d.persist.Stopped) != 0 {
		t.Fatalf("a shell is not a running process, got %v", d.persist.Stopped)
	}
}

// A closed pane is closed, not stopped. Reporting it would put a row in the
// ribbon for something the user deliberately got rid of.
func TestClosedPaneIsNotAStop(t *testing.T) {
	d := newTestDaemon(t)
	d.detectStoppedProcesses(map[string]string{"w1:p1": "vite"}, map[string]bool{"w1:p1": true}, now)
	d.detectStoppedProcesses(map[string]string{}, map[string]bool{}, now)
	if len(d.persist.Stopped) != 0 {
		t.Errorf("closing a pane must not report a stop, got %v", d.persist.Stopped)
	}
	if len(d.persist.LastProcess) != 0 {
		t.Errorf("closed pane left process state behind: %v", d.persist.LastProcess)
	}
}

func TestIsShell(t *testing.T) {
	for _, s := range []string{"bash", "zsh", "fish", "BASH", ""} {
		if !isShell(s) {
			t.Errorf("%q should count as a shell", s)
		}
	}
	for _, s := range []string{"vite", "node", "cargo", "claude"} {
		if isShell(s) {
			t.Errorf("%q should not count as a shell", s)
		}
	}
}

// A stop is news for a while and then it is not. Ctrl-C on a one-off command
// would otherwise sit in the ribbon forever, since nothing is ever going to run
// in that pane again to clear it.
func TestStoppedProcessesAgeOut(t *testing.T) {
	d := newTestDaemon(t)
	live := map[string]bool{"w1:p1": true}
	t0 := time.Now()

	d.detectStoppedProcesses(map[string]string{"w1:p1": "sleep"}, live, t0)
	d.detectStoppedProcesses(map[string]string{"w1:p1": "bash"}, live, t0)
	if len(d.persist.Stopped) != 1 {
		t.Fatal("expected a recorded stop")
	}

	d.detectStoppedProcesses(map[string]string{"w1:p1": "bash"}, live, t0.Add(stoppedTTL-time.Minute))
	if len(d.persist.Stopped) != 1 {
		t.Error("dropped the stop too early")
	}

	d.detectStoppedProcesses(map[string]string{"w1:p1": "bash"}, live, t0.Add(stoppedTTL+time.Minute))
	if len(d.persist.Stopped) != 0 {
		t.Errorf("stop should have aged out, got %v", d.persist.Stopped)
	}
}

// Muster's own overlay is a pane like any other. Without excluding it, it shows
// up in the footer of whatever workspace you opened it from, and closing it
// reads as a process that stopped.
func TestOverlayPaneIsNotTracked(t *testing.T) {
	if !isOverlayPane(herdrPane("w1:p2", "Muster")) {
		t.Error("the overlay should be recognised by its manifest title")
	}
	if isOverlayPane(herdrPane("w1:p1", "dev")) {
		t.Error("an ordinary pane was mistaken for the overlay")
	}
	if isOverlayPane(herdrPane("w1:p1", "")) {
		t.Error("an unlabelled pane was mistaken for the overlay")
	}
}

// The stop and the agent were two readings of one pane, and the stop won.
//
// A dev server dies in a pane, which records a stop. Then an agent is started
// in that same pane. Only non-agent panes are read for a foreground process, so
// nothing left could ever resolve that stop, and a rank 3 stopped row outranks
// the agent's own done or stalled row. The pane sat in the ribbon as a dead
// dev server for the full TTL while the agent underneath it was waiting on you.
//
// buildRepos is the fix: an agent pane is not a pane a stop can be held
// against, so taking one over drops the row on the next reconcile.
func TestAnAgentTakingOverAPaneClearsItsStop(t *testing.T) {
	d := newTestDaemon(t)
	dir := gitRepo(t, filepath.Join(t.TempDir(), "api"), "git@github.com:acme/api.git", "main")
	snap := &herdr.Snapshot{
		Workspaces: []herdr.Workspace{{WorkspaceID: "w1", Number: 1}},
		Panes:      []herdr.Pane{pane("w1:p1", "w1", dir)},
	}
	stop := func() {
		d.persist.Stopped["w1:p1"] = state.StoppedStamp{Process: "vite", At: now}
		d.persist.LastProcess["w1:p1"] = "bash"
	}

	// No agent in the pane: the stop is this pane's to hold, and it stays.
	stop()
	d.buildRepos(snap, map[string]model.Agent{}, model.Orchestrator{})
	if _, held := d.persist.Stopped["w1:p1"]; !held {
		t.Fatal("a stop on a plain pane was dropped")
	}

	// An agent starts in the same pane. Its own status is the only reading now.
	stop()
	snap.Agents = []herdr.Agent{agentPane("w1:p1", "w1", dir, "blocked")}
	d.buildRepos(snap, d.buildAgents(snap, now), model.Orchestrator{})
	if _, held := d.persist.Stopped["w1:p1"]; held {
		t.Errorf("the stop outlived the takeover: %v", d.persist.Stopped)
	}
	if _, held := d.persist.LastProcess["w1:p1"]; held {
		t.Errorf("process state left behind for an agent pane: %v", d.persist.LastProcess)
	}
}
