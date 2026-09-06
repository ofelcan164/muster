// Package daemon is musterd: the long-lived watcher that holds the event
// subscription and keeps the snapshot warm.
//
// The whole reason it exists is the 30-second cadence. If the overlay had to
// enumerate workspaces, tabs, panes and agents at open time it would cost half
// a second on a key you press a hundred times a day. Everything expensive
// happens here, ahead of time.
package daemon

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"sync"
	"time"

	"github.com/ofelcan/muster/internal/chain"
	"github.com/ofelcan/muster/internal/discover"
	"github.com/ofelcan/muster/internal/herdr"
	"github.com/ofelcan/muster/internal/model"
	"github.com/ofelcan/muster/internal/state"
	"github.com/ofelcan/muster/internal/triage"
)

const (
	// minInterval rate-limits reconciles. It matters most during the historical
	// event replay that herdr sends on every subscribe, which arrives at 10
	// events per second: without a floor the daemon would reconcile ten times a
	// second for the length of the replay, all of it redundant.
	minInterval = 300 * time.Millisecond

	// fullInterval forces a reconcile even when nothing has happened. Several
	// triage rules are time-based, so the ranking can change with no event at
	// all.
	fullInterval = 5 * time.Second

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

	herdrVersion string

	// mu guards state shared with the question fetcher goroutine.
	mu        sync.Mutex
	questions map[string]string

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
		rescan:    make(chan struct{}, 1),
	}
}

// Run holds the subscription and reconciles until ctx is cancelled.
func (d *Daemon) Run(ctx context.Context) error {
	if _, err := state.EnsureDir(); err != nil {
		return err
	}

	events := d.client.Subscribe(ctx, herdr.GlobalSubscriptions)

	// Reconcile once up front so the snapshot is valid before any event
	// arrives. The overlay may be opened a millisecond after the daemon starts.
	d.reconcile()

	ticker := time.NewTicker(fullInterval)
	defer ticker.Stop()

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
				if d.reconcile() {
					lastReachable = time.Now()
				}
			}

		case <-d.rescan:
			if d.reconcile() {
				lastReachable = time.Now()
			}

		case <-ticker.C:
			dirty = false
			if d.reconcile() {
				lastReachable = time.Now()
			} else if time.Since(lastReachable) > serverGrace {
				d.logf("server unreachable for %s, exiting", serverGrace)
				d.savePersisted()
				return nil
			}

		}
	}
}

// reconcile rebuilds the whole world from an authoritative snapshot and writes
// the result. It never builds state from the event stream: herdr replays
// historical events on every subscribe, including events for panes that no
// longer exist, so the stream cannot be trusted as a log.
func (d *Daemon) reconcile() bool {
	snap, err := d.client.SessionSnapshot()
	if err != nil {
		d.logf("snapshot: %v", err)
		return false
	}
	d.herdrVersion = snap.Version
	now := time.Now()

	agents := d.buildAgents(snap, now)
	orch := d.findOrchestrator(snap, agents, now)
	repos := d.buildRepos(snap, agents, orch)

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
		go d.fetchQuestions(context.Background(), want)
	}

	everDone := make(map[string]bool, len(d.persist.LastDoneSeq))
	for pane := range d.persist.LastDoneSeq {
		everDone[pane] = true
	}

	ribbon := triage.Rank(triage.Input{
		Now:      now,
		Repos:    repos,
		Orch:     orch,
		Stopped:  d.stopped,
		Chain:    chain.Load(state.Dir()),
		EverDone: everDone,
	})

	if repos == nil {
		repos = []model.Repo{}
	}
	if ribbon == nil {
		ribbon = []model.Attention{}
	}

	d.trackFocus(snap.FocusedPaneID, agents)

	out := model.Snapshot{
		Schema:        model.SchemaVersion,
		GeneratedAt:   now,
		DaemonPID:     os.Getpid(),
		HerdrVersion:  snap.Version,
		Repos:         repos,
		Attention:     ribbon,
		Orch:          orch,
		FocusedPane:   snap.FocusedPaneID,
		PreviousAgent: d.previousAgent(),
		FocusHistory:  append([]string(nil), d.persist.FocusHistory...),
		Counts:        countOf(repos, ribbon),
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

func countOf(repos []model.Repo, ribbon []model.Attention) model.Counts {
	c := model.Counts{Repos: len(repos), NeedsYou: len(ribbon)}
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
