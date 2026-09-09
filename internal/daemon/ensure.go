package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/ofelcan164/muster/internal/state"
)

// Ensure starts a daemon if one is not already running, and returns
// immediately either way.
//
// Two things about herdr make this shape mandatory rather than tidy:
//
//   - herdr kills a pane's entire process group when the pane closes, so a
//     plainly forked child dies with whatever started it. The daemon has to be
//     in its own session, which is what Setsid gives it.
//   - herdr tracks plugin commands with a running/succeeded status and caps
//     concurrency, returning plugin_command_limit_reached once the cap is hit.
//     A startup command that simply never returns would sit in that table as
//     permanently running for the whole session and eat a slot. So this starts
//     the daemon and exits in single-digit milliseconds.
//
// Idempotency is required because [[startup]] runs again on
// `herdr update --handoff`, and because the install action starts the daemon
// too: startup hooks do not fire when a plugin is linked mid-session, so
// linking Muster has to work without waiting for a restart.
// daemonName is the executable the manifest's [[build]] steps produce.
const daemonName = "musterd"

func Ensure() (started bool, err error) {
	if _, err := state.EnsureDir(); err != nil {
		return false, err
	}

	// The spawn lock serialises concurrent --ensure calls. Without it two
	// simultaneous invocations could both observe a free daemon lock and both
	// spawn. It is held for milliseconds.
	spawn, err := state.LockBlocking(state.SpawnLockPath())
	if err != nil {
		return false, fmt.Errorf("spawn lock: %w", err)
	}
	defer spawn.Release()

	if running() {
		return false, nil
	}

	exe, err := daemonBinary()
	if err != nil {
		return false, err
	}

	// Detach stdio to the log file so the daemon holds no pipe belonging to the
	// pane that started it. A held pipe would keep the daemon tied to a
	// terminal that is about to disappear.
	logFile, err := os.OpenFile(state.LogPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return false, err
	}
	defer logFile.Close()
	devNull, err := os.OpenFile(os.DevNull, os.O_RDONLY, 0)
	if err != nil {
		return false, err
	}
	defer devNull.Close()

	// Pass the state dir through explicitly. The child may not inherit a
	// plugin environment, and it must land in the same directory as its parent.
	cmd := exec.Command(exe, "--state-dir", state.Dir(), "--daemon")
	cmd.Stdin = devNull
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = os.Environ()
	// Setsid detaches the child from the pane's process group and controlling
	// terminal, so it survives the pane closing.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		return false, err
	}
	// Release the child so it is reparented to init rather than left a zombie
	// when this short-lived process exits.
	if err := cmd.Process.Release(); err != nil {
		return false, err
	}

	// Wait briefly for the daemon to take the lock, so --ensure does not report
	// success for a daemon that died on startup. Bounded tightly: returning
	// fast matters more than certainty here, and the overlay self-heals anyway.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if running() {
			return true, nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return true, nil
}

// daemonBinary resolves the musterd executable.
//
// Ensure is called both by musterd itself and by the muster client, which the
// install and event actions run. Spawning os.Executable() unconditionally would
// make the client try to run itself as a daemon, so the musterd binary is
// resolved by name from the directory the running binary sits in. The manifest
// builds both into the same bin/ directory.
func daemonBinary() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if filepath.Base(exe) == daemonName {
		return exe, nil
	}
	candidate := filepath.Join(filepath.Dir(exe), daemonName)
	if _, err := os.Stat(candidate); err != nil {
		return "", fmt.Errorf("cannot find %s next to %s: %w", daemonName, exe, err)
	}
	return candidate, nil
}

// running reports whether a daemon currently holds the lock. The kernel drops
// an flock when its holder dies, so this can never see a stale lock from a
// killed daemon.
func running() bool {
	l, ok, err := state.TryLock(state.LockPath())
	if err != nil {
		return false
	}
	if ok {
		// Acquiring it means nobody held it. Release immediately.
		_ = l.Release()
		return false
	}
	return true
}

// AcquireDaemonLock is called by the daemon process itself and holds the lock
// for its whole lifetime. A second daemon that loses the race exits quietly.
func AcquireDaemonLock() (*state.Lock, bool, error) {
	if _, err := state.EnsureDir(); err != nil {
		return nil, false, err
	}
	l, ok, err := state.TryLock(state.LockPath())
	if err != nil || !ok {
		return nil, false, err
	}
	if err := l.WritePID(); err != nil {
		_ = l.Release()
		return nil, false, err
	}
	return l, true, nil
}

// Status reports whether a daemon is running and its pid.
func Status() (bool, int) {
	if !running() {
		return false, 0
	}
	return true, state.ReadPID(state.LockPath())
}
