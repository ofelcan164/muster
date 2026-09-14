// Package doctor checks Muster's health and fixes it with permission.
//
// It exists because a stranded daemon fails in ways that look like a broken
// overlay: deleting the checkout under a running daemon leaves its self-restart
// watch statting a path that no longer exists, so it never fires and the old
// daemon keeps the lock through every reinstall. Every fix here only stops or
// starts musterd, never a file, and Run prints each one before applying it.
package doctor

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ofelcan164/muster/internal/daemon"
	"github.com/ofelcan164/muster/internal/install"
	"github.com/ofelcan164/muster/internal/model"
	"github.com/ofelcan164/muster/internal/state"
)

// States of the binary a running daemon was started from, read from /proc.
const (
	exeOK = "ok"
	// exeReplaced is a daemon rebuilt under it. go build unlinks the old file,
	// so /proc reads "(deleted)" for the few seconds before the daemon's own
	// watch execs the new one. Every reinstall looks exactly like this.
	exeReplaced = "replaced"
	// exeDeleted is a daemon whose binary is gone with nothing at its path,
	// usually because the checkout was removed. Its watch can never fire.
	exeDeleted = "deleted"
	// exeElsewhere is a daemon started from another copy of Muster than the
	// one running doctor. Which copy herdr runs cannot be told from here, so
	// doctor reports it and never swaps one daemon for the other.
	exeElsewhere = "elsewhere"
	// exeUnknown is anywhere /proc cannot be read, which is every non-Linux
	// platform.
	exeUnknown = "unknown"
)

// Options tunes Run.
type Options struct {
	// Dir is the state directory, already resolved from HERDR_PLUGIN_STATE_DIR
	// or --state-dir before Run is called.
	Dir string
	// Yes applies the fixes without asking.
	Yes bool
	// CanPrompt is whether stdin is a terminal. Without one there is nobody to
	// answer, so Run diagnoses and stops unless Yes is set.
	CanPrompt bool
}

type finding struct {
	bad  bool
	text string
}

// fixKind is what Run knows how to apply, in the order check lists them.
type fixKind int

const (
	fixStop fixKind = iota
	fixStart
)

type fix struct {
	kind  fixKind
	title string
}

// report is what check found: findings to show, advice that needs a human,
// and fixes Run can apply with permission.
type report struct {
	findings []finding
	advice   []string
	fixes    []fix
}

// deps is everything check reads, as data so tests can build any state
// without processes or files.
type deps struct {
	Dir             string
	Now             time.Time
	DaemonRunning   bool
	DaemonPID       int
	Exe             string
	ExePath         string // the binary the running daemon was started from
	Musterd         string // the binary a start would run, beside this muster
	KeysLetter      string
	KeysPresent     bool
	KeysErr         error
	SkillInstalled  bool
	OptedOut        bool
	Snapshot        *model.Snapshot
	SnapshotErr     error
	SnapshotMissing bool
}

// runner applies fixes, injected so tests never signal processes.
type runner struct {
	stop  func(wait time.Duration) error
	start func() error
}

