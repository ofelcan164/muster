package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setup(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	path := filepath.Join(dir, "herdr", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const userConfig = `onboarding = false

[keys]
prefix = "alt+q"
new_tab = "prefix+c"
`

func TestKeysAddsTheBindings(t *testing.T) {
	path := setup(t, userConfig)
	res, err := Keys("")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("expected a change")
	}
	body, _ := os.ReadFile(path)
	for _, b := range Bindings {
		if !strings.Contains(string(body), b.Key) {
			t.Errorf("missing binding %s", b.Key)
		}
		if !strings.Contains(string(body), b.Command) {
			t.Errorf("missing command %s", b.Command)
		}
	}
	// The user's own config must survive untouched.
	if !strings.Contains(string(body), `prefix = "alt+q"`) {
		t.Error("clobbered the user's prefix")
	}
	if !strings.Contains(string(body), `new_tab = "prefix+c"`) {
		t.Error("clobbered the user's bindings")
	}
}

// Running the installer twice must not bind anything twice. A duplicate key is
// silently disabled by herdr, so a second run could quietly break the first.
func TestKeysIsIdempotent(t *testing.T) {
	path := setup(t, userConfig)
	if _, err := Keys(""); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(path)

	res, err := Keys("")
	if err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(path)

	if string(first) != string(second) {
		t.Errorf("second run changed the file:\n%s", second)
	}
	if res.Changed {
		t.Error("second run should report no change")
	}
	if n := strings.Count(string(second), `key = "prefix+m"`); n != 1 {
		t.Errorf("prefix+m appears %d times, want 1", n)
	}
}

func TestKeysBacksUpFirst(t *testing.T) {
	path := setup(t, userConfig)
	res, err := Keys("")
	if err != nil {
		t.Fatal(err)
	}
	if res.Backup == "" {
		t.Fatal("no backup was taken")
	}
	backup, err := os.ReadFile(res.Backup)
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != userConfig {
		t.Error("the backup does not match what was there before")
	}
	_ = path
}

// An edited block must be replaced wholesale, not merged, or a renamed action
// would linger.
func TestKeysReplacesAnOutdatedBlock(t *testing.T) {
	path := setup(t, userConfig)
	if _, err := Keys(""); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(path)
	stale := strings.Replace(string(body), "muster.open", "muster.home", 1)
	if err := os.WriteFile(path, []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Keys(""); err != nil {
		t.Fatal(err)
	}
	updated, _ := os.ReadFile(path)
	if strings.Contains(string(updated), "muster.home") {
		t.Error("a stale command survived reinstall")
	}
	if !strings.Contains(string(updated), "muster.open") {
		t.Error("the current command is missing")
	}
}

func TestRemoveTakesTheBlockBackOut(t *testing.T) {
	path := setup(t, userConfig)
	if _, err := Keys(""); err != nil {
		t.Fatal(err)
	}
	if _, err := Remove(); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(path)
	if strings.Contains(string(body), "muster") {
		t.Errorf("removal left something behind:\n%s", body)
	}
	if !strings.Contains(string(body), `prefix = "alt+q"`) {
		t.Error("removal took the user's config with it")
	}
}

func TestWorksWithNoExistingConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	res, err := Keys("")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Error("expected a change")
	}
	if res.Backup != "" {
		t.Error("nothing to back up, so no backup should be written")
	}
	body, _ := os.ReadFile(filepath.Join(dir, "herdr", "config.toml"))
	if !strings.Contains(string(body), "prefix+m") {
		t.Error("bindings missing")
	}
}
