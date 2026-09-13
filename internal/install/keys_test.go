package install

import (
	"fmt"
	"os"
	"os/exec"
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
	res, err := Keys("", "")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("expected a change")
	}
	body, _ := os.ReadFile(path)
	for _, b := range res.Bindings {
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
	if _, err := Keys("", ""); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(path)

	res, err := Keys("", "")
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
	setup(t, userConfig)
	res, err := Keys("", "")
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
}

// An edited block must be replaced wholesale, not merged, or a renamed action
// would linger.
func TestKeysReplacesAnOutdatedBlock(t *testing.T) {
	path := setup(t, userConfig)
	if _, err := Keys("", ""); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(path)
	stale := strings.Replace(string(body), "muster.open", "muster.home", 1)
	if err := os.WriteFile(path, []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Keys("", ""); err != nil {
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
	if _, err := Keys("", ""); err != nil {
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
	res, err := Keys("", "")
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

// After uninstalling there must be nothing left that only makes sense with
// Muster installed. herdr has no uninstall hook, so this command is the only
// thing that can make that true.
func TestUninstallLeavesNothingMusterSpecific(t *testing.T) {
	path := setup(t, userConfig)
	if _, err := Keys("", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := Uninstall(); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ToLower(string(body))
	for _, trace := range []string{"muster", "plugin_action", "keys.command"} {
		if strings.Contains(text, trace) {
			t.Errorf("uninstall left %q behind:\n%s", trace, body)
		}
	}
	if string(body) != userConfig {
		t.Errorf("config did not return to its original content:\ngot:\n%s\nwant:\n%s", body, userConfig)
	}
}

// Uninstalling twice is not an error and does not keep taking backups of an
// already-clean file.
func TestUninstallIsIdempotent(t *testing.T) {
	setup(t, userConfig)
	if _, err := Keys("", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := Uninstall(); err != nil {
		t.Fatal(err)
	}
	res, err := Uninstall()
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Error("second uninstall reported a change")
	}
	if res.Backup != "" {
		t.Error("second uninstall took an unnecessary backup")
	}
}

// The attack this guards against, found by adversarial review: a user pastes
// the marker into a comment as a note to themselves. The marker text invites
// it, since it says "safe to delete".
//
// Matching a lone begin marker to the end of the file deleted everything below
// it on the first install. Requiring both markers only moved the loss to the
// second install, where the lazy match ran from the user's stray marker to the
// end marker of the block the first run had added, taking their settings with
// it. Refusing is the only answer that keeps the file.
func TestKeysRefusesAConfigWithAStrayMarker(t *testing.T) {
	original := "onboarding = false\n\n" +
		"# for reference, muster install adds a block starting with:\n" +
		beginMarker + "\n\n" +
		"[keys]\nprefix = \"alt+q\"\nnew_tab = \"prefix+c\"\n"
	path := setup(t, original)

	if _, err := Keys("", ""); err == nil {
		t.Fatal("installed over an unpaired marker instead of refusing")
	}
	body, _ := os.ReadFile(path)
	if string(body) != original {
		t.Errorf("a refused install still wrote to the config:\ngot:\n%s\nwant:\n%s", body, original)
	}
}

// The same guard from the other side: an end marker with no begin.
func TestKeysRefusesAStrayEndMarker(t *testing.T) {
	path := setup(t, userConfig+"\n"+endMarker+"\n")
	if _, err := Keys("", ""); err == nil {
		t.Fatal("expected a refusal")
	}
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), `prefix = "alt+q"`) {
		t.Error("a refused install took the user's config with it")
	}
}

// Running twice must stay safe even after a stray marker appears between runs.
func TestKeysRefusesAStrayMarkerAddedAfterInstalling(t *testing.T) {
	path := setup(t, userConfig)
	if _, err := Keys("", ""); err != nil {
		t.Fatal(err)
	}
	installed, _ := os.ReadFile(path)
	tampered := string(installed) + "\n# note to self:\n" + beginMarker + "\nkeep_me = true\n"
	if err := os.WriteFile(path, []byte(tampered), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Keys("", ""); err == nil {
		t.Fatal("expected a refusal")
	}
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), "keep_me = true") {
		t.Errorf("the second run ate the settings below the stray marker:\n%s", body)
	}
}

// The stray marker above the block is the dangerous side. The match starts at
// the stray one and runs to the real end marker, so everything the user has in
// between is inside the match, and the body left afterwards holds no marker at
// all for strayMarker to catch.
func TestKeysRefusesAStrayMarkerAboveTheBlock(t *testing.T) {
	path := setup(t, userConfig)
	if _, err := Keys("", ""); err != nil {
		t.Fatal(err)
	}
	installed, _ := os.ReadFile(path)
	tampered := beginMarker + "\nkeep_me = true\n\n" + string(installed)
	if err := os.WriteFile(path, []byte(tampered), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Keys("", ""); err == nil {
		t.Fatal("installed across a stray marker instead of refusing")
	}
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), "keep_me = true") {
		t.Errorf("the run ate the settings under the stray marker:\n%s", body)
	}

	res, err := Remove()
	if err != nil {
		t.Fatal(err)
	}
	if res.Diagnostic == "" {
		t.Error("uninstall said nothing about the stray marker")
	}
	body, _ = os.ReadFile(path)
	if !strings.Contains(string(body), "keep_me = true") {
		t.Errorf("uninstall ate the settings under the stray marker:\n%s", body)
	}
}

// Uninstall does not refuse, because its whole job is to leave nothing behind.
// It removes every complete block and says what it could not resolve.
func TestRemoveReportsAStrayMarkerRatherThanGuessing(t *testing.T) {
	path := setup(t, userConfig+"\n"+beginMarker+"\nkeep_me = true\n")

	res, err := Remove()
	if err != nil {
		t.Fatal(err)
	}
	if res.Diagnostic == "" {
		t.Error("removed nothing and said nothing about why")
	}
	if res.Changed {
		t.Error("reported a change to a file it did not write")
	}
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), "keep_me = true") {
		t.Errorf("uninstall deleted content below an unpaired marker:\n%s", body)
	}
}

