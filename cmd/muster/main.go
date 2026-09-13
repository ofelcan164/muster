// Command muster is the overlay and the plugin's action entrypoints.
//
//	muster                     open the overlay: the [[panes]] command
//	muster open                ask herdr to open that pane, for a keybinding
//	muster jump <target>       the orchestrator, or the previous agent
//	muster mark-orchestrator   mark the pane this runs in
//	muster discover            rescan after a workspace or worktree appears
//	muster install | uninstall | install-skill | uninstall-skill
//	muster chain get | set | clear
//
// Bare invocation is the overlay itself, one process per opening rather than
// one per keypress: it runs a tea.Program on the alt screen and lives until q.
// Everything it draws comes from the snapshot musterd leaves in the plugin
// state dir, so drawing never touches the herdr socket. Only acting does.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/ofelcan164/muster/internal/chain"
	"github.com/ofelcan164/muster/internal/daemon"
	"github.com/ofelcan164/muster/internal/herdr"
	"github.com/ofelcan164/muster/internal/install"
	"github.com/ofelcan164/muster/internal/state"
	"github.com/ofelcan164/muster/internal/ui"
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

	case "install-skill":
		os.Exit(cmdInstallSkill())

	case "uninstall-skill":
		res, err := install.RemoveSkill()
		if err != nil {
			fmt.Fprintf(os.Stderr, "muster: %v\n", err)
			os.Exit(1)
		}
		if res.Changed {
			fmt.Printf("removed the reporting skill from %s\n", res.Path)
			for _, l := range res.Links {
				fmt.Printf("  unlinked %s\n", l)
			}
		} else {
			fmt.Println("the reporting skill was not installed")
		}

	case "uninstall-keys":
		res, err := install.Remove()
		if err != nil {
			fmt.Fprintf(os.Stderr, "muster: %v\n", err)
			os.Exit(1)
		}
		// Record the refusal before reporting it. The startup hook binds the
		// keys on its own now, so without this the next herdr start puts back
		// what was just removed.
		if err := install.SetOptOut(state.Dir(), true); err != nil {
			fmt.Fprintf(os.Stderr, "muster: could not record the refusal: %v\n", err)
			fmt.Fprintln(os.Stderr, "the next herdr start will bind the keys again. Run this through herdr, or pass --state-dir.")
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
  muster install [--key <letter>]  start the daemon and install keybindings
  muster install-skill             install the reporting skill for the orchestrator
  muster uninstall-keys            drop the keybindings, keep everything else
  muster uninstall-skill           drop the reporting skill
  muster uninstall [--purge]       remove everything Muster wrote outside itself
  muster discover                  rescan after a workspace or worktree appears

chain spec syntax:
  "contracts > api > web,mobile"   ">" is sequence, "," is parallel

The chain is written by the orchestrator during workflow setup and persists
across sessions. Read it back with "chain get" to confirm or replace it.

The reporting skill teaches the orchestrator to write the task line Muster
shows. Without it agents fall back to their terminal titles.
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
// Focus happens after the program has torn down. When Muster was an overlay
// pane that mattered, since herdr restored the overlay's saved focus on exit.
// A popup restores nothing, but finishing the screen before acting is still the
// order that cannot race.
func runOverlay() int {
	bindKeysQuietly()

	target, err := ui.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "muster: %v\n", err)
		logFailure("%v", err)
		return 1
	}
	if target == "" {
		return 0
	}
	if err := ui.Jump(target); err != nil {
		fmt.Fprintf(os.Stderr, "muster: jump: %v\n", err)
		logFailure("jump to %s: %v", target, err)
		return 1
	}
	return 0
}

