package state

import (
	"path/filepath"
	"testing"
)

// herdr gives every session the same plugin state dir, so the session's own
// directory is what keeps two overlays from drawing one snapshot.
func TestDirFollowsTheSession(t *testing.T) {
	old := override
	override = ""
	t.Cleanup(func() { override = old })

	base := t.TempDir()
	t.Setenv("HERDR_PLUGIN_STATE_DIR", base)
	for session, want := range map[string]string{
		"":        base,
		"default": base,
		"demo":    filepath.Join(base, "sessions", "demo"),
		"..":      "",
		"a/b":     "",
	} {
		t.Setenv("HERDR_SESSION", session)
		if got := Dir(); got != want {
			t.Errorf("HERDR_SESSION=%q: Dir() = %q, want %q", session, got, want)
		}
		if got := BaseDir(); got != base {
			t.Errorf("HERDR_SESSION=%q: BaseDir() = %q, want %q", session, got, base)
		}
	}
}
