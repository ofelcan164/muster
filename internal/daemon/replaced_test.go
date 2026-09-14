package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBinaryWatchWaitsForAStableReplacement(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "musterd")
	if err := os.WriteFile(path, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	w := &binaryWatch{path: path, running: fi}

	if w.replaced() {
		t.Fatal("unchanged binary reported as replaced")
	}

	// A new file renamed over the old one, the way a rebuild lands.
	tmp := filepath.Join(dir, "musterd.tmp")
	if err := os.WriteFile(tmp, []byte("new, half"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
	if w.replaced() {
		t.Fatal("replacement reported on first sighting")
	}

	// Still being written: the size moves between checks.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(" and the rest"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if w.replaced() {
		t.Fatal("replacement reported while still changing")
	}

	if !w.replaced() {
		t.Fatal("stable replacement not reported")
	}

	var nilWatch *binaryWatch
	if nilWatch.replaced() {
		t.Fatal("nil watch reported a replacement")
	}
}
