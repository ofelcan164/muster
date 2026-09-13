// Package install writes the two things Muster puts outside its own state
// directory: the keybindings in herdr's config, and the reporting skill in each
// agent runtime.
//
// This is the only place Muster touches files the user owns, so it backs the
// config up first, writes a clearly marked block it can find again, validates
// the result with herdr's own checker rather than trusting itself, and removes
// only what it can show it wrote.
package install

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ofelcan164/muster/internal/state"
)

const (
	beginMarker = "# --- muster: managed keybindings, safe to delete ---"
	endMarker   = "# --- end muster ---"
)

// Binding is one key Muster asks for.
type Binding struct {
	Key     string
	Command string
	Why     string
}

// DefaultLetter is the key Muster asks for first, and letters is the order it
// falls back through when the user has already bound one. Nothing distinguishes
// the fallbacks beyond meaning something here: (m)uster, a(g)ent, (u)nit,
// (y)ard.
const DefaultLetter = "m"

var letters = []string{DefaultLetter, "g", "u", "y"}

// letterRE is what --key accepts. One character, because the whole point of the
// scheme is that the three bindings are chords of a single key.
var letterRE = regexp.MustCompile(`^[a-z0-9]$`)

// BindingsFor is the whole of what Muster binds globally: one letter and its
// two chords. You learn one key and derive the other two.
//
// prefix+<letter> opens the "open" action rather than the pane entrypoint
// directly, because a plugin_action cannot address a [[panes]] entrypoint.
// Verified against 0.8.2: invoking the pane id returns plugin_action_not_found.
func BindingsFor(letter string) []Binding {
	return []Binding{
		{"prefix+" + letter, "muster.open", "open Muster"},
		{"prefix+shift+" + letter, "muster.jump-orchestrator", "jump straight to the orchestrator"},
		{"prefix+ctrl+" + letter, "muster.back", "back to the previous agent"},
	}
}

// ConfigPath is herdr's config file, honouring XDG so it can be pointed
// somewhere harmless during testing.
func ConfigPath() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "herdr", "config.toml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "herdr", "config.toml")
}

// Result reports what an install did, so the caller can say so plainly.
//
// Letter and Bindings are here because the key is no longer a constant the
// caller can assume: a user who has already bound prefix+m gets a different one.
type Result struct {
	Path       string
	Backup     string
	Changed    bool
	Diagnostic string
	Letter     string
	Bindings   []Binding

	// Links is the per-harness paths a skill install pointed at the canonical
	// copy, or that an uninstall took away.
	Links []string
}

// blockRE matches one complete managed block. Both markers are required.
//
// Matching a begin marker to the end of the file instead was tried and is
// wrong. It reads "no end marker" as "our own write was cut short", which
// writeAtomic has since made impossible: a rename either lands or it does not,
// so Muster can no longer leave a half-written block on disk. What that
// spelling actually did was delete everything after any line that happened to
// carry our marker text, including a user's own settings.
var blockRE = regexp.MustCompile(`(?s)\n*` + regexp.QuoteMeta(beginMarker) + `.*?` + regexp.QuoteMeta(endMarker) + `\n?`)

// strayMarker reports a marker left over after every complete block has been
// removed: a begin with no end, an end with no begin, or a marker the user
// pasted somewhere themselves. The marker text invites that, since it says
// "safe to delete".
//
// This has to be refused rather than cleaned up. A leftover begin marker makes
// the next run's lazy match span from it to the end marker of the block we are
// about to write, and everything the user had in between disappears. Refusing
// costs one manual edit; guessing costs a config.
func strayMarker(body string) bool {
	return strings.Contains(body, beginMarker) || strings.Contains(body, endMarker)
}

// swallowsStray reports a match that spans a stray begin marker and reaches the
// end marker of a later block. strayMarker cannot catch this on its own: the
// match consumes both markers, so the body it inspects comes back clean while
// every user line between the two has been deleted.
func swallowsStray(existing string) bool {
	for _, m := range blockRE.FindAllString(existing, -1) {
		if strings.Count(m, beginMarker) > 1 {
			return true
		}
	}
	return false
}

// installedKeyRE pulls the letter back out of a block a previous run wrote. It
// matches the plain binding, which BindingsFor emits first.
var installedKeyRE = regexp.MustCompile(`key = "prefix\+([a-z0-9])"`)

// installedLetter reads back the letter already in the config, so a choice made
// with --key survives the startup hook re-running install on every herdr start.
func installedLetter(existing string) string {
	m := installedKeyRE.FindStringSubmatch(blockRE.FindString(existing))
	if m == nil {
		return ""
	}
	return m[1]
}

// compose is the config file this would write for one letter.
func compose(body, letter string) string {
	out := block(letter) + "\n"
	if body != "" {
		out = body + "\n\n" + out
	}
	return out
}

