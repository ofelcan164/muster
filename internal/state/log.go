package state

import (
	"os"
	"sync"
)

// maxLogBytes is the size at which the log rotates. One old generation is kept,
// so the log costs at most twice this on disk.
//
// The daemon writes one line per failed reconcile, and a reconcile that fails
// because the server is unreachable happens on the five second tick, so this is
// several days of a permanently broken server. It exists for the case the grace
// timeout does not cover: a server that flaps, reconnecting often enough to
// keep resetting the timer and never letting the daemon exit.
const maxLogBytes = 1 << 20

// LogFile is an append-only writer that rotates instead of growing forever.
type LogFile struct {
	mu   sync.Mutex
	path string
	f    *os.File
	// n is the bytes written so far, seeded from the file's existing size. Kept
	// in hand rather than stat-ed per write, which is a syscall on a path that
	// runs on every reconcile.
	n int64
}

// OpenLog opens the daemon's log for appending.
func OpenLog() (*LogFile, error) {
	if _, err := EnsureDir(); err != nil {
		return nil, err
	}
	l := &LogFile{path: LogPath()}
	if err := l.open(); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *LogFile) open() error {
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	l.f = f
	l.n = 0
	if info, err := f.Stat(); err == nil {
		l.n = info.Size()
	}
	return nil
}

func (l *LogFile) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.n+int64(len(p)) > maxLogBytes {
		// A rotation that fails is not worth losing the line over: keep writing
		// to whatever handle is still open and try again on the next write. The
		// count is deliberately left where it is, since zeroing it meant the
		// retry the comment promises waited for another megabyte.
		_ = l.rotate()
	}
	n, err := l.f.Write(p)
	l.n += int64(n)
	return n, err
}

// rotate moves the current log aside and starts a new one. Only one old
// generation is kept, because the interesting lines are the recent ones: this
// is a log you read after something has just gone wrong.
func (l *LogFile) rotate() error {
	if err := l.f.Close(); err != nil {
		return err
	}
	if err := os.Rename(l.path, l.path+".1"); err != nil {
		// Reopen the original so writes keep landing somewhere.
		_ = l.open()
		return err
	}
	return l.open()
}

func (l *LogFile) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.f.Close()
}
