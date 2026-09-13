package state

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"syscall"
)

// Lock is an advisory whole-file flock. The daemon holds one for its entire
// lifetime; --ensure uses a non-blocking attempt on the same path to decide
// whether a daemon is already running.
//
// flock is the right primitive here rather than a pid file: the kernel releases
// it when the holder dies for any reason, so a killed daemon leaves nothing
// stale behind to clean up.
type Lock struct {
	f *os.File
}

// TryLock acquires the lock without blocking. ok is false when another process
// holds it.
func TryLock(path string) (l *Lock, ok bool, err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, false, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if err == syscall.EWOULDBLOCK {
			return nil, false, nil
		}
		return nil, false, err
	}
	return &Lock{f: f}, true, nil
}

// LockBlocking waits for the lock. Used only for the short spawn lock, which is
// held for milliseconds.
func LockBlocking(path string) (*Lock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return &Lock{f: f}, nil
}

// WritePID records the holder's pid in the lock file. Purely informational: the
// lock itself is what establishes ownership.
func (l *Lock) WritePID() error {
	if err := l.f.Truncate(0); err != nil {
		return err
	}
	if _, err := l.f.Seek(0, 0); err != nil {
		return err
	}
	_, err := fmt.Fprintf(l.f, "%d\n", os.Getpid())
	return err
}

func (l *Lock) Release() error {
	if l == nil || l.f == nil {
		return nil
	}
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	return l.f.Close()
}

// ReadPID reports the pid recorded in a lock file, or 0.
func ReadPID(path string) int {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(string(bytes.TrimSpace(b)))
	if err != nil {
		return 0
	}
	return n
}