// logFailure appends to musterd.log as well as stderr. herdr closes the popup
// the moment this process exits, so stderr alone is a message nobody sees, and
// a jump that failed looked like one that closed the screen for no reason.
//
// A plain O_APPEND open, the same one Ensure uses to hand the daemon its stderr.
// The daemon's rotation does not count these bytes, which only means it rotates
// a line or two late.
func logFailure(format string, args ...any) {
	if state.Dir() == "" {
		return
	}
	f, err := os.OpenFile(state.LogPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	log.New(f, "muster ", log.LstdFlags|log.Lmsgprefix).Printf(format, args...)
}

// bindKeysQuietly is the other half of the startup hook, for a plugin
// installed mid-session.
//
// Startup hooks fire at server start and nowhere else, so `herdr plugin
// install` on a running session leaves the keys unbound until the next restart.
// Opening the overlay is the one thing such a user can still do, through
// herdr's action menu, so that is where the keys get their second chance.
//
// Silent by design, and only here. Everything it could report belongs on a
// screen the overlay is about to take over, and a config Muster cannot write is
// not a reason to refuse to draw. The install action stays the loud version.
// This runs before the alt screen, so a stray write cannot corrupt the frame.
func bindKeysQuietly() {
	if install.OptedOut(state.Dir()) {
		return
	}
	res, err := install.Keys(herdrBin(), "")
	if err != nil || !res.Changed {
		return
	}
	_ = herdr.NewClient("").Call("server.reload_config", struct{}{}, nil)
}

// cmdInstallKeys writes the keybindings and says exactly what it did. This is
// the only file Muster edits that the user owns, so nothing about it is silent.
//
// --auto is the startup hook's spelling: same work, but it honours a refusal
// recorded by --no-keys or by uninstall. Run by hand, install means the user
// asked, so it clears any refusal on its way through.
func cmdInstallKeys(args []string) int {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	skipKeys := fs.Bool("no-keys", false, "skip the keybindings, and stop the startup hook binding them")
	auto := fs.Bool("auto", false, "bind unless a previous --no-keys or uninstall said not to")
	key := fs.String("key", "", "the letter to bind, as in --key g; default picks the first free one")
	_ = fs.Parse(args)

	if *skipKeys {
		if err := install.SetOptOut(state.Dir(), true); err != nil {
			fmt.Fprintf(os.Stderr, "muster install: %v\n", err)
		}
		fmt.Println("daemon ensured; keybindings skipped")
		return 0
	}
	if *auto {
		if install.OptedOut(state.Dir()) {
			fmt.Println("keybindings declined earlier, leaving them alone")
			return 0
		}
	} else if err := install.SetOptOut(state.Dir(), false); err != nil {
		fmt.Fprintf(os.Stderr, "muster install: %v\n", err)
	}

	res, err := install.Keys(herdrBin(), *key)
	if err != nil {
		fmt.Fprintf(os.Stderr, "muster install: %v\n", err)
		return 1
	}
	if !res.Changed {
		fmt.Printf("keybindings already installed on prefix+%s, nothing to do\n", res.Letter)
		return 0
	}

	fmt.Printf("wrote keybindings to %s\n", res.Path)
	if res.Backup != "" {
		fmt.Printf("backup:  %s\n", res.Backup)
	}
	for _, b := range res.Bindings {
		fmt.Printf("  %-16s %s\n", b.Key, b.Why)
	}
	// Say so when the default was not available. Someone who read the README
	// will be pressing prefix+m and getting whatever they bound it to.
	if res.Letter != install.DefaultLetter {
		fmt.Printf("\nprefix+%s was already bound, so Muster took prefix+%s instead.\n",
			install.DefaultLetter, res.Letter)
		fmt.Println("choose a different one with: muster install --key <letter>")
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

// cmdInstallSkill installs the reporting skill into the user's personal skills.
//
// It is a separate command from `muster install` on purpose. Keybindings go in
// herdr's config, which is Muster's business; a skill goes in the user's agent
// directories, which is not, so installing it stays something you ask for.
func cmdInstallSkill() int {
	res, err := install.Skill()
	if err != nil {
		fmt.Fprintf(os.Stderr, "muster install-skill: %v\n", err)
		return 1
	}
	if res.Changed {
		fmt.Printf("installed the reporting skill at %s\n", res.Path)
	} else {
		fmt.Printf("the reporting skill is already current at %s\n", res.Path)
	}
	// Naming the links is the only way to see which runtimes were found. A
	// harness installed after this ran gets nothing until it runs again.
	for _, l := range res.Links {
		fmt.Printf("  linked %s\n", l)
	}
	if len(res.Links) == 0 {
		fmt.Println("\nno agent runtime found to link it into.")
		fmt.Println("expected one of ~/.claude, ~/.codex or ~/.config/opencode")
	}
	fmt.Println("\nit triggers on:")
	fmt.Printf("  %s\n", install.SkillDescription())
	fmt.Println("\nremove it with: muster uninstall-skill")
	return 0
}

// cmdUninstall undoes everything Muster wrote outside its own state directory.
func cmdUninstall(args []string) int {
	fs := flag.NewFlagSet("uninstall", flag.ExitOnError)
	purge := fs.Bool("purge", false, "also delete Muster's learned state")
	_ = fs.Parse(args)

	// A config this cannot read must not stop the rest. The reporting skill
	// lives in ~/.agents/skills and the agent directories linked to it, nowhere
	// near herdr's config, and abandoning it because config.toml is unreadable
	// is how an uninstall strands a file the user has no obvious way to find.
	// Do everything that still can be done, then fail.
	code := 0
	// Same reason as uninstall-keys: the startup hook would otherwise rebind
	// them at the next herdr start. --purge deletes the marker along with the
	// rest of the state dir, which is correct, since purge means the plugin is
	// going away entirely.
	if err := install.SetOptOut(state.Dir(), true); err != nil {
		fmt.Fprintf(os.Stderr, "muster uninstall: could not record the refusal: %v\n", err)
		fmt.Fprintln(os.Stderr, "the next herdr start will bind the keys again. Run this through herdr, or pass --state-dir.")
	}
	res, err := install.Remove()
	switch {
	case err != nil:
		fmt.Fprintf(os.Stderr, "muster uninstall: config: %v\n", err)
		code = 1
	case res.Changed:
		fmt.Printf("removed Muster's block from %s\n", res.Path)
		fmt.Printf("backup: %s\n", res.Backup)
	case res.Diagnostic == "":
		fmt.Println("nothing of Muster's was in the config")
	}
	// An unpaired marker is the one thing uninstall cannot clean up on its own,
	// and it is what makes the next install refuse. Saying nothing here would
	// leave the user with a config that quietly will not take the bindings.
	if err == nil && res.Diagnostic != "" {
		fmt.Fprintf(os.Stderr, "muster uninstall: %s\n", res.Diagnostic)
	}

	if skill, err := install.RemoveSkill(); err != nil {
		fmt.Fprintf(os.Stderr, "muster uninstall: skill: %v\n", err)
		code = 1
	} else if skill.Changed {
		fmt.Printf("removed the reporting skill from %s\n", skill.Path)
		for _, l := range skill.Links {
			fmt.Printf("  unlinked %s\n", l)
		}
	}

	switch dir := state.Dir(); {
	case *purge && dir == "":
		fmt.Fprintln(os.Stderr, "muster uninstall: no state directory to purge")
		code = 1
	case *purge:
		// Stop the daemon first or it writes the directory straight back.
		if err := daemon.Stop(2 * time.Second); err != nil {
			fmt.Fprintf(os.Stderr, "muster uninstall: %v\n", err)
			code = 1
		}
		if err := os.RemoveAll(dir); err != nil {
			fmt.Fprintf(os.Stderr, "muster uninstall: purge state: %v\n", err)
			code = 1
		} else {
			fmt.Printf("removed state at %s\n", dir)
		}
	case dir == "":
		fmt.Println("no state directory was in use")
	default:
		fmt.Printf("state left at %s (--purge removes it)\n", dir)
	}

	reloadConfig()
	fmt.Println("\nunlink the plugin with: herdr plugin unlink muster")
	return code
}

func ensure() {
	if _, err := daemon.Ensure(); err != nil {
		fmt.Fprintf(os.Stderr, "muster: %v\n", err)
		os.Exit(1)
	}
}
