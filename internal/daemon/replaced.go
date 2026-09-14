package daemon

import (
	"errors"
	"os"
)

// ErrReplaced is Run's return when the daemon's own binary has been rebuilt
// under it, as `herdr plugin install` does to bin/. The caller execs the new one.
var ErrReplaced = errors.New("daemon binary replaced")

type binaryWatch struct {
	path    string
	running os.FileInfo
	pending os.FileInfo
}

func watchBinary() *binaryWatch {
	path, err := os.Executable()
	if err != nil {
		return nil
	}
	fi, err := os.Stat(path)
	if err != nil {
		return nil
	}
	return &binaryWatch{path: path, running: fi}
}

// replaced reports a changed binary only once it has looked the same on two
// checks in a row. A build that copies into place is still writing when first
// seen, and exec'ing half a binary leaves no daemon at all.
func (w *binaryWatch) replaced() bool {
	if w == nil {
		return false
	}
	fi, err := os.Stat(w.path)
	if err != nil || sameFile(fi, w.running) {
		w.pending = nil
		return false
	}
	stable := w.pending != nil && sameFile(fi, w.pending)
	w.pending = fi
	return stable
}

func sameFile(a, b os.FileInfo) bool {
	return os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}
