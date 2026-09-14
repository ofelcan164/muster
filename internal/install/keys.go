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

	// Badge is whether the tab bar badge is in the config. It is not when there
	// is no state dir to point it at, or no tab_bar_right it could place safely.
	Badge bool
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

// compose is the config file this would write for one letter. ui is a [ui]
// table for the block to carry, or "" when the body has one of its own.
func compose(body, letter, ui string) string {
	out := block(letter, ui) + "\n"
	if body != "" {
		out = body + "\n\n" + out
	}
	return out
}

// The tab bar badge is the one thing Muster writes outside its block. TOML
// allows one [ui] table and one tab_bar_right in it, so a user who has either
// gets the entry inside their own, and removal takes back that exact entry.
//
// herdr runs a tab_bar_right command through /bin/sh on the server, in the
// server's cwd and with none of the plugin env. Checked on 0.9.0 on 2026-09-13
// with a headless session. So the entry names the binary and the state dir by
// absolute path, quoted for sh.
const badgeInterval = 5

var (
	badgeEntryRE = `\{ type = "command", command = "'[^'"\\\n]*' --state-dir '[^'"\\\n]*' badge [a-z0-9]", interval_seconds = \d+ \}`
	// badgeLineRE is the whole line withBadge adds to a [ui] table that had no
	// tab_bar_right, and badgeItemRE the entry it puts at the front of one that did.
	badgeLineRE = regexp.MustCompile(`(?m)\ntab_bar_right = \[` + badgeEntryRE + `\]$`)
	badgeItemRE = regexp.MustCompile(badgeEntryRE + `, `)

	uiHeaderRE = regexp.MustCompile(`(?m)^[ \t]*\[ui\][ \t]*(#.*)?$`)
	// A header is a bare name alone on its line, so a line of a multi-line array
	// such as ["state_icon", "workspace"] does not end the [ui] table early.
	tableHeaderRE = regexp.MustCompile(`(?m)^[ \t]*\[\[?[A-Za-z0-9_.-]+\]\]?[ \t]*(#.*)?$`)
	tabBarRightRE = regexp.MustCompile(`(?m)^[ \t]*tab_bar_right[ \t]*=[ \t]*\[`)
	rootUIKeyRE   = regexp.MustCompile(`(?m)^[ \t]*ui[ \t]*[.=]`)
)

// badgeEntry is the tab_bar_right entry for one letter, or "" when there is no
// state dir to name or a path sh quoting cannot carry.
func badgeEntry(letter string) string {
	cmd, err := musterCommand()
	if err != nil {
		return ""
	}
	return fmt.Sprintf(`{ type = "command", command = "%s badge %s", interval_seconds = %d }`,
		cmd, letter, badgeInterval)
}

// musterCommand is this binary and the state dir it runs against, absolute and
// single-quoted for sh. It is for text that runs with none of the plugin env:
// herdr's tab bar, and an orchestrator following the reporting skill. The base
// dir, since one tab bar entry and one skill serve every session, and the tab
// bar or pane that runs them names its own session in HERDR_SESSION.
func musterCommand() (string, error) {
	dir := state.BaseDir()
	if dir == "" {
		return "", state.ErrNoStateDir
	}
	bin, err := os.Executable()
	if err != nil {
		return "", err
	}
	if dir, err = filepath.Abs(dir); err != nil {
		return "", err
	}
	if strings.ContainsAny(bin+dir, "'\"\\\n") {
		return "", fmt.Errorf("%s or %s holds a character sh quoting cannot carry", bin, dir)
	}
	return fmt.Sprintf("'%s' --state-dir '%s'", bin, dir), nil
}

// withBadge puts the badge into a body that has none. It returns the new body,
// and the [ui] table the block has to carry when the body has no [ui] of its own.
//
// A tab_bar_right it cannot place for certain, such as a dotted ui.tab_bar_right
// or one outside the [ui] table it found, gets no badge. A second tab_bar_right
// would be a TOML error, and herdr drops the whole config on one of those.
func withBadge(body, entry string) (string, string) {
	if entry == "" {
		return body, ""
	}
	loc := uiHeaderRE.FindStringIndex(body)
	if loc == nil {
		if rootUIKeyRE.MatchString(body) {
			return body, ""
		}
		return body, "[ui]\ntab_bar_right = [" + entry + "]\n"
	}
	end := loc[1]
	section := body[end:]
	if next := tableHeaderRE.FindStringIndex(section); next != nil {
		section = section[:next[0]]
	}
	if m := tabBarRightRE.FindStringIndex(section); m != nil {
		at := end + m[1]
		return body[:at] + entry + ", " + body[at:], ""
	}
	if tabBarRightRE.MatchString(body) {
		return body, ""
	}
	return body[:end] + "\ntab_bar_right = [" + entry + "]" + body[end:], ""
}

