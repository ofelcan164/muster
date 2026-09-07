package daemon

import (
	"testing"
	"time"
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