// check maps deps onto findings, advice, and an ordered fix list.
func check(d deps) report {
	var r report
	ok := func(format string, args ...any) {
		r.findings = append(r.findings, finding{text: fmt.Sprintf(format, args...)})
	}
	bad := func(format string, args ...any) {
		r.findings = append(r.findings, finding{bad: true, text: fmt.Sprintf(format, args...)})
	}
	advise := func(format string, args ...any) {
		r.advice = append(r.advice, fmt.Sprintf(format, args...))
	}
	// addFix keeps the list free of duplicates while preserving order, so a
	// snapshot that is both stale and orphaned does not stop the daemon twice.
	addFix := func(kind fixKind, title string) {
		for _, f := range r.fixes {
			if f.kind == kind {
				return
			}
		}
		r.fixes = append(r.fixes, fix{kind: kind, title: title})
	}
	stop := fmt.Sprintf("stop daemon pid=%d", d.DaemonPID)
	start := "start the daemon"
	if d.Musterd != "" {
		start += " from " + d.Musterd
	}

	// The daemon first, since every snapshot finding reads differently
	// depending on whether something is writing it.
	switch {
	case !d.DaemonRunning:
		bad("daemon not running")
		addFix(fixStart, start)
	case d.Exe == exeDeleted:
		bad("daemon pid=%d is orphaned: %s is gone, so it never picks up a reinstall while it holds the lock", d.DaemonPID, d.ExePath)
		addFix(fixStop, stop)
		addFix(fixStart, start)
	case d.Exe == exeElsewhere:
		bad("daemon pid=%d runs %s, not %s", d.DaemonPID, d.ExePath, d.Musterd)
		advise("one of those is not the copy herdr runs: stop the daemon with `kill %d`, then run the Check Muster's health action, which starts herdr's own", d.DaemonPID)
	case d.Exe == exeReplaced:
		ok("daemon pid=%d is restarting onto its rebuilt binary", d.DaemonPID)
	default:
		ok("daemon running (pid %d)", d.DaemonPID)
	}

	// The snapshot: missing, stale, or contradicting itself.
	switch {
	case d.SnapshotMissing:
		bad("no snapshot at %s", d.Dir)
		if d.DaemonRunning {
			advise("a daemon that started seconds ago may not have written one yet; otherwise the reason is in %s", filepath.Join(d.Dir, "musterd.log"))
		}
	case d.SnapshotErr != nil:
		bad("snapshot unreadable: %v", d.SnapshotErr)
		advise("delete %s and reopen the overlay; the daemon rewrites it within seconds", filepath.Join(d.Dir, "snapshot.json"))
	case d.Snapshot != nil:
		age := d.Now.Sub(d.Snapshot.GeneratedAt)
		agents := 0
		for _, repo := range d.Snapshot.Repos {
			agents += len(repo.Agents)
		}
		if age >= model.StaleAfter {
			bad("snapshot is %s old: the daemon is not answering", daemon.CompactDur(age))
			if d.DaemonRunning && d.Exe != exeElsewhere {
				addFix(fixStop, stop)
			}
			if d.Exe != exeElsewhere {
				addFix(fixStart, start)
			}
		} else {
			ok("snapshot %s old: %s, %s", daemon.CompactDur(age),
				daemon.Plural(len(d.Snapshot.Workspaces), "workspace"), daemon.Plural(agents, "agent"))
		}
		if orphans := orphanAgents(d.Snapshot); len(orphans) > 0 {
			bad("%s reference workspaces missing from the snapshot, so the grid has nowhere to draw them", daemon.Plural(len(orphans), "agent"))
			if len(r.fixes) == 0 && d.Exe != exeElsewhere {
				advise("the daemon is healthy but herdr reports agents outside any workspace: update with `herdr plugin install ofelcan164/muster --yes`, and if it persists report it with %s attached", filepath.Join(d.Dir, "snapshot.json"))
			}
		}
	}

	// Keys and skill are advice only. Both live in files the user owns, so
	// doctor reports and points, and never rewrites them itself.
	switch {
	case d.KeysErr != nil:
		bad("could not read herdr config: %v", d.KeysErr)
	case d.KeysPresent:
		if d.OptedOut {
			ok("keybindings on prefix+%s (a recorded refusal keeps the startup hook from rebinding them)", d.KeysLetter)
		} else {
			ok("keybindings on prefix+%s", d.KeysLetter)
		}
	case d.OptedOut:
		ok("keybindings declined earlier (refusal recorded, startup hook leaves them alone)")
	default:
		bad("no keybindings installed")
		advise("run the Install Muster's keybindings action")
	}
	if !d.SkillInstalled {
		advise("reporting skill missing: tiles fall back to terminal titles. Run the Install the reporting skill for the orchestrator action")
	} else {
		ok("reporting skill installed")
	}

	return r
}

// orphanAgents lists agents whose workspace id is in no snapshot workspace.
// Those agents are counted in the header but build no tiles, which is exactly
// the "0 workspaces, N agents, empty grid" state.
func orphanAgents(s *model.Snapshot) []model.Agent {
	known := make(map[string]bool, len(s.Workspaces))
	for _, w := range s.Workspaces {
		known[w.ID] = true
	}
	var out []model.Agent
	for _, r := range s.Repos {
		for _, a := range r.Agents {
			if !known[a.WorkspaceID] {
				out = append(out, a)
			}
		}
	}
	return out
}

// gather reads the live state check reasons about.
func gather(dir string) deps {
	d := deps{Dir: dir, Now: time.Now()}
	d.Musterd, _ = daemon.Binary()
	d.DaemonRunning, d.DaemonPID = daemon.Status()
	if d.DaemonRunning {
		d.Exe, d.ExePath = exeState(d.DaemonPID, d.Musterd)
	}
	d.KeysLetter, d.KeysPresent, d.KeysErr = install.BlockPresent()
	d.SkillInstalled = install.SkillInstalled()
	// A refusal is the user's, not the session's, so it sits in the base dir.
	d.OptedOut = install.OptedOut(state.BaseDir())
	snap, err := daemon.ReadSnapshot()
	if err != nil {
		d.SnapshotErr = err
		d.SnapshotMissing = os.IsNotExist(err)
	} else {
		d.Snapshot = snap
	}
	return d
}

