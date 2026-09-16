package triage

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/ofelcan164/muster/internal/model"
)

var now = time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

func ago(d time.Duration) time.Time { return now.Add(-d) }

func agent(name string, status model.Status, age time.Duration) model.Agent {
	return model.Agent{
		PaneID:      "pane-" + name,
		Name:        name,
		Status:      status,
		StatusSince: ago(age),
		AgeKnown:    true,
	}
}

func repo(key string, agents ...model.Agent) model.Repo {
	return model.Repo{Key: key, Name: key, Agents: agents}
}

func TestBlockedRanksFirstLongestWaitingFirst(t *testing.T) {
	in := Input{
		Now: now,
		Repos: []model.Repo{
			repo("api", agent("recent", model.StatusBlocked, 2*time.Minute)),
			repo("web", agent("older", model.StatusBlocked, 20*time.Minute)),
			repo("infra", agent("busy", model.StatusWorking, time.Minute)),
		},
	}
	got := Rank(in)
	if len(got) != 2 {
		t.Fatalf("want 2 rows, got %d: %+v", len(got), got)
	}
	if got[0].Agent != "older" {
		t.Errorf("longest-waiting blocked agent should lead, got %q", got[0].Agent)
	}
	for _, r := range got {
		if r.Rank != 1 || r.Reason != model.ReasonBlocked {
			t.Errorf("unexpected row %+v", r)
		}
	}
}

func TestWorkingNeverAppears(t *testing.T) {
	in := Input{Now: now, Repos: []model.Repo{
		repo("api", agent("a", model.StatusWorking, time.Hour)),
		repo("web", agent("b", model.StatusWorking, time.Minute)),
	}}
	if got := Rank(in); len(got) != 0 {
		t.Fatalf("working agents must not enter the ribbon, got %+v", got)
	}
}

func TestDoneUnseenNewestFirst(t *testing.T) {
	in := Input{Now: now, Repos: []model.Repo{
		repo("api", agent("old", model.StatusDone, 30*time.Minute)),
		repo("web", agent("new", model.StatusDone, 2*time.Minute)),
	}}
	got := Rank(in)
	if len(got) != 2 {
		t.Fatalf("want 2 rows, got %d", len(got))
	}
	if got[0].Agent != "new" {
		t.Errorf("done rows are newest first, got %q", got[0].Agent)
	}
}

func TestIdleNeverDoneNeedsBothQuietAndNoDoneHistory(t *testing.T) {
	stale := agent("stale", model.StatusIdle, 30*time.Minute)
	fresh := agent("fresh", model.StatusIdle, time.Minute)
	finishedBefore := agent("finished", model.StatusIdle, 30*time.Minute)

	in := Input{
		Now:      now,
		Repos:    []model.Repo{repo("api", stale, fresh, finishedBefore)},
		EverDone: map[string]bool{finishedBefore.PaneID: true},
		EverWorked: map[string]bool{
			stale.PaneID: true, fresh.PaneID: true, finishedBefore.PaneID: true,
		},
	}
	got := Rank(in)
	if len(got) != 1 {
		t.Fatalf("want only the stale agent, got %+v", got)
	}
	if got[0].Agent != "stale" || got[0].Rank != 5 {
		t.Errorf("unexpected row %+v", got[0])
	}
}

// The overlay caps what it draws. The sort needs every row.
func TestRankKeepsEveryRow(t *testing.T) {
	var agents []model.Agent
	for i := 0; i < 10; i++ {
		agents = append(agents, agent(string(rune('a'+i)), model.StatusBlocked, time.Duration(i)*time.Minute))
	}
	got := Rank(Input{Now: now, Repos: []model.Repo{repo("api", agents...)}})
	if len(got) != len(agents) {
		t.Fatalf("got %d rows for %d blocked agents", len(got), len(agents))
	}
}

func TestAgentAppearsOnceAtItsHighestRank(t *testing.T) {
	// An agent that is both blocked and done-unseen is one row, not two.
	a := agent("both", model.StatusBlocked, 5*time.Minute)
	got := Rank(Input{
		Now:      now,
		Repos:    []model.Repo{repo("api", a)},
		EverDone: map[string]bool{a.PaneID: true},
	})
	if len(got) != 1 {
		t.Fatalf("want 1 row, got %d: %+v", len(got), got)
	}
	if got[0].Rank != 1 {
		t.Errorf("highest rank should win, got rank %d", got[0].Rank)
	}
}

