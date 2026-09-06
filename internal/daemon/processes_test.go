package daemon

import "testing"

func TestDetectsAProcessStopping(t *testing.T) {
	d := newTestDaemon(t)
	live := map[string]bool{"w1:p1": true}

	d.detectStoppedProcesses(map[string]string{"w1:p1": "vite"}, live)
	if len(d.persist.Stopped) != 0 {
		t.Fatal("a running process is not a stop")
	}

	d.detectStoppedProcesses(map[string]string{"w1:p1": "bash"}, live)
	if got := d.persist.Stopped["w1:p1"]; got != "vite" {
		t.Fatalf("expected vite recorded as stopped, got %q", got)
	}

	// Still at a prompt. The stop stands rather than being re-reported.
	d.detectStoppedProcesses(map[string]string{"w1:p1": "bash"}, live)
	if d.persist.Stopped["w1:p1"] != "vite" {
		t.Error("stop should persist while the pane sits idle")
	}

	// Started again. Resolved.
	d.detectStoppedProcesses(map[string]string{"w1:p1": "vite"}, live)
	if _, still := d.persist.Stopped["w1:p1"]; still {
		t.Error("restarting the process should clear the stop")
	}
}

func TestShellToShellIsNotAStop(t *testing.T) {
	d := newTestDaemon(t)
	live := map[string]bool{"w1:p1": true}
	d.detectStoppedProcesses(map[string]string{"w1:p1": "bash"}, live)
	d.detectStoppedProcesses(map[string]string{"w1:p1": "zsh"}, live)
	if len(d.persist.Stopped) != 0 {
		t.Fatalf("a shell is not a running process, got %v", d.persist.Stopped)
	}
}

// A closed pane is closed, not stopped. Reporting it would put a row in the
// ribbon for something the user deliberately got rid of.
func TestClosedPaneIsNotAStop(t *testing.T) {
	d := newTestDaemon(t)
	d.detectStoppedProcesses(map[string]string{"w1:p1": "vite"}, map[string]bool{"w1:p1": true})
	d.detectStoppedProcesses(map[string]string{}, map[string]bool{})
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
