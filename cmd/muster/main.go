// Command muster is the overlay client and the plugin's action entrypoints.
//
// Only the parts the daemon depends on exist so far. The TUI is deliberately
// not built yet: the overlay is worthless until something is watching, so the
// daemon comes first. Prove the data model with `musterd dump`.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/ofelcan/muster/internal/chain"
	"github.com/ofelcan/muster/internal/daemon"
	"github.com/ofelcan/muster/internal/herdr"
	"github.com/ofelcan/muster/internal/install"
	"github.com/ofelcan/muster/internal/state"
	"github.com/ofelcan/muster/internal/ui"
)

func main() {
	args := os.Args[1:]
	for len(args) > 0 && (args[0] == "--state-dir" || args[0] == "-state-dir") {
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "muster: --state-dir needs a path")
			os.Exit(2)
		}
		state.SetDir(args[1])
		args = args[2:]
	}

	cmd := ""
	if len(args) > 0 {
		cmd = args[0]
	}

	switch cmd {
	case "discover":
		// Called by the [[events]] hooks for workspace.created and
		// worktree.created, which both mean "a repo may have appeared".
		// Making sure the daemon is up is the whole job: it rediscovers on
		// every reconcile and will have seen the same event itself.
		ensure()

	case "install":
		// Startup hooks do not fire when a plugin is linked mid-session, so the
		// install action has to start the daemon itself. Otherwise linking
		// Muster appears to do nothing until herdr is next restarted.
		ensure()
		os.Exit(cmdInstallKeys(args[1:]))

	case "uninstall":
		os.Exit(cmdUninstall(args[1:]))

	case "uninstall-keys":
		res, err := install.Remove()
		if err != nil {
			fmt.Fprintf(os.Stderr, "muster: %v\n", err)
			os.Exit(1)
		}
		if res.Changed {
			fmt.Printf("removed Muster keybindings from %s\n", res.Path)
			fmt.Printf("backup: %s\n", res.Backup)
			reloadConfig()
		} else {
			fmt.Println("no Muster keybindings were installed")
		}

	case "mark-orchestrator":
		if err := ui.MarkOrchestrator(); err != nil {
			fmt.Fprintf(os.Stderr, "muster: %v\n", err)
			os.Exit(1)
		}

	case "chain":
		os.Exit(cmdChain(args[1:]))

	case "open":
		// The action a keybinding invokes. It cannot open a pane entrypoint
		// itself, so it asks herdr to.
		if err := ui.TogglePane(); err != nil {
			fmt.Fprintf(os.Stderr, "muster open: %v\n", err)
			os.Exit(1)
		}

	case "jump":
		target := ""
		if len(args) > 1 {
			target = args[1]
		}
		if target == "" {
			fmt.Fprintln(os.Stderr, "muster jump: needs a target")
			os.Exit(2)
		}
		pane, err := ui.ResolveTarget(target)
		if err != nil {
			fmt.Fprintf(os.Stderr, "muster jump: %v\n", err)
			os.Exit(1)
		}
		if err := ui.Jump(pane); err != nil {
			fmt.Fprintf(os.Stderr, "muster jump: %v\n", err)
			os.Exit(1)
		}

	case "":
		// No arguments means the [[panes]] entrypoint: this process is the
		// overlay.
		os.Exit(runOverlay())

	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `muster — the Muster client

usage:
  muster chain get [--json]        print the recorded dependency order
  muster chain set <spec> [--independent a,b] [--by NAME]
  muster chain clear
  muster install                   start the daemon and install keybindings
  muster uninstall [--purge]       remove everything Muster wrote outside itself
  muster discover                  rescan after a workspace or worktree appears

chain spec syntax:
  "contracts > api > web,mobile"   ">" is sequence, "," is parallel

The chain is written by the orchestrator during workflow setup and persists
across sessions. Read it back with "chain get" to confirm or replace it.

The overlay is not built yet. Use "musterd dump" to see the current state.
`)
}

