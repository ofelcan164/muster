package install

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// The failure this guards against: `muster install` runs as a plugin action
// inside a herdr pane, and herdr kills the pane's process group when the pane
// closes. A truncate-then-write that loses that race leaves the user's config
// cut in half. A reader must only ever see the whole old file or the whole new
// one.
func TestAConcurrentReaderNeverSeesAHalfWrittenFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	old := bytes.Repeat([]byte("a"), 256*1024)
	fresh := bytes.Repeat([]byte("b"), 256*1024)
	if err := os.WriteFile(path, old, 0o644); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})
	bad := make(chan int, 1)

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			got, err := os.ReadFile(path)
			if err != nil {
				continue // mid-rename is fine, a torn read is not
			}
			if !bytes.Equal(got, old) && !bytes.Equal(got, fresh) {
				select {
				case bad <- len(got):
				default:
				}
				return
			}
		}
	}()

	for i := 0; i < 200; i++ {
		want := fresh
		if i%2 == 1 {
			want = old
		}
		if err := writeAtomic(path, want, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	wg.Wait()

	select {
	case n := <-bad:
		t.Fatalf("a reader saw a %d-byte file, which is neither version", n)
	default:
	}
}

func TestWriteAtomicLeavesNoTemporaryFilesBehind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	for i := 0; i < 5; i++ {
		if err := writeAtomic(path, []byte("hello"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), "muster-tmp") {
			t.Errorf("left a temp file behind: %s", e.Name())
		}
	}
	if len(entries) != 1 {
		t.Errorf("expected one file, found %d", len(entries))
	}
}

// The temp file has to be a sibling of the target. A rename across filesystems
// is not atomic, and os.CreateTemp in the wrong directory is the usual way that
// happens by accident.
func TestWriteAtomicWritesIntoTheTargetDirectory(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "herdr")
	path := filepath.Join(sub, "config.toml")
	if err := writeAtomic(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err) // MkdirAll is part of the contract
	}
	if got := fi.Mode().Perm(); got != 0o600 {
		t.Errorf("mode is %o, want 600: CreateTemp makes 0600 files, so the chmod has to be explicit", got)
	}
}

func TestModeOfDefaultsTo644ForAFileThatIsNotThere(t *testing.T) {
	if got := modeOf(filepath.Join(t.TempDir(), "nope")); got != 0o644 {
		t.Errorf("got %o, want 644", got)
	}
}

// A herdr config symlinked out of a dotfiles repo is ordinary. rename(2) does
// not follow the link, so replacing the path would swap the link for a regular
// file, freeze the repo's copy at its old contents, and hide every later change
// from the user's tooling. os.WriteFile, which writeAtomic replaced, wrote
// through the link.
func TestWriteAtomicWritesThroughASymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "dotfiles", "config.toml")
	if err := os.MkdirAll(filepath.Dir(real), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(real, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "config.toml")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	if err := writeAtomic(link, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	fi, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Error("the symlink was replaced with a regular file, detaching it from the dotfiles repo")
	}
	got, err := os.ReadFile(real)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Errorf("the linked-to file still says %q: the write went somewhere else", got)
	}
}
