package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ofelcan164/muster/internal/herdr"
	"github.com/ofelcan164/muster/internal/model"
)

// newTestDaemon builds a daemon with no client. Everything under test here is
// pure transformation of a session snapshot, so the socket is never needed.
func newTestDaemon(t *testing.T) *Daemon {
	t.Helper()
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	return New(nil, nil)
}

func pane(id, ws, cwd string) herdr.Pane {
	return herdr.Pane{PaneID: id, WorkspaceID: ws, TabID: ws + ":t1", Cwd: cwd}
}

func herdrPane(id, label string) herdr.Pane {
	return herdr.Pane{PaneID: id, Label: label}
}

func agentPane(id, ws, cwd, status string) herdr.Agent {
	return herdr.Agent{
		PaneID: id, WorkspaceID: ws, TabID: ws + ":t1",
		Cwd: cwd, Agent: "claude", AgentStatus: status,
	}
}

// gitRepo creates a real repository on disk, because discovery reads .git
// rather than asking git, and a fake directory would not exercise it.
func gitRepo(t *testing.T, dir, origin, branch string) string {
	t.Helper()
	git := filepath.Join(dir, ".git")
	if err := os.MkdirAll(git, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(git, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("HEAD", "ref: refs/heads/"+branch+"\n")
	cfg := "[core]\n\trepositoryformatversion = 0\n"
	if origin != "" {
		cfg += "[remote \"origin\"]\n\turl = " + origin + "\n"
	}
	write("config", cfg)
	return dir
}

func TestDiscoversReposFromWorkspaceCwds(t *testing.T) {
	d := newTestDaemon(t)
	root := t.TempDir()
	api := gitRepo(t, filepath.Join(root, "api"), "git@github.com:acme/api.git", "feat/billing")
	web := gitRepo(t, filepath.Join(root, "web"), "https://github.com/acme/web.git", "main")

	snap := &herdr.Snapshot{
		Workspaces: []herdr.Workspace{
			{WorkspaceID: "w1", Number: 1},
			{WorkspaceID: "w2", Number: 2},
		},
		Panes: []herdr.Pane{pane("w1:p1", "w1", api), pane("w2:p1", "w2", web)},
		Agents: []herdr.Agent{
			agentPane("w1:p1", "w1", api, "working"),
			agentPane("w2:p1", "w2", web, "idle"),
		},
	}

	agents := d.buildAgents(snap, time.Now())
	repos := d.buildRepos(snap, agents, model.Orchestrator{})

	if len(repos) != 2 {
		t.Fatalf("want 2 repos, got %d", len(repos))
	}
	byKey := map[string]model.Repo{}
	for _, r := range repos {
		byKey[r.Key] = r
	}
	if r, ok := byKey["acme/api"]; !ok {
		t.Errorf("acme/api not discovered, got keys %v", keysOf(byKey))
	} else {
		if r.Branch != "feat/billing" {
			t.Errorf("branch = %q, want feat/billing", r.Branch)
		}
		if !r.IsGit {
			t.Error("expected IsGit")
		}
		if len(r.Agents) != 1 {
			t.Errorf("want 1 agent, got %d", len(r.Agents))
		}
	}
	if _, ok := byKey["acme/web"]; !ok {
		t.Errorf("acme/web not discovered, got keys %v", keysOf(byKey))
	}
}

// Two workspaces open on one repository are one card, not two.
func TestTwoWorkspacesOneRepo(t *testing.T) {
	d := newTestDaemon(t)
	api := gitRepo(t, filepath.Join(t.TempDir(), "api"), "git@github.com:acme/api.git", "main")

	snap := &herdr.Snapshot{
		Workspaces: []herdr.Workspace{{WorkspaceID: "w1", Number: 1}, {WorkspaceID: "w2", Number: 2}},
		Panes:      []herdr.Pane{pane("w1:p1", "w1", api), pane("w2:p1", "w2", api)},
		Agents:     []herdr.Agent{agentPane("w1:p1", "w1", api, "idle"), agentPane("w2:p1", "w2", api, "idle")},
	}
	repos := d.buildRepos(snap, d.buildAgents(snap, time.Now()), model.Orchestrator{})
	if len(repos) != 1 {
		t.Fatalf("want 1 repo, got %d", len(repos))
	}
	if len(repos[0].Agents) != 2 {
		t.Errorf("want both agents on the one card, got %d", len(repos[0].Agents))
	}
	if len(repos[0].WorkspaceIDs) != 2 {
		t.Errorf("want 2 workspace ids, got %v", repos[0].WorkspaceIDs)
	}
}

// Same basename, different org. These must not merge.
func TestSameBasenameDifferentOrg(t *testing.T) {
	d := newTestDaemon(t)
	root := t.TempDir()
	a := gitRepo(t, filepath.Join(root, "web-a"), "git@github.com:acme/web.git", "main")
	b := gitRepo(t, filepath.Join(root, "web-b"), "git@github.com:other/web.git", "main")

	snap := &herdr.Snapshot{
		Workspaces: []herdr.Workspace{{WorkspaceID: "w1", Number: 1}, {WorkspaceID: "w2", Number: 2}},
		Panes:      []herdr.Pane{pane("w1:p1", "w1", a), pane("w2:p1", "w2", b)},
		Agents:     []herdr.Agent{agentPane("w1:p1", "w1", a, "idle"), agentPane("w2:p1", "w2", b, "idle")},
	}
	repos := d.buildRepos(snap, d.buildAgents(snap, time.Now()), model.Orchestrator{})
	if len(repos) != 2 {
		t.Fatalf("want 2 distinct repos, got %d", len(repos))
	}
	// Colliding short names must both widen to the full owner/name.
	for _, r := range repos {
		if r.Display != r.Name {
			t.Errorf("colliding repo %q should display its owner, got %q", r.Name, r.Display)
		}
	}
	if repos[0].Sigil == repos[1].Sigil {
		t.Error("distinct repos must get distinct sigils")
	}
}

func TestDisplayNameDropsOwnerWhenUnique(t *testing.T) {
	d := newTestDaemon(t)
	api := gitRepo(t, filepath.Join(t.TempDir(), "api"), "git@github.com:acme/api.git", "main")
	snap := &herdr.Snapshot{
		Workspaces: []herdr.Workspace{{WorkspaceID: "w1", Number: 1}},
		Panes:      []herdr.Pane{pane("w1:p1", "w1", api)},
		Agents:     []herdr.Agent{agentPane("w1:p1", "w1", api, "idle")},
	}
	repos := d.buildRepos(snap, d.buildAgents(snap, time.Now()), model.Orchestrator{})
	if repos[0].Display != "api" {
		t.Errorf("Display = %q, want api", repos[0].Display)
	}
	if repos[0].Key != "acme/api" {
		t.Errorf("Key must stay the full owner/name, got %q", repos[0].Key)
	}
}

// A repo with no origin falls back to its directory name.
func TestRepoWithoutOrigin(t *testing.T) {
	d := newTestDaemon(t)
	infra := gitRepo(t, filepath.Join(t.TempDir(), "infra"), "", "main")
	snap := &herdr.Snapshot{
		Workspaces: []herdr.Workspace{{WorkspaceID: "w1", Number: 1}},
		Panes:      []herdr.Pane{pane("w1:p1", "w1", infra)},
		Agents:     []herdr.Agent{agentPane("w1:p1", "w1", infra, "idle")},
	}
	repos := d.buildRepos(snap, d.buildAgents(snap, time.Now()), model.Orchestrator{})
	if len(repos) != 1 || repos[0].Name != "infra" {
		t.Fatalf("want a repo named infra, got %+v", repos)
	}
}

// Grid slots are the whole reason the grid is worth having. They must not move.
func TestGridSlotsAreStableAndDeterministic(t *testing.T) {
	root := t.TempDir()
	a := gitRepo(t, filepath.Join(root, "a"), "git@github.com:acme/a.git", "main")
	b := gitRepo(t, filepath.Join(root, "b"), "git@github.com:acme/b.git", "main")
	c := gitRepo(t, filepath.Join(root, "c"), "git@github.com:acme/c.git", "main")

	build := func(d *Daemon) map[string]int {
		snap := &herdr.Snapshot{
			Workspaces: []herdr.Workspace{
				{WorkspaceID: "w3", Number: 3},
				{WorkspaceID: "w1", Number: 1},
				{WorkspaceID: "w2", Number: 2},
			},
			Panes: []herdr.Pane{pane("w1:p1", "w1", a), pane("w2:p1", "w2", b), pane("w3:p1", "w3", c)},
			Agents: []herdr.Agent{
				agentPane("w1:p1", "w1", a, "idle"),
				agentPane("w2:p1", "w2", b, "idle"),
				agentPane("w3:p1", "w3", c, "idle"),
			},
		}
		got := map[string]int{}
		for _, r := range d.buildRepos(snap, d.buildAgents(snap, time.Now()), model.Orchestrator{}) {
			got[r.Key] = r.GridSlot
		}
		return got
	}

	d := newTestDaemon(t)
	first := build(d)
	// Slots are allocated in workspace-number order, not map order, so the same
	// session always lays out the same way.
	if first["acme/a"] != 0 || first["acme/b"] != 1 || first["acme/c"] != 2 {
		t.Fatalf("slots not in workspace order: %v", first)
	}
	for i := 0; i < 20; i++ {
		if got := build(d); got["acme/a"] != first["acme/a"] ||
			got["acme/b"] != first["acme/b"] || got["acme/c"] != first["acme/c"] {
			t.Fatalf("slots moved between reconciles: %v then %v", first, got)
		}
	}
}

// The bug this whole step fixes: a workspace with agents in three repos used
// to be filed under one directory shared by all its panes. Each pane now
// resolves against its own cwd, so three repos come out, each with its own
// agent.
func TestOneWorkspaceThreeReposEachOwnAgent(t *testing.T) {
	d := newTestDaemon(t)
	root := t.TempDir()
	contracts := gitRepo(t, filepath.Join(root, "contracts"), "git@github.com:acme/contracts.git", "main")
	api := gitRepo(t, filepath.Join(root, "api"), "git@github.com:acme/api.git", "main")
	web := gitRepo(t, filepath.Join(root, "web"), "git@github.com:acme/web.git", "main")

	snap := &herdr.Snapshot{
		Workspaces: []herdr.Workspace{{WorkspaceID: "w1", Number: 1}},
		Panes: []herdr.Pane{
			pane("w1:p1", "w1", contracts),
			pane("w1:p2", "w1", api),
			pane("w1:p3", "w1", web),
		},
		Agents: []herdr.Agent{
			agentPane("w1:p1", "w1", contracts, "working"),
			agentPane("w1:p2", "w1", api, "blocked"),
			agentPane("w1:p3", "w1", web, "idle"),
		},
	}
	agents := d.buildAgents(snap, time.Now())
	repos := d.buildRepos(snap, agents, model.Orchestrator{})
	if len(repos) != 3 {
		t.Fatalf("want 3 repos, got %d: %+v", len(repos), repos)
	}
	for _, r := range repos {
		if len(r.Agents) != 1 {
			t.Errorf("repo %s: want 1 agent of its own, got %d", r.Key, len(r.Agents))
		}
		if len(r.WorkspaceIDs) != 1 || r.WorkspaceIDs[0] != "w1" {
			t.Errorf("repo %s: want workspace w1 recorded once, got %v", r.Key, r.WorkspaceIDs)
		}
	}
}

// The worktree hint describes the workspace's own checkout. A pane cd'd into a
// different repo must read its own branch off disk rather than inherit the
// hint's.
func TestWorktreeHintOnlyAppliesToPanesInsideIt(t *testing.T) {
	d := newTestDaemon(t)
	root := t.TempDir()
	hinted := gitRepo(t, filepath.Join(root, "hinted"), "git@github.com:acme/hinted.git", "on-disk-branch")
	other := gitRepo(t, filepath.Join(root, "other"), "git@github.com:acme/other.git", "other-branch")

	snap := &herdr.Snapshot{
		Workspaces: []herdr.Workspace{{
			WorkspaceID: "w1", Number: 1,
			Worktree: &herdr.WorkspaceWorktree{Path: hinted, Branch: "hint-branch"},
		}},
		Panes: []herdr.Pane{pane("w1:p1", "w1", hinted), pane("w1:p2", "w1", other)},
	}
	repos := d.buildRepos(snap, map[string]model.Agent{}, model.Orchestrator{})

	byKey := map[string]model.Repo{}
	for _, r := range repos {
		byKey[r.Key] = r
	}
	if got := byKey["acme/hinted"].Branch; got != "hint-branch" {
		t.Errorf("pane inside the worktree hint: branch = %q, want hint-branch", got)
	}
	if got := byKey["acme/other"].Branch; got != "other-branch" {
		t.Errorf("pane outside the worktree hint: branch = %q, want its own on-disk branch", got)
	}
}

// A pane with no cwd used to be skipped entirely. Resolve("") already answers
// with the neutral "unknown" identity, so there is nothing left to guard
// against: the pane belongs in the snapshot like any other.
func TestPaneWithNoCwdStillAppearsInSnapshot(t *testing.T) {
	d := newTestDaemon(t)
	snap := &herdr.Snapshot{
		Workspaces: []herdr.Workspace{{WorkspaceID: "w1", Number: 1}},
		Panes:      []herdr.Pane{pane("w1:p1", "w1", "")},
	}
	repos := d.buildRepos(snap, map[string]model.Agent{}, model.Orchestrator{})
	if len(repos) != 1 {
		t.Fatalf("want a card for the cwd-less pane, got %+v", repos)
	}
	if repos[0].IsGit {
		t.Error("a pane with no cwd cannot be a git repo")
	}
	if len(repos[0].OtherPanes) != 1 {
		t.Errorf("want the pane recorded, got %+v", repos[0])
	}
}

func TestAgentNamesAreUnambiguous(t *testing.T) {
	// Two agents in different workspaces are both "p1". The fallback must not
	// collapse them into the same label.
	a := agentName(herdr.Agent{PaneID: "w5:p1"})
	b := agentName(herdr.Agent{PaneID: "w6:p1"})
	if a == b {
		t.Fatalf("distinct panes produced the same name %q", a)
	}
	named := agentName(herdr.Agent{PaneID: "w5:p1", Name: "migrations"})
	if named != "migrations" {
		t.Errorf("an explicit name must win, got %q", named)
	}
}

func TestOrchestratorTokenBeatsName(t *testing.T) {
	d := newTestDaemon(t)
	snap := &herdr.Snapshot{
		Agents: []herdr.Agent{
			{PaneID: "w1:p1", Name: "orchestrator", AgentStatus: "idle"},
			{PaneID: "w2:p1", Name: "something-else", AgentStatus: "idle",
				Tokens: map[string]string{"role": "orchestrator"}},
		},
	}
	orch := d.findOrchestrator(snap, d.buildAgents(snap, time.Now()), time.Now())
	if !orch.Found {
		t.Fatal("expected an orchestrator")
	}
	if orch.PaneID != "w2:p1" {
		t.Errorf("token should win over name, got %s via %s", orch.PaneID, orch.DetectedBy)
	}
	if orch.DetectedBy != "token" {
		t.Errorf("DetectedBy = %q, want token", orch.DetectedBy)
	}
}

func TestOrchestratorFallsBackToName(t *testing.T) {
	d := newTestDaemon(t)
	snap := &herdr.Snapshot{
		Agents: []herdr.Agent{{PaneID: "w1:p1", Name: "orchestrator", AgentStatus: "idle"}},
	}
	orch := d.findOrchestrator(snap, d.buildAgents(snap, time.Now()), time.Now())
	if !orch.Found || orch.DetectedBy != "name" {
		t.Fatalf("want name detection, got %+v", orch)
	}
}

// A pane named orchestrator, any case, marks the agent running in it, so an
// orchestrator set up in herdr needs no o in Muster.
func TestOrchestratorFromThePaneName(t *testing.T) {
	d := newTestDaemon(t)
	snap := &herdr.Snapshot{
		Panes: []herdr.Pane{
			{PaneID: "w1:p1", Label: "api"},
			{PaneID: "w2:p1", Label: " Orchestrator "},
		},
		Agents: []herdr.Agent{
			{PaneID: "w1:p1", Name: "migrations", AgentStatus: "working"},
			{PaneID: "w2:p1", Name: "claude", AgentStatus: "idle"},
		},
	}
	orch := d.findOrchestrator(snap, d.buildAgents(snap, time.Now()), time.Now())
	if !orch.Found || orch.PaneID != "w2:p1" || orch.DetectedBy != "pane" {
		t.Fatalf("want w2:p1 via pane, got %+v", orch)
	}
}

// The pane's name is the weakest of the three: marking an agent with o, or an
// agent named orchestrator, beats a pane that is only called that.
func TestOrchestratorPaneNameLosesToTokenAndName(t *testing.T) {
	d := newTestDaemon(t)
	panes := []herdr.Pane{{PaneID: "w1:p1", Label: "orchestrator"}}
	for _, c := range []struct {
		other herdr.Agent
		how   string
	}{
		{herdr.Agent{PaneID: "w2:p1", Tokens: map[string]string{"role": "orchestrator"}}, "token"},
		{herdr.Agent{PaneID: "w2:p1", Name: "Orchestrator"}, "name"},
	} {
		snap := &herdr.Snapshot{Panes: panes, Agents: []herdr.Agent{
			{PaneID: "w1:p1", Name: "claude", AgentStatus: "idle"}, c.other,
		}}
		orch := d.findOrchestrator(snap, d.buildAgents(snap, time.Now()), time.Now())
		if orch.PaneID != "w2:p1" || orch.DetectedBy != c.how {
			t.Errorf("%s should beat the pane's name, got %s via %s", c.how, orch.PaneID, orch.DetectedBy)
		}
	}
}

// A pane named orchestrator with no agent running in it is only a shell.
func TestOrchestratorPaneNameNeedsAnAgent(t *testing.T) {
	d := newTestDaemon(t)
	snap := &herdr.Snapshot{
		Panes:  []herdr.Pane{{PaneID: "w1:p1", Label: "orchestrator"}, {PaneID: "w2:p1", Label: "api"}},
		Agents: []herdr.Agent{{PaneID: "w2:p1", Name: "migrations", AgentStatus: "idle"}},
	}
	if orch := d.findOrchestrator(snap, d.buildAgents(snap, time.Now()), time.Now()); orch.Found {
		t.Fatalf("a pane with no agent must not be the orchestrator, got %+v", orch)
	}
}

func TestNoOrchestratorIsNotGuessed(t *testing.T) {
	d := newTestDaemon(t)
	snap := &herdr.Snapshot{
		Agents: []herdr.Agent{
			{PaneID: "w1:p1", Name: "chatty", AgentStatus: "idle"},
			{PaneID: "w2:p1", Name: "also-chatty", AgentStatus: "working"},
		},
	}
	if orch := d.findOrchestrator(snap, d.buildAgents(snap, time.Now()), time.Now()); orch.Found {
		t.Fatalf("orchestrator must not be guessed, got %+v", orch)
	}
}

func keysOf(m map[string]model.Repo) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// New must return a daemon whose maps and channels are usable. A nil map here
// panicked a background goroutine and took the whole daemon down silently.
func TestNewIsFullyInitialised(t *testing.T) {
	d := newTestDaemon(t)
	if d.questions == nil {
		t.Error("questions map is nil")
	}
	if d.rescan == nil {
		t.Error("rescan channel is nil")
	}
	if d.persist == nil || d.persist.GridSlots == nil || d.persist.StatusSince == nil ||
		d.persist.TaskSeenAt == nil || d.persist.LastDoneSeq == nil ||
		d.persist.LastProcess == nil || d.persist.Stopped == nil {
		t.Error("persisted state has a nil map")
	}
	// Exercise the paths that write to them.
	d.mu.Lock()
	d.questions["w1:p1"] = "Do you want to proceed?"
	d.mu.Unlock()
	if got := d.question("w1:p1"); got != "Do you want to proceed?" {
		t.Errorf("question round trip failed: %q", got)
	}
}

// Every discovered repo gets a card. The overlay handles keeping the quiet ones
// out of the way by ordering, not by hiding them.
func TestQuietReposStillGetACard(t *testing.T) {
	d := newTestDaemon(t)
	root := t.TempDir()
	api := gitRepo(t, filepath.Join(root, "api"), "git@github.com:acme/api.git", "main")

	base := func(agents []herdr.Agent) *herdr.Snapshot {
		return &herdr.Snapshot{
			Workspaces: []herdr.Workspace{{WorkspaceID: "w1", Number: 1}},
			Panes:      []herdr.Pane{pane("w1:p1", "w1", api)},
			Agents:     agents,
		}
	}

	// No agent yet: still a card.
	empty := base(nil)
	if got := d.buildRepos(empty, map[string]model.Agent{}, model.Orchestrator{}); len(got) != 1 {
		t.Fatalf("a discovered repo should get a card, got %+v", got)
	}

	// An agent appears.
	busy := base([]herdr.Agent{agentPane("w1:p1", "w1", api, "working")})
	got := d.buildRepos(busy, d.buildAgents(busy, time.Now()), model.Orchestrator{})
	if len(got) != 1 {
		t.Fatalf("want a card once an agent runs, got %d", len(got))
	}
	slot := got[0].GridSlot

	// The agent goes away: the card stays put.
	got = d.buildRepos(empty, map[string]model.Agent{}, model.Orchestrator{})
	if len(got) != 1 {
		t.Fatalf("the card should survive the agent closing, got %d", len(got))
	}
	if got[0].GridSlot != slot {
		t.Errorf("grid slot moved from %d to %d", slot, got[0].GridSlot)
	}
}

// A scratch workspace with no agents still gets a card, so its shells are
// visible. Ordering keeps it below anything you are actually working in.
func TestScratchWorkspaceGetsACard(t *testing.T) {
	d := newTestDaemon(t)
	scratch := t.TempDir()
	snap := &herdr.Snapshot{
		Workspaces: []herdr.Workspace{{WorkspaceID: "w1", Number: 1}},
		Panes:      []herdr.Pane{pane("w1:p1", "w1", scratch)},
	}
	got := d.buildRepos(snap, map[string]model.Agent{}, model.Orchestrator{})
	if len(got) != 1 {
		t.Fatalf("want a card, got %+v", got)
	}
	if got[0].IsGit {
		t.Error("a scratch workspace is not a git repo")
	}
}

// The bug: a stop recorded in a pane stayed there once an agent started in it.
// Only non-agent panes are read for a foreground process, so nothing could
// clear the entry, and at rank 3 it outranked the agent's own rows for the full
// fifteen-minute TTL, hiding what the agent was actually doing.
func TestStopClearsWhenAnAgentTakesThePaneOver(t *testing.T) {
	d := newTestDaemon(t)
	api := gitRepo(t, filepath.Join(t.TempDir(), "api"), "git@github.com:acme/api.git", "main")

	// A dev server in a plain pane, then back at a prompt: a stop.
	tracked := map[string]bool{"w1:p1": true}
	d.detectStoppedProcesses(map[string]string{"w1:p1": "vite"}, tracked, time.Now())
	d.detectStoppedProcesses(map[string]string{"w1:p1": "bash"}, tracked, time.Now())
	if len(d.persist.Stopped) != 1 {
		t.Fatalf("expected a recorded stop, got %v", d.persist.Stopped)
	}

	// The same pane now runs an agent.
	snap := &herdr.Snapshot{
		Workspaces: []herdr.Workspace{{WorkspaceID: "w1", Number: 1}},
		Panes:      []herdr.Pane{pane("w1:p1", "w1", api)},
		Agents:     []herdr.Agent{agentPane("w1:p1", "w1", api, "working")},
	}
	d.buildRepos(snap, d.buildAgents(snap, time.Now()), model.Orchestrator{})

	if len(d.persist.Stopped) != 0 {
		t.Errorf("an agent running in the pane should clear the stop, got %v", d.persist.Stopped)
	}
	if len(d.persist.LastProcess) != 0 {
		// Otherwise the agent exiting back to a shell reports the long-dead
		// process as having only just stopped.
		t.Errorf("stale process state left behind: %v", d.persist.LastProcess)
	}
}

// The overlay is a pane like any other and its cwd is the plugin checkout, so a
// workspace hosting it has two cwds to choose from. Counting the overlay let it
// rename the card it was drawn over: a one-pane workspace ties 1-1, the tie
// breaks on the path, and the repo changed name, colour and slot for as long as
// you were looking at it.
func TestTheOverlayDoesNotReidentifyTheWorkspaceItOpensOver(t *testing.T) {
	d := newTestDaemon(t)
	dir := gitRepo(t, filepath.Join(t.TempDir(), "api"), "git@github.com:acme/api.git", "main")
	overlay := pane("w1:p2", "w1", "/home/dev/.local/share/herdr/plugins/muster")
	overlay.Label = overlayTitle
	snap := &herdr.Snapshot{
		Workspaces: []herdr.Workspace{{WorkspaceID: "w1", Number: 1}},
		Panes:      []herdr.Pane{pane("w1:p1", "w1", dir), overlay},
	}
	repos := d.buildRepos(snap, map[string]model.Agent{}, model.Orchestrator{})
	if len(repos) != 1 || repos[0].Key != "acme/api" {
		t.Fatalf("want one card for acme/api, got %+v", repos)
	}
}

// Workspaces come out in number order, whatever order herdr lists them in.
func TestBuildWorkspacesSortsByNumber(t *testing.T) {
	snap := &herdr.Snapshot{
		Workspaces: []herdr.Workspace{
			{WorkspaceID: "w3", Number: 3, Label: "spike"},
			{WorkspaceID: "w1", Number: 1, Label: "rollout"},
			{WorkspaceID: "w2", Number: 2, Label: "mobile"},
		},
	}
	got := buildWorkspaces(snap)
	if len(got) != 3 {
		t.Fatalf("want 3 workspaces, got %d", len(got))
	}
	want := []model.Workspace{
		{ID: "w1", Number: 1, Label: "rollout"},
		{ID: "w2", Number: 2, Label: "mobile"},
		{ID: "w3", Number: 3, Label: "spike"},
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("position %d: got %+v, want %+v", i, got[i], w)
		}
	}
}

// A non-agent pane's workspace id is what lets an empty workspace tile show
// which repos its panes sit in.
func TestOtherPanesCarryTheirWorkspaceID(t *testing.T) {
	d := newTestDaemon(t)
	api := gitRepo(t, filepath.Join(t.TempDir(), "api"), "git@github.com:acme/api.git", "main")
	snap := &herdr.Snapshot{
		Workspaces: []herdr.Workspace{{WorkspaceID: "w1", Number: 1}},
		Panes:      []herdr.Pane{pane("w1:p1", "w1", api)},
	}
	repos := d.buildRepos(snap, map[string]model.Agent{}, model.Orchestrator{})
	if len(repos) != 1 || len(repos[0].OtherPanes) != 1 {
		t.Fatalf("want one repo with one other pane, got %+v", repos)
	}
	if got := repos[0].OtherPanes[0].WorkspaceID; got != "w1" {
		t.Errorf("OtherPanes[0].WorkspaceID = %q, want w1", got)
	}
}
