package daemon

import (
	"testing"
	"time"

	"github.com/ofelcan164/muster/internal/herdr"
	"github.com/ofelcan164/muster/internal/model"
)

func TestFallbackLadderOrder(t *testing.T) {
	seen := time.Now()
	cases := []struct {
		name       string
		agent      herdr.Agent
		pane       herdr.Pane
		wantTask   string
		wantSource model.TaskSource
	}{
		{
			name:       "tier 1 orchestrator token",
			agent:      herdr.Agent{Tokens: map[string]string{"task": "add billing migration"}, Title: "ignored"},
			wantTask:   "add billing migration",
			wantSource: model.TaskFromOrchestrator,
		},
		{
			name:       "tier 3 self report",
			agent:      herdr.Agent{Tokens: map[string]string{"self_task": "ran the suite"}, Title: "ignored"},
			wantTask:   "ran the suite",
			wantSource: model.TaskFromSelfReport,
		},
		{
			name:       "tier 4 terminal title",
			agent:      herdr.Agent{TerminalTitleStripped: "Claude Code"},
			wantTask:   "Claude Code",
			wantSource: model.TaskFromTerminalTitle,
		},
		{
			name:       "tier 4 from the pane when the agent has none",
			pane:       herdr.Pane{TerminalTitleStripped: "vite dev"},
			wantTask:   "vite dev",
			wantSource: model.TaskFromTerminalTitle,
		},
		{
			name:       "tier 5 nothing at all",
			wantTask:   "",
			wantSource: model.TaskFromNone,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			task, src := taskFor(c.agent, c.pane, seen.Add(-time.Hour), seen)
			if task != c.wantTask || src != c.wantSource {
				t.Errorf("got (%q, %s), want (%q, %s)", task, src, c.wantTask, c.wantSource)
			}
		})
	}
}

// Tier 2. A task line the daemon first saw before the agent last changed state
// is stale: the agent has moved on since the line was written.
func TestTaskGoesStaleWhenTheAgentMovedOnAfterIt(t *testing.T) {
	now := time.Now()
	a := herdr.Agent{Tokens: map[string]string{"task": "old work"}}

	_, src := taskFor(a, herdr.Pane{}, now.Add(-time.Minute), now.Add(-time.Hour))
	if src != model.TaskFromOrchestratorStale {
		t.Errorf("task written an hour before the last state change should be stale, got %s", src)
	}

	_, src = taskFor(a, herdr.Pane{}, now.Add(-time.Hour), now.Add(-time.Minute))
	if src != model.TaskFromOrchestrator {
		t.Errorf("task written after the last state change should be fresh, got %s", src)
	}
}

// The daemon has no timestamp from herdr, so it timestamps the value itself and
// must keep that stamp until the value actually changes.
func TestTaskStampHoldsUntilTheValueChanges(t *testing.T) {
	d := newTestDaemon(t)
	t0 := time.Now()

	first := d.stampTask("w1:p1", "build the thing", t0)
	if !first.Equal(t0) {
		t.Fatalf("first sighting should stamp now, got %v", first)
	}
	again := d.stampTask("w1:p1", "build the thing", t0.Add(time.Hour))
	if !again.Equal(t0) {
		t.Errorf("unchanged task must keep its original stamp, got %v", again)
	}
	changed := d.stampTask("w1:p1", "build a different thing", t0.Add(2*time.Hour))
	if !changed.Equal(t0.Add(2 * time.Hour)) {
		t.Errorf("a new task value must restamp, got %v", changed)
	}
	if cleared := d.stampTask("w1:p1", "", t0); !cleared.IsZero() {
		t.Errorf("clearing the token should clear the stamp, got %v", cleared)
	}
}

// herdr exposes no transition timestamp, so an age is only honest once the
// daemon has watched the status change.
func TestAgeIsUnknownUntilATransitionIsObserved(t *testing.T) {
	d := newTestDaemon(t)
	now := time.Now()

	snap := &herdr.Snapshot{Agents: []herdr.Agent{agentPane("w1:p1", "w1", "/x", "blocked")}}
	agents := d.buildAgents(snap, now)
	if agents["w1:p1"].AgeKnown {
		t.Fatal("an agent found already blocked has no knowable age")
	}

	// Same status again: still nothing observed.
	agents = d.buildAgents(snap, now.Add(time.Minute))
	if agents["w1:p1"].AgeKnown {
		t.Fatal("no transition happened, age still unknown")
	}

	// Now it moves. The daemon saw this one.
	moved := &herdr.Snapshot{Agents: []herdr.Agent{agentPane("w1:p1", "w1", "/x", "done")}}
	agents = d.buildAgents(moved, now.Add(2*time.Minute))
	if !agents["w1:p1"].AgeKnown {
		t.Fatal("age should be known after an observed transition")
	}
	if got := agents["w1:p1"].StatusSince; !got.Equal(now.Add(2 * time.Minute)) {
		t.Errorf("StatusSince = %v, want the transition time", got)
	}
}

func TestStatusSinceSurvivesUnchangedStatus(t *testing.T) {
	d := newTestDaemon(t)
	t0 := time.Now()
	snap := &herdr.Snapshot{Agents: []herdr.Agent{agentPane("w1:p1", "w1", "/x", "working")}}

	d.buildAgents(snap, t0)
	agents := d.buildAgents(snap, t0.Add(10*time.Minute))
	if got := agents["w1:p1"].StatusSince; !got.Equal(t0) {
		t.Errorf("StatusSince must not creep forward while the status holds: %v", got)
	}
}