// A stopped dev server is invisible until you open the workspace, which is
// exactly what the ribbon is for.
func TestStoppedProcessRanksThird(t *testing.T) {
	got := Rank(Input{
		Now:   now,
		Repos: []model.Repo{repo("web", agent("ui", model.StatusWorking, time.Minute))},
		Stopped: []model.Stopped{
			{PaneID: "w1:p2", RepoKey: "web", Label: "dev", Process: "vite"},
		},
	})
	if len(got) != 1 {
		t.Fatalf("want 1 row, got %+v", got)
	}
	if got[0].Rank != 3 || got[0].Reason != model.ReasonProcessStopped {
		t.Errorf("unexpected row %+v", got[0])
	}
	if got[0].Detail != "vite stopped" {
		t.Errorf("detail = %q", got[0].Detail)
	}
}

// Superseded: a blocked orchestrator now does reach the ribbon. See
// TestBlockedOrchestratorReachesTheRibbon and
// TestOrchestratorStaysOutOfInformationalRanks.
func TestOrchestratorIsNotTriagedWhenWorking(t *testing.T) {
	o := agent("orchestrator", model.StatusWorking, time.Hour)
	o.IsOrchestrator = true
	if got := Rank(Input{Now: now, Repos: []model.Repo{repo("api", o)}}); len(got) != 0 {
		t.Fatalf("a working orchestrator has its own strip, got %+v", got)
	}
}

// dependent is an agent waiting on work in another repo, which landed landedAgo
// ago, or has not landed when landedAgo is zero.
func dependent(name, dep string, status model.Status, age, landedAgo time.Duration) model.Agent {
	a := agent(name, status, age)
	a.DependsOn = dep + "#412"
	a.DependsOnRepo = dep
	if landedAgo > 0 {
		a.LandedAt = ago(landedAgo)
	}
	return a
}

// restingSince is a marked orchestrator that ended its last turn d ago.
func restingSince(d time.Duration) model.Orchestrator {
	return model.Orchestrator{Found: true, Status: model.StatusIdle, StatusSince: ago(d)}
}

func nothingLanded(t *testing.T, in Input, why string) {
	t.Helper()
	for _, r := range Rank(in) {
		if r.Reason == model.ReasonLanded {
			t.Fatalf("%s: %+v", why, r)
		}
	}
}

// The landed rule: api landed, the orchestrator has ended a turn since it
// recorded that, and web is still where it was before the landing.
func TestLandedRowAfterLandingNobodyMoved(t *testing.T) {
	got := Rank(Input{
		Now: now,
		Repos: []model.Repo{
			repo("api"),
			repo("web", dependent("checkout-ui", "api", model.StatusIdle, 40*time.Minute, 10*time.Minute)),
		},
		Orch: restingSince(2 * time.Minute),
	})
	if len(got) != 1 || got[0].Rank != 2 || got[0].Reason != model.ReasonLanded {
		t.Fatalf("want one landed row, got %+v", got)
	}
	if got[0].PaneID != "pane-checkout-ui" {
		t.Errorf("the row should land on the dependent agent, got %+v", got[0])
	}
	if got[0].Detail != "api landed · still needed by web" {
		t.Errorf("detail = %q", got[0].Detail)
	}
	if len(got[0].Dependents) != 1 || got[0].Dependents[0] != "web/checkout-ui" {
		t.Errorf("dependents = %v", got[0].Dependents)
	}
}

// Everything that depends on one repo is one row, and the agents it names do not
// take rows of their own below it.
func TestLandedGroupsEveryAgentDependingOnOneRepo(t *testing.T) {
	got := Rank(Input{
		Now: now,
		Repos: []model.Repo{
			repo("api"),
			repo("web", dependent("checkout-ui", "api", model.StatusIdle, 40*time.Minute, 10*time.Minute)),
			repo("mobile", dependent("checkout", "api", model.StatusDone, 30*time.Minute, 10*time.Minute)),
		},
		Orch: restingSince(2 * time.Minute),
	})
	if len(got) != 1 {
		t.Fatalf("want one row for both, got %+v", got)
	}
	if got[0].Agent != "checkout-ui" {
		t.Errorf("the agent waiting longest should lead, got %q", got[0].Agent)
	}
	if got[0].Detail != "api landed · still needed by web, mobile" {
		t.Errorf("detail = %q", got[0].Detail)
	}
}