// A user who never ran `muster install` still has to be able to uninstall: the
// error here aborted the command before it reached the reporting skill.
func TestRemoveWithNoConfigIsNotAnError(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	res, err := Remove()
	if err != nil {
		t.Fatalf("uninstall failed on a machine with no herdr config: %v", err)
	}
	if res.Changed {
		t.Error("reported a change to a file that does not exist")
	}
}

// A config the user chmodded to 0600 must not come back world-readable, and
// neither must the backup left sitting next to it.
//
// 0640 on purpose, not 0600: os.CreateTemp already makes its files 0600, so a
// test that asks for 0600 passes whether the chmod ran or not. Mutation
// testing caught that, with the chmod deleted and every test still green.
func TestKeysPreservesTheFileMode(t *testing.T) {
	for _, mode := range []os.FileMode{0o600, 0o640, 0o644} {
		t.Run(fmt.Sprintf("%o", mode), func(t *testing.T) {
			path := setup(t, userConfig)
			if err := os.Chmod(path, mode); err != nil {
				t.Fatal(err)
			}
			res, err := Keys("", "")
			if err != nil {
				t.Fatal(err)
			}
			fi, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if got := fi.Mode().Perm(); got != mode {
				t.Errorf("config is now %o, want %o", got, mode)
			}
			bi, err := os.Stat(res.Backup)
			if err != nil {
				t.Fatal(err)
			}
			if got := bi.Mode().Perm(); got != mode {
				t.Errorf("backup is %o, want %o: it holds the same content as the config", got, mode)
			}
		})
	}
}

// Uninstall rewrites the config too, and nothing covered its mode until
// mutation testing isolated that write site on its own.
func TestRemovePreservesTheFileMode(t *testing.T) {
	path := setup(t, userConfig)
	if _, err := Keys("", ""); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	res, err := Remove()
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("expected a change")
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != 0o640 {
		t.Errorf("config is now %o, want 640", got)
	}
	bi, err := os.Stat(res.Backup)
	if err != nil {
		t.Fatal(err)
	}
	if got := bi.Mode().Perm(); got != 0o640 {
		t.Errorf("backup is %o, want 640", got)
	}
}