// Run diagnoses, explains the fixes, asks, and applies. It returns 0 when
// healthy or fixed and verified, 1 otherwise.
func Run(w io.Writer, stdin io.Reader, opts Options) int {
	live := runner{
		stop: daemon.Stop,
		start: func() error {
			_, err := daemon.Ensure()
			return err
		},
	}
	return runWith(w, stdin, opts, gather, live, waitFresh)
}

func runWith(w io.Writer, stdin io.Reader, opts Options, gather func(string) deps, run runner, wait func(since time.Time)) int {
	if opts.Dir == "" {
		fmt.Fprintln(w, "muster doctor: no state directory: run through herdr, or pass --state-dir <dir> first")
		return 1
	}
	fmt.Fprintf(w, "state dir: %s\n", opts.Dir)
	rep := check(gather(opts.Dir))
	printReport(w, rep)

	if len(rep.fixes) == 0 {
		return exitFor(rep)
	}

	fmt.Fprintln(w, "proposed fixes:")
	for i, f := range rep.fixes {
		fmt.Fprintf(w, "  %d. %s\n", i+1, f.title)
	}
	if !opts.Yes {
		if !opts.CanPrompt {
			fmt.Fprintln(w, "not a terminal: re-run in one to answer, or pass --yes to apply without asking")
			return 1
		}
		fmt.Fprint(w, "apply these fixes? [y/N] ")
		if !confirm(stdin) {
			fmt.Fprintln(w, "no changes made")
			return 1
		}
	}
	started := time.Now()
	for _, f := range rep.fixes {
		fmt.Fprintf(w, "- %s... ", f.title)
		var err error
		switch f.kind {
		case fixStop:
			err = run.stop(2 * time.Second)
		case fixStart:
			err = run.start()
		}
		if err != nil {
			fmt.Fprintf(w, "failed: %v\n", err)
			return 1
		}
		fmt.Fprintln(w, "done")
	}

	// Verify against a fresh read rather than assuming the fixes worked.
	wait(started)
	fmt.Fprintln(w, "rechecking:")
	after := check(gather(opts.Dir))
	printReport(w, after)
	if len(after.fixes) > 0 {
		fmt.Fprintf(w, "still broken: update with `herdr plugin install ofelcan164/muster --yes`, and report it with %s attached\n",
			filepath.Join(opts.Dir, "snapshot.json"))
		return 1
	}
	return exitFor(after)
}

func exitFor(rep report) int {
	for _, f := range rep.findings {
		if f.bad {
			return 1
		}
	}
	return 0
}

func printReport(w io.Writer, rep report) {
	for _, f := range rep.findings {
		mark := "[ok] "
		if f.bad {
			mark = "[FAIL] "
		}
		fmt.Fprintf(w, "%s%s\n", mark, f.text)
	}
	for _, a := range rep.advice {
		fmt.Fprintf(w, "[info] %s\n", a)
	}
}

// confirm reads one line and accepts an explicit yes. Anything else,
// including EOF, is no.
func confirm(stdin io.Reader) bool {
	if stdin == nil {
		return false
	}
	line, err := bufio.NewReader(stdin).ReadString('\n')
	if err != nil && len(line) == 0 {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	}
	return false
}

// waitFresh waits for a snapshot written after the fixes began. The old
// daemon's snapshot can still look fresh, and a recheck before the new daemon
// takes the lock reports it not running. The daemon takes the lock before its
// first write, so a newer snapshot means both have happened.
func waitFresh(since time.Time) {
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(25 * time.Millisecond) {
		if snap, err := daemon.ReadSnapshot(); err == nil && snap.GeneratedAt.After(since) {
			return
		}
	}
}

// exeState classifies the binary a daemon pid was started from, and returns
// its path.
func exeState(pid int, musterd string) (string, string) {
	target, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		return exeUnknown, ""
	}
	cmdline, _ := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	path, deleted := startedFrom(target, string(cmdline))
	return classify(path, deleted, musterd), path
}

// startedFrom is the path the daemon's own watch stats, which is argv[0] when
// absolute, as Ensure always passes it. The exe link is no substitute: herdr
// install moves the old checkout aside before deleting it, and the link
// follows the move, naming a directory that is gone while the install path
// already holds the new build.
func startedFrom(target, cmdline string) (string, bool) {
	path, deleted := strings.CutSuffix(target, " (deleted)")
	if argv0, _, _ := strings.Cut(cmdline, "\x00"); filepath.IsAbs(argv0) {
		path = argv0
	}
	return path, deleted
}

func classify(path string, deleted bool, musterd string) string {
	switch {
	case deleted:
		if _, err := os.Stat(path); err != nil {
			return exeDeleted
		}
		return exeReplaced
	case musterd != "" && path != musterd:
		return exeElsewhere
	}
	return exeOK
}
