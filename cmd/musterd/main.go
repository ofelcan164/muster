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
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ofelcan164/muster/internal/daemon"
	"github.com/ofelcan164/muster/internal/herdr"
	"github.com/ofelcan164/muster/internal/state"
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
	// Resolved now: once a rebuild replaces the file, Linux reports this path
	// with " (deleted)" appended.
	exe, exeErr := os.Executable()

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

	// The daemon's own handle on the log, so it can rotate mid-run. Ensure also
	// points the child's stderr here, which stays as it is: a panic writes once
	// and is exactly what you want in the file.
	//
	// ponytail: stderr keeps pointing at the file it was opened on, so after a
	// rotation a panic trace lands in musterd.log.1 rather than musterd.log. It
	// is still on disk, and the process dies with the panic, so a second
	// rotation cannot follow it. Re-pointing fd 2 needs dup2, which differs per
	// platform; do that if traces ever go missing for real.
	out := io.Writer(os.Stderr)
	if logFile, err := state.OpenLog(); err == nil {
		defer logFile.Close()
		out = logFile
	} else {
		fmt.Fprintf(os.Stderr, "musterd: log: %v, writing to stderr\n", err)
	}

	logger := log.New(out, "musterd ", log.LstdFlags|log.Lmsgprefix)
	logger.Printf("start pid=%d socket=%s state=%s", os.Getpid(), herdr.SocketPath(), state.Dir())

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	d := daemon.New(herdr.NewClient(""), logger)
	err = d.Run(ctx)
	if errors.Is(err, daemon.ErrReplaced) && exeErr == nil {
		_ = lock.Release()
		err = syscall.Exec(exe, os.Args, os.Environ())
		logger.Printf("exec %s: %v", exe, err)
		return 1
	}
	if err != nil && ctx.Err() == nil {
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
