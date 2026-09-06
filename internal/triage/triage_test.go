package triage

import (
	"strings"
	"testing"
	"time"

	"github.com/ofelcan/muster/internal/chain"
	"github.com/ofelcan/muster/internal/model"
)

// linear is the shape that actually occurs: contracts, then api, then web and
// mobile in parallel, with infra outside the order entirely.
func linear() *chain.Chain {
	return &chain.Chain{
		Stages:      [][]string{{"contracts"}, {"api"}, {"web", "mobile"}},
		Independent: []string{"infra"},
	}
}

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
	}
	got := Rank(in)
	if len(got) != 1 {
		t.Fatalf("want only the stale agent, got %+v", got)
	}
	if got[0].Agent != "stale" || got[0].Rank != 5 {
		t.Errorf("unexpected row %+v", got[0])
	}
}

func TestRibbonIsCappedAtFour(t *testing.T) {
	var agents []model.Agent
	for i := 0; i < 10; i++ {
		agents = append(agents, agent(string(rune('a'+i)), model.StatusBlocked, time.Duration(i)*time.Minute))
	}
	got := Rank(Input{Now: now, Repos: []model.Repo{repo("api", agents...)}})
	if len(got) != RibbonMax {
		t.Fatalf("ribbon must cap at %d, got %d", RibbonMax, len(got))
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

func TestOrchestratorIsNotTriaged(t *testing.T) {
	o := agent("orchestrator", model.StatusBlocked, time.Hour)
	o.IsOrchestrator = true
	if got := Rank(Input{Now: now, Repos: []model.Repo{repo("api", o)}}); len(got) != 0 {
		t.Fatalf("orchestrator has its own strip, got %+v", got)
	}
}

// The gate rule is the propagation repair: an agent finished, the orchestrator
// has been idle since before it finished so cannot have reacted, and work
// elsewhere has been idle even longer.
func TestGateOpenNobodyTold(t *testing.T) {
	finished := agent("migrations", model.StatusDone, 6*time.Minute)
	waiting := agent("checkout-ui", model.StatusIdle, 18*time.Minute)

	in := Input{
		Now: now,
		Repos: []model.Repo{
			repo("api", finished),
			repo("web", waiting),
		},
		Orch: model.Orchestrator{
			Found:       true,
			Status:      model.StatusIdle,
			StatusSince: ago(20 * time.Minute), // idle since before api finished
		},
		Chain: linear(),
	}
	got := Rank(in)
	if len(got) == 0 {
		t.Fatal("expected a gate row")
	}
	if got[0].Rank != 2 || got[0].Reason != model.ReasonGateUntold {
		t.Fatalf("expected gate row first, got %+v", got[0])
	}
	if got[0].Agent != "migrations" {
		t.Errorf("gate row should name the finished agent, got %q", got[0].Agent)
	}
	if !strings.Contains(got[0].Detail, "orchestrator not told") {
		t.Errorf("detail should say the orchestrator was not told, got %q", got[0].Detail)
	}
	if len(got[0].Downstream) != 1 {
		t.Errorf("expected one downstream waiter, got %v", got[0].Downstream)
	}
}

func TestGateSilentWhenOrchestratorAlreadyReacted(t *testing.T) {
	in := Input{
		Now: now,
		Repos: []model.Repo{
			repo("api", agent("migrations", model.StatusDone, 6*time.Minute)),
			repo("web", agent("checkout-ui", model.StatusIdle, 18*time.Minute)),
		},
		Orch: model.Orchestrator{
			Found:       true,
			Status:      model.StatusIdle,
			StatusSince: ago(2 * time.Minute), // changed state after api finished
		},
		Chain: linear(),
	}
	for _, r := range Rank(in) {
		if r.Reason == model.ReasonGateUntold {
			t.Fatalf("gate must stay silent when the orchestrator has since acted: %+v", r)
		}
	}
}

func TestGateSilentWithoutOrchestrator(t *testing.T) {
	in := Input{
		Now: now,
		Repos: []model.Repo{
			repo("api", agent("migrations", model.StatusDone, 6*time.Minute)),
			repo("web", agent("checkout-ui", model.StatusIdle, 18*time.Minute)),
		},
		Orch:  model.Orchestrator{Found: false},
		Chain: linear(),
	}
	for _, r := range Rank(in) {
		if r.Reason == model.ReasonGateUntold {
			t.Fatal("gate rule must not guess without a marked orchestrator")
		}
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
	got := Rank(Input{Now: now, Repos: []model.Repo{repo("api", a)}})
	if len(got) != 1 || got[0].Rank != 5 {
		t.Fatalf("threshold rules should still fire on a lower bound, got %+v", got)
	}
}

// An independent repo being quiet is not a blocked downstream. This is the
// false positive the chain exists to remove.
func TestGateIgnoresIndependentRepos(t *testing.T) {
	in := Input{
		Now: now,
		Repos: []model.Repo{
			repo("api", agent("migrations", model.StatusDone, 6*time.Minute)),
			repo("infra", agent("tf", model.StatusIdle, 40*time.Minute)),
		},
		Orch: model.Orchestrator{
			Found: true, Status: model.StatusIdle, StatusSince: ago(time.Hour),
		},
		Chain: linear(),
	}
	for _, r := range Rank(in) {
		if r.Reason == model.ReasonGateUntold {
			t.Fatalf("infra is independent and must not count as downstream: %+v", r)
		}
	}
}

// Upstream work going quiet is not a gate either. Only later stages wait.
func TestGateIgnoresUpstreamRepos(t *testing.T) {
	in := Input{
		Now: now,
		Repos: []model.Repo{
			repo("api", agent("migrations", model.StatusDone, 6*time.Minute)),
			repo("contracts", agent("bump", model.StatusIdle, 40*time.Minute)),
		},
		Orch: model.Orchestrator{
			Found: true, Status: model.StatusIdle, StatusSince: ago(time.Hour),
		},
		Chain: linear(),
	}
	for _, r := range Rank(in) {
		if r.Reason == model.ReasonGateUntold {
			t.Fatalf("contracts is upstream of api and cannot be waiting on it: %+v", r)
		}
	}
}

// Repos in the same stage run in parallel, so neither waits on the other.
func TestGateIgnoresSameStageRepos(t *testing.T) {
	in := Input{
		Now: now,
		Repos: []model.Repo{
			repo("web", agent("ui", model.StatusDone, 6*time.Minute)),
			repo("mobile", agent("app", model.StatusIdle, 40*time.Minute)),
		},
		Orch: model.Orchestrator{
			Found: true, Status: model.StatusIdle, StatusSince: ago(time.Hour),
		},
		Chain: linear(),
	}
	for _, r := range Rank(in) {
		if r.Reason == model.ReasonGateUntold {
			t.Fatalf("web and mobile run in parallel: %+v", r)
		}
	}
}

func TestGateSilentWithoutAChain(t *testing.T) {
	in := Input{
		Now: now,
		Repos: []model.Repo{
			repo("api", agent("migrations", model.StatusDone, 6*time.Minute)),
			repo("web", agent("checkout-ui", model.StatusIdle, 18*time.Minute)),
		},
		Orch: model.Orchestrator{
			Found: true, Status: model.StatusIdle, StatusSince: ago(time.Hour),
		},
	}
	for _, r := range Rank(in) {
		if r.Reason == model.ReasonGateUntold {
			t.Fatal("no chain means no way to know what downstream is")
		}
	}
}
