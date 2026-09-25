// Package daemon is musterd: the long-lived watcher that holds the event
// subscription and keeps the snapshot warm.
//
// The whole reason it exists is the 30-second cadence. If the overlay had to
// enumerate workspaces, tabs, panes and agents at open time it would cost half
// a second on a key you press a hundred times a day. Everything expensive
// happens here, ahead of time.
package daemon

import (
	"cmp"
	"context"
	"encoding/json"
	"log"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/ofelcan164/muster/internal/discover"
	"github.com/ofelcan164/muster/internal/herdr"
	"github.com/ofelcan164/muster/internal/model"
	"github.com/ofelcan164/muster/internal/state"
	"github.com/ofelcan164/muster/internal/triage"
)

const (
	// minInterval rate-limits reconciles. Events come in bursts, and one burst
	// describes one change, so reconciling per event is mostly redundant work.
	// This floor was sized against 0.8.2's subscribe-time replay, which arrived
	// at 10 events per second. 0.9.0 no longer replays, so the floor now only
	// has live bursts to collapse and could be tightened if it ever felt slow.
	minInterval = 300 * time.Millisecond

	// fullInterval forces a reconcile even when nothing has happened. Several
	// triage rules are time-based, so the ranking can change with no event at
	// all.
	fullInterval = 5 * time.Second

	// procInterval is the floor between foreground-process polls. That poll is
	// one RPC per non-agent pane, and an event burst can drive several
	// reconciles a second, so without a floor most of a burst is spent asking a
	// question nothing has changed the answer to. Only stop detection reads it,
	// and a process that died is no more useful to know about a second sooner.
	procInterval = time.Second

	// serverGrace is how long the server may stay unreachable before the daemon
	// gives up and exits. The daemon is meant to live and die with the herdr
	// server, but `herdr update --handoff` restarts the server underneath it, so
	// the window has to be comfortably longer than a handoff takes.
	serverGrace = 60 * time.Second
)

type Daemon struct {
	client   *herdr.Client
	resolver *discover.Resolver
	persist  *state.Persisted
	log      *log.Logger

	// procs is the last foreground-process reading, kept so reconciles arriving
	// faster than procInterval reuse it instead of re-polling every pane.
	procs   map[string]string
	procsAt time.Time

	// mu guards state shared with the question fetcher goroutine. asked is the
	// state_change_seq each blocked pane was last claimed for reading at.
	mu        sync.Mutex
	questions map[string]string
	asked     map[string]uint64

	// said is the orchestrator's last message, the pane it was read from, and
	// the pane-and-status it was read at. One pane is ever read this way, so
	// these are fields rather than a map. saidPending is the read in flight.
	said        string
	saidAt      time.Time
	saidPane    string
	saidKey     string
	saidPending string

	// rescan asks the reconcile loop to run again after the fetcher learns
	// something the last snapshot did not have.
	rescan chan struct{}

	// stopped is the last computed set of panes whose process went away,
	// recomputed on every reconcile.
	stopped []model.Stopped
}

func New(client *herdr.Client, logger *log.Logger) *Daemon {
	return &Daemon{
		client:    client,
		resolver:  discover.NewResolver(),
		persist:   state.LoadPersisted(),
		log:       logger,
		questions: map[string]string{},
		asked:     map[string]uint64{},
		rescan:    make(chan struct{}, 1),
	}
}

// Run holds the subscription and reconciles until ctx is cancelled.
func (d *Daemon) Run(ctx context.Context) error {
	if _, err := state.EnsureDir(); err != nil {
		return err
	}

	// Subscribing first and reconciling second is the order herdr's own docs
	// prescribe: the subscription buffers while the snapshot is in flight, so
	// nothing that changes in between is missed. The acknowledgement arrives as
	// an event, which reconciles again on every reconnect.
	events := d.client.Subscribe(ctx, herdr.GlobalSubscriptions, d.logf)

	// Reconcile once up front so the snapshot is valid before any event
	// arrives. The overlay may be opened a millisecond after the daemon starts.
	d.reconcile(ctx)

	ticker := time.NewTicker(fullInterval)
	defer ticker.Stop()
	bin := watchBinary()

	// Coalescing loop: events set a dirty flag, and the flag is drained no more
	// often than minInterval. Events are only ever a hint that something
	// changed, never the change itself, so collapsing a burst loses nothing.
	dirty := false
	rateLimit := time.NewTimer(0)
	if !rateLimit.Stop() {
		<-rateLimit.C
	}
	limited := false

	// lastReachable tracks when the server last answered. The daemon exits once
	// it has been gone longer than serverGrace, so musterd lives and dies with
	// herdr instead of lingering as an orphan after the server stops.
	lastReachable := time.Now()

	for {
		select {
		case <-ctx.Done():
			d.savePersisted()
			return ctx.Err()

		case _, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			dirty = true
			if !limited {
				limited = true
				rateLimit.Reset(minInterval)
			}

		case <-rateLimit.C:
			limited = false
			if dirty {
				dirty = false
				if d.reconcile(ctx) {
					lastReachable = time.Now()
				}
			}

		case <-d.rescan:
			if d.reconcile(ctx) {
				lastReachable = time.Now()
			}

		case <-ticker.C:
			dirty = false
			if d.reconcile(ctx) {
				lastReachable = time.Now()
			} else if time.Since(lastReachable) > serverGrace {
				d.logf("server unreachable for %s, exiting", serverGrace)
				d.savePersisted()
				return nil
			}
			if bin.replaced() {
				d.logf("%s was rebuilt, restarting on the new build", bin.path)
				d.savePersisted()
				return ErrReplaced
			}
		}
	}
}