func TestLandedSilentBeforeLanding(t *testing.T) {
	nothingLanded(t, Input{
		Now: now,
		Repos: []model.Repo{
			repo("api"),
			repo("web", dependent("checkout-ui", "api", model.StatusIdle, 40*time.Minute, 0)),
		},
		Orch: restingSince(2 * time.Minute),
	}, "a landed row fired before anything landed")
}

// A dependent agent that changed state after the landing has been moved on,
// whether it is still working or already back at its prompt.
func TestLandedSilentOnceTheDependentAgentMoved(t *testing.T) {
	for _, st := range []model.Status{model.StatusWorking, model.StatusIdle} {
		nothingLanded(t, Input{
			Now: now,
			Repos: []model.Repo{
				repo("api"),
				repo("web", dependent("checkout-ui", "api", st, 5*time.Minute, 10*time.Minute)),
			},
			Orch: restingSince(2 * time.Minute),
		}, fmt.Sprintf("the dependent agent moved and is %s", st))
	}
}

// The orchestrator gets its turn first. A working one may be moving the dependent
// work right now, and one resting since before the landing has not been woken
// since, so it cannot have acted on it.
func TestLandedSilentUntilTheOrchestratorEndsATurn(t *testing.T) {
	web := dependent("checkout-ui", "api", model.StatusIdle, 40*time.Minute, 10*time.Minute)
	for _, o := range []model.Orchestrator{
		{Found: true, Status: model.StatusWorking, StatusSince: ago(2 * time.Minute)},
		restingSince(20 * time.Minute),
		{Found: false},
	} {
		nothingLanded(t, Input{Now: now, Repos: []model.Repo{repo("api"), repo("web", web)}, Orch: o},
			fmt.Sprintf("orchestrator %+v", o))
	}
}

// Parking is normal. An agent whose code is written and cannot land yet waits
// by design, so it never reads as stalled, and when it finishes a turn its row
// says it is ready rather than finished.
func TestDependentAgentReadsAsReadyNotStalled(t *testing.T) {
	idle := dependent("checkout-ui", "api", model.StatusIdle, 40*time.Minute, 0)
	if got := Rank(Input{
		Now: now, Repos: []model.Repo{repo("api"), repo("web", idle)},
		EverWorked: map[string]bool{idle.PaneID: true},
	}); len(got) != 0 {
		t.Errorf("a dependent agent was flagged: %+v", got)
	}

	done := dependent("checkout-ui", "api", model.StatusDone, 5*time.Minute, 0)
	got := Rank(Input{Now: now, Repos: []model.Repo{repo("api"), repo("web", done)}})
	if len(got) != 1 || got[0].Detail != "ready, depends on api" {
		t.Errorf("want one ready row, got %+v", got)
	}
}

// An age the daemon never watched accumulate is a lower bound, so it must not
// outrank an agent whose wait is actually known.
func TestBlockedWithUnknownAgeSortsLast(t *testing.T) {
	known := agent("known", model.StatusBlocked, 5*time.Minute)
	unknown := agent("unknown", model.StatusBlocked, 90*time.Minute)
	unknown.AgeKnown = false

	got := Rank(Input{Now: now, Repos: []model.Repo{repo("api", unknown, known)}})
	if len(got) != 2 {
		t.Fatalf("want 2 rows, got %d", len(got))
	}
	if got[0].Agent != "known" {
		t.Errorf("known age should lead despite a larger unknown age, got %q", got[0].Agent)
	}
	if got[0].AgeKnown != true || got[1].AgeKnown != false {
		t.Error("AgeKnown should carry through to the ribbon row")
	}
}

func TestDoneWithUnknownAgeSortsLast(t *testing.T) {
	known := agent("known", model.StatusDone, 30*time.Minute)
	unknown := agent("unknown", model.StatusDone, time.Second)
	unknown.AgeKnown = false

	got := Rank(Input{Now: now, Repos: []model.Repo{repo("api", unknown, known)}})
	if got[0].Agent != "known" {
		t.Errorf("an unknown age must not win newest-first, got %q", got[0].Agent)
	}
}

