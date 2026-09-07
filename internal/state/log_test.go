package state

import (
	"os"
	"strings"
	"testing"
)

// The daemon writes a line per failed reconcile and nothing rotated it. A
// server that flaps keeps resetting the grace timer, so the daemon never exits
// and the log never stops growing.
func TestLogRotatesInsteadOfGrowing(t *testing.T) {
	SetDir(t.TempDir())
	t.Cleanup(func() { SetDir("") })

	l, err := OpenLog()
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer l.Close()

	line := []byte(strings.Repeat("x", 1023) + "\n")
	for written := 0; written < 3*maxLogBytes; written += len(line) {
		if _, err := l.Write(line); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	total := int64(0)
	for _, path := range []string{LogPath(), LogPath() + ".1"} {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if info.Size() > maxLogBytes {
			t.Errorf("%s grew to %d bytes, cap is %d", path, info.Size(), maxLogBytes)
		}
		total += info.Size()
	}
	if total > 2*maxLogBytes {
		t.Errorf("log costs %d bytes on disk, expected at most %d", total, 2*maxLogBytes)
	}
	if _, err := os.Stat(LogPath() + ".1"); err != nil {
		t.Errorf("nothing was rotated aside: %v", err)
	}
}

// Reopening has to continue the existing file rather than start counting from
// zero, or a daemon restarted often enough would never rotate.
func TestLogCountsWhatIsAlreadyThere(t *testing.T) {
	SetDir(t.TempDir())
	t.Cleanup(func() { SetDir("") })

	if err := os.WriteFile(LogPath(), []byte(strings.Repeat("y", maxLogBytes)), 0o644); err != nil {
		t.Fatal(err)
	}
	l, err := OpenLog()
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer l.Close()

	if _, err := l.Write([]byte("one more line\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	info, err := os.Stat(LogPath())
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != int64(len("one more line\n")) {
		t.Errorf("log is %d bytes, expected a fresh one holding only the new line", info.Size())
	}
}