func cmdChain(args []string) int {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	dir := state.Dir()
	if dir == "" {
		fmt.Fprintln(os.Stderr, "muster: no state directory: run through herdr, or pass --state-dir")
		return 1
	}

	switch sub {
	case "get":
		fs := flag.NewFlagSet("chain get", flag.ExitOnError)
		asJSON := fs.Bool("json", false, "print raw JSON")
		_ = fs.Parse(args[1:])

		c := chain.Load(dir)
		if *asJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			_ = enc.Encode(c)
			return 0
		}
		if c.Empty() {
			fmt.Println("no chain recorded")
			return 0
		}
		fmt.Printf("order:       %s\n", chain.Format(c.Stages))
		if len(c.Independent) > 0 {
			fmt.Printf("independent: %v\n", c.Independent)
		}
		if c.SetBy != "" {
			fmt.Printf("set by:      %s\n", c.SetBy)
		}
		if !c.SetAt.IsZero() {
			fmt.Printf("set at:      %s\n", c.SetAt.Format("2006-01-02 15:04:05"))
		}
		return 0

	case "set":
		fs := flag.NewFlagSet("chain set", flag.ExitOnError)
		independent := fs.String("independent", "", "comma-separated repos outside the order")
		by := fs.String("by", "", "who is recording this")

		// Go's flag parser stops at the first positional, and the spec is a
		// positional, so flags after it would be silently dropped. Parse in a
		// loop so `chain set "a > b" --by x` works in the order anyone would
		// naturally type it.
		var spec string
		rest := args[1:]
		for {
			if err := fs.Parse(rest); err != nil {
				return 2
			}
			if fs.NArg() == 0 {
				break
			}
			if spec == "" {
				spec = fs.Arg(0)
			}
			rest = fs.Args()[1:]
		}
		stages := chain.Parse(spec)
		if len(stages) == 0 && *independent == "" {
			fmt.Fprintln(os.Stderr, `muster chain set: nothing to record`)
			fmt.Fprintln(os.Stderr, `example: muster chain set "contracts > api > web,mobile" --independent infra`)
			return 2
		}
		c := &chain.Chain{
			Stages:      stages,
			Independent: chain.ParseList(*independent),
			SetBy:       *by,
		}
		if c.Independent == nil {
			c.Independent = []string{}
		}
		if err := chain.Save(dir, c, state.WriteAtomic); err != nil {
			fmt.Fprintf(os.Stderr, "muster chain set: %v\n", err)
			return 1
		}
		fmt.Printf("chain recorded: %s\n", chain.Format(c.Stages))
		return 0

	case "clear":
		if err := os.Remove(chain.Path(dir)); err != nil && !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "muster chain clear: %v\n", err)
			return 1
		}
		fmt.Println("chain cleared")
		return 0

	default:
		usage()
		return 2
	}
}

// runOverlay draws the screen, then focuses whatever was chosen.
//
// Focus happens after the program has torn down, because the overlay restoring
// its own focus on exit would otherwise fight an outbound focus call.
func runOverlay() int {
	target, err := ui.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "muster: %v\n", err)
		return 1
	}
	if target == "" {
		return 0
	}
	if err := ui.Jump(target); err != nil {
		fmt.Fprintf(os.Stderr, "muster: jump: %v\n", err)
		return 1
	}
	return 0
}

// cmdInstallKeys writes the keybindings and says exactly what it did. This is
// the only file Muster edits that the user owns, so nothing about it is silent.
func cmdInstallKeys(args []string) int {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	skipKeys := fs.Bool("no-keys", false, "skip the keybindings")
	_ = fs.Parse(args)

	if *skipKeys {
		fmt.Println("daemon ensured; keybindings skipped")
		return 0
	}

	res, err := install.Keys(herdrBin())
	if err != nil {
		fmt.Fprintf(os.Stderr, "muster install: %v\n", err)
		return 1
	}
	if !res.Changed {
		fmt.Println("keybindings already installed, nothing to do")
		return 0
	}

	fmt.Printf("wrote keybindings to %s\n", res.Path)
	if res.Backup != "" {
		fmt.Printf("backup:  %s\n", res.Backup)
	}
	for _, b := range install.Bindings {
		fmt.Printf("  %-16s %s\n", b.Key, b.Why)
	}

	// A conflicting key is disabled rather than rejected, so the diagnostic is
	// the only way to find out a binding did not actually take.
	if res.Diagnostic != "" && res.Diagnostic != "config: ok" {
		fmt.Printf("\nherdr config check:\n%s\n", res.Diagnostic)
	}
	reloadConfig()
	return 0
}

// reloadConfig makes the bindings live without restarting herdr.
func reloadConfig() {
	c := herdr.NewClient("")
	if err := c.Call("server.reload_config", struct{}{}, nil); err != nil {
		fmt.Fprintf(os.Stderr, "reload config: %v (restart herdr to pick them up)\n", err)
		return
	}
	fmt.Println("config reloaded")
}

// herdrBin honours the path herdr injects, never a bare `herdr` off PATH.
func herdrBin() string { return os.Getenv("HERDR_BIN_PATH") }

// cmdUninstall undoes everything Muster wrote outside its own state directory.
func cmdUninstall(args []string) int {
	fs := flag.NewFlagSet("uninstall", flag.ExitOnError)
	purge := fs.Bool("purge", false, "also delete Muster's learned state")
	_ = fs.Parse(args)

	res, err := install.Uninstall()
	if err != nil {
		fmt.Fprintf(os.Stderr, "muster uninstall: %v\n", err)
		return 1
	}
	if res.Changed {
		fmt.Printf("removed Muster's block from %s\n", res.Path)
		fmt.Printf("backup: %s\n", res.Backup)
	} else {
		fmt.Println("nothing of Muster's was in the config")
	}

	if *purge {
		dir := state.Dir()
		if dir == "" {
			fmt.Fprintln(os.Stderr, "no state directory to purge")
		} else if err := os.RemoveAll(dir); err != nil {
			fmt.Fprintf(os.Stderr, "purge state: %v\n", err)
		} else {
			fmt.Printf("removed state at %s\n", dir)
		}
	} else {
		fmt.Printf("state left at %s (--purge removes it)\n", state.Dir())
	}

	reloadConfig()
	fmt.Println("\nunlink the plugin with: herdr plugin unlink muster")
	return 0
}

func ensure() {
	if _, err := daemon.Ensure(); err != nil {
		fmt.Fprintf(os.Stderr, "muster: %v\n", err)
		os.Exit(1)
	}
}