// Dead panes must not accumulate in the state file across a long session.
func TestStateForgetsDeadPanes(t *testing.T) {
	d := newTestDaemon(t)
	now := time.Now()

	two := &herdr.Snapshot{Agents: []herdr.Agent{
		{PaneID: "w1:p1", AgentStatus: "done", Tokens: map[string]string{"task": "a"}},
		{PaneID: "w2:p1", AgentStatus: "idle", Tokens: map[string]string{"task": "b"}},
	}}
	d.buildAgents(two, now)
	if len(d.persist.StatusSince) != 2 || len(d.persist.TaskSeenAt) != 2 {
		t.Fatalf("expected both panes tracked, got %d/%d",
			len(d.persist.StatusSince), len(d.persist.TaskSeenAt))
	}

	one := &herdr.Snapshot{Agents: []herdr.Agent{
		{PaneID: "w1:p1", AgentStatus: "done", Tokens: map[string]string{"task": "a"}},
	}}
	d.buildAgents(one, now)
	if len(d.persist.StatusSince) != 1 {
		t.Errorf("StatusSince kept a dead pane: %v", d.persist.StatusSince)
	}
	if len(d.persist.TaskSeenAt) != 1 {
		t.Errorf("TaskSeenAt kept a dead pane: %v", d.persist.TaskSeenAt)
	}
	if len(d.persist.LastDoneSeq) != 1 {
		t.Errorf("LastDoneSeq kept a dead pane: %v", d.persist.LastDoneSeq)
	}
}

// The idle-never-done rule depends on this: passing through done has to be
// remembered after the agent leaves that state.
func TestDoneIsRemembered(t *testing.T) {
	d := newTestDaemon(t)
	now := time.Now()

	d.buildAgents(&herdr.Snapshot{Agents: []herdr.Agent{
		{PaneID: "w1:p1", AgentStatus: "done", StateChangeSeq: 7},
	}}, now)
	if _, ok := d.persist.LastDoneSeq["w1:p1"]; !ok {
		t.Fatal("done was not recorded")
	}

	d.buildAgents(&herdr.Snapshot{Agents: []herdr.Agent{
		{PaneID: "w1:p1", AgentStatus: "idle", StateChangeSeq: 9},
	}}, now.Add(time.Minute))
	if _, ok := d.persist.LastDoneSeq["w1:p1"]; !ok {
		t.Error("passing through done must be remembered after leaving it")
	}
}

func TestPaneLabelPrefersLabelThenProcess(t *testing.T) {
	cases := []struct {
		pane herdr.Pane
		proc string
		want string
	}{
		{herdr.Pane{Label: "dev"}, "vite", "dev"},
		{herdr.Pane{TerminalTitleStripped: "ofelcan@box:~/work"}, "vite", "vite"},
		{herdr.Pane{TerminalTitleStripped: "ofelcan@box:~/work"}, "", "shell"},
		{herdr.Pane{PaneID: "w1:p1"}, "", "w1:p1"},
	}
	for _, c := range cases {
		if got := paneLabel(c.pane, c.proc); got != c.want {
			t.Errorf("paneLabel(%+v, %q) = %q, want %q", c.pane, c.proc, got, c.want)
		}
	}
}

func TestTokenPrefersAgentOverPane(t *testing.T) {
	got := tokenOf(map[string]string{"task": "from agent"}, map[string]string{"task": "from pane"}, "task")
	if got != "from agent" {
		t.Errorf("got %q", got)
	}
	if got := tokenOf(nil, map[string]string{"task": "from pane"}, "task"); got != "from pane" {
		t.Errorf("pane fallback failed, got %q", got)
	}
	if got := tokenOf(nil, nil, "task"); got != "" {
		t.Errorf("missing token should be empty, got %q", got)
	}
}

// An age learned honestly before a restart is still honest after it, so the
// observed flag has to persist alongside the timestamp.
func TestAgeKnownSurvivesARestart(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HERDR_PLUGIN_STATE_DIR", dir)
	now := time.Now()

	d1 := New(nil, nil)
	idle := &herdr.Snapshot{Agents: []herdr.Agent{agentPane("w1:p1", "w1", "/x", "idle")}}
	d1.buildAgents(idle, now)

	done := &herdr.Snapshot{Agents: []herdr.Agent{agentPane("w1:p1", "w1", "/x", "done")}}
	if !d1.buildAgents(done, now.Add(time.Minute))["w1:p1"].AgeKnown {
		t.Fatal("age should be known after an observed transition")
	}
	if err := d1.persist.Save(); err != nil {
		t.Fatal(err)
	}

	// A fresh daemon over the same state dir, as after a herdr restart.
	d2 := New(nil, nil)
	got := d2.buildAgents(done, now.Add(2*time.Minute))["w1:p1"]
	if !got.AgeKnown {
		t.Error("a restart must not turn a known age back into an unknown one")
	}
	if !got.StatusSince.Equal(now.Add(time.Minute)) {
		t.Errorf("StatusSince = %v, want the original transition time", got.StatusSince)
	}
}

// The strip labels this line "told", so only a message someone actually sent
// belongs on it. The task ladder's lower rungs are the pane's terminal title,
// which for a Claude pane is the words "Claude Code" forever.
func TestToldIsOnlyWhatSomeoneSaid(t *testing.T) {
	d := newTestDaemon(t)
	a := herdr.Agent{PaneID: "w1:p1", Name: "orchestrator"}

	agents := map[string]model.Agent{"w1:p1": {
		Task: "Claude Code", TaskSource: model.TaskFromTerminalTitle,
	}}
	if got := d.orchFrom(&a, agents, "token").LastMessage; got != "" {
		t.Errorf("terminal title reached the told line as %q", got)
	}

	agents["w1:p1"] = model.Agent{
		Task: "ship the api", TaskSource: model.TaskFromOrchestrator,
	}
	if got := d.orchFrom(&a, agents, "token").LastMessage; got != "ship the api" {
		t.Errorf("got %q, want the message that was sent", got)
	}
}
