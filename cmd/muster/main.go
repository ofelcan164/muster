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
		fmt.Fprintln(os.Stderr, "muster: daemon ensured; the reporting skill installer is not built yet")

	case "chain":
		os.Exit(cmdChain(args[1:]))

	case "open":
		// The action a keybinding invokes. It cannot open a pane entrypoint
		// itself, so it asks herdr to.
		if err := ui.OpenPane(); err != nil {
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
		if err := ui.Jump(target); err != nil {
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
  muster install                   start the daemon and install reporting
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

func ensure() {
	if _, err := daemon.Ensure(); err != nil {
		fmt.Fprintf(os.Stderr, "muster: %v\n", err)
		os.Exit(1)
	}
}