// Backing up before comparing meant N runs left N copies of the config lying
// around. The one-second timestamp resolution is why the idempotency test did
// not catch it: two runs in the same second overwrite the same backup name.
func TestKeysDoesNotBackUpWhenNothingChanges(t *testing.T) {
	path := setup(t, userConfig)
	if _, err := Keys("", ""); err != nil {
		t.Fatal(err)
	}
	res, err := Keys("", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Backup != "" {
		t.Errorf("a no-op run took a backup: %s", res.Backup)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	var backups int
	for _, e := range entries {
		if strings.Contains(e.Name(), "muster-backup") {
			backups++
		}
	}
	if backups != 1 {
		t.Errorf("found %d backups after two runs, want 1", backups)
	}
}

// TestOptOut covers the marker that keeps an uninstall uninstalled: the startup
// hook runs `install --auto` on every herdr start, so a refusal that did not
// persist would be undone the next morning.
func TestOptOut(t *testing.T) {
	dir := t.TempDir()

	if OptedOut(dir) {
		t.Fatal("a fresh state dir is not a refusal")
	}
	if err := SetOptOut(dir, true); err != nil {
		t.Fatal(err)
	}
	if !OptedOut(dir) {
		t.Fatal("refusal did not stick")
	}
	// Setting it again must not fail: uninstall can be run twice.
	if err := SetOptOut(dir, true); err != nil {
		t.Fatal(err)
	}
	if err := SetOptOut(dir, false); err != nil {
		t.Fatal(err)
	}
	if OptedOut(dir) {
		t.Fatal("install did not clear the refusal")
	}
	// Clearing one that was never there is what a first install does.
	if err := SetOptOut(dir, false); err != nil {
		t.Fatal(err)
	}

	// No state directory means nowhere to record a refusal, and no relative
	// path dropped in whatever directory the command ran from.
	if p := OptOutPath(""); p != "" {
		t.Fatalf("OptOutPath(%q) = %q, want empty", "", p)
	}
	if OptedOut("") {
		t.Fatal("no state dir must not read as a refusal")
	}
	// Recording a refusal with nowhere to record it has to be an error. Silence
	// made `uninstall-keys` outside herdr look like it worked, and the startup
	// hook put the keys straight back at the next start.
	if err := SetOptOut("", true); err == nil {
		t.Fatal("recording a refusal with no state dir reported success")
	}
	if err := SetOptOut("", false); err != nil {
		t.Fatalf("clearing a refusal that cannot exist: %v", err)
	}
}

// herdrBin is the real herdr, or a skip. Conflict detection has no meaning
// without it: herdr's validator is the only thing that knows whether a key
// collides, and faking it would only test the fake.
func herdrBin(t *testing.T) string {
	t.Helper()
	bin, err := exec.LookPath("herdr")
	if err != nil {
		t.Skip("herdr not on PATH")
	}
	return bin
}

// conflictConfig already binds prefix+m, which is the key Muster wants first.
const conflictConfig = `onboarding = false

[keys]
prefix = "alt+q"

[[keys.command]]
key = "prefix+m"
type = "shell"
command = "echo mine"
`

// A user who has taken prefix+m must still end up with a working key. Landing
// on nothing is the one outcome ruled out: without a binding the overlay is
// reachable only through herdr's action menu, which no new user goes looking
// for.
func TestKeysFallsBackWhenTheDefaultIsTaken(t *testing.T) {
	bin := herdrBin(t)
	path := setup(t, conflictConfig)

	res, err := Keys(bin, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Letter == DefaultLetter {
		t.Fatalf("took prefix+%s, which the user had already bound", DefaultLetter)
	}
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), "key = \"prefix+"+res.Letter+"\"") {
		t.Errorf("config has no prefix+%s binding", res.Letter)
	}
	// The user's own binding survives, and stays the one herdr keeps.
	if !strings.Contains(string(body), "echo mine") {
		t.Error("clobbered the user's own prefix+m binding")
	}
}

// An explicit --key is a decision, not a hint. Silently binding something else
// would leave the user pressing a key that does nothing.
func TestExplicitKeyRefusesAConflict(t *testing.T) {
	bin := herdrBin(t)
	setup(t, conflictConfig)

	if _, err := Keys(bin, DefaultLetter); err == nil {
		t.Fatal("expected --key m to be refused: the user already bound it")
	}
	if _, err := Keys(bin, "zz"); err == nil {
		t.Fatal("expected a two-character key to be refused")
	}
}

// The startup hook re-runs install on every herdr start with no --key, so a
// choice that did not survive that would last until the next reboot.
func TestExplicitKeySurvivesALaterAutoRun(t *testing.T) {
	bin := herdrBin(t)
	setup(t, userConfig)

	if _, err := Keys(bin, "y"); err != nil {
		t.Fatal(err)
	}
	res, err := Keys(bin, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Letter != "y" {
		t.Fatalf("letter = %q, want the y chosen earlier", res.Letter)
	}
	if res.Changed {
		t.Error("rerun rewrote the config for no reason")
	}
}

// Every candidate bound is the case where Muster cannot do its job. It has to
// say so, not write a block herdr will disable and call that an install.
func TestKeysFailsWhenEveryCandidateIsTaken(t *testing.T) {
	bin := herdrBin(t)
	var b strings.Builder
	b.WriteString("[keys]\nprefix = \"alt+q\"\n")
	for _, l := range letters {
		fmt.Fprintf(&b, "\n[[keys.command]]\nkey = %q\ntype = \"shell\"\ncommand = \"echo mine\"\n", "prefix+"+l)
	}
	path := setup(t, b.String())
	before, _ := os.ReadFile(path)

	if _, err := Keys(bin, ""); err == nil {
		t.Fatal("expected an error when every candidate is bound")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Error("a failed install still wrote to the user's config")
	}
}