// reconcile rebuilds the whole world from an authoritative snapshot and writes
// the result. It never builds state from the event stream: events are a hint
// that something changed, never the change itself, and 0.8.2 replayed closed
// panes on every subscribe, so the stream cannot be trusted as a log.
//
// ctx is Run's, handed on to the pane reads it launches so none starts once the
// daemon is shutting down.
func (d *Daemon) reconcile(ctx context.Context) bool {
	snap, err := d.client.SessionSnapshot()
	if err != nil {
		d.logf("snapshot: %v", err)
		return false
	}
	now := time.Now()

	agents := d.buildAgents(snap, now)
	orch := d.findOrchestrator(snap, agents, now)
	repos := d.buildRepos(snap, agents, orch)
	workspaces := buildWorkspaces(snap)

	// The question a blocked agent is waiting on is the single most useful
	// string in the ribbon, and it only exists in the pane. Attach whatever the
	// fetcher has learned, then ask it for anything still missing.
	for i := range repos {
		for j := range repos[i].Agents {
			a := &repos[i].Agents[j]
			if a.Status == model.StatusBlocked {
				a.Question = d.question(a.PaneID)
			}
		}
	}
	if want := d.blockedNeedingQuestion(agents); len(want) > 0 && d.client != nil {
		go d.fetchQuestions(ctx, want)
	}

	// The other half of the orchestrator strip: what it said back, as opposed to
	// what it was told, which is all the task ladder can report.
	d.attachSaid(ctx, &orch, agents[orch.PaneID].StateChangeSeq)

	everDone := make(map[string]bool, len(d.persist.LastDoneSeq))
	for pane := range d.persist.LastDoneSeq {
		everDone[pane] = true
	}

	ribbon := triage.Rank(triage.Input{
		Now:      now,
		Repos:    repos,
		Orch:     orch,
		Stopped:  d.stopped,
		EverDone: everDone,
		EverWorked: func() map[string]bool {
			out := make(map[string]bool, len(d.persist.EverWorked))
			for k, v := range d.persist.EverWorked {
				out[k] = v
			}
			return out
		}(),
	})

	if repos == nil {
		repos = []model.Repo{}
	}
	if ribbon == nil {
		ribbon = []model.Attention{}
	}

	d.trackFocus(snap.FocusedPaneID, agents)

	out := model.Snapshot{
		Version:          model.SnapshotVersion,
		GeneratedAt:      now,
		DaemonPID:        os.Getpid(),
		HerdrVersion:     snap.Version,
		Repos:            repos,
		Workspaces:       workspaces,
		Attention:        ribbon,
		Orch:             orch,
		FocusedPane:      snap.FocusedPaneID,
		FocusedWorkspace: snap.FocusedWorkspaceID,
		PreviousAgent:    d.previousAgent(),
		FocusHistory:     append([]string(nil), d.persist.FocusHistory...),
		Counts:           countOf(repos, workspaces, ribbon),
	}

	body, err := json.Marshal(out)
	if err != nil {
		d.logf("marshal snapshot: %v", err)
		return true
	}
	if err := state.WriteAtomic(state.SnapshotPath(), body); err != nil {
		d.logf("write snapshot: %v", err)
		return true
	}
	d.savePersisted()
	return true
}

// buildWorkspaces carries herdr's workspaces through in number order, so the
// overlay can draw a tile for one holding no agent at all.
func buildWorkspaces(snap *herdr.Snapshot) []model.Workspace {
	out := make([]model.Workspace, 0, len(snap.Workspaces))
	for _, w := range snap.Workspaces {
		out = append(out, model.Workspace{ID: w.WorkspaceID, Number: w.Number, Label: w.Label})
	}
	slices.SortStableFunc(out, func(a, b model.Workspace) int { return cmp.Compare(a.Number, b.Number) })
	return out
}

func countOf(repos []model.Repo, workspaces []model.Workspace, ribbon []model.Attention) model.Counts {
	c := model.Counts{Repos: len(repos), Workspaces: len(workspaces), NeedsYou: len(ribbon)}
	for _, r := range repos {
		c.Agents += len(r.Agents)
		c.NonAgents += len(r.OtherPanes)
		for _, a := range r.Agents {
			if a.Status == model.StatusWorking {
				c.Working++
			}
		}
	}
	return c
}

func (d *Daemon) savePersisted() {
	if err := d.persist.Save(); err != nil {
		d.logf("save state: %v", err)
	}
}

func (d *Daemon) logf(format string, args ...any) {
	if d.log != nil {
		d.log.Printf(format, args...)
	}
}
