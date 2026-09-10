package install

import (
	"os"
	"path/filepath"
)

// writeAtomic replaces path in one step, so an interrupted write cannot leave
// the file half-rewritten.
//
// This matters more here than anywhere else in Muster. `muster install` runs as
// a plugin action inside a herdr pane, and herdr kills the pane's whole process
// group when the pane closes. A plain truncate-then-write loses the race badly:
// what is left is the user's herdr config with everything after the cut point
// gone, prefix key included.
//
// state.WriteAtomic is deliberately not reused. That file is a cache the daemon
// rebuilds in about 15ms, so it skips fsync and forces mode 0644. Both are
// wrong for a file the user owns and cannot regenerate.
func writeAtomic(path string, data []byte, mode os.FileMode) error {
	// Replace what a symlink points at, not the symlink. A herdr config linked
	// out of a dotfiles repo (stow, chezmoi, yadm, a bare git checkout) is
	// ordinary, and rename(2) does not follow the link: it would swap the link
	// for a regular file, orphaning the repo's copy at its old contents and
	// hiding every later change from the user's tooling. os.WriteFile, which
	// this replaced, wrote through the link.
	//
	// A dangling link resolves to nothing, so it falls through and gets
	// replaced. That link was already broken.
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, filepath.Base(path)+".muster-tmp*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)

	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	// CreateTemp makes the file 0600, so the mode has to be set explicitly
	// rather than inherited.
	if err := os.Chmod(tmp, mode); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	// Best effort: on Linux this persists the new directory entry, so the
	// rename survives a power cut as well as a kill. On macOS plain fsync stops
	// at the drive's write cache and only F_FULLFSYNC goes further, so there it
	// buys less than it does here. Either way the rename is already atomic for
	// readers, which is the guarantee this function is actually for, so a
	// failure is not worth failing the install over.
	if d, err := os.Open(dir); err == nil {
		d.Sync()
		d.Close()
	}
	return nil
}

// ponytail: no lock around the read-compute-write in Keys and Remove. Two
// installs racing, or an install racing the user's editor, resolve
// last-writer-wins: the file is always valid, but one side's change can be
// lost. Take an flock on the config, the same primitive internal/state already
// uses, if that ever stops being acceptable.

// modeOf returns the permissions to write path with, preserving whatever the
// user set. A config chmodded to 0600 must not come back world-readable.
func modeOf(path string) os.FileMode {
	if fi, err := os.Stat(path); err == nil {
		return fi.Mode().Perm()
	}
	return 0o644
}
