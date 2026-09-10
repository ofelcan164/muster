// Package install writes Muster's keybindings into herdr's config.
//
// This is the only place Muster touches a file the user owns, so it backs the
// file up first, writes a clearly marked block it can find again, and validates
// the result with herdr's own checker rather than trusting itself.
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

// Bindings is the whole of what Muster binds globally: one letter and its two
// chords. You learn one key and derive the other two.
//
// prefix+m opens the "open" action rather than the pane entrypoint directly,
// because a plugin_action cannot address a [[panes]] entrypoint. Verified
// against 0.8.2: invoking the pane id returns plugin_action_not_found.
var Bindings = []Binding{
	{"prefix+m", "muster.open", "open Muster"},
	{"prefix+shift+m", "muster.jump-orchestrator", "jump straight to the orchestrator"},
	{"prefix+ctrl+m", "muster.back", "back to the previous agent"},
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
type Result struct {
	Path       string
	Backup     string
	Changed    bool
	Diagnostic string
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

// Keys writes the bindings, replacing any block a previous run left behind.
//
// It is safe to run repeatedly: the managed block is found and replaced rather
// than appended, so running it twice does not bind anything twice.
func Keys(herdrBin string) (*Result, error) {
	path := ConfigPath()
	if path == "" {
		return nil, errors.New("cannot locate herdr config")
	}

	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	res := &Result{Path: path}

	body := strings.TrimRight(blockRE.ReplaceAllString(string(existing), "\n"), "\n")
	if strayMarker(body) {
		return nil, fmt.Errorf("%s has an unpaired muster marker: remove the %q line by hand, then run this again", path, beginMarker)
	}
	updated := block() + "\n"
	if body != "" {
		updated = body + "\n\n" + updated
	}
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

func block() string {
	var b strings.Builder
	b.WriteString(beginMarker + "\n")
	b.WriteString("# Written by `muster install`. Re-running it replaces this block.\n")
	for _, bind := range Bindings {
		fmt.Fprintf(&b, "\n# %s\n[[keys.command]]\nkey = %q\ntype = \"plugin_action\"\ncommand = %q\n",
			bind.Why, bind.Key, bind.Command)
	}
	b.WriteString(endMarker)
	return b.String()
}

// Uninstall removes everything Muster put in the config.
//
// The goal is that after unlinking the plugin there is nothing left that only
// makes sense with Muster installed. herdr has no uninstall hook, so this
// cannot run automatically; it exists so that `muster uninstall` genuinely
// returns the config to how it was.
//
// Everything Muster writes goes inside one marked block for exactly this
// reason: removal is then a single deletion rather than a hunt for scattered
// keys that might or might not have been ours.
func Uninstall() (*Result, error) { return Remove() }

// Remove takes the managed block back out, for undoing an install.
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
		// skill in their Claude directory and no way to remove it.
		if os.IsNotExist(err) {
			return res, nil
		}
		return nil, err
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