// taken reports whether herdr would disable any of the bindings a letter
// produces, which is what happens when the user has already bound that key.
//
// herdr's own validator is the only thing that knows the answer, and it answers
// only about a config on disk. So the candidate goes into a throwaway copy
// rather than the real file: trying four letters in place would mean rewriting
// the user's config once per guess. `herdr config check` honours
// XDG_CONFIG_HOME, which is what makes the copy possible.
//
// A collision reads as `prefix+m: kept keys.command[0].key, disabled
// keys.command[1].key`. Muster's block is appended last, so the disabled one is
// always Muster's. Exit status is 0 either way, so the text is the signal.
//
// No herdr binary means no answer, and an unanswerable check must not block an
// install. That degrades to the old behaviour: bind the default and let the
// post-write diagnostic report it.
func taken(herdrBin, body, letter string) bool {
	if herdrBin == "" {
		return false
	}
	dir, err := os.MkdirTemp("", "muster-keycheck")
	if err != nil {
		return false
	}
	defer os.RemoveAll(dir)

	path := filepath.Join(dir, "herdr", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false
	}
	if err := os.WriteFile(path, []byte(compose(body, letter)), 0o600); err != nil {
		return false
	}

	// Replace XDG_CONFIG_HOME rather than appending it. Which of two duplicate
	// entries wins is the C library's business, not something to bet a config
	// read on.
	env := os.Environ()
	kept := env[:0]
	for _, kv := range env {
		if !strings.HasPrefix(kv, "XDG_CONFIG_HOME=") {
			kept = append(kept, kv)
		}
	}
	cmd := exec.Command(herdrBin, "config", "check")
	cmd.Env = append(kept, "XDG_CONFIG_HOME="+dir)
	out, _ := cmd.CombinedOutput()

	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, "disabled") {
			continue
		}
		for _, b := range BindingsFor(letter) {
			if strings.HasPrefix(line, b.Key+":") {
				return true
			}
		}
	}
	return false
}

// chooseLetter settles which key Muster binds.
//
// An explicit --key wins, and collides loudly rather than quietly landing
// somewhere else: the user named a key, so substituting one behind their back
// is worse than refusing. Otherwise the letter already in the config is kept,
// then the first candidate herdr will not disable.
//
// The one outcome ruled out is an install that leaves the user with no key at
// all. Without a binding the overlay is only reachable through herdr's action
// menu, which is not something a new user knows to look for, so exhausting the
// candidates is an error naming --key rather than a silent shrug.
func chooseLetter(herdrBin, existing, body, want string) (string, error) {
	if want != "" {
		if !letterRE.MatchString(want) {
			return "", fmt.Errorf("--key %q: want one letter or digit, as in --key g", want)
		}
		if taken(herdrBin, body, want) {
			return "", fmt.Errorf("prefix+%s is already bound in %s: pick another with --key", want, ConfigPath())
		}
		return want, nil
	}
	if cur := installedLetter(existing); cur != "" && !taken(herdrBin, body, cur) {
		return cur, nil
	}
	for _, l := range letters {
		if !taken(herdrBin, body, l) {
			return l, nil
		}
	}
	return "", fmt.Errorf(
		"every key Muster tries (prefix+%s) is already bound in %s: free one, or choose another with `muster install --key <letter>`",
		strings.Join(letters, ", prefix+"), ConfigPath())
}

// Keys writes the bindings, replacing any block a previous run left behind.
//
// It is safe to run repeatedly: the managed block is found and replaced rather
// than appended, so running it twice does not bind anything twice.
//
// letter is the key to bind, or "" to let chooseLetter work it out.
func Keys(herdrBin, letter string) (*Result, error) {
	path := ConfigPath()
	if path == "" {
		return nil, errors.New("cannot locate herdr config")
	}

	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	res := &Result{Path: path}

	if swallowsStray(string(existing)) {
		return nil, fmt.Errorf("%s has a stray muster marker above the managed block: remove the extra %q line by hand, then run this again", path, beginMarker)
	}
	body := strings.TrimRight(blockRE.ReplaceAllString(string(existing), "\n"), "\n")
	if strayMarker(body) {
		return nil, fmt.Errorf("%s has an unpaired muster marker: remove the %q line by hand, then run this again", path, beginMarker)
	}
	letter, err = chooseLetter(herdrBin, string(existing), body, letter)
	if err != nil {
		return nil, err
	}
	res.Letter = letter
	res.Bindings = BindingsFor(letter)

	updated := compose(body, letter)
	if string(existing) == updated {
		return res, nil
	}

	// Back up only when there is something to lose and something is actually
	// about to change. Backing up before the comparison meant every no-op run
	// left another copy beside the user's config.
	//
	// The mode is read once and used for both writes: a config the user
	// chmodded to 0600 must not come back world-readable, and neither must the
	// backup sitting next to it.
	mode := modeOf(path)
	if len(existing) > 0 {
		res.Backup = fmt.Sprintf("%s.muster-backup-%s", path, time.Now().Format("20060102-150405.000"))
		if err := writeAtomic(res.Backup, existing, mode); err != nil {
			return nil, fmt.Errorf("backup: %w", err)
		}
	}
	if err := writeAtomic(path, []byte(updated), mode); err != nil {
		return nil, err
	}
	res.Changed = true

	// Validate with herdr rather than trusting ourselves. A conflicting key is
	// silently disabled rather than rejected, so the only way to know a binding
	// actually took is to ask.
	if herdrBin != "" {
		out, _ := exec.Command(herdrBin, "config", "check").CombinedOutput()
		res.Diagnostic = strings.TrimSpace(string(out))
	}
	return res, nil
}

