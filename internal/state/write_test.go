package state

import (
	"os"
	"path/filepath"
	"testing"
)

// The second write is shorter than the first, so a write that overwrote in
// place instead of replacing would leave the tail of the first behind.
func TestWriteAtomic(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "not", "yet")
	path := filepath.Join(dir, "snapshot.json")
	for _, want := range []string{`{"v":1,"long":true}`, `{"v":2}`} {
		if err := WriteAtomic(path, []byte(want)); err != nil {
			t.Fatalf("write %s: %v", want, err)
		}
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatalf("read %q, %v; want %q", got, err, want)
		}
	}

	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o644 {
		t.Fatalf("mode %v, want 0644", st.Mode().Perm())
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if len(names) != 1 || names[0] != "snapshot.json" {
		t.Fatalf("dir holds %v, want only snapshot.json", names)
	}
}
