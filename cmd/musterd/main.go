// Command musterd is the Muster daemon.
//
//	musterd --ensure    start a daemon if one is not running, then exit at once
//	musterd --daemon    run in the foreground as the daemon (started by --ensure)
//	musterd dump        print the current snapshot as text
//	musterd status      report whether a daemon is running
//
// --ensure is what the manifest's [[startup]] hook calls. It is idempotent, so
// it does not matter how many times herdr invokes it.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ofelcan/muster/internal/daemon"
	"github.com/ofelcan/muster/internal/herdr"
	"github.com/ofelcan/muster/internal/state"
)

func main() {
	args := os.Args[1:]

	// --state-dir may lead any command. herdr always supplies the state dir via
	// the environment, so this exists only for running musterd by hand.
	for len(args) > 0 && (args[0] == "--state-dir" || args[0] == "-state-dir") {
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "musterd: --state-dir needs a path")
			os.Exit(2)
		}
		state.SetDir(args[1])
		args = args[2:]
	}
	if len(args) == 0 {
		usage()
		os.Exit(2)
	}

	// Subcommands are dispatched before any flag parsing so that flags may
	// follow the subcommand, as in `musterd dump --json`.
	switch args[0] {
	case "--ensure", "-ensure":
		os.Exit(cmdEnsure())
	case "--daemon", "-daemon":
		os.Exit(cmdDaemon())
	case "dump":
		fs := flag.NewFlagSet("dump", flag.ExitOnError)
		asJSON := fs.Bool("json", false, "print the raw snapshot JSON")
		_ = fs.Parse(args[1:])
		os.Exit(cmdDump(*asJSON))
	case "status":
		os.Exit(cmdStatus())
	case "-h", "--help", "help":
		usage()
		os.Exit(0)
	default:
		fmt.Fprintf(os.Stderr, "musterd: unknown command %q\n", args[0])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `musterd — the Muster daemon

usage:
  musterd [--state-dir DIR] <command>

  musterd --ensure     start a daemon if one is not running, then exit
  musterd --daemon     run as the daemon in the foreground
  musterd dump [--json]  print the current snapshot
  musterd status       report whether a daemon is running

herdr supplies the state directory to plugin commands. --state-dir is only
needed when running musterd by hand outside herdr.
`)
}

func cmdEnsure() int {
	started, err := daemon.Ensure()
	if err != nil {
		fmt.Fprintf(os.Stderr, "musterd --ensure: %v\n", err)
		return 1
	}
	if started {
		fmt.Fprintln(os.Stderr, "musterd: started")
	}
	return 0
}

func cmdDaemon() int {
	lock, ok, err := daemon.AcquireDaemonLock()
	if err != nil {
		fmt.Fprintf(os.Stderr, "musterd: lock: %v\n", err)
		return 1
	}
	if !ok {
		// Another daemon won the race. Exit quietly: this is the expected
		// outcome of a second [[startup]] after `herdr update --handoff`.
		return 0
	}
	defer lock.Release()

	logger := log.New(os.Stderr, "musterd ", log.LstdFlags|log.Lmsgprefix)
	logger.Printf("start pid=%d socket=%s state=%s", os.Getpid(), herdr.SocketPath(), state.Dir())

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	d := daemon.New(herdr.NewClient(""), logger)
	if err := d.Run(ctx); err != nil && ctx.Err() == nil {
		logger.Printf("exit: %v", err)
		return 1
	}
	logger.Printf("stop pid=%d", os.Getpid())
	return 0
}

func cmdDump(asJSON bool) int {
	s, err := daemon.ReadSnapshot()
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "musterd: no snapshot at %s\nis the daemon running? try: musterd --ensure\n", state.SnapshotPath())
			return 1
		}
		fmt.Fprintf(os.Stderr, "musterd dump: %v\n", err)
		return 1
	}
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(s)
		return 0
	}
	daemon.Dump(os.Stdout, s, time.Now())
	return 0
}

func cmdStatus() int {
	running, pid := daemon.Status()
	if !running {
		fmt.Println("musterd: not running")
		return 1
	}
	fmt.Printf("musterd: running pid=%d\n", pid)
	fmt.Printf("  state dir: %s\n", state.Dir())
	fmt.Printf("  snapshot:  %s\n", state.SnapshotPath())
	if st, err := os.Stat(state.SnapshotPath()); err == nil {
		fmt.Printf("  updated:   %s ago\n", time.Since(st.ModTime()).Round(time.Millisecond))
	}
	return 0
}
