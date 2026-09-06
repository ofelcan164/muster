// Package install writes Muster's keybindings into herdr's config.
//
// This is the only place Muster touches a file the user owns, so it backs the
// file up first, writes a clearly marked block it can find again, and validates
// the result with herdr's own checker rather than trusting itself.
package install

import (
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

var blockRE = regexp.MustCompile(`(?s)\n*` + regexp.QuoteMeta(beginMarker) + `.*?` + regexp.QuoteMeta(endMarker) + `\n?`)

// Keys writes the bindings, replacing any block a previous run left behind.
//
// It is safe to run repeatedly: the managed block is found and replaced rather
// than appended, so running it twice does not bind anything twice.
func Keys(herdrBin string) (*Result, error) {
	path := ConfigPath()
	if path == "" {
		return nil, fmt.Errorf("cannot locate herdr config")
	}

	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	res := &Result{Path: path}

	// Back up before touching it, and only when there is something to lose.
	if len(existing) > 0 {
		res.Backup = fmt.Sprintf("%s.muster-backup-%s", path, time.Now().Format("20060102-150405"))
		if err := os.WriteFile(res.Backup, existing, 0o644); err != nil {
			return nil, fmt.Errorf("backup: %w", err)
		}
	}

	body := blockRE.ReplaceAllString(string(existing), "\n")
	body = strings.TrimRight(body, "\n")
	updated := body + "\n\n" + block() + "\n"

	if string(existing) == updated {
		res.Changed = false
		return res, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
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

// Remove takes the managed block back out, for undoing an install.
func Remove() (*Result, error) {
	path := ConfigPath()
	existing, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	res := &Result{Path: path}
	if !strings.Contains(string(existing), beginMarker) {
		return res, nil
	}
	res.Backup = fmt.Sprintf("%s.muster-backup-%s", path, time.Now().Format("20060102-150405"))
	if err := os.WriteFile(res.Backup, existing, 0o644); err != nil {
		return nil, err
	}
	body := strings.TrimRight(blockRE.ReplaceAllString(string(existing), "\n"), "\n") + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return nil, err
	}
	res.Changed = true
	return res, nil
}