// A lower bound is still enough to prove a threshold was crossed, so rank 5
// must keep firing for agents whose exact age is unknown.
func TestIdleNeverDoneFiresOnALowerBound(t *testing.T) {
	a := agent("quiet", model.StatusIdle, 30*time.Minute)
	a.AgeKnown = false
	got := Rank(Input{Now: now, Repos: []model.Repo{repo("api", a)},
		EverWorked: map[string]bool{a.PaneID: true}})
	if len(got) != 1 || got[0].Rank != 5 {
		t.Fatalf("threshold rules should still fire on a lower bound, got %+v", got)
	}
}

// The noisiest thing the ribbon did: an agent you opened and never used sat in
// it forever claiming to be stalled. It was idle, which is a different thing.
func TestIdleAgentThatNeverWorkedIsNotStalled(t *testing.T) {
	never := agent("untouched", model.StatusIdle, 3*time.Hour)
	worked := agent("stalled", model.StatusIdle, 30*time.Minute)

	got := Rank(Input{
		Now:        now,
		Repos:      []model.Repo{repo("api", never, worked)},
		EverWorked: map[string]bool{worked.PaneID: true},
	})
	if len(got) != 1 {
		t.Fatalf("want only the agent that actually worked, got %+v", got)
	}
	if got[0].Agent != "stalled" {
		t.Errorf("flagged %q, want stalled", got[0].Agent)
	}
}

// A blocked orchestrator produced an empty ribbon while herdr's own sidebar
// showed it as blocked. Its strip cannot convey a permission prompt with the
// urgency the ribbon can.
func TestBlockedOrchestratorReachesTheRibbon(t *testing.T) {
	o := agent("orchestrator", model.StatusBlocked, 2*time.Minute)
	o.IsOrchestrator = true
	o.Question = "Do you want to proceed?"

	got := Rank(Input{Now: now, Repos: []model.Repo{repo("work", o)}})
	if len(got) != 1 {
		t.Fatalf("a blocked orchestrator must reach the ribbon, got %+v", got)
	}
	if got[0].Rank != 1 || got[0].Detail != "Do you want to proceed?" {
		t.Errorf("unexpected row %+v", got[0])
	}
}

// It still stays out of the ranks its own strip already covers.
func TestOrchestratorStaysOutOfInformationalRanks(t *testing.T) {
	for _, st := range []model.Status{model.StatusDone, model.StatusIdle} {
		o := agent("orchestrator", st, time.Hour)
		o.IsOrchestrator = true
		got := Rank(Input{
			Now: now, Repos: []model.Repo{repo("work", o)},
			EverWorked: map[string]bool{o.PaneID: true},
		})
		if len(got) != 0 {
			t.Errorf("status %s: orchestrator should stay in its strip, got %+v", st, got)
		}
	}
}

// A detail cut at 80 bytes can land inside a multi-byte character, and the
// invalid tail comes out of json.Marshal as U+FFFD in the ribbon. Cutting
// characters is what keeps the sentence readable.
func TestDetailCutsCharactersNotBytes(t *testing.T) {
	long := strings.Repeat("é", 100)
	got := truncate(long, 80)
	if !utf8.ValidString(got) {
		t.Fatalf("invalid utf-8 out of truncate: %q", got)
	}
	if n := utf8.RuneCountInString(got); n != 80 {
		t.Errorf("want 80 characters, got %d", n)
	}
}

// herdr says done, not idle, for an agent that finished its turn while you were
// looking at another pane. That is the ordinary state of an orchestrator you
// have walked away from, which is exactly when a landing goes unnoticed, so
// testing for idle alone missed the case the rule exists for.
func TestLandedRowWhileOrchestratorIsDone(t *testing.T) {
	o := restingSince(2 * time.Minute)
	o.Status = model.StatusDone
	got := Rank(Input{
		Now: now,
		Repos: []model.Repo{
			repo("api"),
			repo("web", dependent("checkout-ui", "api", model.StatusIdle, 40*time.Minute, 10*time.Minute)),
		},
		Orch: o,
	})
	if len(got) == 0 || got[0].Reason != model.ReasonLanded {
		t.Fatalf("expected a landed row first, got %+v", got)
	}
}
