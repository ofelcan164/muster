package state

import (
	"os"
	"path/filepath"
	"testing"
)

// --ensure decides whether a daemon is running from a second TryLock, so that
// attempt must fail while the first holder lives and succeed once it lets go.
func TestTryLockHeldUntilRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "musterd.lock")
	l, ok, err := TryLock(path)
	if err != nil || !ok {
		t.Fatalf("first TryLock: ok=%v err=%v", ok, err)
	}
	if _, ok, err := TryLock(path); err != nil || ok {
		t.Fatalf("TryLock while held: ok=%v err=%v, want ok=false", ok, err)
	}
	if err := l.Release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	l, ok, err = TryLock(path)
	if err != nil || !ok {
		t.Fatalf("TryLock after release: ok=%v err=%v", ok, err)
	}
	l.Release()
}

func TestPIDRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "musterd.lock")
	if got := ReadPID(path); got != 0 {
		t.Fatalf("ReadPID of a missing file = %d, want 0", got)
	}
	// A longer pid left by an earlier holder must not survive as a suffix.
	if err := os.WriteFile(path, []byte("123456789\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	l, ok, err := TryLock(path)
	if err != nil || !ok {
		t.Fatalf("TryLock: ok=%v err=%v", ok, err)
	}
	defer l.Release()
	if err := l.WritePID(); err != nil {
		t.Fatalf("WritePID: %v", err)
	}
	if got := ReadPID(path); got != os.Getpid() {
		t.Fatalf("ReadPID = %d, want %d", got, os.Getpid())
	}
}