func block(letter string) string {
	var b strings.Builder
	b.WriteString(beginMarker + "\n")
	b.WriteString("# Written by `muster install`. Re-running it replaces this block.\n")
	for _, bind := range BindingsFor(letter) {
		fmt.Fprintf(&b, "\n# %s\n[[keys.command]]\nkey = %q\ntype = \"plugin_action\"\ncommand = %q\n",
			bind.Why, bind.Key, bind.Command)
	}
	b.WriteString(endMarker)
	return b.String()
}

// Remove takes the managed block back out, for undoing an install.
//
// The goal is that after unlinking the plugin there is nothing left that only
// makes sense with Muster installed. herdr has no uninstall hook, so this
// cannot run automatically; it exists so that `muster uninstall` genuinely
// returns the config to how it was.
//
// Everything Muster writes goes inside one marked block for exactly this
// reason: removal is then a single deletion rather than a hunt for scattered
// keys that might or might not have been ours.
func Remove() (*Result, error) {
	path := ConfigPath()
	if path == "" {
		return nil, errors.New("cannot locate herdr config")
	}
	res := &Result{Path: path}

	existing, err := os.ReadFile(path)
	if err != nil {
		// A config that is not there has nothing of ours in it. Returning the
		// error aborted `muster uninstall` before it reached the reporting
		// skill, so anyone who had not run `muster install` was left with the
		// skill installed and no way to remove it.
		if os.IsNotExist(err) {
			return res, nil
		}
		return nil, err
	}

	// A stray marker above the block would make the removal take the user's
	// lines with it. Leaving the keybindings in place is the lesser harm, so
	// this says what it found and changes nothing.
	if swallowsStray(string(existing)) {
		res.Diagnostic = fmt.Sprintf("left the block in %s: a stray %q line above it would take your own lines with it. Remove that line by hand, then run this again", path, beginMarker)
		return res, nil
	}

	// What counts as a change is what the regex actually removed, not whether a
	// marker is present. Testing for the begin marker alone reported a clean
	// uninstall for a file the block was still in.
	body := blockRE.ReplaceAllString(string(existing), "\n")

	// Uninstall says what it could not do rather than refusing outright: the
	// point of this command is to leave nothing of Muster's behind, so it
	// removes every complete block and reports the leftover instead of
	// stopping with the blocks still in place.
	if strayMarker(body) {
		res.Diagnostic = fmt.Sprintf("left an unpaired muster marker in %s: remove the %q line by hand", path, beginMarker)
	}
	if body == string(existing) {
		return res, nil
	}
	if body = strings.TrimRight(body, "\n"); body != "" {
		body += "\n"
	}

	mode := modeOf(path)
	res.Backup = fmt.Sprintf("%s.muster-backup-%s", path, time.Now().Format("20060102-150405.000"))
	if err := writeAtomic(res.Backup, existing, mode); err != nil {
		return nil, fmt.Errorf("backup: %w", err)
	}
	if err := writeAtomic(path, []byte(body), mode); err != nil {
		return nil, err
	}
	res.Changed = true
	return res, nil
}

// The startup hook binds the keys on its own, so a refusal has to persist
// somewhere or `muster uninstall` undoes itself at the next herdr start. The
// marker is one file in Muster's own state dir, which is the one place Muster
// may write without asking, and its presence is the whole signal.
const optOutFile = "keys.optout"

// OptOutPath is the marker, or "" when there is no state directory to put it
// in. Outside a herdr pane there is nowhere to record a refusal, and a relative
// path would drop the file in whatever directory the command happened to run
// from.
func OptOutPath(stateDir string) string {
	if stateDir == "" {
		return ""
	}
	return filepath.Join(stateDir, optOutFile)
}

// OptedOut reports whether the user has said no to the keybindings.
//
// No state directory reads as no refusal. The only caller that can reach this
// without one is a hand-run command, which is someone asking for the keys.
func OptedOut(stateDir string) bool {
	p := OptOutPath(stateDir)
	if p == "" {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}

// SetOptOut records a refusal, or clears one when the user installs again.
//
// Recording without a state directory is an error rather than a quiet no-op.
// The refusal is the only thing stopping the startup hook from binding the keys
// again, so silently dropping it made `uninstall-keys` run from a plain shell
// look like it worked and undo itself at the next herdr start. Clearing a
// refusal that cannot exist is genuinely nothing to do.
func SetOptOut(stateDir string, on bool) error {
	p := OptOutPath(stateDir)
	if p == "" {
		if on {
			return state.ErrNoStateDir
		}
		return nil
	}
	if !on {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return writeAtomic(p, []byte("muster: keybindings declined\n"), 0o644)
}