// removeBadge takes back what withBadge added to the body, and nothing else.
func removeBadge(body string) string {
	return badgeItemRE.ReplaceAllString(badgeLineRE.ReplaceAllString(body, ""), "")
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
	if err := os.WriteFile(path, []byte(compose(body, letter, "")), 0o600); err != nil {
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
		"every key Muster tries (prefix+%s) is already bound in %s: free one, or put a free letter in Muster's block there, then run the Install Muster's keybindings action again",
		strings.Join(letters, ", prefix+"), ConfigPath())
}

// Keys writes the bindings and the tab bar badge, replacing whatever a previous
// run left behind.
//
// It is safe to run repeatedly: the managed block and the badge are found and
// replaced rather than appended, so running it twice does not bind anything
// twice.
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
	body = removeBadge(body)
	letter, err = chooseLetter(herdrBin, string(existing), body, letter)
	if err != nil {
		return nil, err
	}
	res.Letter = letter
	res.Bindings = BindingsFor(letter)

	entry := badgeEntry(letter)
	body, ui := withBadge(body, entry)
	res.Badge = entry != "" && strings.Contains(body+ui, entry)
	updated := compose(body, letter, ui)
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

// block is the managed block. description is what herdr's own help lists for a
// custom command; it is the only label [[keys.command]] accepts.
func block(letter, ui string) string {
	var b strings.Builder
	b.WriteString(beginMarker + "\n")
	b.WriteString("# Written by `muster install`. Re-running it replaces this block.\n")
	if ui != "" {
		b.WriteString("\n# what needs you, and the key that opens Muster, in the tab bar\n" + ui)
	}
	for _, bind := range BindingsFor(letter) {
		fmt.Fprintf(&b, "\n# %s\n[[keys.command]]\nkey = %q\ntype = \"plugin_action\"\ncommand = %q\ndescription = %q\n",
			bind.Why, bind.Key, bind.Command, bind.Why)
	}
	b.WriteString(endMarker)
	return b.String()
}

// BlockPresent reports whether the managed block is in herdr's config, and
// which letter it binds. A missing config file reads as absent rather than an
// error: nobody having run install is a state, not a failure.
func BlockPresent() (letter string, present bool, err error) {
	path := ConfigPath()
	if path == "" {
		return "", false, errors.New("cannot locate herdr config")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	if blockRE.FindString(string(body)) == "" {
		return "", false, nil
	}
	return installedLetter(string(body)), true, nil
}

// Remove takes the managed block and the badge back out, for undoing an install.
//
// The goal is that after unlinking the plugin there is nothing left that only
// makes sense with Muster installed. herdr has no uninstall hook, so this
// cannot run automatically; it exists so that `muster uninstall` genuinely
// returns the config to how it was.
//
// Everything Muster writes goes inside one marked block for exactly this
// reason: removal is then a single deletion rather than a hunt for scattered
// keys that might or might not have been ours. The badge is the exception TOML
// forces, and it is written in one exact shape so it can be taken back exactly.
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
	body := removeBadge(blockRE.ReplaceAllString(string(existing), "\n"))

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

// The startup hook binds the keys and writes the reporting skill on its own, so
// a refusal of either has to persist somewhere or uninstalling it undoes itself
// at the next herdr start. Each marker is one file in Muster's own state dir,
// which is the one place Muster may write without asking, and its presence is
// the whole signal.
const (
	optOutFile      = "keys.optout"
	skillOptOutFile = "skill.optout"
)

// OptOutPath is the keys marker, or "" when there is no state directory to put
// it in. Outside a herdr pane there is nowhere to record a refusal, and a
// relative path would drop the file in whatever directory the command happened
// to run from.
func OptOutPath(stateDir string) string { return markerPath(stateDir, optOutFile) }

func markerPath(stateDir, name string) string {
	if stateDir == "" {
		return ""
	}
	return filepath.Join(stateDir, name)
}

// OptedOut reports whether the user has said no to the keybindings.
//
// No state directory reads as no refusal. The only caller that can reach this
// without one is a hand-run command, which is someone asking for the keys.
func OptedOut(stateDir string) bool { return marked(stateDir, optOutFile) }

// SkillOptedOut reports whether the user removed the reporting skill, which the
// startup hook then leaves removed.
func SkillOptedOut(stateDir string) bool { return marked(stateDir, skillOptOutFile) }

func marked(stateDir, name string) bool {
	p := markerPath(stateDir, name)
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
	return setMarker(stateDir, optOutFile, "muster: keybindings declined\n", on)
}

// SetSkillOptOut records or clears a refusal of the reporting skill, the same
// way SetOptOut does for the keys.
func SetSkillOptOut(stateDir string, on bool) error {
	return setMarker(stateDir, skillOptOutFile, "muster: reporting skill declined\n", on)
}

func setMarker(stateDir, name, body string, on bool) error {
	p := markerPath(stateDir, name)
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
	return writeAtomic(p, []byte(body), 0o644)
}
